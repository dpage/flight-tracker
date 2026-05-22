-- Verified email addresses per user. OAuth-provided addresses are inserted
-- pre-verified on sign-in; additional addresses (added via a future UI)
-- start unverified and require a click-through.
CREATE TABLE user_emails (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    address         TEXT NOT NULL,
    verified        BOOLEAN NOT NULL DEFAULT FALSE,
    verify_token    TEXT,
    verify_sent_at  TIMESTAMPTZ,
    verified_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX user_emails_address_lower_idx ON user_emails (lower(address));
CREATE INDEX user_emails_user_idx ON user_emails (user_id);

-- Audit trail. One row per processed (or rejected) inbound message.
CREATE TABLE email_ingests (
    id              BIGSERIAL PRIMARY KEY,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    message_id      TEXT,
    from_address    TEXT NOT NULL,
    subject         TEXT NOT NULL DEFAULT '',
    dkim_pass       BOOLEAN NOT NULL DEFAULT FALSE,
    user_id         BIGINT REFERENCES users(id) ON DELETE SET NULL,
    status          TEXT NOT NULL,
    flights_added   INT NOT NULL DEFAULT 0,
    flights_failed  INT NOT NULL DEFAULT 0,
    error           TEXT NOT NULL DEFAULT ''
);
CREATE INDEX email_ingests_user_idx ON email_ingests (user_id, received_at DESC);
CREATE INDEX email_ingests_received_idx ON email_ingests (received_at DESC);
