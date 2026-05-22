package store

import (
	"errors"
	"strings"
	"testing"
)

func TestUpsertVerifiedEmail_Insert(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{GitHubLogin: "alice"})

	if err := s.UpsertVerifiedEmail(ctx, u.ID, "Alice@Example.com"); err != nil {
		t.Fatalf("UpsertVerifiedEmail: %v", err)
	}
	got, err := s.UserByVerifiedEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("UserByVerifiedEmail: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("user_id = %d, want %d", got.ID, u.ID)
	}
}

func TestUpsertVerifiedEmail_TrimsWhitespace(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{GitHubLogin: "alice"})
	if err := s.UpsertVerifiedEmail(ctx, u.ID, "  alice@example.com  "); err != nil {
		t.Fatalf("UpsertVerifiedEmail: %v", err)
	}
	if _, err := s.UserByVerifiedEmail(ctx, "alice@example.com"); err != nil {
		t.Errorf("trimmed lookup failed: %v", err)
	}
}

func TestUpsertVerifiedEmail_EmptyRejected(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{GitHubLogin: "alice"})
	if err := s.UpsertVerifiedEmail(ctx, u.ID, "   "); err == nil {
		t.Error("expected error for empty address")
	}
}

func TestUpsertVerifiedEmail_IdempotentSameUser(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{GitHubLogin: "alice"})

	for i := 0; i < 3; i++ {
		if err := s.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	emails, err := s.EmailsByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("EmailsByUser: %v", err)
	}
	if len(emails) != 1 {
		t.Errorf("len(emails) = %d, want 1", len(emails))
	}
	if !emails[0].Verified {
		t.Error("expected row to be verified")
	}
}

func TestUpsertVerifiedEmail_OtherUserRejected(t *testing.T) {
	s := newStore(t)
	u1, _ := s.InviteUser(ctx, InvitePayload{GitHubLogin: "alice"})
	u2, _ := s.InviteUser(ctx, InvitePayload{GitHubLogin: "bob"})

	if err := s.UpsertVerifiedEmail(ctx, u1.ID, "shared@example.com"); err != nil {
		t.Fatalf("first: %v", err)
	}
	err := s.UpsertVerifiedEmail(ctx, u2.ID, "shared@example.com")
	if err == nil {
		t.Fatal("expected error when another user owns the address, got nil")
	}
	if !strings.Contains(err.Error(), "address already") {
		t.Errorf("error = %v, want one mentioning 'address already'", err)
	}
}

func TestUserByVerifiedEmail_NotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.UserByVerifiedEmail(ctx, "nobody@example.com")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestUserByVerifiedEmail_RequiresVerified(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{GitHubLogin: "alice"})
	// Manually insert an unverified row, bypassing UpsertVerifiedEmail.
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO user_emails (user_id, address, verified) VALUES ($1,$2,FALSE)`,
		u.ID, "pending@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserByVerifiedEmail(ctx, "pending@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound (unverified rows must not match)", err)
	}
}

func TestEmailsByUser_Empty(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{GitHubLogin: "alice"})
	got, err := s.EmailsByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("EmailsByUser: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestEmailsByUser_MultipleNewestFirst(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{GitHubLogin: "alice"})
	if err := s.UpsertVerifiedEmail(ctx, u.ID, "first@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertVerifiedEmail(ctx, u.ID, "second@example.com"); err != nil {
		t.Fatal(err)
	}
	got, err := s.EmailsByUser(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// Newest first by created_at, then id.
	if got[0].Address != "second@example.com" || got[1].Address != "first@example.com" {
		t.Errorf("order = [%s, %s]", got[0].Address, got[1].Address)
	}
}
