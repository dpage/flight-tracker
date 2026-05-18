CREATE TABLE users (
    id              BIGSERIAL PRIMARY KEY,
    -- Null until the user first signs in. A superuser can pre-create an
    -- invitee row by github_login alone; first sign-in fills github_id in.
    github_id       BIGINT UNIQUE,
    github_login    TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    avatar_url      TEXT NOT NULL DEFAULT '',
    is_superuser    BOOLEAN NOT NULL DEFAULT FALSE,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX users_github_login_lower_idx ON users (lower(github_login));

CREATE TABLE flights (
    id              BIGSERIAL PRIMARY KEY,
    ident           TEXT NOT NULL,
    scheduled_out   TIMESTAMPTZ NOT NULL,
    scheduled_in   TIMESTAMPTZ NOT NULL,
    estimated_out   TIMESTAMPTZ,
    estimated_in    TIMESTAMPTZ,
    actual_out      TIMESTAMPTZ,
    actual_in       TIMESTAMPTZ,
    origin_iata     TEXT NOT NULL DEFAULT '',
    origin_lat      DOUBLE PRECISION,
    origin_lon      DOUBLE PRECISION,
    dest_iata       TEXT NOT NULL DEFAULT '',
    dest_lat        DOUBLE PRECISION,
    dest_lon        DOUBLE PRECISION,
    status          TEXT NOT NULL DEFAULT 'Scheduled',
    -- The AeroAPI fa_flight_id, set once resolved from the ident.
    aeroapi_id      TEXT,
    last_polled_at  TIMESTAMPTZ,
    created_by      BIGINT REFERENCES users(id) ON DELETE SET NULL,
    notes           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX flights_scheduled_out_idx ON flights (scheduled_out);
CREATE INDEX flights_active_idx ON flights (scheduled_in)
    WHERE status NOT IN ('Arrived', 'Cancelled');

CREATE TABLE flight_passengers (
    flight_id   BIGINT NOT NULL REFERENCES flights(id) ON DELETE CASCADE,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (flight_id, user_id)
);

CREATE INDEX flight_passengers_user_idx ON flight_passengers (user_id);

CREATE TABLE positions (
    id              BIGSERIAL PRIMARY KEY,
    flight_id       BIGINT NOT NULL REFERENCES flights(id) ON DELETE CASCADE,
    ts              TIMESTAMPTZ NOT NULL,
    lat             DOUBLE PRECISION NOT NULL,
    lon             DOUBLE PRECISION NOT NULL,
    altitude_ft     INTEGER,
    groundspeed_kt  INTEGER,
    heading_deg     SMALLINT
);

CREATE INDEX positions_flight_ts_idx ON positions (flight_id, ts DESC);
