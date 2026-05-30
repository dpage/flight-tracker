package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dpage/aerly/internal/testsupport"
	"github.com/dpage/aerly/migrations"
)

// readUpDown returns the bodies of a migration's .up.sql / .down.sql.
func readUpDown(t *testing.T, base string) (up, down string) {
	t.Helper()
	ub, err := migrations.FS.ReadFile(base + ".up.sql")
	if err != nil {
		t.Fatalf("read %s.up.sql: %v", base, err)
	}
	db, err := migrations.FS.ReadFile(base + ".down.sql")
	if err != nil {
		t.Fatalf("read %s.down.sql: %v", base, err)
	}
	return string(ub), string(db)
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables
		                WHERE table_schema='public' AND table_name=$1)`, name,
	).Scan(&ok); err != nil {
		t.Fatalf("table exists %q: %v", name, err)
	}
	return ok
}

func columnExists(t *testing.T, pool *pgxpool.Pool, table, col string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		                WHERE table_schema='public' AND table_name=$1 AND column_name=$2)`,
		table, col,
	).Scan(&ok); err != nil {
		t.Fatalf("column exists %q.%q: %v", table, col, err)
	}
	return ok
}

// TestMigration0010UpDown verifies the trip-core migration creates its tables,
// re-keys positions, installs the passenger⇒viewer trigger, and that the down
// migration cleanly reverses it (leaving the surviving legacy tables intact and
// re-applies up afterwards).
func TestMigration0010UpDown(t *testing.T) {
	pool := testsupport.NewPool(t) // applies all .up.sql migrations, incl. 0010
	if pool == nil {
		return
	}
	ctx := context.Background()

	newTables := []string{
		"trips", "trip_members", "trip_tags", "plans", "plan_parts",
		"flight_details", "hotel_details", "train_details", "ground_details",
		"dining_details", "excursion_details", "plan_passengers",
		"plan_visibility", "plan_visibility_members", "alert_prefs",
		"plan_alert_optin", "calendar_tokens",
	}
	for _, tbl := range newTables {
		if !tableExists(t, pool, tbl) {
			t.Errorf("expected table %q after up", tbl)
		}
	}
	if !columnExists(t, pool, "positions", "plan_part_id") {
		t.Error("positions.plan_part_id missing after up")
	}

	// The passenger⇒viewer trigger: inserting a plan_passenger should create a
	// viewer trip_members row for that user on the plan's trip.
	uid := testsupport.InsertUser(t, pool, "trigtest", false, true)
	var tripID, planID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('T', $1) RETURNING id`, uid,
	).Scan(&tripID); err != nil {
		t.Fatalf("insert trip: %v", err)
	}
	other := testsupport.InsertUser(t, pool, "trigpax", false, true)
	if err := pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type) VALUES ($1, 'flight') RETURNING id`, tripID,
	).Scan(&planID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO plan_passengers (plan_id, user_id) VALUES ($1, $2)`, planID, other,
	); err != nil {
		t.Fatalf("insert plan_passenger: %v", err)
	}
	var role string
	if err := pool.QueryRow(ctx,
		`SELECT role FROM trip_members WHERE trip_id=$1 AND user_id=$2`, tripID, other,
	).Scan(&role); err != nil {
		t.Fatalf("trigger should have inserted trip_members row: %v", err)
	}
	if role != "viewer" {
		t.Errorf("trigger role = %q, want viewer", role)
	}

	// Down then up again — exercises the reverse, and that the FK is restored.
	up, down := readUpDown(t, "0010_trip_core")
	if _, err := pool.Exec(ctx, down); err != nil {
		t.Fatalf("apply down: %v", err)
	}
	for _, tbl := range newTables {
		if tableExists(t, pool, tbl) {
			t.Errorf("table %q still present after down", tbl)
		}
	}
	if columnExists(t, pool, "positions", "plan_part_id") {
		t.Error("positions.plan_part_id still present after down")
	}
	// Legacy tables must survive the down (Wave 3 drops them, not 0010).
	if !tableExists(t, pool, "flights") {
		t.Error("legacy flights table should survive 0010 down")
	}
	// Re-apply up to confirm reversibility.
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("re-apply up: %v", err)
	}
	if !tableExists(t, pool, "trips") {
		t.Error("trips missing after re-applying up")
	}
}
