package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// AlertPrefs is a user's per-channel alert configuration. MinDelayMin
// suppresses time changes below the threshold.
type AlertPrefs struct {
	UserID      int64
	InApp       bool
	Email       bool
	MinDelayMin int
}

// defaultAlertPrefs mirrors the alert_prefs column defaults (spec §9 / PRD
// §6.8): in-app + email on, 15-minute delay threshold. Returned by
// AlertPrefsFor when a user has no explicit row, and used as the upsert base.
func defaultAlertPrefs(userID int64) AlertPrefs {
	return AlertPrefs{UserID: userID, InApp: true, Email: true, MinDelayMin: 15}
}

// AlertPrefsFor returns a user's alert preferences, defaulting to the column
// defaults (in-app + email on, 15-minute threshold) when no row exists.
func (s *Store) AlertPrefsFor(ctx context.Context, userID int64) (*AlertPrefs, error) {
	p := defaultAlertPrefs(userID)
	err := s.pool.QueryRow(ctx, `
		SELECT in_app, email, min_delay_min
		FROM alert_prefs WHERE user_id = $1`, userID).
		Scan(&p.InApp, &p.Email, &p.MinDelayMin)
	if errors.Is(err, pgx.ErrNoRows) {
		return &p, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SetAlertPrefs upserts a user's alert preferences.
func (s *Store) SetAlertPrefs(ctx context.Context, in AlertPrefs) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO alert_prefs (user_id, in_app, email, min_delay_min)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE
		SET in_app = EXCLUDED.in_app,
		    email = EXCLUDED.email,
		    min_delay_min = EXCLUDED.min_delay_min`,
		in.UserID, in.InApp, in.Email, in.MinDelayMin)
	return err
}

// AddPlanAlertOptin records a viewer opting in to a plan's alerts. Idempotent.
func (s *Store) AddPlanAlertOptin(ctx context.Context, planID, userID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO plan_alert_optin (plan_id, user_id) VALUES ($1, $2)
		ON CONFLICT (plan_id, user_id) DO NOTHING`, planID, userID)
	return err
}

// RemovePlanAlertOptin removes a viewer's opt-in to a plan's alerts. A no-op
// (no error) when the row doesn't exist.
func (s *Store) RemovePlanAlertOptin(ctx context.Context, planID, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM plan_alert_optin WHERE plan_id = $1 AND user_id = $2`,
		planID, userID)
	return err
}

// AlertRecipients returns the user IDs to alert for a plan: the plan owner,
// its passengers, and opted-in viewers, before per-user alert_prefs filtering.
func (s *Store) AlertRecipients(ctx context.Context, planID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT uid FROM (
			SELECT created_by AS uid FROM plans WHERE id = $1 AND created_by IS NOT NULL
			UNION
			SELECT user_id AS uid FROM plan_passengers WHERE plan_id = $1
			UNION
			SELECT user_id AS uid FROM plan_alert_optin WHERE plan_id = $1
		) r
		ORDER BY uid`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}

// AlertRecipient is one resolved alert target: a recipient user joined with
// their effective alert_prefs (column defaults when no row exists) and the
// best available verified email address (empty when none is verified).
type AlertRecipient struct {
	UserID      int64
	InApp       bool
	Email       bool
	MinDelayMin int
	EmailAddr   string // newest verified address, "" when none
}

// AlertRecipientsWithPrefs returns the plan's recipient set (owner + passengers
// + opted-in viewers) with each recipient's effective alert_prefs and verified
// email folded in, so the poller can decide per-channel delivery in one query.
// LEFT JOINs keep the column defaults for users without an alert_prefs row.
func (s *Store) AlertRecipientsWithPrefs(ctx context.Context, planID int64) ([]AlertRecipient, error) {
	rows, err := s.pool.Query(ctx, `
		WITH recip AS (
			SELECT created_by AS uid FROM plans WHERE id = $1 AND created_by IS NOT NULL
			UNION
			SELECT user_id AS uid FROM plan_passengers WHERE plan_id = $1
			UNION
			SELECT user_id AS uid FROM plan_alert_optin WHERE plan_id = $1
		)
		SELECT r.uid,
		       COALESCE(ap.in_app, TRUE),
		       COALESCE(ap.email, TRUE),
		       COALESCE(ap.min_delay_min, 15),
		       COALESCE((
		           SELECT e.address FROM user_emails e
		           WHERE e.user_id = r.uid AND e.verified = TRUE
		           ORDER BY e.verified_at DESC NULLS LAST, e.id DESC
		           LIMIT 1
		       ), '')
		FROM (SELECT DISTINCT uid FROM recip) r
		LEFT JOIN alert_prefs ap ON ap.user_id = r.uid
		ORDER BY r.uid`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRecipient
	for rows.Next() {
		var ar AlertRecipient
		if err := rows.Scan(&ar.UserID, &ar.InApp, &ar.Email, &ar.MinDelayMin, &ar.EmailAddr); err != nil {
			return nil, err
		}
		out = append(out, ar)
	}
	return out, rows.Err()
}

// FlightPartAlertSig reads the stored last-alerted dedupe signature for a flight
// part. ok is false when the column is NULL (never alerted). ErrNotFound when
// the part has no flight_details row.
func (s *Store) FlightPartAlertSig(ctx context.Context, partID int64) (sig string, ok bool, err error) {
	var v *string
	err = s.pool.QueryRow(ctx,
		`SELECT last_alert_sig FROM flight_details WHERE plan_part_id = $1`, partID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, ErrNotFound
	}
	if err != nil {
		return "", false, err
	}
	if v == nil {
		return "", false, nil
	}
	return *v, true, nil
}

// SetFlightPartAlertSig stamps the last-alerted dedupe signature for a flight
// part so an unchanged state on the next poll tick doesn't re-alert.
func (s *Store) SetFlightPartAlertSig(ctx context.Context, partID int64, sig string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE flight_details SET last_alert_sig = $2 WHERE plan_part_id = $1`,
		partID, sig)
	return err
}
