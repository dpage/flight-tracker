// Command server is the flight-tracker HTTP server.
//
// It serves the React SPA, exposes the JSON API, handles GitHub OAuth, runs the
// AeroAPI polling loop, and broadcasts flight updates over Server-Sent Events.
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

	"github.com/dpage/flight-tracker/internal/aeroapi"
	"github.com/dpage/flight-tracker/internal/auth"
	"github.com/dpage/flight-tracker/internal/config"
	"github.com/dpage/flight-tracker/internal/db"
	"github.com/dpage/flight-tracker/internal/handlers"
	"github.com/dpage/flight-tracker/internal/poller"
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
	api := handlers.New(s, authH, hub, cfg)

	// Pick the upstream tracker. OpenSky if credentials are configured (or
	// anonymous OpenSky if requested), otherwise the in-memory stub. Either
	// way we wrap with DeadReckoner so coverage gaps fall back to an
	// extrapolation from the last real fix.
	var inner aeroapi.Tracker
	switch {
	case cfg.UseOpenSky():
		inner = aeroapi.NewOpenSky(cfg.OpenSkyUsername, cfg.OpenSkyPassword)
		slog.Info("tracker: opensky",
			"authed", cfg.OpenSkyUsername != "")
	default:
		inner = aeroapi.NewStub()
		slog.Info("tracker: stub")
	}
	tracker := aeroapi.NewDeadReckoner(inner, s)
	p := poller.New(s, tracker, hub, cfg.PollInterval)
	go p.Run(rootCtx)

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
