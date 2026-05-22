package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// generateToken returns a 32-byte cryptographically-random URL-safe token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ResendVerification regenerates verify_token and bumps verify_sent_at for
// an unverified row owned by userID. Returns ErrNotFound when the row
// doesn't exist or is owned by someone else; ErrAlreadyVerified when the
// row is already verified.
func (s *Store) ResendVerification(ctx context.Context, userID, emailID int64) (*UserEmail, string, error) {
	// First, fetch the row to distinguish "not yours" (ErrNotFound) from
	// "already verified" (ErrAlreadyVerified). The handler maps these to
	// different HTTP status codes.
	row, err := scanUserEmail(s.pool.QueryRow(ctx,
		`SELECT `+userEmailColumns+`
		FROM user_emails WHERE id = $1 AND user_id = $2`, emailID, userID))
	if err != nil {
		return nil, "", err
	}
	if row.Verified {
		return nil, "", ErrAlreadyVerified
	}
	token, err := generateToken()
	if err != nil {
		return nil, "", err
	}
	updated, err := scanUserEmail(s.pool.QueryRow(ctx, `
		UPDATE user_emails
		SET verify_token = $3, verify_sent_at = NOW()
		WHERE id = $1 AND user_id = $2
		RETURNING `+userEmailColumns,
		emailID, userID, token))
	if err != nil {
		return nil, "", err
	}
	return updated, token, nil
}

// VerifyEmailByToken flips an unverified row matching token to verified.
// Returns ErrNotFound when the token is unknown, already consumed, or
// older than 24 hours. The update is atomic — the token is cleared as
// part of the same statement so a second call returns ErrNotFound.
func (s *Store) VerifyEmailByToken(ctx context.Context, token string) (*UserEmail, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	row, err := scanUserEmail(s.pool.QueryRow(ctx, `
		UPDATE user_emails
		SET verified = TRUE, verified_at = NOW(), verify_token = NULL
		WHERE verify_token = $1
		  AND verify_sent_at > NOW() - INTERVAL '24 hours'
		RETURNING `+userEmailColumns,
		token))
	if err != nil {
		return nil, err
	}
	return row, nil
}

// InsertUnverifiedEmail inserts a new email row for userID with a fresh
// random verify_token and verify_sent_at = NOW(). Returns the row and the
// raw token (so the caller can embed it in a verification URL). The token
// is not exposed by the store outside of this return value.
//
// Returns ErrAddressTaken if the address (case-insensitive) is already
// owned by any user — including userID itself.
func (s *Store) InsertUnverifiedEmail(ctx context.Context, userID int64, address string) (*UserEmail, string, error) {
	addr := strings.TrimSpace(address)
	if addr == "" {
		return nil, "", errors.New("address required")
	}
	token, err := generateToken()
	if err != nil {
		return nil, "", err
	}
	row, err := scanUserEmail(s.pool.QueryRow(ctx, `
		INSERT INTO user_emails (user_id, address, verified, verify_token, verify_sent_at)
		VALUES ($1, $2, FALSE, $3, NOW())
		RETURNING `+userEmailColumns,
		userID, addr, token))
	if err != nil {
		// Surface the unique-violation on lower(address) as ErrAddressTaken.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, "", ErrAddressTaken
		}
		return nil, "", err
	}
	return row, token, nil
}
