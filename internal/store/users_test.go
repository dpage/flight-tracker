package store

import (
	"errors"
	"testing"
)

func TestInviteAndQueryUsers(t *testing.T) {
	s := newStore(t)

	if _, err := s.InviteUser(ctx, InvitePayload{GitHubLogin: "  "}); err == nil {
		t.Error("empty login should error")
	}

	u, err := s.InviteUser(ctx, InvitePayload{GitHubLogin: "Alice", Name: "Alice A", IsSuperuser: true})
	if err != nil {
		t.Fatalf("InviteUser: %v", err)
	}
	if !u.IsSuperuser || !u.IsActive || u.GitHubID != nil {
		t.Errorf("unexpected invited user %+v", u)
	}

	got, err := s.UserByID(ctx, u.ID)
	if err != nil || got.GitHubLogin != "Alice" {
		t.Fatalf("UserByID: %v %v", got, err)
	}
	byLogin, err := s.UserByLogin(ctx, "alice") // case-insensitive
	if err != nil || byLogin.ID != u.ID {
		t.Fatalf("UserByLogin: %v %v", byLogin, err)
	}
	if _, err := s.UserByLogin(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing login → ErrNotFound, got %v", err)
	}
	if _, err := s.UserByID(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing id → ErrNotFound, got %v", err)
	}
	if _, err := s.UserByGitHubID(ctx, 424242); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing github id → ErrNotFound, got %v", err)
	}
}

func TestListUsersOrdered(t *testing.T) {
	s := newStore(t)
	if us, err := s.ListUsers(ctx); err != nil || len(us) != 0 {
		t.Fatalf("empty list: %v %v", us, err)
	}
	_, _ = s.InviteUser(ctx, InvitePayload{GitHubLogin: "Charlie"})
	_, _ = s.InviteUser(ctx, InvitePayload{GitHubLogin: "alice"})
	_, _ = s.InviteUser(ctx, InvitePayload{GitHubLogin: "Bob"})
	us, err := s.ListUsers(ctx)
	if err != nil || len(us) != 3 {
		t.Fatalf("ListUsers: %v %v", us, err)
	}
	// Ordered by lower(github_login): alice, Bob, Charlie.
	if us[0].GitHubLogin != "alice" || us[1].GitHubLogin != "Bob" || us[2].GitHubLogin != "Charlie" {
		t.Errorf("ordering wrong: %s,%s,%s", us[0].GitHubLogin, us[1].GitHubLogin, us[2].GitHubLogin)
	}
}

func TestCountUsers(t *testing.T) {
	s := newStore(t)
	if n, err := s.CountUsers(ctx); err != nil || n != 0 {
		t.Fatalf("CountUsers empty = %d %v", n, err)
	}
	_, _ = s.InviteUser(ctx, InvitePayload{GitHubLogin: "x"})
	if n, _ := s.CountUsers(ctx); n != 1 {
		t.Errorf("CountUsers = %d, want 1", n)
	}
}

func TestUpdateUser(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{GitHubLogin: "edit", Name: "Old"})

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
	u, _ := s.InviteUser(ctx, InvitePayload{GitHubLogin: "del"})
	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if err := s.DeleteUser(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete → ErrNotFound, got %v", err)
	}
}

func TestLinkLoginBootstrapFirstUser(t *testing.T) {
	s := newStore(t)
	p := GitHubProfile{ID: 1001, Login: "boss", Name: "Boss", AvatarURL: "a.png"}
	u, err := s.LinkLogin(ctx, p, true) // bootstrap → superuser
	if err != nil {
		t.Fatalf("LinkLogin bootstrap: %v", err)
	}
	if !u.IsSuperuser || u.GitHubID == nil || *u.GitHubID != 1001 {
		t.Errorf("bootstrap user wrong: %+v", u)
	}
	if u.LastLoginAt == nil {
		t.Error("last_login_at should be set")
	}
}

func TestLinkLoginNotOnAllowlist(t *testing.T) {
	s := newStore(t)
	// Seed one user so this isn't bootstrap.
	_, _ = s.InviteUser(ctx, InvitePayload{GitHubLogin: "someone"})
	_, err := s.LinkLogin(ctx, GitHubProfile{ID: 7, Login: "stranger"}, false)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("uninvited login → ErrNotFound, got %v", err)
	}
}

func TestLinkLoginInvitedUserGetsLinked(t *testing.T) {
	s := newStore(t)
	inv, _ := s.InviteUser(ctx, InvitePayload{GitHubLogin: "Invitee", Name: "Inv"})
	if inv.GitHubID != nil {
		t.Fatal("invited user should start with NULL github_id")
	}
	// First sign-in matches by lower(github_login); github_id filled in.
	u, err := s.LinkLogin(ctx, GitHubProfile{ID: 2002, Login: "invitee", Name: "Real Name", AvatarURL: "x"}, false)
	if err != nil {
		t.Fatalf("LinkLogin invited: %v", err)
	}
	if u.ID != inv.ID || u.GitHubID == nil || *u.GitHubID != 2002 {
		t.Errorf("invited user not linked: %+v", u)
	}
	if u.Name != "Real Name" {
		t.Errorf("profile name should overwrite: %q", u.Name)
	}

	// Second sign-in: now found by github_id; empty name keeps existing.
	u2, err := s.LinkLogin(ctx, GitHubProfile{ID: 2002, Login: "invitee", Name: "", AvatarURL: ""}, false)
	if err != nil {
		t.Fatalf("LinkLogin second: %v", err)
	}
	if u2.Name != "Real Name" {
		t.Errorf("empty name should not overwrite existing: %q", u2.Name)
	}
}

func TestLinkLoginInactiveRejected(t *testing.T) {
	s := newStore(t)
	inv, _ := s.InviteUser(ctx, InvitePayload{GitHubLogin: "blocked"})
	no := false
	_, _ = s.UpdateUser(ctx, inv.ID, UpdateUserPayload{IsActive: &no})
	_, err := s.LinkLogin(ctx, GitHubProfile{ID: 3003, Login: "blocked"}, false)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("inactive invited user → ErrNotFound, got %v", err)
	}
}

func TestLinkLoginUpsertsEmail_Bootstrap(t *testing.T) {
	s := newStore(t)
	u, err := s.LinkLogin(ctx,
		GitHubProfile{ID: 5005, Login: "first", Email: "first@example.com"}, true)
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
	u, _ := s.LinkLogin(ctx, GitHubProfile{ID: 6006, Login: "later"}, true)
	if emails, _ := s.EmailsByUser(ctx, u.ID); len(emails) != 0 {
		t.Fatalf("expected no emails initially, got %d", len(emails))
	}
	// Second login carries an email; it should land in user_emails.
	_, err := s.LinkLogin(ctx, GitHubProfile{ID: 6006, Login: "later", Email: "later@example.com"}, false)
	if err != nil {
		t.Fatalf("second LinkLogin: %v", err)
	}
	if _, err := s.UserByVerifiedEmail(ctx, "later@example.com"); err != nil {
		t.Errorf("email not upserted on second login: %v", err)
	}
}

func TestLinkLoginEmptyEmailIsNoop(t *testing.T) {
	s := newStore(t)
	u, err := s.LinkLogin(ctx, GitHubProfile{ID: 7007, Login: "noemail"}, true)
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
	// Link once (creates github_id row), then deactivate, then re-login.
	u, _ := s.LinkLogin(ctx, GitHubProfile{ID: 4004, Login: "known"}, true)
	no := false
	_, _ = s.UpdateUser(ctx, u.ID, UpdateUserPayload{IsActive: &no})
	_, err := s.LinkLogin(ctx, GitHubProfile{ID: 4004, Login: "known"}, false)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("known-but-inactive → ErrNotFound, got %v", err)
	}
}
