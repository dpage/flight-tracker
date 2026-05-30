package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
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

// validCalendarScope reports whether scope is one of the three feed scopes.
func validCalendarScope(scope string) bool {
	switch scope {
	case "me", "trip", "plan":
		return true
	default:
		return false
	}
}

// CalendarToken returns the user's token for the given scope, issuing one if
// absent. The (user_id, scope) primary key means a user has at most one token
// per scope; the per-scope token authenticates the user, and the feed URL
// carries the trip/plan id.
func (s *Store) CalendarToken(ctx context.Context, userID int64, scope string) (*CalendarToken, error) {
	if !validCalendarScope(scope) {
		return nil, errors.New("invalid calendar scope")
	}
	var ct CalendarToken
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, scope, token, created_at
		   FROM calendar_tokens WHERE user_id = $1 AND scope = $2`,
		userID, scope,
	).Scan(&ct.UserID, &ct.Scope, &ct.Token, &ct.CreatedAt)
	if err == nil {
		return &ct, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	// No token yet — issue one.
	return s.RegenerateCalendarToken(ctx, userID, scope)
}

// RegenerateCalendarToken issues a fresh token for the scope, revoking the old
// one (the unique (user_id, scope) row is overwritten, so the prior feed URL
// stops authenticating).
func (s *Store) RegenerateCalendarToken(ctx context.Context, userID int64, scope string) (*CalendarToken, error) {
	if !validCalendarScope(scope) {
		return nil, errors.New("invalid calendar scope")
	}
	tok, err := generateToken()
	if err != nil {
		return nil, err
	}
	var ct CalendarToken
	err = s.pool.QueryRow(ctx,
		`INSERT INTO calendar_tokens (user_id, scope, token, created_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (user_id, scope)
		   DO UPDATE SET token = EXCLUDED.token, created_at = NOW()
		 RETURNING user_id, scope, token, created_at`,
		userID, scope, tok,
	).Scan(&ct.UserID, &ct.Scope, &ct.Token, &ct.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &ct, nil
}

// ListCalendarTokens returns all of the user's per-scope tokens (0..3 rows),
// ordered by scope for stable output.
func (s *Store) ListCalendarTokens(ctx context.Context, userID int64) ([]*CalendarToken, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, scope, token, created_at
		   FROM calendar_tokens WHERE user_id = $1 ORDER BY scope`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CalendarToken
	for rows.Next() {
		var ct CalendarToken
		if err := rows.Scan(&ct.UserID, &ct.Scope, &ct.Token, &ct.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &ct)
	}
	return out, rows.Err()
}

// RevokeCalendarToken deletes the named token if it belongs to userID. Scoping
// the delete to the owner means one user cannot revoke another's feed by
// guessing the token string. Returns ErrNotFound if no such row was the
// caller's.
func (s *Store) RevokeCalendarToken(ctx context.Context, userID int64, token string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM calendar_tokens WHERE user_id = $1 AND token = $2`, userID, token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UserByCalendarToken resolves a feed token to the owning user id, or
// ErrNotFound if the token is unknown.
func (s *Store) UserByCalendarToken(ctx context.Context, token string) (int64, error) {
	var uid int64
	err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM calendar_tokens WHERE token = $1`, token).Scan(&uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return uid, nil
}

// CalendarEvent is one row's worth of data the ICS renderer needs for a
// VEVENT: the part's time range and place plus the owning plan's identity
// fields. It is assembled here (visibility-gated) rather than depending on the
// backend-core DTO assembly, so the feed is self-contained.
type CalendarEvent struct {
	PartID          int64
	PlanID          int64
	Type            string // plan type: flight|train|hotel|...
	Title           string
	ConfirmationRef string
	Notes           string
	StartsAt        time.Time
	EndsAt          *time.Time
	StartTZ         string
	EndTZ           string
	StartLabel      string
	EndLabel        string
	Status          string // planned|confirmed|cancelled
	UpdatedAt       time.Time
}

// calendarEventSelect is the shared projection + visibility-gated FROM/WHERE
// for the three feed scopes. $1 is always the viewer (token owner). The
// predicate mirrors spec §4 exactly: the viewer must be on the trip (or own
// it), and the plan must not be hidden from them — so a plan hidden from the
// token owner is absent from every feed, and another user's token (a different
// $1) can never surface the owner's private plans.
const calendarEventSelect = `
	SELECT part.id, part.plan_id, pl.type, pl.title, pl.confirmation_ref,
	       pl.notes, part.starts_at, part.ends_at, part.start_tz, part.end_tz,
	       part.start_label, part.end_label, part.status, part.updated_at
	  FROM plan_parts part
	  JOIN plans pl ON pl.id = part.plan_id
	  JOIN trips t ON t.id = pl.trip_id
	 WHERE part.dismissed_at IS NULL
	   AND (
	        t.created_by = $1
	     OR (
	          EXISTS (SELECT 1 FROM trip_members tm
	                  WHERE tm.trip_id = pl.trip_id AND tm.user_id = $1)
	          AND (
	               pl.created_by = $1
	            OR EXISTS (SELECT 1 FROM plan_passengers pp
	                       WHERE pp.plan_id = pl.id AND pp.user_id = $1)
	            OR NOT EXISTS (SELECT 1 FROM plan_visibility pv WHERE pv.plan_id = pl.id)
	            OR EXISTS (SELECT 1 FROM plan_visibility pv
	                       WHERE pv.plan_id = pl.id AND pv.mode = 'hidden_from'
	                         AND NOT EXISTS (SELECT 1 FROM plan_visibility_members m
	                                         WHERE m.plan_id = pl.id AND m.user_id = $1))
	            OR EXISTS (SELECT 1 FROM plan_visibility pv
	                       JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
	                       WHERE pv.plan_id = pl.id AND pv.mode = 'only_visible_to'
	                         AND m.user_id = $1)
	          )
	        )
	   )`

func scanCalendarEvents(rows pgx.Rows) ([]*CalendarEvent, error) {
	defer rows.Close()
	var out []*CalendarEvent
	for rows.Next() {
		var e CalendarEvent
		if err := rows.Scan(&e.PartID, &e.PlanID, &e.Type, &e.Title,
			&e.ConfirmationRef, &e.Notes, &e.StartsAt, &e.EndsAt,
			&e.StartTZ, &e.EndTZ, &e.StartLabel, &e.EndLabel,
			&e.Status, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// CalendarEventsForUser returns every plan_part the viewer may see across all
// their trips — the "me" feed.
func (s *Store) CalendarEventsForUser(ctx context.Context, viewerID int64) ([]*CalendarEvent, error) {
	rows, err := s.pool.Query(ctx,
		calendarEventSelect+` ORDER BY part.starts_at ASC, part.id ASC`, viewerID)
	if err != nil {
		return nil, err
	}
	return scanCalendarEvents(rows)
}

// CalendarEventsForTrip returns the viewer-visible plan_parts of one trip — the
// "trip" feed. Parts the viewer can't see are silently omitted, so a hidden
// plan never leaks even within a trip the viewer is on.
func (s *Store) CalendarEventsForTrip(ctx context.Context, viewerID, tripID int64) ([]*CalendarEvent, error) {
	rows, err := s.pool.Query(ctx,
		calendarEventSelect+` AND pl.trip_id = $2 ORDER BY part.starts_at ASC, part.id ASC`,
		viewerID, tripID)
	if err != nil {
		return nil, err
	}
	return scanCalendarEvents(rows)
}

// CalendarEventsForPlan returns the viewer-visible plan_parts of one plan — the
// single-plan feed (stays live so a delayed flight updates on next refresh). If
// the plan is not visible to the viewer the result is empty.
func (s *Store) CalendarEventsForPlan(ctx context.Context, viewerID, planID int64) ([]*CalendarEvent, error) {
	rows, err := s.pool.Query(ctx,
		calendarEventSelect+` AND pl.id = $2 ORDER BY part.starts_at ASC, part.id ASC`,
		viewerID, planID)
	if err != nil {
		return nil, err
	}
	return scanCalendarEvents(rows)
}
