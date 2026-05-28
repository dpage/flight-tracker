package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound        = errors.New("not found")
	ErrAddressTaken    = errors.New("address already registered")
	ErrAlreadyVerified = errors.New("address already verified")
	ErrUsernameTaken   = errors.New("username already registered")
)

const userColumns = `id, username, name, avatar_url,
	is_superuser, is_active, last_login_at, created_at, updated_at`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	if err := row.Scan(
		&u.ID, &u.Username, &u.Name, &u.AvatarURL,
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

// UserByIdentity looks up a user via a linked external identity. Returns
// ErrNotFound when no row matches (provider, providerUserID).
func (s *Store) UserByIdentity(ctx context.Context, provider, providerUserID string) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		SELECT `+prefixed(userColumns, "u.")+`
		FROM users u
		JOIN user_identities i ON i.user_id = u.id
		WHERE i.provider = $1 AND i.provider_user_id = $2`,
		provider, providerUserID))
}

func (s *Store) UserByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE lower(username) = lower($1)`, username))
}

// ListUsers returns all users ordered by username (case-insensitive).
func (s *Store) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY lower(username)`)
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
	Username    string
	Name        string
	IsSuperuser bool
}

func (s *Store) InviteUser(ctx context.Context, in InvitePayload) (*User, error) {
	username := strings.TrimSpace(in.Username)
	if username == "" {
		return nil, errors.New("username required")
	}
	u, err := scanUser(s.pool.QueryRow(ctx, `
		INSERT INTO users (username, name, is_superuser, is_active)
		VALUES ($1, $2, $3, TRUE)
		RETURNING `+userColumns,
		username, in.Name, in.IsSuperuser))
	if err != nil {
		// Surface the unique-violation on lower(username) as ErrUsernameTaken
		// so the handler can return a clean 409 rather than leaking pg
		// internals to the API client.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}
	return u, nil
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

// IdentitiesByUser returns every linked external identity for a user, ordered
// by provider name. Used by /api/me to render the "linked accounts" UI.
func (s *Store) IdentitiesByUser(ctx context.Context, userID int64) ([]*UserIdentity, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, provider, provider_user_id, created_at, last_used_at
		FROM user_identities
		WHERE user_id = $1
		ORDER BY provider`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*UserIdentity
	for rows.Next() {
		var i UserIdentity
		if err := rows.Scan(&i.ID, &i.UserID, &i.Provider, &i.ProviderUserID,
			&i.CreatedAt, &i.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, &i)
	}
	return out, rows.Err()
}

// OAuthProfile is the provider-agnostic payload LinkLogin consumes. Fields
// that aren't applicable for a given provider are left zero (Google has no
// stable username, for instance).
type OAuthProfile struct {
	Provider       string // e.g. "github", "google"
	ProviderUserID string // stable per-provider user identifier
	// Username is a provider-supplied handle that may be used as a fallback
	// match when an invitee row exists with no identity yet (the original
	// "superuser invites by GitHub login" flow). Empty for providers that
	// don't expose a username (Google).
	Username  string
	Name      string
	AvatarURL string
	// Email is a provider-asserted verified email, or "". Used both as a
	// fallback to find an existing user (cross-provider match by verified
	// address) and to populate user_emails on first link.
	Email string
}

// upsertEmailTx upserts a verified email row for userID inside the given
// transaction. No-op when address is empty (e.g. user hides their email).
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

// linkIdentityTx writes (or refreshes last_used_at on) the identity row that
// ties a user to a (provider, provider_user_id) pair.
func linkIdentityTx(ctx context.Context, tx pgx.Tx, userID int64, provider, providerUserID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO user_identities (user_id, provider, provider_user_id, last_used_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (provider, provider_user_id) DO UPDATE
		SET last_used_at = NOW()`,
		userID, provider, providerUserID)
	return err
}

// LinkOutcome tells the caller which branch of LinkLogin matched. The auth
// handler inspects this to decide whether to send a side-channel
// notification — specifically, LinkOutcomeCrossProvider is the case where
// a new sign-in method gets attached to an existing account via a
// verified-email match (Step 2), which the account holder ought to know
// about so they can react if it wasn't them.
type LinkOutcome int

const (
	// LinkOutcomeExisting — the (provider, provider_user_id) was already
	// known. A normal repeat sign-in.
	LinkOutcomeExisting LinkOutcome = iota
	// LinkOutcomeBootstrap — no users existed; this profile created the
	// very first user (promoted to superuser).
	LinkOutcomeBootstrap
	// LinkOutcomeInviteeLinked — an invitee row matched by username and a
	// fresh identity row was attached to it.
	LinkOutcomeInviteeLinked
	// LinkOutcomeCrossProvider — an existing user was located via a
	// verified-email match and a new identity row was attached.
	LinkOutcomeCrossProvider
)

// LinkLogin records a successful OAuth sign-in. Lookup order:
//  1. existing identity row matching (Provider, ProviderUserID) — known user,
//     fall through and refresh profile fields.
//  2. user with a verified email matching p.Email — cross-provider link
//     (e.g. existing GitHub user signs in with Google for the first time).
//     New identity row is added.
//  3. invitee row with no identity yet, matched by username — only when
//     p.Username is non-empty (i.e. GitHub providing a handle that matches
//     an invitation). New identity row is added.
//  4. no match: if bootstrapAsSuperuser is true the first user is created
//     as a superuser; otherwise ErrNotFound.
//
// Returns ErrNotFound for both "no allowlist match" and "matched user is
// inactive" — the caller surfaces the same allowlist error to the user.
//
// The returned LinkOutcome tells the caller which step matched so it can
// trigger side effects (e.g. notifying the account holder of a new linked
// identity).
func (s *Store) LinkLogin(ctx context.Context, p OAuthProfile, bootstrapAsSuperuser bool) (*User, LinkOutcome, error) {
	if p.Provider == "" || p.ProviderUserID == "" {
		return nil, 0, errors.New("provider and provider_user_id required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)

	u, step, err := s.findUserForLogin(ctx, tx, p)
	if err != nil {
		return nil, 0, err
	}

	outcome := LinkOutcomeExisting
	switch step {
	case findStepEmailMatch:
		outcome = LinkOutcomeCrossProvider
	case findStepInviteeMatch:
		outcome = LinkOutcomeInviteeLinked
	}

	if u == nil {
		// No match anywhere — bootstrap path.
		if !bootstrapAsSuperuser {
			return nil, 0, ErrNotFound
		}
		username := p.Username
		if username == "" {
			// Google etc. don't expose a username; fall back to the local
			// part of the email, then to "user".
			if at := strings.IndexByte(p.Email, '@'); at > 0 {
				username = p.Email[:at]
			} else {
				username = "user"
			}
		}
		row := tx.QueryRow(ctx, `
			INSERT INTO users (username, name, avatar_url,
				is_superuser, is_active, last_login_at)
			VALUES ($1, $2, $3, TRUE, TRUE, NOW())
			RETURNING `+userColumns,
			username, p.Name, p.AvatarURL)
		u = &User{}
		if err := row.Scan(&u.ID, &u.Username, &u.Name, &u.AvatarURL,
			&u.IsSuperuser, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, 0, err
		}
		outcome = LinkOutcomeBootstrap
	} else {
		if !u.IsActive {
			return nil, 0, ErrNotFound
		}
		err = tx.QueryRow(ctx, `
			UPDATE users SET
				name = CASE WHEN $2 = '' THEN name ELSE $2 END,
				avatar_url = CASE WHEN $3 = '' THEN avatar_url ELSE $3 END,
				last_login_at = NOW(),
				updated_at = NOW()
			WHERE id = $1
			RETURNING `+userColumns,
			u.ID, p.Name, p.AvatarURL,
		).Scan(&u.ID, &u.Username, &u.Name, &u.AvatarURL,
			&u.IsSuperuser, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
		if err != nil {
			return nil, 0, err
		}
	}

	if outcome == LinkOutcomeExisting {
		// Step 1 path: identity row already exists; just bump last_used_at.
		if _, err := tx.Exec(ctx,
			`UPDATE user_identities SET last_used_at = NOW()
			 WHERE provider = $1 AND provider_user_id = $2`,
			p.Provider, p.ProviderUserID); err != nil {
			return nil, 0, err
		}
	} else {
		if err := linkIdentityTx(ctx, tx, u.ID, p.Provider, p.ProviderUserID); err != nil {
			return nil, 0, err
		}
	}
	if err := upsertEmailTx(ctx, tx, u.ID, p.Email); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return u, outcome, nil
}

// findStep encodes which branch in findUserForLogin matched.
type findStep int

const (
	findStepNone findStep = iota
	findStepIdentityMatch
	findStepEmailMatch
	findStepInviteeMatch
)

// findUserForLogin runs the three lookup steps documented on LinkLogin and
// returns (user, step, err). step is findStepNone only when user is nil.
func (s *Store) findUserForLogin(ctx context.Context, tx pgx.Tx, p OAuthProfile) (*User, findStep, error) {
	// Step 1: existing identity row.
	u := &User{}
	err := tx.QueryRow(ctx, `
		SELECT `+prefixed(userColumns, "u.")+`
		FROM users u
		JOIN user_identities i ON i.user_id = u.id
		WHERE i.provider = $1 AND i.provider_user_id = $2`,
		p.Provider, p.ProviderUserID,
	).Scan(&u.ID, &u.Username, &u.Name, &u.AvatarURL,
		&u.IsSuperuser, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if err == nil {
		return u, findStepIdentityMatch, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, findStepNone, err
	}

	// Step 2: cross-provider match by verified email.
	if p.Email != "" {
		u = &User{}
		err = tx.QueryRow(ctx, `
			SELECT `+prefixed(userColumns, "u.")+`
			FROM users u
			JOIN user_emails e ON e.user_id = u.id
			WHERE lower(e.address) = lower($1) AND e.verified = TRUE
			LIMIT 1`,
			p.Email,
		).Scan(&u.ID, &u.Username, &u.Name, &u.AvatarURL,
			&u.IsSuperuser, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
		if err == nil {
			return u, findStepEmailMatch, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, findStepNone, err
		}
	}

	// Step 3: invitee row matched by username (when the provider gives one
	// and the row has no identities linked yet). Falling back here lets a
	// superuser pre-create a row by username before the user first signs
	// in via GitHub.
	if p.Username != "" {
		u = &User{}
		err = tx.QueryRow(ctx, `
			SELECT `+userColumns+` FROM users
			WHERE lower(username) = lower($1)
			  AND NOT EXISTS (SELECT 1 FROM user_identities WHERE user_id = users.id)`,
			p.Username,
		).Scan(&u.ID, &u.Username, &u.Name, &u.AvatarURL,
			&u.IsSuperuser, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
		if err == nil {
			return u, findStepInviteeMatch, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, findStepNone, err
		}
	}

	return nil, findStepNone, nil
}

// prefixed prepends `prefix` to each comma-separated column in `cols` so
// the same column list can be reused inside JOIN queries.
func prefixed(cols, prefix string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}
