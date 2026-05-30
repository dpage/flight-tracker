package store

import (
	"testing"
	"time"
)

// mkFlightPart seeds a trip + flight plan + one plan_part + its flight_details
// satellite, returning the plan_part_id (the key positions and the poller now
// use). The trip is owned by ownerID with the owner trip_members row, matching
// the helpers in plans_visibility_test.go. status is the flight_details
// flight_status enum value.
func mkFlightPart(t *testing.T, s *Store, ownerID int64, ident string, out, in time.Time) int64 {
	t.Helper()
	trip := mkTrip(t, s, ownerID)
	return mkFlightPartInTrip(t, s, trip, ownerID, ident, out, in, "Scheduled",
		51.4775, -0.4614, 40.6413, -73.7781)
}

// mkFlightPartInTrip is the fuller seeder: it lets a test place the part in a
// specific trip with chosen coords + status, for the convergence/visibility
// tests. Pass NaN-free coords; the start/end coords land on the plan_part and
// the schedule/status/airframe on flight_details.
func mkFlightPartInTrip(t *testing.T, s *Store, tripID, createdBy int64, ident string,
	out, in time.Time, status string,
	startLat, startLon, endLat, endLon float64) int64 {
	t.Helper()
	var planID int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`,
		tripID, createdBy,
	).Scan(&planID); err != nil {
		t.Fatalf("insert flight plan: %v", err)
	}
	var partID int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_lat, start_lon, end_lat, end_lon, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'confirmed') RETURNING id`,
		planID, out, in, startLat, startLon, endLat, endLon,
	).Scan(&partID); err != nil {
		t.Fatalf("insert flight plan_part: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO flight_details (plan_part_id, ident, scheduled_out, scheduled_in,
			origin_iata, dest_iata, flight_status)
		VALUES ($1, $2, $3, $4, 'LHR', 'JFK', $5)`,
		partID, ident, out, in, status); err != nil {
		t.Fatalf("insert flight_details: %v", err)
	}
	return partID
}

// TestPositions exercises the part-keyed position helpers (positions.go). The
// FlightID field on Position now carries a plan_part_id; the helpers key on it.
func TestPositions(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	part := mkFlightPart(t, s, owner, "POS1", now, now.Add(time.Hour))

	if p, err := s.LatestRealPosition(ctx, part); err != nil || p != nil {
		t.Fatalf("no positions yet → nil: %v %v", p, err)
	}
	if m, _ := s.LatestPartPositions(ctx, nil); len(m) != 0 {
		t.Error("empty ids → empty map")
	}
	if m, _ := s.PartTracks(ctx, nil, 0); len(m) != 0 {
		t.Error("empty ids → empty map")
	}

	t0 := now.Add(-30 * time.Minute)
	hdr := int16(90)
	for i := 0; i < 3; i++ {
		err := s.InsertPartPosition(ctx, Position{
			FlightID: part, Ts: t0.Add(time.Duration(i) * time.Minute),
			Lat: float64(i), Lon: float64(-i), HeadingDeg: &hdr, IsEstimated: i == 2,
		})
		if err != nil {
			t.Fatalf("InsertPosition: %v", err)
		}
	}

	real, err := s.LatestRealPosition(ctx, part)
	if err != nil || real == nil || real.IsEstimated {
		t.Fatalf("LatestRealPosition should skip estimated: %v %v", real, err)
	}
	if real.Lat != 1 {
		t.Errorf("expected latest real (i=1) lat=1, got %v", real.Lat)
	}

	latest, _ := s.LatestPartPositions(ctx, []int64{part})
	if latest[part] == nil || latest[part].Lat != 2 {
		t.Errorf("LatestPositions should pick newest (i=2): %+v", latest[part])
	}

	tracks, _ := s.PartTracks(ctx, []int64{part}, 0) // 0 → default limit
	if len(tracks[part]) != 3 {
		t.Errorf("RecentTracks count = %d, want 3", len(tracks[part]))
	}
	if tracks[part][0].Lat != 0 || tracks[part][2].Lat != 2 {
		t.Errorf("RecentTracks order wrong: %+v", tracks[part])
	}

	pf, _ := s.PositionsForFlight(ctx, part, 0) // 0 → default limit
	if len(pf) != 3 || pf[0].Lat != 2 {
		t.Errorf("PositionsForFlight newest-first wrong: %+v", pf)
	}
	pf2, _ := s.PositionsForFlight(ctx, part, 1)
	if len(pf2) != 1 {
		t.Errorf("PositionsForFlight limit not applied: %d", len(pf2))
	}

	any, err := s.LatestPosition(ctx, part)
	if err != nil || any == nil {
		t.Fatalf("LatestPosition should return the newest row regardless of is_estimated: %v %v", any, err)
	}
	if any.Lat != 2 || !any.IsEstimated {
		t.Errorf("LatestPosition expected estimated i=2 (lat=2), got %+v", any)
	}
}
