package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrNotFound = errors.New("not found")

const userColumns = `id, github_id, github_login, name, avatar_url,
	is_superuser, is_active, last_login_at, created_at, updated_at`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	if err := row.Scan(
		&u.ID, &u.GitHubID, &u.GitHubLogin, &u.Name, &u.AvatarURL,
		&u.IsSuperuser, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

func (s *Store) UserByGitHubID(ctx context.Context, gid int64) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE github_id = $1`, gid))
}

func (s *Store) UserByLogin(ctx context.Context, login string) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE lower(github_login) = lower($1)`, login))
}

// ListUsers returns all users ordered by login (case-insensitive).
func (s *Store) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY lower(github_login)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// InvitePayload is the input for InviteUser (superuser pre-creating a row).
type InvitePayload struct {
	GitHubLogin string
	Name        string
	IsSuperuser bool
}

func (s *Store) InviteUser(ctx context.Context, in InvitePayload) (*User, error) {
	login := strings.TrimSpace(in.GitHubLogin)
	if login == "" {
		return nil, errors.New("github_login required")
	}
	return scanUser(s.pool.QueryRow(ctx, `
		INSERT INTO users (github_login, name, is_superuser, is_active)
		VALUES ($1, $2, $3, TRUE)
		RETURNING `+userColumns,
		login, in.Name, in.IsSuperuser))
}

type UpdateUserPayload struct {
	Name        *string
	IsSuperuser *bool
	IsActive    *bool
}

func (s *Store) UpdateUser(ctx context.Context, id int64, in UpdateUserPayload) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		UPDATE users SET
			name = COALESCE($2, name),
			is_superuser = COALESCE($3, is_superuser),
			is_active = COALESCE($4, is_active),
			updated_at = NOW()
		WHERE id = $1
		RETURNING `+userColumns,
		id, in.Name, in.IsSuperuser, in.IsActive))
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountUsers returns the total number of users (used for bootstrap detection).
func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// GitHubProfile is the subset of /user GitHub returns that we persist.
// Email is the primary verified email from /user/emails; empty if none.
type GitHubProfile struct {
	ID        int64
	Login     string
	Name      string
	AvatarURL string
	Email     string
}

// upsertEmailTx upserts a verified email row for userID inside the given
// transaction. No-op when address is empty (e.g. user hides their email on
// GitHub).
func upsertEmailTx(ctx context.Context, tx pgx.Tx, userID int64, address string) error {
	addr := strings.TrimSpace(address)
	if addr == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO user_emails (user_id, address, verified, verified_at)
		VALUES ($1, $2, TRUE, NOW())
		ON CONFLICT (lower(address)) DO UPDATE
		SET verified = TRUE, verified_at = NOW(), verify_token = NULL
		WHERE user_emails.user_id = EXCLUDED.user_id`,
		userID, addr)
	return err
}

// LinkLogin records a successful GitHub sign-in. It looks up the user by
// github_id, falling back to lower(github_login) (the invite case), updates
// profile fields, and bumps last_login_at. If no user exists at all and
// bootstrapAsSuperuser is true, it creates the first user as a superuser.
// Returns ErrNotFound if the login is not on the allowlist.
func (s *Store) LinkLogin(ctx context.Context, p GitHubProfile, bootstrapAsSuperuser bool) (*User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var u User
	err = tx.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE github_id = $1`, p.ID,
	).Scan(&u.ID, &u.GitHubID, &u.GitHubLogin, &u.Name, &u.AvatarURL,
		&u.IsSuperuser, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)

	switch {
	case err == nil:
		// known user — fall through and update.
	case errors.Is(err, pgx.ErrNoRows):
		err = tx.QueryRow(ctx,
			`SELECT `+userColumns+` FROM users WHERE lower(github_login) = lower($1) AND github_id IS NULL`,
			p.Login,
		).Scan(&u.ID, &u.GitHubID, &u.GitHubLogin, &u.Name, &u.AvatarURL,
			&u.IsSuperuser, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			if !bootstrapAsSuperuser {
				return nil, ErrNotFound
			}
			err = tx.QueryRow(ctx, `
				INSERT INTO users (github_id, github_login, name, avatar_url,
					is_superuser, is_active, last_login_at)
				VALUES ($1, $2, $3, $4, TRUE, TRUE, NOW())
				RETURNING `+userColumns,
				p.ID, p.Login, p.Name, p.AvatarURL,
			).Scan(&u.ID, &u.GitHubID, &u.GitHubLogin, &u.Name, &u.AvatarURL,
				&u.IsSuperuser, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
			if err != nil {
				return nil, err
			}
			if err := upsertEmailTx(ctx, tx, u.ID, p.Email); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return &u, nil
		}
		if err != nil {
			return nil, err
		}
	default:
		return nil, err
	}

	if !u.IsActive {
		return nil, ErrNotFound
	}

	err = tx.QueryRow(ctx, `
		UPDATE users SET
			github_id = $2,
			github_login = $3,
			name = CASE WHEN $4 = '' THEN name ELSE $4 END,
			avatar_url = CASE WHEN $5 = '' THEN avatar_url ELSE $5 END,
			last_login_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
		RETURNING `+userColumns,
		u.ID, p.ID, p.Login, p.Name, p.AvatarURL,
	).Scan(&u.ID, &u.GitHubID, &u.GitHubLogin, &u.Name, &u.AvatarURL,
		&u.IsSuperuser, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := upsertEmailTx(ctx, tx, u.ID, p.Email); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &u, nil
}
