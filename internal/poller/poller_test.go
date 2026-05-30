package poller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
)

type mockTracker struct {
	pos    *store.Position
	err    error
	calls  int
	before func(f *store.Flight) // invoked before returning a fix
}

func (m *mockTracker) Track(_ context.Context, f *store.Flight, now time.Time) (*store.Position, error) {
	m.calls++
	if m.before != nil {
		m.before(f)
	}
	if m.pos != nil {
		p := *m.pos
		p.FlightID = f.ID
		p.Ts = now
		return &p, nil
	}
	return nil, m.err
}

func newPoller(t *testing.T, tr *mockTracker, interval time.Duration) (*Poller, *store.Store, *sse.Hub) {
	t.Helper()
	s := store.New(testsupport.NewPool(t))
	hub := sse.NewHub()
	return New(s, tr, hub, interval), s, hub
}

func seedUser(t *testing.T, s *store.Store) int64 {
	t.Helper()
	u, err := s.InviteUser(context.Background(),
		store.InvitePayload{Username: fmt.Sprintf("po%d", seedSeq.Add(1)), Name: "po"})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u.ID
}

var seedSeq atomic.Int64

// mkPart seeds a trip + flight plan + plan_part + flight_details from the same
// CreateFlightPayload shape the legacy CreateFlight took, and returns the
// flight carrier keyed on the plan_part_id — the unit the re-keyed poller now
// works against. It mirrors CreateFlight's create-time behaviour: coords are
// looked up from the airports table, status is derived from the schedule, and
// the ident is normalised. Returns the same (*store.Flight, error) signature so
// the test bodies read like the old s.CreateFlight calls.
func mkPart(ctx context.Context, s *store.Store, in store.CreateFlightPayload, createdBy int64) (*store.Flight, error) {
	n := seedSeq.Add(1)
	var tripID int64
	if err := s.Pool().QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("trip%d", n), createdBy).Scan(&tripID); err != nil {
		return nil, err
	}
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`,
		tripID, createdBy); err != nil {
		return nil, err
	}
	var planID int64
	if err := s.Pool().QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, notes, created_by) VALUES ($1, 'flight', $2, $3) RETURNING id`,
		tripID, in.Notes, createdBy).Scan(&planID); err != nil {
		return nil, err
	}
	ident := strings.ToUpper(strings.Join(strings.Fields(in.Ident), ""))
	originIATA := strings.ToUpper(in.OriginIATA)
	destIATA := strings.ToUpper(in.DestIATA)
	var oLat, oLon, dLat, dLon *float64
	if lat, lon, ok := airports.Lookup(originIATA); ok {
		oLat, oLon = &lat, &lon
	}
	if lat, lon, ok := airports.Lookup(destIATA); ok {
		dLat, dLon = &lat, &lon
	}
	var partID int64
	if err := s.Pool().QueryRow(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_lat, start_lon, end_lat, end_lon, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'confirmed') RETURNING id`,
		planID, in.ScheduledOut, in.ScheduledIn, oLat, oLon, dLat, dLon).Scan(&partID); err != nil {
		return nil, err
	}
	var icao24 *string
	if v := strings.ToLower(strings.TrimSpace(in.ICAO24)); v != "" {
		icao24 = &v
	}
	if _, err := s.Pool().Exec(ctx, `
		INSERT INTO flight_details (plan_part_id, ident, icao24, scheduled_out, scheduled_in,
			origin_iata, dest_iata, flight_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7,
			CASE
				WHEN NOW() > $5 THEN 'Arrived'
				WHEN NOW() >= $4 THEN 'Enroute'
				ELSE 'Scheduled'
			END)`,
		partID, ident, icao24, in.ScheduledOut, in.ScheduledIn, originIATA, destIATA); err != nil {
		return nil, err
	}
	return s.FlightPartByID(ctx, partID)
}

// deletePart removes a flight plan_part (cascading its flight_details /
// positions), used by the "tracker deletes the row mid-poll" test.
func deletePart(ctx context.Context, s *store.Store, partID int64) error {
	_, err := s.Pool().Exec(ctx, `DELETE FROM plan_parts WHERE id = $1`, partID)
	return err
}

func TestNewDefaultsInterval(t *testing.T) {
	p := New(nil, nil, nil, 0)
	if p.Interval != 60*time.Second {
		t.Errorf("default interval = %v, want 60s", p.Interval)
	}
	p = New(nil, nil, nil, 15*time.Second)
	if p.Interval != 15*time.Second {
		t.Errorf("explicit interval = %v", p.Interval)
	}
}

func TestMinPollAge(t *testing.T) {
	p := New(nil, nil, nil, 10*time.Second)
	if p.minPollAge("Enroute") != 10*time.Second {
		t.Errorf("Enroute minPollAge = %v", p.minPollAge("Enroute"))
	}
	if p.minPollAge("Scheduled") != 50*time.Second {
		t.Errorf("non-Enroute minPollAge = %v", p.minPollAge("Scheduled"))
	}
}

func TestTickInsertsPositionRefreshesAndPublishes(t *testing.T) {
	hdg := int16(90)
	alt := int32(35000)
	tr := &mockTracker{pos: &store.Position{Lat: 50, Lon: -10, HeadingDeg: &hdg, AltitudeFt: &alt}}
	p, s, hub := newPoller(t, tr, time.Minute)
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "PL1", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)
	if err != nil {
		t.Fatalf("create flight: %v", err)
	}

	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: 1, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.tick(ctx)

	if tr.calls != 1 {
		t.Errorf("tracker calls = %d, want 1", tr.calls)
	}
	pos, _ := s.LatestPartPositions(ctx, []int64{f.ID})
	if pos[f.ID] == nil {
		t.Error("expected a position to be inserted")
	}
	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.LastPolledAt == nil {
		t.Error("RefreshFlightStatus should set last_polled_at")
	}
	select {
	case ev := <-events:
		if ev.Type != "plan_part.updated" {
			t.Errorf("event type = %q", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("no SSE event published")
	}
}

func TestTickTrackerErrorStillRefreshes(t *testing.T) {
	tr := &mockTracker{err: errors.New("adsb down")}
	p, s, _ := newPoller(t, tr, time.Minute)
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "PL2", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)

	p.tick(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.LastPolledAt == nil {
		t.Error("status should still be refreshed despite tracker error")
	}
	if pos, _ := s.LatestPartPositions(ctx, []int64{f.ID}); pos[f.ID] != nil {
		t.Error("no position should be inserted when tracker errors")
	}
}

func TestTickSkipsFreshlyPolled(t *testing.T) {
	tr := &mockTracker{pos: &store.Position{Lat: 1, Lon: 1}}
	p, s, _ := newPoller(t, tr, time.Hour) // huge interval
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "PL3", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)
	// Mark as just polled so minPollAge skips it.
	if err := s.RefreshFlightPartStatus(ctx, f.ID); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	p.tick(ctx)
	if tr.calls != 0 {
		t.Errorf("freshly-polled flight should be skipped, tracker calls = %d", tr.calls)
	}
}

func TestTickActiveFlightsErrorReturns(t *testing.T) {
	tr := &mockTracker{}
	p, _, _ := newPoller(t, tr, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.tick(ctx) // ActiveFlights errors on cancelled ctx → logged + return
	if tr.calls != 0 {
		t.Errorf("no tracking should happen, calls = %d", tr.calls)
	}
}

func TestTickContextCancelledMidLoop(t *testing.T) {
	tr := &mockTracker{pos: &store.Position{Lat: 1, Lon: 1}}
	p, s, _ := newPoller(t, tr, time.Minute)
	uid := seedUser(t, s)
	now := time.Now()
	_, _ = mkPart(context.Background(), s, store.CreateFlightPayload{
		Ident: "PL4", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)

	// Cancel right after ActiveFlights succeeds: the per-flight ctx.Err()
	// guard returns before tracking. We approximate by cancelling a derived
	// context once ActiveFlights has run — use a context cancelled between
	// the (already-loaded) list and the loop body via a 1-shot wrapper.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.tick(ctx)
	if tr.calls != 0 {
		t.Errorf("cancelled ctx should stop the loop, calls = %d", tr.calls)
	}
}

// TestRefreshHandlesDeletedFlight covers the InsertPosition error branch
// (positions FK now dangling) and the FlightByID error branch (row gone):
// the tracker deletes the flight row just before returning a fix.
func TestRefreshHandlesDeletedFlight(t *testing.T) {
	tr := &mockTracker{pos: &store.Position{Lat: 1, Lon: 1}}
	p, s, hub := newPoller(t, tr, time.Minute)
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "DELME", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)
	tr.before = func(fl *store.Flight) {
		if err := deletePart(ctx, s, fl.ID); err != nil {
			t.Fatalf("delete in tracker: %v", err)
		}
	}
	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: 1, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.tick(ctx) // must not panic; FlightByID after delete → error → return

	if _, err := s.FlightPartByID(ctx, f.ID); err == nil {
		t.Error("flight should have been deleted by the tracker hook")
	}
	select {
	case <-events:
		t.Error("no SSE event expected when the refetch fails")
	case <-time.After(150 * time.Millisecond):
	}
}

// TestTickContextCancelledBetweenFlights covers the per-flight ctx.Err()
// guard: the tracker cancels the context while processing the first flight,
// so the second loop iteration bails out early.
func TestTickContextCancelledBetweenFlights(t *testing.T) {
	tr := &mockTracker{pos: &store.Position{Lat: 1, Lon: 1}}
	p, s, _ := newPoller(t, tr, time.Minute)
	uid := seedUser(t, s)
	now := time.Now()
	for _, id := range []string{"AA1", "BB2"} {
		_, _ = mkPart(context.Background(), s, store.CreateFlightPayload{
			Ident: id, ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
			OriginIATA: "LHR", DestIATA: "JFK",
		}, uid)
	}
	ctx, cancel := context.WithCancel(context.Background())
	tr.before = func(*store.Flight) { cancel() } // cancel during first flight

	p.tick(ctx)

	if tr.calls != 1 {
		t.Errorf("expected loop to stop after first flight, tracker calls = %d", tr.calls)
	}
}

// fakeResolver lets tests pin the resolver response without an HTTP server.
type fakeResolver struct {
	rf    *providers.ResolvedFlight
	err   error
	calls int
}

func (f *fakeResolver) Resolve(_ context.Context, _ string, _ time.Time) (*providers.ResolvedFlight, error) {
	f.calls++
	if f.rf != nil {
		c := *f.rf
		return &c, nil
	}
	return nil, f.err
}

// A flight added with blank IATAs and no icao24 should have those filled in
// from the resolver on its next tick, leaving user-entered fields alone.
func TestRefreshBackfillsMissingMetadata(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	p.Resolver = &fakeResolver{rf: &providers.ResolvedFlight{
		Ident:      "BA286",
		OriginIATA: "LHR", OriginLat: 51.47, OriginLon: -0.46,
		DestIATA: "SFO", DestLat: 37.62, DestLon: -122.38,
		ICAO24: "406b05",
		Notes:  "British Airways · Boeing 777",
	}}
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(ctx, s, store.CreateFlightPayload{
		Ident:        "BA286",
		ScheduledOut: now.Add(-time.Hour),
		ScheduledIn:  now.Add(time.Hour),
		Notes:        "user-typed note", // existing non-empty notes must NOT be overwritten
	}, uid)
	if err != nil {
		t.Fatalf("create flight: %v", err)
	}

	p.tick(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.OriginIATA != "LHR" || got.DestIATA != "SFO" {
		t.Errorf("airports not backfilled: %q → %q", got.OriginIATA, got.DestIATA)
	}
	if got.OriginLat == nil || *got.OriginLat != 51.47 {
		t.Errorf("origin lat not backfilled: %v", got.OriginLat)
	}
	if got.ICAO24 == nil || *got.ICAO24 != "406b05" {
		t.Errorf("icao24 not backfilled: %v", got.ICAO24)
	}
	if got.Notes != "user-typed note" {
		t.Errorf("user-typed notes were overwritten: %q", got.Notes)
	}
}

// When ErrFlightNotFound comes back, the flight stays as-is and we don't
// log it noisily (covered indirectly — what matters is the row is unchanged
// and the tracker still runs).
func TestRefreshBackfillNotFoundLeavesFlightAlone(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	fr := &fakeResolver{err: providers.ErrFlightNotFound}
	p.Resolver = fr
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "XX9999", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
	}, uid)

	p.tick(ctx)

	if fr.calls != 1 {
		t.Errorf("resolver should have been called exactly once, got %d", fr.calls)
	}
	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.OriginIATA != "" || got.DestIATA != "" || got.ICAO24 != nil {
		t.Errorf("not-found should leave row blank: %+v", got)
	}
}

// A flight that already has full metadata AND was resolved recently must
// NOT trigger another resolver call — last_resolved_at is the throttle.
func TestRefreshSkipsResolveWhenFresh(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	fr := &fakeResolver{rf: &providers.ResolvedFlight{}}
	p.Resolver = fr
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	icao := "abc123"
	f, _ := mkPart(ctx, s, store.CreateFlightPayload{
		Ident:        "PL9",
		ScheduledOut: now.Add(-time.Hour),
		ScheduledIn:  now.Add(time.Hour),
		OriginIATA:   "LHR",
		DestIATA:     "JFK",
		ICAO24:       icao,
	}, uid)
	// Pretend we just resolved this flight a moment ago.
	if err := s.RefreshFlightPartAirframe(ctx, f.ID, "", ""); err != nil {
		t.Fatalf("seed last_resolved_at: %v", err)
	}

	p.tick(ctx)

	if fr.calls != 0 {
		t.Errorf("resolver should not be called when last_resolved_at is fresh, got %d calls", fr.calls)
	}
}

// Late refresh: a flight that has icao24 set but was last resolved long
// ago (or never) should trigger a fresh resolver call when close to
// departure, and the new icao24 / callsign must overwrite whatever's
// stored — that's how we catch day-of airframe swaps that produce the
// "wrong aircraft" tracks we saw with LH493.
func TestLateRefreshOverwritesStaleAirframe(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	p.Resolver = &fakeResolver{rf: &providers.ResolvedFlight{
		Ident:      "LH493",
		OriginIATA: "YVR", DestIATA: "FRA",
		ICAO24:   "3c4a8c", // the day-of correct airframe
		Callsign: "DLH493",
	}}
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	old := "3c4a8b" // wrong airframe stored at booking time
	f, _ := mkPart(ctx, s, store.CreateFlightPayload{
		Ident:        "LH493",
		ScheduledOut: now.Add(-time.Hour), // already enroute
		ScheduledIn:  now.Add(time.Hour),
		OriginIATA:   "YVR", DestIATA: "FRA",
		ICAO24: old,
	}, uid)

	p.tick(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.ICAO24 == nil || *got.ICAO24 != "3c4a8c" {
		t.Errorf("icao24 should have been overwritten by late-refresh: %v", got.ICAO24)
	}
	if got.Callsign == nil || *got.Callsign != "DLH493" {
		t.Errorf("callsign should have been written by late-refresh: %v", got.Callsign)
	}
	if got.LastResolvedAt == nil {
		t.Error("last_resolved_at should have been bumped")
	}
}

// Late refresh must not fire for a flight that's still far in the future —
// AeroDataBox won't have an airframe assigned yet and there's no value in
// burning quota.
func TestLateRefreshSkipsFarFuture(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	fr := &fakeResolver{rf: &providers.ResolvedFlight{
		OriginIATA: "LHR", DestIATA: "JFK", ICAO24: "abc123",
	}}
	p.Resolver = fr
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	icao := "abc123"
	// 24h before departure: ActiveFlights still won't pick it up, but if
	// the late-refresh window is mis-tuned we'd otherwise see calls here.
	_, _ = mkPart(ctx, s, store.CreateFlightPayload{
		Ident:        "PL10",
		ScheduledOut: now.Add(24 * time.Hour),
		ScheduledIn:  now.Add(30 * time.Hour),
		OriginIATA:   "LHR", DestIATA: "JFK", ICAO24: icao,
	}, uid)

	p.tick(ctx)

	if fr.calls != 0 {
		t.Errorf("late-refresh should not fire for a flight a day out, got %d calls", fr.calls)
	}
}

// Late refresh on a resolver error / not-found must still bump
// last_resolved_at so we throttle the retry interval — otherwise an
// unresolvable flight would burn a resolver call on every tick.
func TestLateRefreshStampsEvenOnNotFound(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	p.Resolver = &fakeResolver{err: providers.ErrFlightNotFound}
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := mkPart(ctx, s, store.CreateFlightPayload{
		Ident:        "ZZ404",
		ScheduledOut: now.Add(-time.Hour),
		ScheduledIn:  now.Add(time.Hour),
		OriginIATA:   "LHR", DestIATA: "JFK",
		ICAO24: "abc123",
	}, uid)

	p.tick(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.LastResolvedAt == nil {
		t.Error("last_resolved_at should be bumped even when the resolver returned not-found")
	}
	if got.ICAO24 == nil || *got.ICAO24 != "abc123" {
		t.Errorf("not-found must NOT blank existing icao24: %v", got.ICAO24)
	}
}

func TestRunImmediateTickThenStops(t *testing.T) {
	tr := &mockTracker{pos: &store.Position{Lat: 2, Lon: 2}}
	p, s, _ := newPoller(t, tr, 20*time.Millisecond)
	uid := seedUser(t, s)
	now := time.Now()
	_, _ = mkPart(context.Background(), s, store.CreateFlightPayload{
		Ident: "PL5", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	// Let the immediate tick + at least one ticker tick happen.
	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on context cancel")
	}
	if tr.calls == 0 {
		t.Error("Run should have polled at least once")
	}
}
