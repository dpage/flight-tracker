-- Decouple the users table from GitHub-specific columns so OAuth identities
-- can come from multiple providers (GitHub, Google, ...) and so a future
-- username+password flow has a place to live.
--
--   users.username        - chosen display handle, shown throughout the UI
--                           (used to be users.github_login).
--   user_identities       - (provider, provider_user_id) -> user_id, one
--                           row per linked external account.
--
-- Every existing user gets a 'github' identity row backfilled from the old
-- users.github_id; users invited but never signed in keep their username and
-- just have no identity row yet (first OAuth login links it).

ALTER TABLE users ADD COLUMN username TEXT NOT NULL DEFAULT '';
UPDATE users SET username = github_login;
ALTER TABLE users ALTER COLUMN username DROP DEFAULT;

DROP INDEX users_github_login_lower_idx;
CREATE UNIQUE INDEX users_username_lower_idx ON users (lower(username));

CREATE TABLE user_identities (
    id                BIGSERIAL PRIMARY KEY,
    user_id           BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider          TEXT NOT NULL,
    provider_user_id  TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at      TIMESTAMPTZ,
    UNIQUE (provider, provider_user_id)
);
CREATE INDEX user_identities_user_id_idx ON user_identities (user_id);

INSERT INTO user_identities (user_id, provider, provider_user_id, last_used_at, created_at)
SELECT id, 'github', github_id::TEXT, last_login_at, created_at
FROM users
WHERE github_id IS NOT NULL;

ALTER TABLE users DROP COLUMN github_id;
ALTER TABLE users DROP COLUMN github_login;
