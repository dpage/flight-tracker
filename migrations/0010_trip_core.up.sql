-- Trips as the core of Aerly (spec 2026-05-29).
--
-- Introduces the trip → plan → plan_part entity stack and demotes the flight
-- tracker to a per-plan satellite. Flights become one *type* of plan; their
-- tracker-specific columns move to flight_details keyed on plan_part_id, and
-- positions re-keys from flight_id → plan_part_id.
--
-- The legacy flights / flight_passengers / flight_shares tables are NOT
-- dropped here — they survive through the transition so a rollback is a
-- data restore rather than a reconstruction (spec §3.3 step 8, §11). Wave 3
-- removes them in a later migration.

----------------------------------------------------------------------
-- §3.1 New tables
----------------------------------------------------------------------

CREATE TABLE trips (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT NOT NULL,
    destination   TEXT NOT NULL DEFAULT '',
    starts_on     DATE,                 -- nullable; derived/edited
    ends_on       DATE,
    created_by    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE trip_members (
    trip_id   BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    user_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role      TEXT NOT NULL CHECK (role IN ('owner','editor','viewer')),
    added_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (trip_id, user_id)
);

CREATE INDEX trip_members_user_idx ON trip_members (user_id);

CREATE TABLE trip_tags (
    trip_id        BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    label_norm     TEXT NOT NULL,       -- lowercased/trimmed for matching
    label_display  TEXT NOT NULL,       -- as first typed
    PRIMARY KEY (trip_id, label_norm)
);
CREATE INDEX trip_tags_label_idx ON trip_tags (label_norm);

CREATE TABLE plans (
    id              BIGSERIAL PRIMARY KEY,
    trip_id         BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    type            TEXT NOT NULL CHECK (type IN
                      ('flight','train','hotel','ground','dining','excursion')),
    title           TEXT NOT NULL DEFAULT '',
    confirmation_ref TEXT NOT NULL DEFAULT '',
    notes           TEXT NOT NULL DEFAULT '',
    source          TEXT NOT NULL DEFAULT 'manual'  -- manual|paste|upload|email
                      CHECK (source IN ('manual','paste','upload','email')),
    created_by      BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX plans_trip_idx ON plans (trip_id);

CREATE TABLE plan_parts (
    id            BIGSERIAL PRIMARY KEY,
    plan_id       BIGINT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    seq           INT NOT NULL DEFAULT 0,        -- order within the plan
    starts_at     TIMESTAMPTZ NOT NULL,
    ends_at       TIMESTAMPTZ,
    start_tz      TEXT NOT NULL DEFAULT '',      -- IANA, for local-day grouping
    end_tz        TEXT NOT NULL DEFAULT '',
    start_label   TEXT NOT NULL DEFAULT '',      -- e.g. origin / hotel name
    start_lat     DOUBLE PRECISION,
    start_lon     DOUBLE PRECISION,
    end_label     TEXT NOT NULL DEFAULT '',
    end_lat       DOUBLE PRECISION,
    end_lon       DOUBLE PRECISION,
    status        TEXT NOT NULL DEFAULT 'planned' -- planned|confirmed|cancelled
                    CHECK (status IN ('planned','confirmed','cancelled')),
    supersedes_id BIGINT REFERENCES plan_parts(id) ON DELETE SET NULL,
    dismissed_at  TIMESTAMPTZ,                    -- "tidied away" superseded part
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX plan_parts_plan_idx ON plan_parts (plan_id);
CREATE INDEX plan_parts_starts_idx ON plan_parts (starts_at);

----------------------------------------------------------------------
-- §3.1 / §3.2 Per-type 1:1 satellites keyed on plan_part_id
----------------------------------------------------------------------

CREATE TABLE flight_details (
    plan_part_id  BIGINT PRIMARY KEY REFERENCES plan_parts(id) ON DELETE CASCADE,
    ident         TEXT NOT NULL,
    icao24        TEXT,
    callsign      TEXT,
    scheduled_out TIMESTAMPTZ NOT NULL,
    scheduled_in  TIMESTAMPTZ NOT NULL,
    estimated_out TIMESTAMPTZ,
    estimated_in  TIMESTAMPTZ,
    actual_out    TIMESTAMPTZ,
    actual_in     TIMESTAMPTZ,
    origin_iata   TEXT NOT NULL DEFAULT '',
    dest_iata     TEXT NOT NULL DEFAULT '',
    flight_status TEXT NOT NULL DEFAULT 'Scheduled', -- the rich enum, unchanged
    last_polled_at   TIMESTAMPTZ,
    last_resolved_at TIMESTAMPTZ
);

CREATE TABLE hotel_details (
    plan_part_id      BIGINT PRIMARY KEY REFERENCES plan_parts(id) ON DELETE CASCADE,
    property_name     TEXT NOT NULL DEFAULT '',
    address           TEXT NOT NULL DEFAULT '',
    phone             TEXT NOT NULL DEFAULT '',
    room_type         TEXT NOT NULL DEFAULT '',
    guests            INT,
    -- Local time-of-day; NULL falls back to the 15:00 / 11:00 defaults used by
    -- the smart-times calc. The actual check-in/out instants are the part's
    -- starts_at / ends_at.
    standard_checkin  TIME,
    standard_checkout TIME
);

CREATE TABLE train_details (
    plan_part_id  BIGINT PRIMARY KEY REFERENCES plan_parts(id) ON DELETE CASCADE,
    operator      TEXT NOT NULL DEFAULT '',
    service_no    TEXT NOT NULL DEFAULT '',
    coach         TEXT NOT NULL DEFAULT '',
    seat          TEXT NOT NULL DEFAULT '',
    class         TEXT NOT NULL DEFAULT '',
    platform      TEXT NOT NULL DEFAULT ''
    -- Live-tracking columns (akin to flight_details' icao24/last_polled_at)
    -- are added here if/when train tracking is built; positions already keys
    -- on plan_part_id, so no structural change is needed then.
);

CREATE TABLE ground_details (
    plan_part_id  BIGINT PRIMARY KEY REFERENCES plan_parts(id) ON DELETE CASCADE,
    provider      TEXT NOT NULL DEFAULT '',
    phone         TEXT NOT NULL DEFAULT '',
    vehicle       TEXT NOT NULL DEFAULT '',
    driver        TEXT NOT NULL DEFAULT '',
    pax           INT
);

CREATE TABLE dining_details (
    plan_part_id     BIGINT PRIMARY KEY REFERENCES plan_parts(id) ON DELETE CASCADE,
    party_size       INT,
    reservation_name TEXT NOT NULL DEFAULT '',
    phone            TEXT NOT NULL DEFAULT ''
);

CREATE TABLE excursion_details (
    plan_part_id  BIGINT PRIMARY KEY REFERENCES plan_parts(id) ON DELETE CASCADE,
    provider      TEXT NOT NULL DEFAULT '',
    ticket_count  INT
);

CREATE TABLE plan_passengers (
    plan_id   BIGINT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    user_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (plan_id, user_id)
);

CREATE INDEX plan_passengers_user_idx ON plan_passengers (user_id);

-- Per-plan privacy. A plan with no plan_visibility row uses the default
-- "everyone on the trip". The mode lives on a single parent row keyed by
-- plan_id, so "exactly one mode per plan" is structurally guaranteed — a
-- mixed-mode plan is simply unrepresentable, no trigger needed. The named
-- people are child rows.
CREATE TABLE plan_visibility (
    plan_id  BIGINT PRIMARY KEY REFERENCES plans(id) ON DELETE CASCADE,
    mode     TEXT NOT NULL CHECK (mode IN ('hidden_from','only_visible_to'))
);

CREATE TABLE plan_visibility_members (
    plan_id  BIGINT NOT NULL REFERENCES plan_visibility(plan_id) ON DELETE CASCADE,
    user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (plan_id, user_id)
);

CREATE TABLE alert_prefs (
    user_id        BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    in_app         BOOLEAN NOT NULL DEFAULT TRUE,
    email          BOOLEAN NOT NULL DEFAULT TRUE,
    min_delay_min  INT NOT NULL DEFAULT 15   -- suppress changes below threshold
);

-- Viewer opt-in to a specific plan's alerts.
CREATE TABLE plan_alert_optin (
    plan_id  BIGINT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (plan_id, user_id)
);

-- Per-user, per-scope secret tokens for the read-only iCal feeds. Regenerating
-- a token revokes the old feed URL.
CREATE TABLE calendar_tokens (
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scope       TEXT NOT NULL,   -- 'me' | 'trip' | 'plan'
    token       TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, scope)
);
CREATE UNIQUE INDEX calendar_tokens_token_idx ON calendar_tokens (token);

----------------------------------------------------------------------
-- §3.1 Re-key positions: drop the flights FK, key on plan_part_id, and
-- rename the per-track index from flight to part.
----------------------------------------------------------------------

ALTER TABLE positions DROP CONSTRAINT positions_flight_id_fkey;
ALTER TABLE positions
    ADD COLUMN plan_part_id BIGINT REFERENCES plan_parts(id) ON DELETE CASCADE;
ALTER INDEX positions_flight_ts_idx RENAME TO positions_part_ts_idx;

----------------------------------------------------------------------
-- §4 passenger ⇒ viewer trigger.
--
-- Keeping this rule in the database means any client that adds a passenger
-- gets the trip-membership rule for free and can't diverge. It ensures a
-- 'viewer' trip_members row exists for the new passenger on the plan's trip,
-- and is a no-op if they are already a member of any role (so an existing
-- owner/editor is not demoted). It is not undone on passenger removal.
----------------------------------------------------------------------

CREATE FUNCTION plan_passenger_ensure_member() RETURNS trigger AS $$
BEGIN
    INSERT INTO trip_members (trip_id, user_id, role)
    SELECT p.trip_id, NEW.user_id, 'viewer'
    FROM plans p
    WHERE p.id = NEW.plan_id
    ON CONFLICT (trip_id, user_id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER plan_passengers_ensure_member
    AFTER INSERT ON plan_passengers
    FOR EACH ROW
    EXECUTE FUNCTION plan_passenger_ensure_member();

----------------------------------------------------------------------
-- §3.3 Migration of existing data.
--
-- Each distinct flights.created_by gets one "Imported flights" trip (owner
-- membership). Each flight becomes a plan (type flight) + one plan_part +
-- flight_details. positions re-points to the new plan_part. Passengers and
-- shares carry across; is_public flights seed viewer rows for the creator's
-- accepted friends, mirroring the predicate in internal/store/flights.go.
----------------------------------------------------------------------

-- 2. One import trip per creator (skip NULL created_by — orphaned legacy rows).
WITH creators AS (
    SELECT DISTINCT created_by FROM flights WHERE created_by IS NOT NULL
), new_trips AS (
    INSERT INTO trips (name, created_by)
    SELECT 'Imported flights', created_by FROM creators
    RETURNING id, created_by
)
INSERT INTO trip_members (trip_id, user_id, role)
SELECT id, created_by, 'owner' FROM new_trips;

-- 3. One plan + plan_part + flight_details per flight. A temp table records
--    the flight_id → plan_part_id mapping for the steps that follow.
CREATE TEMP TABLE flight_part_map (flight_id BIGINT PRIMARY KEY, plan_id BIGINT, plan_part_id BIGINT);

DO $$
DECLARE
    f RECORD;
    v_trip_id BIGINT;
    v_plan_id BIGINT;
    v_part_id BIGINT;
BEGIN
    FOR f IN SELECT * FROM flights WHERE created_by IS NOT NULL ORDER BY id LOOP
        SELECT t.id INTO v_trip_id
        FROM trips t
        WHERE t.created_by = f.created_by AND t.name = 'Imported flights'
        LIMIT 1;

        INSERT INTO plans (trip_id, type, source, notes, created_by)
        VALUES (v_trip_id, 'flight', 'manual', f.notes, f.created_by)
        RETURNING id INTO v_plan_id;

        -- tz columns left empty: airports.LookupTZ isn't available in SQL, so
        -- the app fills them lazily (spec §3.3 step 3 allows this).
        INSERT INTO plan_parts (plan_id, seq, starts_at, ends_at,
            start_label, start_lat, start_lon,
            end_label, end_lat, end_lon, status)
        VALUES (v_plan_id, 0, f.scheduled_out, f.scheduled_in,
            f.origin_iata, f.origin_lat, f.origin_lon,
            f.dest_iata, f.dest_lat, f.dest_lon,
            CASE WHEN f.status = 'Cancelled' THEN 'cancelled' ELSE 'confirmed' END)
        RETURNING id INTO v_part_id;

        INSERT INTO flight_details (plan_part_id, ident, icao24, callsign,
            scheduled_out, scheduled_in, estimated_out, estimated_in,
            actual_out, actual_in, origin_iata, dest_iata, flight_status,
            last_polled_at, last_resolved_at)
        VALUES (v_part_id, f.ident, f.icao24, f.callsign,
            f.scheduled_out, f.scheduled_in, f.estimated_out, f.estimated_in,
            f.actual_out, f.actual_in, f.origin_iata, f.dest_iata, f.status,
            f.last_polled_at, f.last_resolved_at);

        INSERT INTO flight_part_map (flight_id, plan_id, plan_part_id)
        VALUES (f.id, v_plan_id, v_part_id);
    END LOOP;
END $$;

-- 4. Re-point positions at the new plan_parts.
UPDATE positions p
SET plan_part_id = m.plan_part_id
FROM flight_part_map m
WHERE p.flight_id = m.flight_id;

-- 5. flight_passengers → plan_passengers. Insert trip_members viewer rows
--    directly too — the trigger fires on the plan_passengers insert and would
--    add the same rows, but doing it explicitly keeps the migration's intent
--    obvious and is idempotent via ON CONFLICT.
INSERT INTO plan_passengers (plan_id, user_id, added_at)
SELECT m.plan_id, fp.user_id, fp.added_at
FROM flight_passengers fp
JOIN flight_part_map m ON m.flight_id = fp.flight_id
ON CONFLICT DO NOTHING;

INSERT INTO trip_members (trip_id, user_id, role)
SELECT DISTINCT pl.trip_id, fp.user_id, 'viewer'
FROM flight_passengers fp
JOIN flight_part_map m ON m.flight_id = fp.flight_id
JOIN plans pl ON pl.id = m.plan_id
ON CONFLICT (trip_id, user_id) DO NOTHING;

-- 6. flight_shares → plan_visibility(mode='only_visible_to') + members, so
--    the original per-flight share scope is preserved rather than over-sharing
--    the whole import bucket (spec §3.3 step 6 / §12).
INSERT INTO plan_visibility (plan_id, mode)
SELECT DISTINCT m.plan_id, 'only_visible_to'
FROM flight_shares fs
JOIN flight_part_map m ON m.flight_id = fs.flight_id
ON CONFLICT (plan_id) DO NOTHING;

INSERT INTO plan_visibility_members (plan_id, user_id)
SELECT m.plan_id, fs.user_id
FROM flight_shares fs
JOIN flight_part_map m ON m.flight_id = fs.flight_id
ON CONFLICT DO NOTHING;

-- Share recipients also need to be on the trip for the §4 predicate's
-- trip_members gate to pass.
INSERT INTO trip_members (trip_id, user_id, role)
SELECT DISTINCT pl.trip_id, fs.user_id, 'viewer'
FROM flight_shares fs
JOIN flight_part_map m ON m.flight_id = fs.flight_id
JOIN plans pl ON pl.id = m.plan_id
ON CONFLICT (trip_id, user_id) DO NOTHING;

-- 7. is_public flights → seed viewer rows for the creator's accepted friends,
--    mirroring the predicate in flights.go (one-off expansion that matches the
--    set visible today).
INSERT INTO trip_members (trip_id, user_id, role)
SELECT DISTINCT pl.trip_id,
       CASE WHEN fr.user_low = f.created_by THEN fr.user_high ELSE fr.user_low END,
       'viewer'
FROM flights f
JOIN flight_part_map m ON m.flight_id = f.id
JOIN plans pl ON pl.id = m.plan_id
JOIN friendships fr ON fr.status = 'accepted'
                   AND f.created_by IN (fr.user_low, fr.user_high)
WHERE f.is_public = TRUE
ON CONFLICT (trip_id, user_id) DO NOTHING;

DROP TABLE flight_part_map;
