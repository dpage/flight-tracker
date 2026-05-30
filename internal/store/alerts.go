package store

import "context"

// AlertPrefs is a user's per-channel alert configuration. MinDelayMin
// suppresses time changes below the threshold.
type AlertPrefs struct {
	UserID      int64
	InApp       bool
	Email       bool
	MinDelayMin int
}

// AlertPrefsFor returns a user's alert preferences, defaulting to the column
// defaults (in-app + email on, 15-minute threshold) when no row exists.
func (s *Store) AlertPrefsFor(ctx context.Context, userID int64) (*AlertPrefs, error) {
	return nil, ErrNotImplemented
}

// SetAlertPrefs upserts a user's alert preferences.
func (s *Store) SetAlertPrefs(ctx context.Context, in AlertPrefs) error {
	return ErrNotImplemented
}

// AddPlanAlertOptin records a viewer opting in to a plan's alerts.
func (s *Store) AddPlanAlertOptin(ctx context.Context, planID, userID int64) error {
	return ErrNotImplemented
}

// RemovePlanAlertOptin removes a viewer's opt-in to a plan's alerts.
func (s *Store) RemovePlanAlertOptin(ctx context.Context, planID, userID int64) error {
	return ErrNotImplemented
}

// AlertRecipients returns the user IDs to alert for a plan: the plan owner,
// its passengers, and opted-in viewers, before per-user alert_prefs filtering.
func (s *Store) AlertRecipients(ctx context.Context, planID int64) ([]int64, error) {
	return nil, ErrNotImplemented
}
