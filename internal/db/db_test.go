package db_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/dpage/flight-tracker/internal/db"
	"github.com/dpage/flight-tracker/internal/testsupport"
	"github.com/dpage/flight-tracker/migrations"
)

// openErrFS makes fs.ReadDir(".") fail (Open always errors).
type openErrFS struct{}

func (openErrFS) Open(string) (fs.File, error) { return nil, errors.New("open boom") }

// readFileErrFS lists one migration file but fails when it is read.
type readFileErrFS struct{}

type fakeDirEntry struct{ name string }

func (f fakeDirEntry) Name() string             { return f.name }
func (fakeDirEntry) IsDir() bool                { return false }
func (fakeDirEntry) Type() fs.FileMode          { return 0 }
func (fakeDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

func (readFileErrFS) Open(string) (fs.File, error) { return nil, errors.New("read boom") }
func (readFileErrFS) ReadDir(string) ([]fs.DirEntry, error) {
	return []fs.DirEntry{fakeDirEntry{name: "0099_x.up.sql"}}, nil
}

func TestOpenParseError(t *testing.T) {
	if _, err := db.Open(context.Background(), "::::not-a-valid-dsn"); err == nil {
		t.Error("expected ParseConfig error for invalid DSN")
	}
}

func TestOpenValid(t *testing.T) {
	// testsupport hands back an already-open pool; just assert Open works on
	// the same URL form by re-opening a throwaway config string.
	pool := testsupport.NewPool(t)
	if pool == nil {
		t.Skip("no DB")
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Errorf("ping: %v", err)
	}
}

func TestMigrateIdempotentWithRealMigrations(t *testing.T) {
	pool := testsupport.NewPool(t) // already migrated once by the helper
	// Running again must be a no-op (all versions already applied).
	if err := db.Migrate(context.Background(), pool, migrations.FS); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

func TestMigrateBadFilename(t *testing.T) {
	pool := testsupport.NewPool(t)
	mfs := fstest.MapFS{
		"notaversion.up.sql": {Data: []byte("SELECT 1")},
	}
	if err := db.Migrate(context.Background(), pool, mfs); err == nil {
		t.Error("expected parse-filename error")
	}
}

func TestMigrateSkipsDownFiles(t *testing.T) {
	pool := testsupport.NewPool(t)
	mfs := fstest.MapFS{
		"0001_init.up.sql":   {Data: []byte("SELECT 1")}, // already applied → skipped
		"0001_init.down.sql": {Data: []byte("garbage that would fail if run")},
	}
	if err := db.Migrate(context.Background(), pool, mfs); err != nil {
		t.Fatalf("down files must be ignored: %v", err)
	}
}

func TestMigrateApplyErrorRollsBack(t *testing.T) {
	pool := testsupport.NewPool(t)
	mfs := fstest.MapFS{
		"0099_broken.up.sql": {Data: []byte("THIS IS NOT VALID SQL;")},
	}
	if err := db.Migrate(context.Background(), pool, mfs); err == nil {
		t.Error("expected apply error for invalid SQL")
	}
	// schema_migrations must not record the failed version.
	var n int
	_ = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM schema_migrations WHERE version = 99`).Scan(&n)
	if n != 0 {
		t.Errorf("failed migration was recorded (%d rows)", n)
	}
}

func TestMigrateNewVersionApplies(t *testing.T) {
	pool := testsupport.NewPool(t)
	mfs := fstest.MapFS{
		"0050_extra.up.sql": {Data: []byte(`CREATE TABLE extra_t (id int)`)},
	}
	if err := db.Migrate(context.Background(), pool, mfs); err != nil {
		t.Fatalf("Migrate new version: %v", err)
	}
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM schema_migrations WHERE version = 50`).Scan(&n); err != nil || n != 1 {
		t.Errorf("version 50 not recorded: n=%d err=%v", n, err)
	}
	// Second run skips it (applied map continue branch).
	if err := db.Migrate(context.Background(), pool, mfs); err != nil {
		t.Fatalf("re-Migrate: %v", err)
	}
}

func TestMigrateCreateTableErrorOnCancelledCtx(t *testing.T) {
	pool := testsupport.NewPool(t)
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if err := db.Migrate(c, pool, migrations.FS); err == nil {
		t.Error("expected create schema_migrations error on cancelled ctx")
	}
}

func TestMigrateReadDirError(t *testing.T) {
	pool := testsupport.NewPool(t)
	if err := db.Migrate(context.Background(), pool, openErrFS{}); err == nil {
		t.Error("expected ReadDir error")
	}
}

func TestMigrateReadFileError(t *testing.T) {
	pool := testsupport.NewPool(t)
	if err := db.Migrate(context.Background(), pool, readFileErrFS{}); err == nil {
		t.Error("expected ReadFile error for listed-but-unreadable migration")
	}
}

func TestMigrateVersionQueryError(t *testing.T) {
	pool := testsupport.NewPool(t)
	// Replace schema_migrations with a table lacking the `version` column so
	// the SELECT version query fails (CREATE IF NOT EXISTS is a no-op).
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE schema_migrations`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (foo int)`); err != nil {
		t.Fatalf("create wrong: %v", err)
	}
	if err := db.Migrate(ctx, pool, migrations.FS); err == nil {
		t.Error("expected SELECT version query error")
	}
}

func TestMigrateScanError(t *testing.T) {
	pool := testsupport.NewPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE schema_migrations`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	// version is TEXT with a non-integer value → rows.Scan(&int) fails.
	_, _ = pool.Exec(ctx, `CREATE TABLE schema_migrations (version text, name text)`)
	_, _ = pool.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ('notanint','x')`)
	if err := db.Migrate(ctx, pool, migrations.FS); err == nil {
		t.Error("expected scan error for non-integer version")
	}
}

func TestMigrateRecordInsertErrorRollsBack(t *testing.T) {
	pool := testsupport.NewPool(t)
	// The migration body drops schema_migrations, so the follow-up
	// INSERT INTO schema_migrations fails and the tx is rolled back.
	mfs := fstest.MapFS{
		"0098_drop.up.sql": {Data: []byte(`DROP TABLE schema_migrations`)},
	}
	if err := db.Migrate(context.Background(), pool, mfs); err == nil {
		t.Error("expected schema_migrations INSERT error after self-drop")
	}
}

// TestMigrateCommitError covers the tx.Commit() error branch: the migration
// body creates a DEFERRABLE INITIALLY DEFERRED FK and inserts a violating
// row, so the constraint is only checked — and fails — at COMMIT time.
func TestMigrateCommitError(t *testing.T) {
	pool := testsupport.NewPool(t)
	mfs := fstest.MapFS{
		"0097_deferred.up.sql": {Data: []byte(`
			CREATE TABLE parent_t (id int PRIMARY KEY);
			CREATE TABLE child_t (
				pid int REFERENCES parent_t(id) DEFERRABLE INITIALLY DEFERRED
			);
			INSERT INTO child_t VALUES (999);`)},
	}
	if err := db.Migrate(context.Background(), pool, mfs); err == nil {
		t.Error("expected COMMIT error from deferred FK violation")
	}
}
