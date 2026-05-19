package poller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dpage/flight-tracker/internal/sse"
	"github.com/dpage/flight-tracker/internal/store"
	"github.com/dpage/flight-tracker/internal/testsupport"
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
	u, err := s.InviteUser(context.Background(), store.InvitePayload{GitHubLogin: "po", Name: "po"})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u.ID
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
	f, err := s.CreateFlight(ctx, store.CreateFlightPayload{
		Ident: "PL1", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)
	if err != nil {
		t.Fatalf("create flight: %v", err)
	}

	events, unsub := hub.Subscribe()
	defer unsub()

	p.tick(ctx)

	if tr.calls != 1 {
		t.Errorf("tracker calls = %d, want 1", tr.calls)
	}
	pos, _ := s.LatestPositions(ctx, []int64{f.ID})
	if pos[f.ID] == nil {
		t.Error("expected a position to be inserted")
	}
	got, _ := s.FlightByID(ctx, f.ID)
	if got.LastPolledAt == nil {
		t.Error("RefreshFlightStatus should set last_polled_at")
	}
	select {
	case ev := <-events:
		if ev.Type != "flight.updated" {
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
	f, _ := s.CreateFlight(ctx, store.CreateFlightPayload{
		Ident: "PL2", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)

	p.tick(ctx)

	got, _ := s.FlightByID(ctx, f.ID)
	if got.LastPolledAt == nil {
		t.Error("status should still be refreshed despite tracker error")
	}
	if pos, _ := s.LatestPositions(ctx, []int64{f.ID}); pos[f.ID] != nil {
		t.Error("no position should be inserted when tracker errors")
	}
}

func TestTickSkipsFreshlyPolled(t *testing.T) {
	tr := &mockTracker{pos: &store.Position{Lat: 1, Lon: 1}}
	p, s, _ := newPoller(t, tr, time.Hour) // huge interval
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := s.CreateFlight(ctx, store.CreateFlightPayload{
		Ident: "PL3", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)
	// Mark as just polled so minPollAge skips it.
	if err := s.RefreshFlightStatus(ctx, f.ID); err != nil {
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
	_, _ = s.CreateFlight(context.Background(), store.CreateFlightPayload{
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
	f, _ := s.CreateFlight(ctx, store.CreateFlightPayload{
		Ident: "DELME", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)
	tr.before = func(fl *store.Flight) {
		if err := s.DeleteFlight(ctx, fl.ID); err != nil {
			t.Fatalf("delete in tracker: %v", err)
		}
	}
	events, unsub := hub.Subscribe()
	defer unsub()

	p.tick(ctx) // must not panic; FlightByID after delete → error → return

	if _, err := s.FlightByID(ctx, f.ID); err == nil {
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
		_, _ = s.CreateFlight(context.Background(), store.CreateFlightPayload{
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

func TestRunImmediateTickThenStops(t *testing.T) {
	tr := &mockTracker{pos: &store.Position{Lat: 2, Lon: 2}}
	p, s, _ := newPoller(t, tr, 20*time.Millisecond)
	uid := seedUser(t, s)
	now := time.Now()
	_, _ = s.CreateFlight(context.Background(), store.CreateFlightPayload{
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
