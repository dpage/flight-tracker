package main

import (
	"context"
	"net"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dpage/flight-tracker/internal/testsupport"
)

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	return l.Addr().String()
}

// devEnv sets a minimal valid dev configuration; callers override as needed.
func devEnv(t *testing.T, dbURL, addr string) {
	t.Helper()
	t.Setenv("SESSION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("DATABASE_URL", dbURL)
	t.Setenv("DEV_AUTH_BYPASS", "1")
	t.Setenv("PUBLIC_URL", "http://localhost")
	t.Setenv("LISTEN_ADDR", addr)
	t.Setenv("GITHUB_CLIENT_ID", "")
	t.Setenv("GITHUB_CLIENT_SECRET", "")
	t.Setenv("POLL_INTERVAL", "60s")
	t.Setenv("OPENSKY_USERNAME", "")
	t.Setenv("OPENSKY_PASSWORD", "")
	t.Setenv("OPENSKY_ENABLED", "")
	t.Setenv("AERODATABOX_RAPIDAPI_KEY", "")
}

func TestRunConfigError(t *testing.T) {
	t.Setenv("SESSION_KEY", "short")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("GITHUB_CLIENT_ID", "")
	t.Setenv("GITHUB_CLIENT_SECRET", "")
	t.Setenv("DEV_AUTH_BYPASS", "")
	t.Setenv("POLL_INTERVAL", "")
	if err := run(); err == nil {
		t.Fatal("expected run() to fail with an invalid config")
	}
}

func TestRunBadDatabaseURL(t *testing.T) {
	devEnv(t, "::::not-a-valid-dsn", "127.0.0.1:0")
	if err := run(); err == nil {
		t.Fatal("expected run() to fail opening an invalid DATABASE_URL")
	}
}

func TestRunMigrateError(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t)
	if dbURL == "" {
		t.Skip("no database available")
	}
	// Pre-create a conflicting `users` table so migration 0001 fails.
	conn, err := pgx.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(context.Background(), `CREATE TABLE users (x int)`); err != nil {
		t.Fatalf("taint db: %v", err)
	}
	conn.Close(context.Background())

	devEnv(t, dbURL, "127.0.0.1:0")
	if err := run(); err == nil {
		t.Fatal("expected run() to fail when migrations conflict")
	}
}

func TestRunListenError(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t)
	if dbURL == "" {
		t.Skip("no database available")
	}
	// Hold a listener so ListenAndServe on the same address fails.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	devEnv(t, dbURL, l.Addr().String())
	if err := run(); err == nil {
		t.Fatal("expected run() to surface a ListenAndServe error")
	}
}

func TestMainGracefulShutdown(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t)
	if dbURL == "" {
		t.Skip("no database available")
	}
	addr := freePort(t)
	devEnv(t, dbURL, addr)
	// Exercise the AeroDataBox resolver and OpenSky tracker selection
	// branches in the same happy-path startup.
	t.Setenv("AERODATABOX_RAPIDAPI_KEY", "test-key")
	t.Setenv("OPENSKY_ENABLED", "1")

	done := make(chan struct{})
	go func() { main(); close(done) }()

	healthOK := false
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				healthOK = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !healthOK {
		t.Fatal("server never became healthy")
	}
	// dev-login route is registered when DEV_AUTH_BYPASS=1.
	if resp, err := http.Get("http://" + addr + "/auth/dev-login?login=tester"); err == nil {
		resp.Body.Close()
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("main() did not shut down after SIGTERM")
	}
}
