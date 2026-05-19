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
	if _, err := s.UserByGitHubID(cc, 1); err == nil {
		t.Error("UserByGitHubID should error on cancelled ctx")
	}
	if _, err := s.UserByLogin(cc, "x"); err == nil {
		t.Error("UserByLogin should error on cancelled ctx")
	}
	if _, err := s.ListUsers(cc); err == nil {
		t.Error("ListUsers should error on cancelled ctx")
	}
	if _, err := s.InviteUser(cc, InvitePayload{GitHubLogin: "x"}); err == nil {
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
	if _, err := s.LinkLogin(cc, GitHubProfile{ID: 1, Login: "x"}, true); err == nil {
		t.Error("LinkLogin should error on cancelled ctx (tx.Begin)")
	}
}

// TestLinkLoginFirstQueryErrors covers the `default: return nil, err` branch:
// the initial github_id lookup fails with a non-ErrNoRows error. We force
// this by dropping the users table after Begin would otherwise succeed.
func TestLinkLoginFirstQueryErrors(t *testing.T) {
	s := newStore(t)
	if _, err := s.pool.Exec(ctx, `DROP TABLE flight_passengers, positions, flights, users CASCADE`); err != nil {
		t.Fatalf("drop tables: %v", err)
	}
	_, err := s.LinkLogin(ctx, GitHubProfile{ID: 1, Login: "x"}, true)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("expected a real DB error, got %v", err)
	}
}

// TestLinkLoginBootstrapInsertConflict covers the bootstrap INSERT error
// branch: no github_id match, no NULL-github_id invite match, bootstrap=true,
// but the INSERT violates the lower(github_login) unique index because a row
// with that login already exists with a non-NULL github_id.
func TestLinkLoginBootstrapInsertConflict(t *testing.T) {
	s := newStore(t)
	// Seed user A with github_id=100, login "dup" (linked, github_id NOT NULL).
	if _, err := s.LinkLogin(ctx, GitHubProfile{ID: 100, Login: "dup"}, true); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// New github_id 200, same login: first query (id=200) → NoRows; second
	// query (login=dup AND github_id IS NULL) → NoRows (A's id is non-null);
	// bootstrap insert then collides on the login unique index.
	_, err := s.LinkLogin(ctx, GitHubProfile{ID: 200, Login: "dup"}, true)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("expected unique-violation error, got %v", err)
	}
}
