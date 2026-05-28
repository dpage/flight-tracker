package handlers

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/store"
)

// seedVerifiedEmail attaches a verified address to userID so the friend-
// invite path can find them via UserByVerifiedEmail.
func seedVerifiedEmail(t *testing.T, e *testEnv, userID int64, addr string) {
	t.Helper()
	if err := e.store.UpsertVerifiedEmail(context.Background(), userID, addr); err != nil {
		t.Fatalf("UpsertVerifiedEmail: %v", err)
	}
}

func TestListFriendsRequiresAuth(t *testing.T) {
	e := setup(t, nil, nil)
	w := e.req(t, "GET", "/api/friends", nil, 0)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", w.Code)
	}
}

func TestInviteFriendByEmailKnownUser(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	target := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, target, "bob@example.com")

	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, inviter)
	if w.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202; body=%s", w.Code, w.Body.String())
	}

	// Confirm a pending friendship now exists between alice → bob with alice
	// as the requester.
	f, err := e.store.FriendshipBetween(context.Background(), inviter, target)
	if err != nil {
		t.Fatalf("FriendshipBetween: %v", err)
	}
	if f.Status != "pending" || f.RequestedBy != inviter {
		t.Errorf("unexpected friendship: %+v", f)
	}
}

func TestInviteFriendByEmailUnknownAddressQueues(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)

	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "stranger@example.com"}, inviter)
	if w.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202; body=%s", w.Code, w.Body.String())
	}

	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pending_friend_invites
		 WHERE inviter_id = $1 AND email_lower = 'stranger@example.com'`,
		inviter).Scan(&n); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 pending invite, got %d", n)
	}
}

func TestInviteFriendResponseIdenticalForKnownAndUnknown(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	known := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, known, "bob@example.com")

	known1 := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, inviter)
	unknown := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "ghost@example.com"}, inviter)

	if known1.Code != unknown.Code {
		t.Errorf("status codes differ: %d vs %d", known1.Code, unknown.Code)
	}
	if known1.Body.String() != unknown.Body.String() {
		t.Errorf("response bodies leak target existence:\n  known=%q\n  unknown=%q",
			known1.Body.String(), unknown.Body.String())
	}
}

func TestInviteFriendBadEmail(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "not-an-email"}, inviter)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", w.Code)
	}
}

func TestInviteFriendSelfMatchesQuietly(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "alice", false)
	seedVerifiedEmail(t, e, me, "alice@example.com")

	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "alice@example.com"}, me)
	// Self-invite must produce the same accepted response (no leak), and
	// must NOT create a friendship row.
	if w.Code != http.StatusAccepted {
		t.Errorf("code = %d, want 202", w.Code)
	}
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM friendships WHERE user_low = $1 OR user_high = $1`,
		me).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("self-invite created %d rows, want 0", n)
	}
}

func TestAcceptAndRemoveFriendRoundTrip(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, bob, "bob@example.com")

	// Alice invites Bob; pending row created with Alice as the requester.
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, alice); w.Code != http.StatusAccepted {
		t.Fatalf("invite: code=%d body=%s", w.Code, w.Body.String())
	}

	// Bob (the recipient) accepts.
	acceptPath := "/api/friends/" + strconv.FormatInt(alice, 10) + "/accept"
	w := e.req(t, "POST", acceptPath, nil, bob)
	if w.Code != http.StatusOK {
		t.Fatalf("accept: code=%d body=%s", w.Code, w.Body.String())
	}
	var dto api.FriendshipDTO
	dto = decodeBody[api.FriendshipDTO](t, w)
	if dto.Status != "accepted" || dto.FriendID != alice {
		t.Errorf("bad accept DTO: %+v", dto)
	}

	// Bob unfriends Alice.
	removePath := "/api/friends/" + strconv.FormatInt(alice, 10)
	w = e.req(t, "DELETE", removePath, nil, bob)
	if w.Code != http.StatusNoContent {
		t.Errorf("remove: code=%d body=%s", w.Code, w.Body.String())
	}

	if _, err := e.store.FriendshipBetween(context.Background(), alice, bob); err == nil {
		t.Error("friendship should be gone after unfriend")
	}
}

func TestAcceptFriendMissingPendingReturns404(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	path := "/api/friends/" + strconv.FormatInt(alice, 10) + "/accept"
	w := e.req(t, "POST", path, nil, bob)
	if w.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", w.Code)
	}
}

func TestListFriendsReturnsViewerOrientedDTOs(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, bob, "bob@example.com")
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, alice); w.Code != http.StatusAccepted {
		t.Fatalf("invite: %s", w.Body.String())
	}

	// From Alice's view the pending request is outgoing.
	w := e.req(t, "GET", "/api/friends", nil, alice)
	if w.Code != http.StatusOK {
		t.Fatalf("alice list: %d %s", w.Code, w.Body.String())
	}
	rows := decodeBody[[]api.FriendshipDTO](t, w)
	if len(rows) != 1 {
		t.Fatalf("alice rows = %d, want 1", len(rows))
	}
	if rows[0].Direction != "outgoing" || rows[0].FriendID != bob {
		t.Errorf("alice DTO = %+v", rows[0])
	}

	// From Bob's view it's incoming.
	w = e.req(t, "GET", "/api/friends", nil, bob)
	rows = decodeBody[[]api.FriendshipDTO](t, w)
	if len(rows) != 1 {
		t.Fatalf("bob rows = %d", len(rows))
	}
	if rows[0].Direction != "incoming" || rows[0].FriendID != alice {
		t.Errorf("bob DTO = %+v", rows[0])
	}
}

func TestFriendshipDTOOmitsDirectionForAccepted(t *testing.T) {
	// Lightweight DTO-shape check that doesn't need a DB round-trip.
	accepted := store.Friendship{
		UserLow: 1, UserHigh: 2, Status: "accepted", RequestedBy: 1,
	}
	dto := api.ToFriendshipDTO(&accepted, 2)
	if dto.Direction != "" {
		t.Errorf("accepted friendship should have empty direction, got %q", dto.Direction)
	}
	if dto.FriendID != 1 {
		t.Errorf("FriendID = %d, want 1", dto.FriendID)
	}
}
