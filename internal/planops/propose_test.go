package planops

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
)

var ctx = context.Background()

var userSeq atomic.Int64

// env bundles a store with the pool behind it so tests can insert users (which
// needs the raw pool) and call the store API.
type env struct {
	s    *store.Store
	pool *pgxpool.Pool
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testsupport.NewPool(t)
	return env{s: store.New(pool), pool: pool}
}

func (e env) mkUser(t *testing.T) int64 {
	t.Helper()
	return testsupport.InsertUser(t, e.pool, fmt.Sprintf("user%d", userSeq.Add(1)), false, true)
}

// fakeExtractor returns canned plans, ignoring its inputs.
type fakeExtractor struct {
	plans []ExtractedPlan
	err   error
}

func (f *fakeExtractor) ExtractPlans(_ context.Context, _ string, _ []Document) ([]ExtractedPlan, error) {
	return f.plans, f.err
}

// mkTrip creates a trip owned by userID via the store and returns its id.
func (e env) mkTrip(t *testing.T, userID int64) int64 {
	t.Helper()
	tr, err := e.s.CreateTrip(ctx, store.CreateTripPayload{Name: "Trip"}, userID)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}
	return tr.ID
}

// mkFlightPlan inserts a flight plan with one part into the trip, owned by
// userID and with userID as passenger. Returns the plan id and the part id.
func (e env) mkFlightPlan(t *testing.T, tripID, userID int64, ident, ref string, out, in time.Time) (int64, int64) {
	t.Helper()
	s := e.s
	plan, err := s.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: tripID, Type: "flight", Title: ident, ConfirmationRef: ref,
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: out, EndsAt: &in, StartLabel: "LHR", EndLabel: "JFK",
			Flight: &store.FlightDetail{
				Ident: ident, ScheduledOut: out, ScheduledIn: in,
				OriginIATA: "LHR", DestIATA: "JFK",
			},
		}},
	}, userID)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := s.AddPlanPassenger(ctx, plan.ID, userID); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}
	parts, err := s.PartsByPlan(ctx, plan.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan = %d, %v", len(parts), err)
	}
	return plan.ID, parts[0].ID
}

// TestPropose_RebookingMatchByPNR proposes a flight that shares the trip's
// existing PNR; the proposal must carry a supersession pointing at the old
// part.
func TestPropose_RebookingMatchByPNR(t *testing.T) {
	e := newEnv(t)
	s := e.s
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	_, oldPart := e.mkFlightPlan(t, trip, owner, "BA286", "PNR123", out, in)

	// Incoming: same PNR, a day later (a rebooking).
	fx := &fakeExtractor{plans: []ExtractedPlan{{
		Type: "flight", Title: "BA286 (rebooked)", ConfirmationRef: "pnr123",
		Parts: []ExtractedPart{{
			Type: "flight", Confidence: "high",
			Flight: FlightFields{
				Ident: "BA286", Date: "2026-06-02",
				OriginIATA: "LHR", DestIATA: "JFK",
				DepartTimeLocal: "09:00", ArriveDate: "2026-06-02", ArriveTimeLocal: "17:00",
			},
		}},
	}}}
	deps := Deps{Store: s, Extractor: fx}
	props, err := Propose(ctx, deps, owner, trip, "body", nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("len(props) = %d, want 1", len(props))
	}
	if props[0].SupersedesPartID == nil {
		t.Fatalf("expected a proposed supersession by PNR")
	}
	if *props[0].SupersedesPartID != oldPart {
		t.Errorf("supersedes = %d, want old part %d", *props[0].SupersedesPartID, oldPart)
	}
}

// TestPropose_RebookingMatchesRightTraveller checks that the match prefers the
// proposing user's own visible flight over a trip-mate's flight on the same
// route/day (PRD §6.9 "by traveller and route").
func TestPropose_RebookingMatchesRightTraveller(t *testing.T) {
	e := newEnv(t)
	s := e.s
	alice := e.mkUser(t)
	bob := e.mkUser(t)
	trip := e.mkTrip(t, alice)
	if err := s.AddTripMember(ctx, trip, bob, "editor"); err != nil {
		t.Fatalf("AddTripMember: %v", err)
	}
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	// Both Alice and Bob have BA286 on the same day, no shared PNR.
	_, alicePart := e.mkFlightPlan(t, trip, alice, "BA286", "ALICEPNR", out, in)
	e.mkFlightPlan(t, trip, bob, "BA286", "BOBPNR", out, in)

	// Bob's plan is hidden from Alice so it's not in her visible candidate set;
	// the match must land on Alice's part.
	bobPlans, _ := s.PlansByTrip(ctx, trip)
	for _, pl := range bobPlans {
		if pl.ConfirmationRef == "BOBPNR" {
			if err := s.SetPlanVisibility(ctx, pl.ID, "only_visible_to", []int64{bob}); err != nil {
				t.Fatalf("SetPlanVisibility: %v", err)
			}
		}
	}

	fx := &fakeExtractor{plans: []ExtractedPlan{{
		Type: "flight", Title: "BA286 rebooked",
		Parts: []ExtractedPart{{
			Type: "flight", Confidence: "high",
			Flight: FlightFields{
				Ident: "BA286", Date: "2026-06-01",
				OriginIATA: "LHR", DestIATA: "JFK",
				DepartTimeLocal: "11:00", ArriveDate: "2026-06-01", ArriveTimeLocal: "19:00",
			},
		}},
	}}}
	deps := Deps{Store: s, Extractor: fx}
	props, err := Propose(ctx, deps, alice, trip, "body", nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(props) != 1 || props[0].SupersedesPartID == nil {
		t.Fatalf("expected one proposal with a supersession, got %+v", props)
	}
	if *props[0].SupersedesPartID != alicePart {
		t.Errorf("matched part %d, want Alice's %d (not Bob's)", *props[0].SupersedesPartID, alicePart)
	}
}

// TestCommit_SupersessionCancelsOldPart verifies that committing a proposal
// with a supersession inserts the new part with supersedes_id set and stamps
// the OLD part status='cancelled' (the signal the FE greys on).
func TestCommit_SupersessionCancelsOldPart(t *testing.T) {
	e := newEnv(t)
	s := e.s
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	_, oldPart := e.mkFlightPlan(t, trip, owner, "BA286", "PNR123", out, in)

	newOut := out.AddDate(0, 0, 1)
	newIn := in.AddDate(0, 0, 1)
	plans := []ConfirmPlanInput{{
		Type: "flight", Title: "BA286 (rebooked)", ConfirmationRef: "PNR123",
		SupersedesPartID: &oldPart,
		Parts: []ConfirmPartInput{{
			Type: "flight", StartsAt: newOut, EndsAt: &newIn,
			Flight: &store.FlightDetail{
				Ident: "BA286", ScheduledOut: newOut, ScheduledIn: newIn,
				OriginIATA: "LHR", DestIATA: "JFK",
			},
		}},
	}}
	created, err := Commit(ctx, Deps{Store: s}, trip, owner, plans)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("len(created) = %d, want 1", len(created))
	}
	// Old part must be cancelled.
	op, err := s.PlanPartByID(ctx, oldPart)
	if err != nil {
		t.Fatalf("PlanPartByID(old): %v", err)
	}
	if op.Status != "cancelled" {
		t.Errorf("old part status = %q, want cancelled", op.Status)
	}
	// New part must link to the old via supersedes_id.
	newParts, err := s.PartsByPlan(ctx, created[0].ID)
	if err != nil || len(newParts) != 1 {
		t.Fatalf("PartsByPlan(new) = %d, %v", len(newParts), err)
	}
	if newParts[0].SupersedesID == nil || *newParts[0].SupersedesID != oldPart {
		t.Errorf("new part supersedes_id = %v, want %d", newParts[0].SupersedesID, oldPart)
	}
}
