package store

import (
	"errors"
	"strings"
	"testing"
)

func TestRequestFriendshipFreshPending(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s), mkUser(t, s)
	f, err := s.RequestFriendship(ctx, a, b)
	if err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	if f.Status != "pending" || f.RequestedBy != a {
		t.Errorf("unexpected: %+v", f)
	}
	if f.FriendID(a) != b || f.FriendID(b) != a {
		t.Errorf("FriendID orientation broken: %+v", f)
	}
}

func TestRequestFriendshipCrossDirectionAccepts(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s), mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, a, b); err != nil {
		t.Fatalf("first request: %v", err)
	}
	// b initiating the reverse direction implicitly accepts a's pending.
	got, err := s.RequestFriendship(ctx, b, a)
	if err != nil {
		t.Fatalf("reverse request: %v", err)
	}
	if got.Status != "accepted" {
		t.Errorf("status = %q, want accepted", got.Status)
	}
	if got.AcceptedAt == nil {
		t.Error("accepted_at should be set after implicit accept")
	}
}

func TestRequestFriendshipNoopOnDuplicate(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s), mkUser(t, s)
	first, _ := s.RequestFriendship(ctx, a, b)
	second, _ := s.RequestFriendship(ctx, a, b)
	if first.Status != "pending" || second.Status != "pending" {
		t.Errorf("status should stay pending: %+v / %+v", first, second)
	}
	if !first.RequestedAt.Equal(second.RequestedAt) {
		t.Error("duplicate request should not refresh requested_at")
	}
}

func TestRequestFriendshipRejectsSelf(t *testing.T) {
	s := newStore(t)
	a := mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, a, a); err == nil {
		t.Error("self-friend should error")
	}
}

func TestAcceptFriendshipRequiresOtherParty(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s), mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, a, b); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// The requester themselves can't accept their own pending row.
	if _, err := s.AcceptFriendship(ctx, a, b); !errors.Is(err, ErrNotFound) {
		t.Errorf("self-accept should be ErrNotFound, got %v", err)
	}
	got, err := s.AcceptFriendship(ctx, b, a)
	if err != nil || got.Status != "accepted" {
		t.Fatalf("recipient accept: %v %+v", err, got)
	}
}

func TestRemoveFriendshipDeletesPendingOrAccepted(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s), mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, a, b); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	if _, err := s.AcceptFriendship(ctx, b, a); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if err := s.RemoveFriendship(ctx, b, a); err != nil {
		t.Fatalf("remove accepted: %v", err)
	}
	if _, err := s.FriendshipBetween(ctx, a, b); !errors.Is(err, ErrNotFound) {
		t.Errorf("after remove, FriendshipBetween should be ErrNotFound, got %v", err)
	}
	if err := s.RemoveFriendship(ctx, b, a); !errors.Is(err, ErrNotFound) {
		t.Errorf("double-remove → ErrNotFound, got %v", err)
	}
}

func TestListFriendshipsOrientedAroundViewer(t *testing.T) {
	s := newStore(t)
	a, b, c := mkUser(t, s), mkUser(t, s), mkUser(t, s)

	// a outgoing → b (pending)
	if _, err := s.RequestFriendship(ctx, a, b); err != nil {
		t.Fatalf("a→b: %v", err)
	}
	// c incoming ← a (pending, from a's view it's outgoing)
	if _, err := s.RequestFriendship(ctx, a, c); err != nil {
		t.Fatalf("a→c: %v", err)
	}
	// b later sends request back (accepts a↔b)
	if _, err := s.RequestFriendship(ctx, b, a); err != nil {
		t.Fatalf("b→a: %v", err)
	}

	rows, err := s.ListFriendships(ctx, a)
	if err != nil {
		t.Fatalf("ListFriendships(a): %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("a sees %d rows, want 2", len(rows))
	}
	var sawAcceptedB, sawPendingC bool
	for _, r := range rows {
		switch r.FriendID(a) {
		case b:
			if r.Status != "accepted" {
				t.Errorf("a↔b should be accepted, got %s", r.Status)
			}
			sawAcceptedB = true
		case c:
			if r.Status != "pending" || r.RequestedBy != a {
				t.Errorf("a↔c should be pending requested_by=a, got %+v", r)
			}
			sawPendingC = true
		}
	}
	if !sawAcceptedB || !sawPendingC {
		t.Errorf("missing rows: %+v", rows)
	}
}

func TestUpsertPendingFriendInviteAndConsume(t *testing.T) {
	s := newStore(t)
	inviter := mkUser(t, s)
	created, err := s.UpsertPendingFriendInvite(ctx, inviter, "  NewFriend@Example.COM  ", "join us")
	if err != nil || !created {
		t.Fatalf("UpsertPendingFriendInvite: created=%v err=%v", created, err)
	}
	// Duplicate must return created=false (so the caller skips a second email).
	again, err := s.UpsertPendingFriendInvite(ctx, inviter, "newfriend@example.com", "")
	if err != nil || again {
		t.Fatalf("duplicate: created=%v err=%v", again, err)
	}
	// Different inviter, same email is its own queue entry.
	other := mkUser(t, s)
	if c, _ := s.UpsertPendingFriendInvite(ctx, other, "newfriend@example.com", ""); !c {
		t.Error("second inviter should get its own pending row")
	}
}

func TestLinkLoginConsumesPendingInvites(t *testing.T) {
	s := newStore(t)
	inviter1 := mkUser(t, s)
	inviter2 := mkUser(t, s)
	// Pre-seed two pending invites addressed at the same email from two
	// different inviters; LinkLogin should turn both into accepted
	// friendships once the new user signs in with that email.
	if _, err := s.UpsertPendingFriendInvite(ctx, inviter1, "joiner@example.com", ""); err != nil {
		t.Fatalf("seed inv1: %v", err)
	}
	if _, err := s.UpsertPendingFriendInvite(ctx, inviter2, "JOINER@example.com", ""); err != nil {
		t.Fatalf("seed inv2: %v", err)
	}

	joined, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "777",
			Username: "joiner", Email: "joiner@example.com"}, false)
	if err != nil {
		t.Fatalf("LinkLogin joiner: %v", err)
	}

	for _, inviter := range []int64{inviter1, inviter2} {
		got, err := s.FriendshipBetween(ctx, joined.ID, inviter)
		if err != nil {
			t.Errorf("missing friendship to inviter %d: %v", inviter, err)
			continue
		}
		if got.Status != "accepted" {
			t.Errorf("inviter %d → status %q, want accepted", inviter, got.Status)
		}
	}

	// Pending rows should be drained.
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_friend_invites WHERE email_lower = 'joiner@example.com'`,
	).Scan(&n); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if n != 0 {
		t.Errorf("pending invites not drained: %d remain", n)
	}
}

// Sanity: open signups default to non-superuser and a unique username when
// the provider-supplied login collides via mixed case.
func TestLinkLoginCaseInsensitiveUsernameCollision(t *testing.T) {
	s := newStore(t)
	_, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "111", Username: "Alice"}, true)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	u, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "222", Username: "ALICE"}, false)
	if err != nil {
		t.Fatalf("conflicting username: %v", err)
	}
	if strings.EqualFold(u.Username, "alice") {
		t.Errorf("expected suffix, got %q", u.Username)
	}
}
