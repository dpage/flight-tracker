package poller

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

func TestSweep_TableFillsRow(t *testing.T) {
	p, s, hub := newPoller(t, &mockTracker{}, time.Minute)
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()

	// Seed a flight whose origin IATA (BRS) is in the embedded airports
	// table but whose dest IATA (ZZZ) is not — origin coords get filled
	// at create time, dest coords stay NULL.
	f, err := s.CreateFlight(ctx, store.CreateFlightPayload{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Simulate the "deploy added SID to the airports table" case by
	// switching dest_iata to "SID" and clearing the coords directly
	// in SQL — the public UpdateFlight would re-run lookupCoords and
	// fill them immediately, defeating the test.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE flights SET dest_iata = 'SID', dest_lat = NULL, dest_lon = NULL WHERE id = $1`,
		f.ID); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Subscribe to SSE so we can assert the sweep publishes an update.
	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: uid, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.Sweep(ctx)

	got, err := s.FlightByID(ctx, f.ID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	if got.DestLat == nil || *got.DestLat == 0 {
		t.Fatalf("dest_lat should be table-filled (SID = 16.7414), got %v", got.DestLat)
	}
	if *got.DestLat != 16.7414 {
		t.Errorf("dest_lat = %v, want 16.7414 (SID table value)", *got.DestLat)
	}

	select {
	case <-events:
		// good — SSE published.
	case <-time.After(500 * time.Millisecond):
		t.Errorf("expected SSE flight.updated event after sweep, got none")
	}
}

func TestSweep_NoNullRowsIsNoOp(t *testing.T) {
	p, s, hub := newPoller(t, &mockTracker{}, time.Minute)
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()

	// Single flight, both IATAs known → all four coord columns populated
	// at create time. Sweep should find zero candidates.
	if _, err := s.CreateFlight(ctx, store.CreateFlightPayload{
		Ident: "LH400", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "FRA", DestIATA: "JFK",
	}, uid); err != nil {
		t.Fatalf("create: %v", err)
	}

	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: uid, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.Sweep(ctx)

	select {
	case e := <-events:
		t.Errorf("no-op sweep should not publish; got %s", e.Type)
	case <-time.After(100 * time.Millisecond):
		// good — no event.
	}
}

