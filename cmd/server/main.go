// Command server is the flight-tracker HTTP server.
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

	"github.com/dpage/flight-tracker/internal/auth"
	"github.com/dpage/flight-tracker/internal/config"
	"github.com/dpage/flight-tracker/internal/db"
	"github.com/dpage/flight-tracker/internal/emailingest"
	"github.com/dpage/flight-tracker/internal/flightops"
	"github.com/dpage/flight-tracker/internal/handlers"
	"github.com/dpage/flight-tracker/internal/poller"
	"github.com/dpage/flight-tracker/internal/providers"
	"github.com/dpage/flight-tracker/internal/sse"
	"github.com/dpage/flight-tracker/internal/store"
	"github.com/dpage/flight-tracker/migrations"
	"github.com/dpage/flight-tracker/web"
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
	authH := auth.NewHandler(cfg.GitHubID, cfg.GitHubSecret, cfg.SessionKey, cfg.PublicURL, s)
	hub := sse.NewHub()

	var resolver providers.Resolver
	if cfg.AeroDataBoxKey != "" {
		resolver = providers.NewCachedResolver(
			providers.NewAeroDataBox(cfg.AeroDataBoxKey),
			24*time.Hour,
		)
		slog.Info("resolver: aerodatabox (cached, ttl=24h)")
	}
	api := handlers.New(s, authH, hub, cfg, resolver)

	// Pick the upstream tracker. OpenSky if credentials are configured (or
	// anonymous OpenSky if requested), otherwise the in-memory stub. Either
	// way we wrap with DeadReckoner so coverage gaps fall back to an
	// extrapolation from the last real fix.
	var inner providers.Tracker
	switch {
	case cfg.UseOpenSky():
		inner = providers.NewOpenSky(cfg.OpenSkyUsername, cfg.OpenSkyPassword)
		slog.Info("tracker: opensky",
			"authed", cfg.OpenSkyUsername != "")
	default:
		inner = providers.NewStub()
		slog.Info("tracker: stub")
	}
	tracker := providers.NewDeadReckoner(inner, s)
	p := poller.New(s, tracker, hub, cfg.PollInterval)
	// Give the poller the resolver too so it can backfill missing metadata
	// on flights that were added manually with blanks.
	p.Resolver = resolver
	go p.Run(rootCtx)

	if cfg.EmailIngestEnabled {
		if resolver == nil {
			return errors.New("EMAIL_INGEST_ENABLED=1 requires a configured resolver (set AERODATABOX_RAPIDAPI_KEY)")
		}
		llmClient, err := emailingest.NewRealLLM(cfg.LLMProvider, cfg.LLMModel, cfg.LLMAPIKey)
		if err != nil {
			return err
		}
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
			Extractor:  emailingest.NewExtractor(llmClient, cfg.LLMModel),
			FlightDeps: flightops.Deps{Store: s, Resolver: resolver},
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
