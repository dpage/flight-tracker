package store

import (
	"testing"
	"time"
)

// mkTrip inserts a trip owned by ownerID (with the owner trip_members row) and
// returns its id. The plan/trip CRUD is stubbed in Wave 0a, so the visibility
// tests build their fixtures with direct SQL.
func mkTrip(t *testing.T, s *Store, ownerID int64) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('Trip', $1) RETURNING id`, ownerID,
	).Scan(&id); err != nil {
		t.Fatalf("insert trip: %v", err)
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`, id, ownerID,
	); err != nil {
		t.Fatalf("insert owner member: %v", err)
	}
	return id
}

func addMember(t *testing.T, s *Store, tripID, userID int64, role string) {
	t.Helper()
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (trip_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		tripID, userID, role); err != nil {
		t.Fatalf("add member: %v", err)
	}
}

func mkPlan(t *testing.T, s *Store, tripID int64, createdBy int64) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`,
		tripID, createdBy,
	).Scan(&id); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	return id
}

func addPlanPart(t *testing.T, s *Store, planID int64, startsAt time.Time) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at) VALUES ($1, $2) RETURNING id`,
		planID, startsAt,
	).Scan(&id); err != nil {
		t.Fatalf("insert plan_part: %v", err)
	}
	return id
}

func addPlanPassenger(t *testing.T, s *Store, planID, userID int64) {
	t.Helper()
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO plan_passengers (plan_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		planID, userID); err != nil {
		t.Fatalf("add plan passenger: %v", err)
	}
}

func setVisibility(t *testing.T, s *Store, planID int64, mode string, members ...int64) {
	t.Helper()
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO plan_visibility (plan_id, mode) VALUES ($1, $2)
		 ON CONFLICT (plan_id) DO UPDATE SET mode = EXCLUDED.mode`, planID, mode); err != nil {
		t.Fatalf("set visibility mode: %v", err)
	}
	for _, m := range members {
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO plan_visibility_members (plan_id, user_id) VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`, planID, m); err != nil {
			t.Fatalf("set visibility member: %v", err)
		}
	}
}

func mustCanView(t *testing.T, s *Store, planID, viewerID int64) bool {
	t.Helper()
	ok, err := s.CanViewPlan(ctx, planID, viewerID, false)
	if err != nil {
		t.Fatalf("CanViewPlan: %v", err)
	}
	return ok
}

// TestCanViewPlanDefault: a trip member sees a plan with no visibility row,
// while a non-member never does.
func TestCanViewPlanDefault(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	member := mkUser(t, s)
	stranger := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, member, "viewer")
	plan := mkPlan(t, s, trip, owner)

	if !mustCanView(t, s, plan, member) {
		t.Error("trip member should see a default-visibility plan")
	}
	if mustCanView(t, s, plan, stranger) {
		t.Error("non-member must not see the plan")
	}
}

// TestCanViewPlanOwnerAlwaysSees: the trip owner sees every plan even when a
// stray hidden_from row names them.
func TestCanViewPlanOwnerAlwaysSees(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	creator := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, creator, "editor")
	plan := mkPlan(t, s, trip, creator)
	setVisibility(t, s, plan, "hidden_from", owner) // inert against the owner

	if !mustCanView(t, s, plan, owner) {
		t.Error("trip owner must always see the plan, even when named in hidden_from")
	}
}

// TestCanViewPlanPassengerAlwaysSees: a passenger sees a plan even under
// only_visible_to that doesn't name them.
func TestCanViewPlanPassengerAlwaysSees(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	pax := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)
	addPlanPassenger(t, s, plan, pax) // trigger makes pax a trip viewer
	setVisibility(t, s, plan, "only_visible_to" /* nobody named */)

	if !mustCanView(t, s, plan, pax) {
		t.Error("passenger must always see their own plan")
	}
}

// TestCanViewPlanHiddenFrom: a member named in hidden_from cannot see it; an
// unnamed member still can.
func TestCanViewPlanHiddenFrom(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	hidden := mkUser(t, s)
	allowed := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, hidden, "viewer")
	addMember(t, s, trip, allowed, "viewer")
	plan := mkPlan(t, s, trip, owner)
	setVisibility(t, s, plan, "hidden_from", hidden)

	if mustCanView(t, s, plan, hidden) {
		t.Error("member named in hidden_from must not see the plan")
	}
	if !mustCanView(t, s, plan, allowed) {
		t.Error("member not named in hidden_from should see the plan")
	}
}

// TestCanViewPlanOnlyVisibleTo: only named members (plus the always-granted
// trio) see the plan.
func TestCanViewPlanOnlyVisibleTo(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	named := mkUser(t, s)
	other := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, named, "viewer")
	addMember(t, s, trip, other, "viewer")
	plan := mkPlan(t, s, trip, owner)
	setVisibility(t, s, plan, "only_visible_to", named)

	if !mustCanView(t, s, plan, named) {
		t.Error("named member must see an only_visible_to plan")
	}
	if mustCanView(t, s, plan, other) {
		t.Error("un-named member must not see an only_visible_to plan")
	}
}

// TestListVisiblePlanParts respects the same predicate as CanViewPlan.
func TestListVisiblePlanParts(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	member := mkUser(t, s)
	stranger := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, member, "viewer")
	plan := mkPlan(t, s, trip, owner)
	part := addPlanPart(t, s, plan, time.Now().Add(48*time.Hour))

	parts, err := s.ListVisiblePlanParts(ctx, member, ListVisiblePlanPartsOpts{TripID: trip})
	if err != nil {
		t.Fatalf("ListVisiblePlanParts: %v", err)
	}
	if len(parts) != 1 || parts[0].ID != part {
		t.Fatalf("member should see exactly the one part, got %d", len(parts))
	}

	parts, err = s.ListVisiblePlanParts(ctx, stranger, ListVisiblePlanPartsOpts{TripID: trip})
	if err != nil {
		t.Fatalf("ListVisiblePlanParts stranger: %v", err)
	}
	if len(parts) != 0 {
		t.Errorf("stranger should see no parts, got %d", len(parts))
	}
}

// TestVisiblePlanUserIDs returns exactly the set that can see the plan.
func TestVisiblePlanUserIDs(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	named := mkUser(t, s)
	other := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, named, "viewer")
	addMember(t, s, trip, other, "viewer")
	plan := mkPlan(t, s, trip, owner)
	setVisibility(t, s, plan, "only_visible_to", named)

	ids, err := s.VisiblePlanUserIDs(ctx, plan)
	if err != nil {
		t.Fatalf("VisiblePlanUserIDs: %v", err)
	}
	got := map[int64]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got[owner] || !got[named] {
		t.Errorf("expected owner(%d) and named(%d) in %v", owner, named, ids)
	}
	if got[other] {
		t.Errorf("un-named member %d must not be in visible set %v", other, ids)
	}
}
