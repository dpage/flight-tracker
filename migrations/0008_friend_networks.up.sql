-- Friend networks: replace the invite-only allowlist with an open signup
-- model where flight visibility is scoped to your accepted friendships.
--
--   friendships              - one row per pair, canonical (user_low < user_high)
--                              to avoid duplicates. status is 'pending' until
--                              the recipient (the user_id != requested_by)
--                              accepts. Declines delete the row.
--   pending_friend_invites   - friend requests addressed at an email that
--                              isn't yet a verified user_emails address.
--                              Consumed on first sign-in by the new owner of
--                              that address.
--
-- At migration time everyone-is-friends-with-everyone — the previous model
-- effectively gave every user visibility into every flight. We preserve that
-- by seeding the cartesian product as 'accepted' friendships.

CREATE TABLE friendships (
    user_low      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_high     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status        TEXT NOT NULL CHECK (status IN ('pending', 'accepted')),
    requested_by  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    requested_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    accepted_at   TIMESTAMPTZ,
    PRIMARY KEY (user_low, user_high),
    CHECK (user_low < user_high),
    CHECK (requested_by = user_low OR requested_by = user_high)
);

CREATE INDEX friendships_user_low_idx  ON friendships (user_low);
CREATE INDEX friendships_user_high_idx ON friendships (user_high);

CREATE TABLE pending_friend_invites (
    email_lower   TEXT   NOT NULL,
    inviter_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message       TEXT   NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (email_lower, inviter_id)
);

CREATE INDEX pending_friend_invites_email_idx ON pending_friend_invites (email_lower);

INSERT INTO friendships (user_low, user_high, status, requested_by, accepted_at)
SELECT u1.id, u2.id, 'accepted', u1.id, NOW()
FROM users u1
JOIN users u2 ON u1.id < u2.id;
