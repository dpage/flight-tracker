package testsupport

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// InsertUser inserts a user row directly and returns its id.
func InsertUser(t *testing.T, pool *pgxpool.Pool, login string, superuser, active bool) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (github_login, name, is_superuser, is_active)
		VALUES ($1, $1, $2, $3) RETURNING id`,
		login, superuser, active,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert user %q: %v", login, err)
	}
	return id
}

// InsertFlight inserts a minimal flight row and returns its id. out/in are the
// scheduled times; status defaults to 'Scheduled'.
func InsertFlight(t *testing.T, pool *pgxpool.Pool, ident string, out, in time.Time) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO flights (ident, scheduled_out, scheduled_in,
			origin_iata, origin_lat, origin_lon, dest_iata, dest_lat, dest_lon)
		VALUES ($1, $2, $3, 'LHR', 51.4775, -0.4614, 'JFK', 40.6413, -73.7781)
		RETURNING id`,
		ident, out, in,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert flight %q: %v", ident, err)
	}
	return id
}

// InsertPosition inserts a single position row for a flight.
func InsertPosition(t *testing.T, pool *pgxpool.Pool, flightID int64, ts time.Time, lat, lon float64, estimated bool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO positions (flight_id, ts, lat, lon, is_estimated)
		VALUES ($1, $2, $3, $4, $5)`,
		flightID, ts, lat, lon, estimated)
	if err != nil {
		t.Fatalf("insert position: %v", err)
	}
}
