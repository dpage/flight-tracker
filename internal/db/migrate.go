package db

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrate applies any unapplied *.up.sql files from mfs in version order.
// Each file is executed inside its own transaction.
func Migrate(ctx context.Context, pool *pgxpool.Pool, mfs fs.FS) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	type mig struct {
		version int
		name    string
	}
	var ups []mig
	entries, err := fs.ReadDir(mfs, ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".up.sql") {
			continue
		}
		var v int
		if _, err := fmt.Sscanf(n, "%04d_", &v); err != nil {
			return fmt.Errorf("parse migration filename %q: %w", n, err)
		}
		ups = append(ups, mig{v, n})
	}
	sort.Slice(ups, func(i, j int) bool { return ups[i].version < ups[j].version })

	applied := map[int]bool{}
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	// pgx surfaces query errors lazily via Err(); without this check a failed
	// applied-versions read would be silently treated as "nothing applied".
	if err := rows.Err(); err != nil {
		return err
	}

	for _, m := range ups {
		if applied[m.version] {
			continue
		}
		sqlBytes, err := fs.ReadFile(mfs, m.name)
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", m.name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations(version, name) VALUES ($1, $2)`,
			m.version, m.name,
		); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		slog.Info("migration applied", "version", m.version, "name", m.name)
	}
	return nil
}
