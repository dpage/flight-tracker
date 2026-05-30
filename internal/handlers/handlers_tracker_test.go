package handlers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
)

// seedFlightPart inserts a trip (owned by owner), a flight plan, one plan_part,
// and its flight_details, returning the plan_part_id. The part's effective
// arrival is `in`. Visibility tests layer plan_visibility rows on top.
func seedFlightPart(t *testing.T, e *testEnv, owner int64, ident string, out, in time.Time) (tripID, planID, partID int64) {
	t.Helper()
	ctx := context.Background()
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('Trip', $1) RETURNING id`, owner,
	).Scan(&tripID); err != nil {
		t.Fatalf("insert trip: %v", err)
	}
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`, tripID, owner,
	); err != nil {
		t.Fatalf("insert owner member: %v", err)
	}
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`, tripID, owner,
	).Scan(&planID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	if err := e.pool.QueryRow(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_lat, start_lon, end_lat, end_lon, status)
		VALUES ($1, $2, $3, 51.47, -0.46, 40.64, -73.78, 'confirmed') RETURNING id`,
		planID, out, in,
	).Scan(&partID); err != nil {
		t.Fatalf("insert plan_part: %v", err)
	}
	if _, err := e.pool.Exec(ctx, `
		INSERT INTO flight_details (plan_part_id, ident, scheduled_out, scheduled_in,
			origin_iata, dest_iata, flight_status)
		VALUES ($1, $2, $3, $4, 'LHR', 'JFK', 'Enroute')`,
		partID, ident, out, in,
	); err != nil {
		t.Fatalf("insert flight_details: %v", err)
	}
	return tripID, planID, partID
}

func addMember(t *testing.T, e *testEnv, tripID, userID int64, role string) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (trip_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		tripID, userID, role); err != nil {
		t.Fatalf("add member: %v", err)
	}
}

func hideFrom(t *testing.T, e *testEnv, planID, userID int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO plan_visibility (plan_id, mode) VALUES ($1, 'hidden_from')
		 ON CONFLICT (plan_id) DO UPDATE SET mode = 'hidden_from'`, planID); err != nil {
		t.Fatalf("set visibility: %v", err)
	}
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO plan_visibility_members (plan_id, user_id) VALUES ($1, $2)
		 ON CONFLICT DO NOTHING`, planID, userID); err != nil {
		t.Fatalf("set visibility member: %v", err)
	}
}

// TestTrackerConvergenceWindow: an in-window visible flight part shows up; one
// whose arrival is outside the window does not. No ranking — just the parts.
func TestTrackerConvergenceWindow(t *testing.T) {
	e := setup(t, nil, nil)
	if e == nil {
		return
	}
	owner := e.user(t, "owner", false)
	now := time.Now()
	// In-window: arriving in 2h.
	_, _, inPart := seedFlightPart(t, e, owner, "IN1", now.Add(-time.Hour), now.Add(2*time.Hour))
	// Out-of-window: arriving in 30 days (default window is 7d each side).
	seedFlightPart(t, e, owner, "OUT1", now.Add(29*24*time.Hour), now.Add(30*24*time.Hour))

	w := e.req(t, "GET", "/api/tracker", nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/tracker = %d, body=%s", w.Code, w.Body.String())
	}
	got := decodeBody[[]api.TrackerPartDTO](t, w)
	if len(got) != 1 {
		t.Fatalf("expected exactly the in-window part, got %d: %+v", len(got), got)
	}
	if got[0].PlanPartID != inPart || got[0].Ident != "IN1" {
		t.Errorf("wrong part returned: %+v", got[0])
	}
}

// TestTrackerHiddenPlanNotVisible is the required privacy test: a flight part
// hidden from a viewer (per-plan privacy) must NOT appear in their convergence
// results, even though they are on the trip and the part is in-window.
func TestTrackerHiddenPlanNotVisible(t *testing.T) {
	e := setup(t, nil, nil)
	if e == nil {
		return
	}
	owner := e.user(t, "owner", false)
	viewer := e.user(t, "viewer", false)
	now := time.Now()
	tripID, planID, partID := seedFlightPart(t, e, owner, "SECRET1", now.Add(-time.Hour), now.Add(2*time.Hour))
	addMember(t, e, tripID, viewer, "viewer") // viewer is on the trip…
	hideFrom(t, e, planID, viewer)            // …but the plan is hidden from them

	// The owner still sees it.
	w := e.req(t, "GET", "/api/tracker", nil, owner)
	owns := decodeBody[[]api.TrackerPartDTO](t, w)
	if len(owns) != 1 || owns[0].PlanPartID != partID {
		t.Fatalf("owner should see their own part, got %d: %+v", len(owns), owns)
	}

	// The hidden viewer must NOT.
	w = e.req(t, "GET", "/api/tracker", nil, viewer)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/tracker (viewer) = %d, body=%s", w.Code, w.Body.String())
	}
	hidden := decodeBody[[]api.TrackerPartDTO](t, w)
	for _, p := range hidden {
		if p.PlanPartID == partID {
			t.Fatalf("hidden part %d leaked into viewer's convergence results: %+v", partID, hidden)
		}
	}
	if len(hidden) != 0 {
		t.Errorf("viewer should see no parts, got %d: %+v", len(hidden), hidden)
	}
}

// TestTrackerTagWindow: when a tag is given with no explicit window, the default
// window is derived from the tagged trips' span, so a part far in the future
// (outside the 7d default) still surfaces because its trip carries the tag.
func TestTrackerTagWindow(t *testing.T) {
	e := setup(t, nil, nil)
	if e == nil {
		return
	}
	owner := e.user(t, "owner", false)
	now := time.Now()
	// A tagged trip whose flight arrives 30 days out — outside the 7d default,
	// but inside the tag-derived span.
	tripID, _, farPart := seedFlightPart(t, e, owner, "FAR1", now.Add(30*24*time.Hour), now.Add(31*24*time.Hour))
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO trip_tags (trip_id, label_norm, label_display) VALUES ($1, 'ski', 'Ski')`, tripID,
	); err != nil {
		t.Fatalf("tag trip: %v", err)
	}

	// Without the tag, the default 7d window excludes it.
	w := e.req(t, "GET", "/api/tracker", nil, owner)
	if n := len(decodeBody[[]api.TrackerPartDTO](t, w)); n != 0 {
		t.Fatalf("default window should exclude the far part, got %d", n)
	}

	// With the tag, the derived span includes it.
	w = e.req(t, "GET", "/api/tracker?tag=ski", nil, owner)
	got := decodeBody[[]api.TrackerPartDTO](t, w)
	if len(got) != 1 || got[0].PlanPartID != farPart {
		t.Fatalf("tag-derived window should include the far part, got %d: %+v", len(got), got)
	}
}
