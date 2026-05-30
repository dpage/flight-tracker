// Command server is the Aerly HTTP server.
//
// It serves the React SPA, exposes the JSON API, handles GitHub OAuth, runs
// the flight-tracking poller (OpenSky / stub + dead-reckoning), and
// broadcasts flight updates over Server-Sent Events.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/config"
	"github.com/dpage/aerly/internal/db"
	"github.com/dpage/aerly/internal/emailingest"
	"github.com/dpage/aerly/internal/flightops"
	"github.com/dpage/aerly/internal/handlers"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/poller"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/migrations"
	"github.com/dpage/aerly/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if err := run(); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// Load .env from the current working directory if present, so we don't
	// have to depend on the shell parsing values that contain quotes, $, etc.
	// godotenv's parser handles single-quoted values literally.
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn(".env present but failed to parse", "err", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(rootCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := db.Migrate(rootCtx, pool, migrations.FS); err != nil {
		return err
	}

	s := store.New(pool)
	authH := auth.NewHandler(cfg.SessionKey, cfg.PublicURL, s)
	authH.MailFromAddress = cfg.MailFromAddress
	authH.SendmailPath = cfg.SendmailPath
	if cfg.GitHubID != "" {
		authH.AddProvider(auth.NewGitHubProvider(cfg.GitHubID, cfg.GitHubSecret))
		slog.Info("auth provider: github")
	}
	if cfg.GoogleID != "" {
		authH.AddProvider(auth.NewGoogleProvider(cfg.GoogleID, cfg.GoogleSecret))
		slog.Info("auth provider: google")
	}
	hub := sse.NewHub()

	// Two resolver handles share one upstream AeroDataBox client. The
	// cached wrapper sits in front of the handler-driven paths (Add Flight
	// dialog, email ingest, flightops) where the 24h TTL hides repeated
	// lookups for the same ident/date. The poller bypasses the cache and
	// uses the raw resolver instead, because (a) it needs fresh airframe
	// data on the day of departure to catch swaps, and (b) it has its own
	// per-flight throttle via last_resolved_at.
	var resolver, rawResolver providers.Resolver
	if cfg.AeroDataBoxKey != "" {
		rawResolver = providers.NewAeroDataBox(cfg.AeroDataBoxKey)
		resolver = providers.NewCachedResolver(rawResolver, 24*time.Hour)
		slog.Info("resolver: aerodatabox (cached, ttl=24h; poller uses uncached)")
	}
	api := handlers.New(s, authH, hub, cfg, resolver)

	// Pick the upstream tracker. OpenSky if credentials are configured (or
	// anonymous OpenSky if requested), otherwise the in-memory stub. The
	// OpenSky path is gated through a SpeedGate first — OpenSky's
	// /states/all?icao24=… happily returns the wrong aircraft when an
	// airframe is reused for a different sector, and the resulting
	// teleport would otherwise pollute the stored track. Either tracker is
	// then wrapped with DeadReckoner so coverage gaps (and gate rejections)
	// fall back to an extrapolation.
	var inner providers.Tracker
	switch {
	case cfg.UseOpenSky():
		inner = providers.NewSpeedGate(
			providers.NewOpenSky(cfg.OpenSkyUsername, cfg.OpenSkyPassword), s)
		slog.Info("tracker: opensky",
			"authed", cfg.OpenSkyUsername != "")
	default:
		inner = providers.NewStub()
		slog.Info("tracker: stub")
	}
	tracker := providers.NewDeadReckoner(inner, s)
	p := poller.New(s, tracker, hub, cfg.PollInterval)
	// Give the poller the *uncached* resolver so its day-of refresh sees
	// fresh AeroDataBox state (last_resolved_at handles throttling). Falls
	// back to the cached one when no upstream is configured (i.e. nil).
	p.Resolver = rawResolver
	go p.Run(rootCtx)

	if cfg.EmailIngestEnabled {
		if resolver == nil {
			return errors.New("EMAIL_INGEST_ENABLED=1 requires a configured resolver (set AERODATABOX_RAPIDAPI_KEY)")
		}
		llmClient, err := emailingest.NewRealLLM(cfg.LLMProvider, cfg.LLMModel, cfg.LLMAPIKey)
		if err != nil {
			return err
		}
		extractor := emailingest.NewExtractor(llmClient, cfg.LLMModel)
		// Wire the same LLM-backed extractor into the HTTP ingest endpoints
		// (paste/upload → propose/confirm).
		api.Extractor = extractor
		svc := &emailingest.Service{
			Cfg: emailingest.Config{
				MaildirPath:   cfg.EmailIngestMaildir,
				PollInterval:  cfg.EmailIngestPollInterval,
				RequireDKIM:   cfg.EmailIngestRequireDKIM,
				MaxBodyBytes:  cfg.EmailIngestMaxBodyBytes,
				IngestAddress: cfg.EmailIngestAddress,
				SendmailPath:  cfg.EmailIngestSendmail,
				PublicURL:     cfg.PublicURL,
			},
			Store:      s,
			Extractor:  extractor,
			FlightDeps: flightops.Deps{Store: s, Resolver: resolver},
			PlanDeps:   planops.Deps{Store: s, Extractor: extractor, Resolver: resolver},
			Hub:        hub,
		}
		go func() {
			if err := svc.Run(rootCtx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("emailingest: stopped", "err", err)
			}
		}()
		slog.Info("emailingest: started",
			"maildir", cfg.EmailIngestMaildir,
			"address", cfg.EmailIngestAddress,
			"llm", cfg.LLMProvider+"/"+cfg.LLMModel)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	authH.Register(mux)
	if cfg.DevAuthBypass {
		authH.RegisterDevLogin(mux)
	}
	api.Register(mux)

	spa, err := web.FS()
	if err != nil {
		return err
	}
	mux.Handle("/", handlers.SPAHandler(spa))

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.ListenAddr, "public_url", cfg.PublicURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-rootCtx.Done():
		slog.Info("shutdown signal received")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
