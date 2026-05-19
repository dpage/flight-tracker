package providers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dpage/flight-tracker/internal/store"
)

type fakeTracker struct {
	pos *store.Position
	err error
}

func (f fakeTracker) Track(context.Context, *store.Flight, time.Time) (*store.Position, error) {
	return f.pos, f.err
}

type fakeAnchor struct {
	pos *store.Position
	err error
}

func (f fakeAnchor) LatestRealPosition(context.Context, int64) (*store.Position, error) {
	return f.pos, f.err
}

func TestNewDeadReckonerDefaults(t *testing.T) {
	d := NewDeadReckoner(fakeTracker{}, fakeAnchor{})
	if d.FreshThreshold != 5*time.Minute {
		t.Errorf("default FreshThreshold = %v", d.FreshThreshold)
	}
	if d.Inner == nil || d.Anchor == nil {
		t.Error("Inner/Anchor not wired")
	}
}

func TestDeadReckonInnerHasFix(t *testing.T) {
	want := &store.Position{Lat: 1, Lon: 2}
	d := NewDeadReckoner(fakeTracker{pos: want}, fakeAnchor{})
	got, err := d.Track(context.Background(), baseFlight(), time.Now())
	if err != nil || got != want {
		t.Errorf("should pass through inner fix, got %v %v", got, err)
	}
}

func TestDeadReckonInnerErrorFallsThrough(t *testing.T) {
	// Inner errors; no anchor → nil, nil (error swallowed).
	d := &DeadReckoner{Inner: fakeTracker{err: errors.New("boom")}, Anchor: nil}
	got, err := d.Track(context.Background(), baseFlight(), time.Now())
	if got != nil || err != nil {
		t.Errorf("expected (nil,nil), got %v %v", got, err)
	}
}

func TestDeadReckonNoAnchorConfigured(t *testing.T) {
	d := &DeadReckoner{Inner: fakeTracker{}}
	if p, _ := d.Track(context.Background(), baseFlight(), time.Now()); p != nil {
		t.Error("expected nil when Anchor is nil")
	}
}

func TestDeadReckonAnchorErrorOrNil(t *testing.T) {
	d := NewDeadReckoner(fakeTracker{}, fakeAnchor{err: errors.New("db")})
	if p, _ := d.Track(context.Background(), baseFlight(), time.Now()); p != nil {
		t.Error("anchor error → nil")
	}
	d = NewDeadReckoner(fakeTracker{}, fakeAnchor{pos: nil})
	if p, _ := d.Track(context.Background(), baseFlight(), time.Now()); p != nil {
		t.Error("nil anchor → nil")
	}
}

func TestDeadReckonNoDestGeometry(t *testing.T) {
	f := baseFlight()
	f.DestLat = nil
	anchor := &store.Position{Ts: time.Now().Add(-time.Hour), Lat: 50, Lon: -10}
	d := NewDeadReckoner(fakeTracker{}, fakeAnchor{pos: anchor})
	if p, _ := d.Track(context.Background(), f, time.Now()); p != nil {
		t.Error("missing dest geometry → nil")
	}
}

func TestDeadReckonAnchorInFuture(t *testing.T) {
	f := baseFlight()
	now := time.Now()
	anchor := &store.Position{Ts: now.Add(time.Hour), Lat: 50, Lon: -10}
	d := NewDeadReckoner(fakeTracker{}, fakeAnchor{pos: anchor})
	if p, _ := d.Track(context.Background(), f, now); p != nil {
		t.Error("anchor in the future → nil")
	}
}

func TestDeadReckonExtrapolates(t *testing.T) {
	f := baseFlight()
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	f.ScheduledIn = now.Add(2 * time.Hour)
	alt := int32(35000)
	gs := int32(450)
	anchor := &store.Position{
		Ts: now.Add(-1 * time.Hour), Lat: 50, Lon: -20,
		AltitudeFt: &alt, GroundspeedKt: &gs,
	}
	d := NewDeadReckoner(fakeTracker{}, fakeAnchor{pos: anchor})
	p, err := d.Track(context.Background(), f, now)
	if err != nil || p == nil {
		t.Fatalf("expected extrapolated position, got %v %v", p, err)
	}
	if !p.IsEstimated {
		t.Error("dead-reckoned position must be flagged estimated")
	}
	if p.HeadingDeg == nil {
		t.Error("expected a heading")
	}
	if p.AltitudeFt != anchor.AltitudeFt || p.GroundspeedKt != anchor.GroundspeedKt {
		t.Error("alt/gs should be carried from the anchor")
	}
}

func TestDeadReckonClampsFractionPastArrival(t *testing.T) {
	f := baseFlight()
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	// totalRemaining <= 0: scheduled arrival is before the anchor.
	f.ScheduledIn = now.Add(-3 * time.Hour)
	anchor := &store.Position{Ts: now.Add(-2 * time.Hour), Lat: 50, Lon: -20}
	d := NewDeadReckoner(fakeTracker{}, fakeAnchor{pos: anchor})
	p, _ := d.Track(context.Background(), f, now)
	if p == nil {
		t.Fatal("expected a clamped position at destination")
	}
	// frac clamped to 1 → at destination.
	if abs(p.Lat-*f.DestLat) > 0.5 || abs(p.Lon-*f.DestLon) > 0.5 {
		t.Errorf("expected position at destination, got (%v,%v)", p.Lat, p.Lon)
	}
}

// Step-3 fallback: no inner fix AND no real anchor in the DB, but the flight
// has origin / destination / schedule and is currently airborne. The
// DeadReckoner should synthesise a schedule-interpolated position (same
// math the Stub uses) so the map isn't blank for flights that never get
// any real ADS-B contact.
func TestDeadReckonScheduleFallbackWhenNoAnchorEver(t *testing.T) {
	f := baseFlight() // out=10:00 UTC, in=14:00 UTC, LHR → JFK
	// Halfway through.
	now := f.ScheduledOut.Add(2 * time.Hour)
	d := NewDeadReckoner(fakeTracker{}, fakeAnchor{}) // anchor returns nil, nil
	p, err := d.Track(context.Background(), f, now)
	if err != nil {
		t.Fatalf("Track: %v", err)
	}
	if p == nil {
		t.Fatal("expected a synthesised position, got nil")
	}
	if !p.IsEstimated {
		t.Error("schedule fallback must flag is_estimated=true")
	}
	// At t=halfway, expect a position mid-Atlantic. The great-circle arc
	// from LHR to JFK bends north over the ocean (passes over southern
	// Greenland-ish), so latitude at the halfway point sits ABOVE both
	// endpoints (typically ~52°N) rather than between them.
	if p.Lat < 40 || p.Lat > 60 || p.Lon > -5 || p.Lon < -73 {
		t.Errorf("midpoint should be over the Atlantic, got (%v, %v)", p.Lat, p.Lon)
	}
	if p.HeadingDeg == nil {
		t.Error("heading should be populated from bearing(now → dest)")
	}
}

// And the boundary: a flight outside its air-time window with no anchor
// and no inner fix returns nil — we don't fabricate positions for flights
// that haven't departed or have already landed.
func TestDeadReckonScheduleFallbackOutsideWindow(t *testing.T) {
	f := baseFlight()
	d := NewDeadReckoner(fakeTracker{}, fakeAnchor{})
	pre := f.ScheduledOut.Add(-time.Hour)
	if p, _ := d.Track(context.Background(), f, pre); p != nil {
		t.Errorf("pre-departure should be nil, got %+v", p)
	}
	post := f.ScheduledIn.Add(time.Hour)
	if p, _ := d.Track(context.Background(), f, post); p != nil {
		t.Errorf("post-arrival should be nil, got %+v", p)
	}
}

func TestDeadReckonElapsedExceedsRemaining(t *testing.T) {
	f := baseFlight()
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	// totalRemaining > 0 (arrival after anchor) but elapsed >= totalRemaining
	// because "now" is already past the scheduled arrival.
	anchor := &store.Position{Ts: now.Add(-2 * time.Hour), Lat: 50, Lon: -20}
	f.ScheduledIn = now.Add(-30 * time.Minute)
	d := NewDeadReckoner(fakeTracker{}, fakeAnchor{pos: anchor})
	p, _ := d.Track(context.Background(), f, now)
	if p == nil {
		t.Fatal("expected a position")
	}
	if abs(p.Lat-*f.DestLat) > 0.5 {
		t.Errorf("elapsed>=remaining should clamp to destination, got lat %v", p.Lat)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
