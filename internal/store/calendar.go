package store

import (
	"context"
	"time"
)

// CalendarToken is a per-user, per-scope secret used to authenticate the
// read-only iCal feeds (not the session cookie, since calendar clients won't
// carry it). Regenerating revokes the prior feed URL.
type CalendarToken struct {
	UserID    int64
	Scope     string // "me" | "trip" | "plan"
	Token     string
	CreatedAt time.Time
}

// CalendarToken returns the user's token for the given scope, issuing one if
// absent (filled in by Wave 1D).
func (s *Store) CalendarToken(ctx context.Context, userID int64, scope string) (*CalendarToken, error) {
	return nil, ErrNotImplemented
}

// RegenerateCalendarToken issues a fresh token for the scope, revoking the old
// one.
func (s *Store) RegenerateCalendarToken(ctx context.Context, userID int64, scope string) (*CalendarToken, error) {
	return nil, ErrNotImplemented
}

// UserByCalendarToken resolves a feed token to the owning user id, or
// ErrNotFound if the token is unknown.
func (s *Store) UserByCalendarToken(ctx context.Context, token string) (int64, error) {
	return 0, ErrNotImplemented
}
