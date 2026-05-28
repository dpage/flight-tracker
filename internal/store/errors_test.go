package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// canceled returns an already-cancelled context so pool queries fail fast,
// exercising the DB-error return branches.
func canceled() context.Context {
	c, cancel := context.WithCancel(context.Background())
	cancel()
	return c
}

func TestFlightQueryErrorPaths(t *testing.T) {
	s := newStore(t)
	cc := canceled()

	if _, err := s.ListFlights(cc); err == nil {
		t.Error("ListFlights should error on cancelled ctx")
	}
	if _, err := s.ActiveFlights(cc, time.Now()); err == nil {
		t.Error("ActiveFlights should error on cancelled ctx")
	}
	if _, err := s.FlightByID(cc, 1); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("FlightByID cancelled should be a non-NotFound error, got %v", err)
	}
	if _, err := s.CreateFlight(cc, CreateFlightPayload{
		Ident: "X1", ScheduledOut: time.Now(), ScheduledIn: time.Now().Add(time.Hour),
	}, 1); err == nil {
		t.Error("CreateFlight should error on cancelled ctx")
	}
	if _, err := s.UpdateFlight(cc, 1, UpdateFlightPayload{}); err == nil {
		t.Error("UpdateFlight should error on cancelled ctx")
	}
	if err := s.RefreshFlightStatus(cc, 1); err == nil {
		t.Error("RefreshFlightStatus should error on cancelled ctx")
	}
	if err := s.DeleteFlight(cc, 1); err == nil {
		t.Error("DeleteFlight should error on cancelled ctx")
	}
	if err := s.AddPassenger(cc, 1, 1); err == nil {
		t.Error("AddPassenger should error on cancelled ctx")
	}
	if err := s.RemovePassenger(cc, 1, 1); err == nil {
		t.Error("RemovePassenger should error on cancelled ctx")
	}
	if _, err := s.PassengersByFlight(cc, []int64{1}); err == nil {
		t.Error("PassengersByFlight should error on cancelled ctx")
	}
	if _, err := s.LatestRealPosition(cc, 1); err == nil {
		t.Error("LatestRealPosition should error on cancelled ctx")
	}
	if _, err := s.RecentTracks(cc, []int64{1}, 10); err == nil {
		t.Error("RecentTracks should error on cancelled ctx")
	}
	if _, err := s.LatestPositions(cc, []int64{1}); err == nil {
		t.Error("LatestPositions should error on cancelled ctx")
	}
	if _, err := s.PositionsForFlight(cc, 1, 10); err == nil {
		t.Error("PositionsForFlight should error on cancelled ctx")
	}
	if err := s.InsertPosition(cc, Position{FlightID: 1, Ts: time.Now()}); err == nil {
		t.Error("InsertPosition should error on cancelled ctx")
	}
}

func TestUserQueryErrorPaths(t *testing.T) {
	s := newStore(t)
	cc := canceled()

	if _, err := s.UserByID(cc, 1); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("UserByID cancelled should be non-NotFound error, got %v", err)
	}
	if _, err := s.UserByIdentity(cc, "github", "1"); err == nil {
		t.Error("UserByIdentity should error on cancelled ctx")
	}
	if _, err := s.UserByUsername(cc, "x"); err == nil {
		t.Error("UserByUsername should error on cancelled ctx")
	}
	if _, err := s.ListUsers(cc); err == nil {
		t.Error("ListUsers should error on cancelled ctx")
	}
	if _, err := s.InviteUser(cc, InvitePayload{Username: "x"}); err == nil {
		t.Error("InviteUser should error on cancelled ctx")
	}
	if _, err := s.UpdateUser(cc, 1, UpdateUserPayload{}); err == nil {
		t.Error("UpdateUser should error on cancelled ctx")
	}
	if err := s.DeleteUser(cc, 1); err == nil {
		t.Error("DeleteUser should error on cancelled ctx")
	}
	if _, err := s.CountUsers(cc); err == nil {
		t.Error("CountUsers should error on cancelled ctx")
	}
	if _, _, err := s.LinkLogin(cc,
		OAuthProfile{Provider: "github", ProviderUserID: "1", Username: "x"}, true); err == nil {
		t.Error("LinkLogin should error on cancelled ctx (tx.Begin)")
	}
}

// TestLinkLoginFirstQueryErrors covers the error path on the initial identity
// lookup — a non-ErrNoRows database failure. We force it by dropping the
// users / user_identities tables after Begin would otherwise succeed.
func TestLinkLoginFirstQueryErrors(t *testing.T) {
	s := newStore(t)
	if _, err := s.pool.Exec(ctx, `DROP TABLE flight_passengers, positions, flights, user_emails, user_identities, users CASCADE`); err != nil {
		t.Fatalf("drop tables: %v", err)
	}
	_, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "1", Username: "x"}, true)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("expected a real DB error, got %v", err)
	}
}

// TestLinkLoginBootstrapInsertConflict covers the bootstrap INSERT error
// branch: no identity match, no email match, no invitee match, bootstrap=true,
// but the INSERT collides with the lower(username) unique index because a
// linked user with the same username already exists.
func TestLinkLoginBootstrapInsertConflict(t *testing.T) {
	s := newStore(t)
	// Seed an existing linked user with username "dup".
	if _, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "100", Username: "dup"}, true); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Different provider_user_id, same username: no identity match, no email,
	// step-3 invitee lookup excludes users with any linked identity, so the
	// bootstrap INSERT collides on the lower(username) unique index.
	_, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "200", Username: "dup"}, true)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("expected unique-violation error, got %v", err)
	}
}
