package store

import (
	"testing"
	"time"
)

// TestActiveFlightParts: only non-terminal flight parts at/after departure are
// returned, keyed on plan_part_id, mirroring the legacy ActiveFlights filter.
func TestActiveFlightParts(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	// Active: departed an hour ago, arrives in an hour, Enroute.
	active := mkFlightPartInTrip(t, s, trip, owner, "ACT1",
		now.Add(-time.Hour), now.Add(time.Hour), "Enroute", 51.47, -0.46, 40.64, -73.78)
	// Terminal: Arrived → excluded.
	mkFlightPartInTrip(t, s, trip, owner, "DONE1",
		now.Add(-3*time.Hour), now.Add(-time.Hour), "Arrived", 51.47, -0.46, 40.64, -73.78)
	// Far future: departs in 2 days → excluded by the departure bound.
	mkFlightPartInTrip(t, s, trip, owner, "FUT1",
		now.Add(48*time.Hour), now.Add(50*time.Hour), "Scheduled", 51.47, -0.46, 40.64, -73.78)

	parts, err := s.ActiveFlightParts(ctx, now)
	if err != nil {
		t.Fatalf("ActiveFlightParts: %v", err)
	}
	if len(parts) != 1 || parts[0].ID != active {
		t.Fatalf("expected only the active part %d, got %d: %+v", active, len(parts), parts)
	}
	// The carrier must be populated for the providers: id is the plan_part_id,
	// coords from the part, schedule/status from flight_details.
	got := parts[0]
	if got.Ident != "ACT1" || got.Status != "Enroute" || got.OriginIATA != "LHR" {
		t.Errorf("carrier not populated from join: %+v", got)
	}
	if got.OriginLat == nil || got.DestLat == nil {
		t.Errorf("coords should come from the plan_part: %+v", got)
	}
}

// TestFlightPartWriteHelpers exercises the part-keyed status / airframe /
// backfill writers — the mechanical counterparts the poller now calls.
func TestFlightPartWriteHelpers(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	// Departed already → status should derive to Enroute on refresh.
	part := mkFlightPart(t, s, owner, "BA286", now.Add(-time.Hour), now.Add(time.Hour))

	if err := s.RefreshFlightPartStatus(ctx, part); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	f, _ := s.FlightPartByID(ctx, part)
	if f.Status != "Enroute" {
		t.Errorf("status should derive to Enroute, got %q", f.Status)
	}
	if f.LastPolledAt == nil {
		t.Error("RefreshFlightPartStatus should set last_polled_at")
	}

	// Backfill protects user-typed values: notes already set, so it's kept;
	// blank airframe gets filled.
	if err := s.BackfillFlightPart(ctx, part, BackfillPayload{
		ICAO24: "406B05", Callsign: "BAW286",
	}); err != nil {
		t.Fatalf("BackfillFlightPart: %v", err)
	}
	f, _ = s.FlightPartByID(ctx, part)
	if f.ICAO24 == nil || *f.ICAO24 != "406b05" {
		t.Errorf("icao24 not backfilled: %v", f.ICAO24)
	}
	if f.LastResolvedAt != nil {
		t.Error("BackfillFlightPart must NOT bump last_resolved_at")
	}

	// RefreshFlightPartAirframe overwrites and stamps last_resolved_at.
	if err := s.RefreshFlightPartAirframe(ctx, part, "3c4a8c", "DLH1"); err != nil {
		t.Fatalf("RefreshFlightPartAirframe: %v", err)
	}
	f, _ = s.FlightPartByID(ctx, part)
	if f.ICAO24 == nil || *f.ICAO24 != "3c4a8c" {
		t.Errorf("airframe should be overwritten, got %v", f.ICAO24)
	}
	if f.LastResolvedAt == nil {
		t.Error("RefreshFlightPartAirframe should bump last_resolved_at")
	}
}

// TestConvergenceWindowAndVisibility: the convergence query respects the §4
// gate and the arrival window. A plan hidden from a trip member must not appear
// in that member's results.
func TestConvergenceWindowAndVisibility(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	member := mkUser(t, s)
	stranger := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, member, "viewer")

	in := mkFlightPartInTrip(t, s, trip, owner, "VIS1",
		now.Add(-time.Hour), now.Add(2*time.Hour), "Enroute", 51.47, -0.46, 40.64, -73.78)
	// Out of the window.
	mkFlightPartInTrip(t, s, trip, owner, "FAR1",
		now.Add(20*24*time.Hour), now.Add(21*24*time.Hour), "Scheduled", 51.47, -0.46, 40.64, -73.78)

	from, to := now.Add(-7*24*time.Hour), now.Add(7*24*time.Hour)

	// Owner sees the in-window part, not the far one.
	parts, err := s.ConvergenceParts(ctx, owner, from, to, "")
	if err != nil {
		t.Fatalf("ConvergenceParts owner: %v", err)
	}
	if len(parts) != 1 || parts[0].PlanPartID != in {
		t.Fatalf("owner: expected the in-window part, got %d: %+v", len(parts), parts)
	}

	// A non-member sees nothing.
	parts, _ = s.ConvergenceParts(ctx, stranger, from, to, "")
	if len(parts) != 0 {
		t.Errorf("stranger should see no parts, got %d", len(parts))
	}

	// Hide the plan from member → member must not see it.
	planID := planOf(t, s, in)
	setVisibility(t, s, planID, "hidden_from", member)
	parts, _ = s.ConvergenceParts(ctx, member, from, to, "")
	for _, p := range parts {
		if p.PlanPartID == in {
			t.Fatalf("hidden part leaked to member: %+v", parts)
		}
	}

	// TrackerPartByID enforces the same gate.
	if _, err := s.TrackerPartByID(ctx, member, in); err != ErrNotFound {
		t.Errorf("TrackerPartByID for a hidden viewer should be ErrNotFound, got %v", err)
	}
	if _, err := s.TrackerPartByID(ctx, owner, in); err != nil {
		t.Errorf("owner should see their own part via TrackerPartByID: %v", err)
	}
}

// TestTaggedTripSpan: the derived span covers the tagged trip's parts.
func TestTaggedTripSpan(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now().Truncate(time.Second)
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	tagTrip(t, s, trip, "ski")
	start := now.Add(10 * 24 * time.Hour)
	end := now.Add(12 * 24 * time.Hour)
	mkFlightPartInTrip(t, s, trip, owner, "SKI1", start, end, "Scheduled", 51.47, -0.46, 40.64, -73.78)

	from, to, ok, err := s.TaggedTripSpan(ctx, owner, "ski")
	if err != nil {
		t.Fatalf("TaggedTripSpan: %v", err)
	}
	if !ok {
		t.Fatal("expected a span for the tagged trip")
	}
	if from.Unix() != start.Unix() || to.Unix() != end.Unix() {
		t.Errorf("span = [%v, %v], want [%v, %v]", from, to, start, end)
	}

	// An untagged / unknown tag yields no span.
	if _, _, ok, _ := s.TaggedTripSpan(ctx, owner, "beach"); ok {
		t.Error("unknown tag should yield no span")
	}
}

func planOf(t *testing.T, s *Store, partID int64) int64 {
	t.Helper()
	var planID int64
	if err := s.pool.QueryRow(ctx,
		`SELECT plan_id FROM plan_parts WHERE id = $1`, partID).Scan(&planID); err != nil {
		t.Fatalf("planOf: %v", err)
	}
	return planID
}

func tagTrip(t *testing.T, s *Store, tripID int64, label string) {
	t.Helper()
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO trip_tags (trip_id, label_norm, label_display) VALUES ($1, $2, $2)`,
		tripID, label); err != nil {
		t.Fatalf("tagTrip: %v", err)
	}
}
