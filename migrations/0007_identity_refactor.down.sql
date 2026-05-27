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
