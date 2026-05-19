// Package testsupport provides shared helpers for DB-backed tests: it spins
// up an isolated, migrated PostgreSQL database per test and tears it down on
// cleanup. It is imported only from _test.go files.
package testsupport

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dpage/flight-tracker/internal/db"
	"github.com/dpage/flight-tracker/migrations"
)

var counter atomic.Int64

// adminURL is the maintenance connection string (points at a database that
// always exists, e.g. "postgres") used to CREATE/DROP the per-test database.
func adminURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://flight_tracker:flight_tracker@127.0.0.1:5432/postgres?sslmode=disable"
}

// NewPool creates a fresh, migrated database and returns a connected pool.
// The database is dropped when the test finishes. If PostgreSQL is not
// reachable the test is skipped (so `go test ./...` stays green in
// environments without a database), unless FT_REQUIRE_DB=1 is set.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, adminURL())
	if err != nil {
		skipOrFatal(t, fmt.Sprintf("connect admin db: %v", err))
		return nil
	}
	defer admin.Close(ctx)

	name := fmt.Sprintf("ft_test_%d_%d_%d",
		os.Getpid(), time.Now().UnixNano(), counter.Add(1))
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		skipOrFatal(t, fmt.Sprintf("create database: %v", err))
		return nil
	}

	u, err := url.Parse(adminURL())
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	u.Path = "/" + name
	pool, err := db.Open(ctx, u.String())
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		a, err := pgx.Connect(ctx, adminURL())
		if err != nil {
			return
		}
		defer a.Close(ctx)
		_, _ = a.Exec(ctx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
	})
	return pool
}

// NewDatabaseURL creates a fresh, EMPTY (un-migrated) database and returns
// its connection string. The database is dropped on test cleanup. Useful for
// exercising code paths that run migrations themselves (e.g. the server's
// startup sequence).
func NewDatabaseURL(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, adminURL())
	if err != nil {
		skipOrFatal(t, fmt.Sprintf("connect admin db: %v", err))
		return ""
	}
	defer admin.Close(ctx)

	name := fmt.Sprintf("ft_url_%d_%d_%d",
		os.Getpid(), time.Now().UnixNano(), counter.Add(1))
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		skipOrFatal(t, fmt.Sprintf("create database: %v", err))
		return ""
	}
	t.Cleanup(func() {
		a, err := pgx.Connect(ctx, adminURL())
		if err != nil {
			return
		}
		defer a.Close(ctx)
		_, _ = a.Exec(ctx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
	})

	u, err := url.Parse(adminURL())
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

func skipOrFatal(t *testing.T, msg string) {
	t.Helper()
	if os.Getenv("FT_REQUIRE_DB") == "1" {
		t.Fatalf("DB required but unavailable: %s", msg)
	}
	t.Skipf("skipping DB-backed test: %s", msg)
}
