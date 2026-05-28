-- WARNING: this migration is lossy. Non-github identities (google, dev,
-- and anything added later) are dropped along with user_identities. It
-- also assumes every github provider_user_id fits in a BIGINT — true for
-- real GitHub IDs, but the synthetic IDs RegisterDevLogin writes are
-- uint64 and may overflow. Only run this if you're certain the live
-- database holds nothing but real GitHub identities.

ALTER TABLE users ADD COLUMN github_id BIGINT UNIQUE;
ALTER TABLE users ADD COLUMN github_login TEXT NOT NULL DEFAULT '';

UPDATE users SET github_login = username;

UPDATE users u
SET github_id = i.provider_user_id::BIGINT
FROM user_identities i
WHERE i.user_id = u.id AND i.provider = 'github';

ALTER TABLE users ALTER COLUMN github_login DROP DEFAULT;

DROP INDEX users_username_lower_idx;
CREATE UNIQUE INDEX users_github_login_lower_idx ON users (lower(github_login));

DROP TABLE user_identities;

ALTER TABLE users DROP COLUMN username;
