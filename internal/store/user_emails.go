package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const userEmailColumns = `id, user_id, address, verified, verify_token,
	verify_sent_at, verified_at, created_at`

func scanUserEmail(row pgx.Row) (*UserEmail, error) {
	var e UserEmail
	if err := row.Scan(
		&e.ID, &e.UserID, &e.Address, &e.Verified,
		&e.VerifyToken, &e.VerifySentAt, &e.VerifiedAt, &e.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &e, nil
}

// UpsertVerifiedEmail inserts (or marks-verified) the given address for userID.
// Returns an error if the address is already owned by a different user.
func (s *Store) UpsertVerifiedEmail(ctx context.Context, userID int64, address string) error {
	addr := strings.TrimSpace(address)
	if addr == "" {
		return errors.New("address required")
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO user_emails (user_id, address, verified, verified_at)
		VALUES ($1, $2, TRUE, NOW())
		ON CONFLICT (lower(address)) DO UPDATE
		SET verified = TRUE, verified_at = NOW(), verify_token = NULL
		WHERE user_emails.user_id = EXCLUDED.user_id`,
		userID, addr)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("address already owned by another user")
	}
	return nil
}

// UserByVerifiedEmail looks up a user by a case-insensitive match on a
// verified email address. Returns ErrNotFound if no verified row matches.
func (s *Store) UserByVerifiedEmail(ctx context.Context, address string) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		SELECT u.id, u.github_id, u.github_login, u.name, u.avatar_url,
			u.is_superuser, u.is_active, u.last_login_at, u.created_at, u.updated_at
		FROM users u
		JOIN user_emails e ON e.user_id = u.id
		WHERE lower(e.address) = lower($1) AND e.verified = TRUE
		LIMIT 1`,
		address))
}

// EmailsByUser returns all email rows for a user, newest first.
func (s *Store) EmailsByUser(ctx context.Context, userID int64) ([]*UserEmail, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+userEmailColumns+`
		FROM user_emails WHERE user_id = $1 ORDER BY created_at DESC, id DESC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*UserEmail
	for rows.Next() {
		e, err := scanUserEmail(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
