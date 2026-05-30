package planops

import (
	"testing"
	"time"
)

// TestSelectTrip_AttachesToOverlappingTrip: a plan whose dates fall inside an
// existing trip's effective span attaches to that trip.
func TestSelectTrip_AttachesToOverlappingTrip(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	// Trip's effective span comes from a flight 2026-06-01..06-08.
	tOut := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	tIn := time.Date(2026, 6, 8, 17, 0, 0, 0, time.UTC)
	e.mkFlightPlan(t, trip, owner, "BA286", "PNR1", tOut, tIn)

	// A dinner mid-trip.
	planStart := time.Date(2026, 6, 4, 19, 0, 0, 0, time.UTC)
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planStart)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if !ok || id != trip {
		t.Errorf("SelectTrip = (%d, %v), want (%d, true)", id, ok, trip)
	}
}

// TestSelectTrip_AttachesAdjacent: a plan one day after a trip ends is within
// the adjacency tolerance and still attaches.
func TestSelectTrip_AttachesAdjacent(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	tOut := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	tIn := time.Date(2026, 6, 8, 17, 0, 0, 0, time.UTC)
	e.mkFlightPlan(t, trip, owner, "BA286", "PNR1", tOut, tIn)

	// Dinner the evening the trip ends + a few hours later (< 1 day gap).
	planStart := time.Date(2026, 6, 8, 20, 0, 0, 0, time.UTC)
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planStart)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if !ok || id != trip {
		t.Errorf("SelectTrip = (%d, %v), want (%d, true)", id, ok, trip)
	}
}

// TestSelectTrip_NoMatchCreatesNew: a plan far from any trip's span does not
// attach (the caller then creates a new trip).
func TestSelectTrip_NoMatchCreatesNew(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	tOut := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	tIn := time.Date(2026, 6, 8, 17, 0, 0, 0, time.UTC)
	e.mkFlightPlan(t, trip, owner, "BA286", "PNR1", tOut, tIn)

	// A plan two months away.
	planStart := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planStart)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if ok {
		t.Errorf("SelectTrip = (%d, true), want no match", id)
	}
}

// TestSelectTrip_PrefersGreatestOverlap: with two candidate trips, the one with
// the larger overlap wins.
func TestSelectTrip_PrefersGreatestOverlap(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)

	// Trip A: 06-01..06-03 (barely overlaps the plan's start).
	tripA := e.mkTrip(t, owner)
	e.mkFlightPlan(t, tripA, owner, "AA1", "PA",
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC))

	// Trip B: 06-02..06-10 (overlaps the plan span fully).
	tripB := e.mkTrip(t, owner)
	e.mkFlightPlan(t, tripB, owner, "BB1", "PB",
		time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC))

	planStart := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	planEnd := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planEnd)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if !ok || id != tripB {
		t.Errorf("SelectTrip = (%d, %v), want trip B (%d)", id, ok, tripB)
	}
}

// TestSelectTrip_DatelessTripNeverMatches: a trip with no plan_parts and no
// starts_on/ends_on is not a candidate.
func TestSelectTrip_DatelessTripNeverMatches(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	e.mkTrip(t, owner) // date-less

	planStart := time.Date(2026, 6, 4, 19, 0, 0, 0, time.UTC)
	_, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planStart)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if ok {
		t.Error("date-less trip should not match")
	}
}
