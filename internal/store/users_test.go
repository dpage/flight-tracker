package store

import (
	"errors"
	"testing"
)

func TestInviteAndQueryUsers(t *testing.T) {
	s := newStore(t)

	if _, err := s.InviteUser(ctx, InvitePayload{Username: "  "}); err == nil {
		t.Error("empty username should error")
	}

	u, err := s.InviteUser(ctx, InvitePayload{Username: "Alice", Name: "Alice A", IsSuperuser: true})
	if err != nil {
		t.Fatalf("InviteUser: %v", err)
	}
	if !u.IsSuperuser || !u.IsActive {
		t.Errorf("unexpected invited user %+v", u)
	}

	got, err := s.UserByID(ctx, u.ID)
	if err != nil || got.Username != "Alice" {
		t.Fatalf("UserByID: %v %v", got, err)
	}
	byUsername, err := s.UserByUsername(ctx, "alice") // case-insensitive
	if err != nil || byUsername.ID != u.ID {
		t.Fatalf("UserByUsername: %v %v", byUsername, err)
	}
	if _, err := s.UserByUsername(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing username → ErrNotFound, got %v", err)
	}
	if _, err := s.UserByID(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing id → ErrNotFound, got %v", err)
	}
	if _, err := s.UserByIdentity(ctx, "github", "424242"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing identity → ErrNotFound, got %v", err)
	}
}

func TestInviteUserDuplicateUsername(t *testing.T) {
	s := newStore(t)
	if _, err := s.InviteUser(ctx, InvitePayload{Username: "Alice"}); err != nil {
		t.Fatalf("first invite: %v", err)
	}
	// Case-insensitive collision on the lower(username) unique index — the
	// store should surface ErrUsernameTaken rather than the raw pg error.
	_, err := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	if !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("duplicate invite → got %v, want ErrUsernameTaken", err)
	}
}

func TestListUsersOrdered(t *testing.T) {
	s := newStore(t)
	if us, err := s.ListUsers(ctx); err != nil || len(us) != 0 {
		t.Fatalf("empty list: %v %v", us, err)
	}
	_, _ = s.InviteUser(ctx, InvitePayload{Username: "Charlie"})
	_, _ = s.InviteUser(ctx, InvitePayload{Username: "alice"})
	_, _ = s.InviteUser(ctx, InvitePayload{Username: "Bob"})
	us, err := s.ListUsers(ctx)
	if err != nil || len(us) != 3 {
		t.Fatalf("ListUsers: %v %v", us, err)
	}
	// Ordered by lower(username): alice, Bob, Charlie.
	if us[0].Username != "alice" || us[1].Username != "Bob" || us[2].Username != "Charlie" {
		t.Errorf("ordering wrong: %s,%s,%s", us[0].Username, us[1].Username, us[2].Username)
	}
}

func TestCountUsers(t *testing.T) {
	s := newStore(t)
	if n, err := s.CountUsers(ctx); err != nil || n != 0 {
		t.Fatalf("CountUsers empty = %d %v", n, err)
	}
	_, _ = s.InviteUser(ctx, InvitePayload{Username: "x"})
	if n, _ := s.CountUsers(ctx); n != 1 {
		t.Errorf("CountUsers = %d, want 1", n)
	}
}

func TestUpdateUser(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "edit", Name: "Old"})

	name := "New Name"
	su := true
	active := false
	upd, err := s.UpdateUser(ctx, u.ID, UpdateUserPayload{Name: &name, IsSuperuser: &su, IsActive: &active})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if upd.Name != "New Name" || !upd.IsSuperuser || upd.IsActive {
		t.Errorf("update not applied: %+v", upd)
	}
	// Partial update leaves other fields (COALESCE).
	onlyName := "Final"
	upd, _ = s.UpdateUser(ctx, u.ID, UpdateUserPayload{Name: &onlyName})
	if upd.Name != "Final" || !upd.IsSuperuser {
		t.Errorf("partial update lost fields: %+v", upd)
	}
	if _, err := s.UpdateUser(ctx, 99999, UpdateUserPayload{Name: &name}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update missing user → ErrNotFound, got %v", err)
	}
}

func TestDeleteUser(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "del"})
	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if err := s.DeleteUser(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete → ErrNotFound, got %v", err)
	}
}

func githubProfile(id, login, name, avatar, email string) OAuthProfile {
	return OAuthProfile{
		Provider: "github", ProviderUserID: id,
		Username: login, Name: name, AvatarURL: avatar, Email: email,
	}
}

func TestLinkLoginBootstrapFirstUser(t *testing.T) {
	s := newStore(t)
	u, _, err := s.LinkLogin(ctx, githubProfile("1001", "boss", "Boss", "a.png", ""), true)
	if err != nil {
		t.Fatalf("LinkLogin bootstrap: %v", err)
	}
	if !u.IsSuperuser || u.Username != "boss" {
		t.Errorf("bootstrap user wrong: %+v", u)
	}
	if u.LastLoginAt == nil {
		t.Error("last_login_at should be set")
	}
	by, err := s.UserByIdentity(ctx, "github", "1001")
	if err != nil || by.ID != u.ID {
		t.Errorf("UserByIdentity should find the bootstrapped user: %v %v", by, err)
	}
}

func TestLinkLoginOpenSignupCreatesUser(t *testing.T) {
	s := newStore(t)
	// Seed one user so this isn't the bootstrap path.
	_, _ = s.InviteUser(ctx, InvitePayload{Username: "someone"})
	u, outcome, err := s.LinkLogin(ctx, githubProfile("7", "stranger", "Stranger", "x", ""), false)
	if err != nil {
		t.Fatalf("open signup should create a new user, got %v", err)
	}
	if u.IsSuperuser {
		t.Error("non-bootstrap signup must not be superuser")
	}
	if u.Username != "stranger" {
		t.Errorf("username should default to provider login, got %q", u.Username)
	}
	if outcome != LinkOutcomeOpenSignup {
		t.Errorf("outcome = %v, want LinkOutcomeOpenSignup", outcome)
	}
}

func TestLinkLoginInvitedUserGetsLinked(t *testing.T) {
	s := newStore(t)
	inv, _ := s.InviteUser(ctx, InvitePayload{Username: "Invitee", Name: "Inv"})
	if ids, _ := s.IdentitiesByUser(ctx, inv.ID); len(ids) != 0 {
		t.Fatal("invited user should start with no identities")
	}
	// First sign-in matches by lower(username); identity row created.
	u, outcome, err := s.LinkLogin(ctx,
		githubProfile("2002", "invitee", "Real Name", "x", ""), false)
	if err != nil {
		t.Fatalf("LinkLogin invited: %v", err)
	}
	if u.ID != inv.ID {
		t.Errorf("invited user not linked: got id=%d, want %d", u.ID, inv.ID)
	}
	if outcome != LinkOutcomeInviteeLinked {
		t.Errorf("first sign-in outcome = %v, want LinkOutcomeInviteeLinked", outcome)
	}
	if u.Name != "Real Name" {
		t.Errorf("profile name should overwrite: %q", u.Name)
	}
	ids, _ := s.IdentitiesByUser(ctx, u.ID)
	if len(ids) != 1 || ids[0].Provider != "github" || ids[0].ProviderUserID != "2002" {
		t.Errorf("identity not linked: %+v", ids)
	}

	// Second sign-in: now found by identity row; empty name keeps existing.
	u2, _, err := s.LinkLogin(ctx,
		githubProfile("2002", "invitee", "", "", ""), false)
	if err != nil {
		t.Fatalf("LinkLogin second: %v", err)
	}
	if u2.Name != "Real Name" {
		t.Errorf("empty name should not overwrite existing: %q", u2.Name)
	}
}

func TestLinkLoginInactiveRejected(t *testing.T) {
	s := newStore(t)
	inv, _ := s.InviteUser(ctx, InvitePayload{Username: "blocked"})
	no := false
	_, _ = s.UpdateUser(ctx, inv.ID, UpdateUserPayload{IsActive: &no})
	_, _, err := s.LinkLogin(ctx,
		githubProfile("3003", "blocked", "", "", ""), false)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("inactive invited user → ErrNotFound, got %v", err)
	}
}

func TestLinkLoginUpsertsEmail_Bootstrap(t *testing.T) {
	s := newStore(t)
	u, _, err := s.LinkLogin(ctx,
		githubProfile("5005", "first", "", "", "first@example.com"), true)
	if err != nil {
		t.Fatalf("LinkLogin: %v", err)
	}
	got, err := s.UserByVerifiedEmail(ctx, "first@example.com")
	if err != nil {
		t.Fatalf("UserByVerifiedEmail: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("verified email links to user %d, want %d", got.ID, u.ID)
	}
}

func TestLinkLoginUpsertsEmail_ExistingUser(t *testing.T) {
	s := newStore(t)
	// First login bootstraps without an email.
	u, _, _ := s.LinkLogin(ctx, githubProfile("6006", "later", "", "", ""), true)
	if emails, _ := s.EmailsByUser(ctx, u.ID); len(emails) != 0 {
		t.Fatalf("expected no emails initially, got %d", len(emails))
	}
	// Second login carries an email; it should land in user_emails.
	_, _, err := s.LinkLogin(ctx,
		githubProfile("6006", "later", "", "", "later@example.com"), false)
	if err != nil {
		t.Fatalf("second LinkLogin: %v", err)
	}
	if _, err := s.UserByVerifiedEmail(ctx, "later@example.com"); err != nil {
		t.Errorf("email not upserted on second login: %v", err)
	}
}

func TestLinkLoginEmptyEmailIsNoop(t *testing.T) {
	s := newStore(t)
	u, _, err := s.LinkLogin(ctx, githubProfile("7007", "noemail", "", "", ""), true)
	if err != nil {
		t.Fatal(err)
	}
	emails, _ := s.EmailsByUser(ctx, u.ID)
	if len(emails) != 0 {
		t.Errorf("empty email should be a no-op, got %d rows", len(emails))
	}
}

func TestLinkLoginKnownInactiveRejected(t *testing.T) {
	s := newStore(t)
	u, _, _ := s.LinkLogin(ctx, githubProfile("4004", "known", "", "", ""), true)
	no := false
	_, _ = s.UpdateUser(ctx, u.ID, UpdateUserPayload{IsActive: &no})
	_, _, err := s.LinkLogin(ctx, githubProfile("4004", "known", "", "", ""), false)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("known-but-inactive → ErrNotFound, got %v", err)
	}
}

// A user signed up via GitHub later signs in with Google. The Google profile
// shares a verified email with the existing user, so LinkLogin attaches a new
// identity row to the same user instead of creating a new account.
func TestLinkLoginCrossProviderEmailMatch(t *testing.T) {
	s := newStore(t)
	gh, ghOutcome, _ := s.LinkLogin(ctx,
		githubProfile("8008", "dpage", "Dave", "", "dave@example.com"), true)
	if ghOutcome != LinkOutcomeBootstrap {
		t.Errorf("first GitHub login: outcome = %v, want LinkOutcomeBootstrap", ghOutcome)
	}

	google := OAuthProfile{
		Provider: "google", ProviderUserID: "g-12345",
		Name: "Dave (Google)", Email: "dave@example.com",
	}
	u, outcome, err := s.LinkLogin(ctx, google, false)
	if err != nil {
		t.Fatalf("LinkLogin via google: %v", err)
	}
	if u.ID != gh.ID {
		t.Errorf("cross-provider should link to same user: got %d, want %d", u.ID, gh.ID)
	}
	if outcome != LinkOutcomeCrossProvider {
		t.Errorf("outcome = %v, want LinkOutcomeCrossProvider", outcome)
	}
	ids, _ := s.IdentitiesByUser(ctx, u.ID)
	if len(ids) != 2 {
		t.Fatalf("expected 2 identities, got %d: %+v", len(ids), ids)
	}

	// Signing in again with the same Google identity now goes through the
	// fast-path: outcome should report LinkOutcomeExisting.
	_, outcome2, err := s.LinkLogin(ctx, google, false)
	if err != nil {
		t.Fatalf("repeat LinkLogin: %v", err)
	}
	if outcome2 != LinkOutcomeExisting {
		t.Errorf("repeat outcome = %v, want LinkOutcomeExisting", outcome2)
	}
}

// Google bootstrap: no users, no username from Google. The local-part of the
// email seeds the username so the row isn't empty.
func TestLinkLoginGoogleBootstrap(t *testing.T) {
	s := newStore(t)
	p := OAuthProfile{
		Provider: "google", ProviderUserID: "g-1",
		Name: "Boss", Email: "boss@example.com",
	}
	u, _, err := s.LinkLogin(ctx, p, true)
	if err != nil {
		t.Fatalf("LinkLogin: %v", err)
	}
	if !u.IsSuperuser {
		t.Error("first user should be superuser")
	}
	if u.Username != "boss" {
		t.Errorf("username should default to email local-part, got %q", u.Username)
	}
}
