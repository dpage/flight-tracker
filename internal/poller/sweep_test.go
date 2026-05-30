package poller

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/providers"
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
	f, err := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Simulate the "deploy added SID to the airports table" case by
	// switching dest_iata (flight_details) to "SID" and clearing the part's
	// end coords directly in SQL. The dest IATA lives on flight_details; the
	// coords live on the plan_part (keyed by f.ID, the plan_part_id).
	if _, err := s.Pool().Exec(ctx,
		`UPDATE flight_details SET dest_iata = 'SID' WHERE plan_part_id = $1`, f.ID); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := s.Pool().Exec(ctx,
		`UPDATE plan_parts SET end_lat = NULL, end_lon = NULL WHERE id = $1`, f.ID); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Subscribe to SSE so we can assert the sweep publishes an update.
	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: uid, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.Sweep(ctx)

	got, err := s.FlightPartByID(ctx, f.ID)
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
	if _, err := mkPart(ctx, s, store.CreateFlightPayload{
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

func TestSweep_ResolverFillsUnknownIATA(t *testing.T) {
	p, s, hub := newPoller(t, &mockTracker{}, time.Minute)
	// Resolver returns SID coords; sweep should pick them up.
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{
		Ident:      "EZY2823",
		OriginIATA: "BRS", OriginLat: 51.3827, OriginLon: -2.7191,
		DestIATA: "ZZZ", DestLat: 12.3456, DestLon: -34.5678,
	}}
	p.Resolver = resolver
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ", // dest not in table
	}, uid)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: uid, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.Sweep(ctx)

	got, err := s.FlightPartByID(ctx, f.ID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	if resolver.calls != 1 {
		t.Errorf("resolver.calls = %d, want 1", resolver.calls)
	}
	if got.DestLat == nil || *got.DestLat != 12.3456 {
		t.Errorf("dest_lat = %v, want 12.3456 (resolver-supplied)", got.DestLat)
	}
	if got.LastResolvedAt == nil {
		t.Errorf("last_resolved_at should be bumped after resolver call")
	}
	select {
	case <-events:
		// good
	case <-time.After(500 * time.Millisecond):
		t.Errorf("expected SSE event")
	}
}

func TestSweep_ResolverNotFoundLeavesNull(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	resolver := &fakeResolver{err: providers.ErrFlightNotFound}
	p.Resolver = resolver
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "XX9999", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)

	p.Sweep(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.DestLat != nil {
		t.Errorf("dest_lat should remain NULL on resolver-not-found; got %v", got.DestLat)
	}
	if resolver.calls != 1 {
		t.Errorf("resolver.calls = %d, want 1", resolver.calls)
	}
	if got.LastResolvedAt == nil {
		t.Errorf("last_resolved_at should be bumped even on not-found")
	}
}

func TestSweep_ThrottleHoldsRecentRow(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{
		Ident: "EZY2823", OriginIATA: "BRS", DestIATA: "ZZZ",
		DestLat: 12.3456, DestLon: -34.5678,
	}}
	p.Resolver = resolver
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)

	// Stamp last_resolved_at to "right now" so the throttle blocks the
	// resolver call on the next sweep. RefreshFlightAirframe with empty
	// strings bumps the timestamp without touching airframe columns.
	if err := s.RefreshFlightPartAirframe(ctx, f.ID, "", ""); err != nil {
		t.Fatalf("stamp: %v", err)
	}

	p.Sweep(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if resolver.calls != 0 {
		t.Errorf("resolver should not have been called (throttled); calls = %d", resolver.calls)
	}
	if got.DestLat != nil {
		t.Errorf("dest_lat should remain NULL (resolver throttled); got %v", got.DestLat)
	}
}

func TestSweep_NoResolverConfiguredTableOnly(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	p.Resolver = nil // explicit
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)

	// Should not panic even with no resolver. Dest remains NULL (table
	// doesn't know ZZZ, resolver path skipped).
	p.Sweep(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.DestLat != nil {
		t.Errorf("dest_lat should remain NULL with no resolver and unknown IATA; got %v", got.DestLat)
	}
}

func TestSweep_MixedBatchPerRowIsolation(t *testing.T) {
	p, s, hub := newPoller(t, &mockTracker{}, time.Minute)
	// Resolver returns coords ONLY for ident "RESOLVE-ME"; everything
	// else gets ErrFlightNotFound. This lets one row depend on the
	// table, one on the resolver, and one on neither.
	resolver := &resolveByIdent{
		match: "RESOLVE-ME",
		rf: &providers.ResolvedFlight{
			Ident: "RESOLVE-ME", OriginIATA: "BRS", OriginLat: 51.3827, OriginLon: -2.7191,
			DestIATA: "ZZZ", DestLat: 12.3456, DestLon: -34.5678,
		},
	}
	p.Resolver = resolver
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()

	// (a) Table-fillable: BRS → SID (both in table); seeded with
	// dest_lat NULL via direct SQL to simulate the "deploy added SID"
	// case.
	a, _ := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "TABLE-ME", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "SID",
	}, uid)
	if _, err := s.Pool().Exec(ctx,
		`UPDATE plan_parts SET end_lat = NULL, end_lon = NULL WHERE id = $1`, a.ID); err != nil {
		t.Fatalf("setup a: %v", err)
	}

	// (b) Resolver-fillable: ident matches the fake resolver's match.
	b, _ := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "RESOLVE-ME", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)

	// (c) Unfillable: ident the resolver returns ErrFlightNotFound for,
	// dest IATA not in the table.
	c, _ := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "UNFILL-ME", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "QQQ",
	}, uid)

	_, unsub := hub.Subscribe(sse.Subscription{ViewerID: uid, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.Sweep(ctx)

	gotA, _ := s.FlightPartByID(ctx, a.ID)
	if gotA.DestLat == nil || *gotA.DestLat != 16.7414 {
		t.Errorf("table-fillable row: dest_lat = %v, want 16.7414", gotA.DestLat)
	}
	gotB, _ := s.FlightPartByID(ctx, b.ID)
	if gotB.DestLat == nil || *gotB.DestLat != 12.3456 {
		t.Errorf("resolver-fillable row: dest_lat = %v, want 12.3456", gotB.DestLat)
	}
	gotC, _ := s.FlightPartByID(ctx, c.ID)
	if gotC.DestLat != nil {
		t.Errorf("unfillable row: dest_lat = %v, want nil", gotC.DestLat)
	}
}

func TestSweep_PartiallyUnknownPreservesTableFilledLeg(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	// Resolver returns DELIBERATELY-WRONG origin coords (99.0) — if the
	// merge clobbered the table-derived BRS value (51.3827), we'd see
	// 99.0 in the result. The fix must skip overwriting the leg that
	// the table already satisfied.
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{
		Ident:      "EZY2823",
		OriginIATA: "BRS", OriginLat: 99.0, OriginLon: 99.0,
		DestIATA: "ZZZ", DestLat: 12.3456, DestLon: -34.5678,
	}}
	p.Resolver = resolver
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	// Seed with origin=BRS (in table) and dest=ZZZ (not in table). The
	// create-time helper fills origin coords, dest stays NULL.
	f, _ := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)
	// Wipe origin coords too so the sweep's table pass has to refill
	// them — this exercises the "table fills one leg, resolver fills
	// the other" code path.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE plan_parts SET start_lat = NULL, start_lon = NULL WHERE id = $1`, f.ID); err != nil {
		t.Fatalf("setup: %v", err)
	}

	p.Sweep(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.OriginLat == nil {
		t.Fatalf("origin_lat should be table-filled (51.3827), got nil")
	}
	if *got.OriginLat != 51.3827 {
		t.Errorf("origin_lat = %v, want 51.3827 (BRS table value, NOT resolver's 99.0)", *got.OriginLat)
	}
	if got.DestLat == nil || *got.DestLat != 12.3456 {
		t.Errorf("dest_lat = %v, want 12.3456 (resolver-supplied)", got.DestLat)
	}
}

// resolveByIdent is a Resolver double that only returns success for one
// specific ident. Used by the mixed-batch test.
type resolveByIdent struct {
	match string
	rf    *providers.ResolvedFlight
	calls int
}

func (r *resolveByIdent) Resolve(_ context.Context, ident string, _ time.Time) (*providers.ResolvedFlight, error) {
	r.calls++
	if ident == r.match {
		c := *r.rf
		return &c, nil
	}
	return nil, providers.ErrFlightNotFound
}
