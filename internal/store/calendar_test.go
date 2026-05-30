package store

import (
	"errors"
	"testing"
	"time"
)

// mkTypedPlan inserts a plan of the given type with a title and returns its id.
func mkTypedPlan(t *testing.T, s *Store, tripID, createdBy int64, typ, title, confirm, notes string) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, title, confirmation_ref, notes, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		tripID, typ, title, confirm, notes, createdBy,
	).Scan(&id); err != nil {
		t.Fatalf("insert typed plan: %v", err)
	}
	return id
}

// mkPart inserts a fully-specified plan_part and returns its id.
func mkPart(t *testing.T, s *Store, planID int64, startsAt time.Time, endsAt *time.Time, startTZ, endTZ, startLabel string) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_tz, end_tz, start_label)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		planID, startsAt, endsAt, startTZ, endTZ, startLabel,
	).Scan(&id); err != nil {
		t.Fatalf("insert part: %v", err)
	}
	return id
}

func TestCalendarTokenIssueAndResolve(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	u := mkUser(t, s)

	// CalendarToken issues on first call.
	ct, err := s.CalendarToken(ctx, u, "me")
	if err != nil {
		t.Fatalf("CalendarToken: %v", err)
	}
	if ct.Token == "" || ct.Scope != "me" || ct.UserID != u {
		t.Fatalf("unexpected token: %+v", ct)
	}

	// Second call returns the same token (idempotent fetch).
	ct2, err := s.CalendarToken(ctx, u, "me")
	if err != nil {
		t.Fatalf("CalendarToken 2: %v", err)
	}
	if ct2.Token != ct.Token {
		t.Errorf("CalendarToken not stable: %q vs %q", ct.Token, ct2.Token)
	}

	// Resolve back to the owner.
	got, err := s.UserByCalendarToken(ctx, ct.Token)
	if err != nil {
		t.Fatalf("UserByCalendarToken: %v", err)
	}
	if got != u {
		t.Errorf("UserByCalendarToken = %d, want %d", got, u)
	}

	// Unknown token → ErrNotFound.
	if _, err := s.UserByCalendarToken(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown token err = %v, want ErrNotFound", err)
	}

	// Invalid scope is rejected.
	if _, err := s.CalendarToken(ctx, u, "bogus"); err == nil {
		t.Error("CalendarToken accepted invalid scope")
	}
}

func TestCalendarTokenRegenerateRevokes(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	u := mkUser(t, s)
	first, err := s.CalendarToken(ctx, u, "trip")
	if err != nil {
		t.Fatalf("CalendarToken: %v", err)
	}
	second, err := s.RegenerateCalendarToken(ctx, u, "trip")
	if err != nil {
		t.Fatalf("RegenerateCalendarToken: %v", err)
	}
	if second.Token == first.Token {
		t.Fatal("regenerate did not change the token")
	}
	// Old token no longer resolves.
	if _, err := s.UserByCalendarToken(ctx, first.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("old token still resolves: err=%v", err)
	}
	// New token resolves.
	if _, err := s.UserByCalendarToken(ctx, second.Token); err != nil {
		t.Errorf("new token does not resolve: %v", err)
	}
}

func TestCalendarTokenListAndRevoke(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	u := mkUser(t, s)
	other := mkUser(t, s)
	me, _ := s.CalendarToken(ctx, u, "me")
	_, _ = s.CalendarToken(ctx, u, "trip")

	toks, err := s.ListCalendarTokens(ctx, u)
	if err != nil {
		t.Fatalf("ListCalendarTokens: %v", err)
	}
	if len(toks) != 2 {
		t.Fatalf("ListCalendarTokens len = %d, want 2", len(toks))
	}

	// Another user cannot revoke u's token.
	if err := s.RevokeCalendarToken(ctx, other, me.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user revoke err = %v, want ErrNotFound", err)
	}
	// Owner can revoke.
	if err := s.RevokeCalendarToken(ctx, u, me.Token); err != nil {
		t.Errorf("owner revoke: %v", err)
	}
	if _, err := s.UserByCalendarToken(ctx, me.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked token still resolves: %v", err)
	}
}

// TestCalendarEventsVisibility is the central security test: a plan hidden from
// the token owner must be absent from their feed, and another user's token
// (resolving to a different viewer) must never see the owner's private plans.
func TestCalendarEventsVisibility(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	member := mkUser(t, s)
	stranger := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, member, "viewer")

	start := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)

	// A public (default-visibility) plan everyone on the trip sees.
	pubPlan := mkTypedPlan(t, s, trip, owner, "flight", "BA286", "ABC123", "window seat")
	mkPart(t, s, pubPlan, start, &end, "Europe/London", "America/New_York", "LHR")

	// A plan hidden from `member`.
	hidPlan := mkTypedPlan(t, s, trip, owner, "hotel", "Secret Hotel", "", "")
	mkPart(t, s, hidPlan, start.Add(24*time.Hour), nil, "Europe/Paris", "", "Hotel")
	setVisibility(t, s, hidPlan, "hidden_from", member)

	// Owner sees both.
	ownerEv, err := s.CalendarEventsForTrip(ctx, owner, trip)
	if err != nil {
		t.Fatalf("CalendarEventsForTrip(owner): %v", err)
	}
	if len(ownerEv) != 2 {
		t.Fatalf("owner trip feed len = %d, want 2", len(ownerEv))
	}

	// Member sees only the public plan — the hidden one is absent.
	memberEv, err := s.CalendarEventsForTrip(ctx, member, trip)
	if err != nil {
		t.Fatalf("CalendarEventsForTrip(member): %v", err)
	}
	if len(memberEv) != 1 {
		t.Fatalf("member trip feed len = %d, want 1 (hidden plan must not leak)", len(memberEv))
	}
	if memberEv[0].PlanID != pubPlan {
		t.Errorf("member sees plan %d, want public plan %d", memberEv[0].PlanID, pubPlan)
	}

	// The owner's "me" feed contains both; the stranger's "me" feed (a totally
	// different token owner) sees none of the owner's plans.
	strangerEv, err := s.CalendarEventsForUser(ctx, stranger)
	if err != nil {
		t.Fatalf("CalendarEventsForUser(stranger): %v", err)
	}
	if len(strangerEv) != 0 {
		t.Errorf("stranger me feed len = %d, want 0 (no membership)", len(strangerEv))
	}

	// Single-plan feed: member cannot see the hidden plan even by id.
	hidForMember, err := s.CalendarEventsForPlan(ctx, member, hidPlan)
	if err != nil {
		t.Fatalf("CalendarEventsForPlan(member,hidden): %v", err)
	}
	if len(hidForMember) != 0 {
		t.Errorf("member single-plan feed for hidden plan len = %d, want 0", len(hidForMember))
	}
	// Owner can.
	hidForOwner, err := s.CalendarEventsForPlan(ctx, owner, hidPlan)
	if err != nil {
		t.Fatalf("CalendarEventsForPlan(owner,hidden): %v", err)
	}
	if len(hidForOwner) != 1 {
		t.Errorf("owner single-plan feed for hidden plan len = %d, want 1", len(hidForOwner))
	}

	// Field assembly check on the public flight event.
	ev := ownerEv[0]
	if ev.PlanID == pubPlan {
		if ev.Title != "BA286" || ev.Type != "flight" || ev.ConfirmationRef != "ABC123" {
			t.Errorf("event field assembly wrong: %+v", ev)
		}
		if ev.StartLabel != "LHR" || ev.StartTZ != "Europe/London" {
			t.Errorf("event place/tz wrong: %+v", ev)
		}
	}
}

// TestCalendarEventsExcludeDismissed: a superseded/dismissed part is omitted.
func TestCalendarEventsExcludeDismissed(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkTypedPlan(t, s, trip, owner, "flight", "BA1", "", "")
	now := time.Now().UTC()
	pid := mkPart(t, s, plan, now, nil, "Europe/London", "", "LHR")
	if _, err := s.pool.Exec(ctx, `UPDATE plan_parts SET dismissed_at = NOW() WHERE id = $1`, pid); err != nil {
		t.Fatalf("dismiss part: %v", err)
	}
	ev, err := s.CalendarEventsForUser(ctx, owner)
	if err != nil {
		t.Fatalf("CalendarEventsForUser: %v", err)
	}
	if len(ev) != 0 {
		t.Errorf("dismissed part should be excluded; got %d events", len(ev))
	}
}
