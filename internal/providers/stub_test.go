package providers

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/flight-tracker/internal/store"
)

func ptrF(v float64) *float64 { return &v }

func baseFlight() *store.Flight {
	out := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	return &store.Flight{
		ID:           1,
		ScheduledOut: out,
		ScheduledIn:  out.Add(4 * time.Hour),
		OriginLat:    ptrF(51.4775), OriginLon: ptrF(-0.4614),
		DestLat: ptrF(40.6413), DestLon: ptrF(-73.7781),
	}
}

func TestStubBeforeDeparture(t *testing.T) {
	f := baseFlight()
	p, err := NewStub().Track(context.Background(), f, f.ScheduledOut.Add(-time.Minute))
	if err != nil || p != nil {
		t.Errorf("expected (nil,nil) before departure, got %v %v", p, err)
	}
}

func TestStubAfterArrival(t *testing.T) {
	f := baseFlight()
	p, _ := Stub{}.Track(context.Background(), f, f.ScheduledIn.Add(time.Minute))
	if p != nil {
		t.Error("expected nil after arrival")
	}
}

func TestStubMissingGeometry(t *testing.T) {
	f := baseFlight()
	f.DestLat = nil
	mid := f.ScheduledOut.Add(2 * time.Hour)
	if p, _ := (Stub{}).Track(context.Background(), f, mid); p != nil {
		t.Error("expected nil with missing dest coords")
	}
}

func TestStubZeroDuration(t *testing.T) {
	f := baseFlight()
	f.ScheduledIn = f.ScheduledOut // total == 0
	if p, _ := (Stub{}).Track(context.Background(), f, f.ScheduledOut); p != nil {
		t.Error("expected nil for zero-duration flight")
	}
}

func TestStubMidFlight(t *testing.T) {
	f := baseFlight()
	mid := f.ScheduledOut.Add(2 * time.Hour) // halfway
	p, err := (Stub{}).Track(context.Background(), f, mid)
	if err != nil || p == nil {
		t.Fatalf("expected a position, got %v %v", p, err)
	}
	if p.IsEstimated {
		t.Error("stub positions must not be flagged estimated")
	}
	if p.AltitudeFt == nil || p.GroundspeedKt == nil || p.HeadingDeg == nil {
		t.Error("expected alt/gs/heading to be populated")
	}
	if *p.AltitudeFt < 30000 {
		t.Errorf("mid-flight should be at cruise, got %d ft", *p.AltitudeFt)
	}
	// Position should be somewhere between origin and destination.
	if p.Lat < 40 || p.Lat > 55 {
		t.Errorf("lat %v not on the LHR→JFK arc", p.Lat)
	}
}

func TestCruiseAltitudeProfile(t *testing.T) {
	climb := cruiseAltitude(0.05)  // < 0.10 → climbing
	cruise := cruiseAltitude(0.50) // mid → full cruise
	descent := cruiseAltitude(0.95)
	if !(climb < cruise) {
		t.Errorf("climb (%v) should be below cruise (%v)", climb, cruise)
	}
	if !(descent < cruise) {
		t.Errorf("descent (%v) should be below cruise (%v)", descent, cruise)
	}
	if cruise != 36000.0 {
		t.Errorf("cruise = %v, want 36000", cruise)
	}
}

func TestStubEarlyAndLateFractions(t *testing.T) {
	f := baseFlight()
	// Just after departure: climbing.
	p, _ := (Stub{}).Track(context.Background(), f, f.ScheduledOut.Add(time.Minute))
	if p == nil || *p.AltitudeFt >= 36000 {
		t.Errorf("just after departure should be climbing, got %+v", p)
	}
	// Just before arrival: descending.
	p, _ = (Stub{}).Track(context.Background(), f, f.ScheduledIn.Add(-time.Minute))
	if p == nil || *p.AltitudeFt >= 36000 {
		t.Errorf("just before arrival should be descending, got %+v", p)
	}
}
