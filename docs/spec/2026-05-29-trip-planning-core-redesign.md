# Design spec: Trips as the core of Aerly

**Status:** Draft for review
**Date:** 2026-05-29
**PRD:** `docs/prd/2026-05-28-trip-planning-core-redesign.md`
**Audience:** Engineering

This spec turns the trip-planning PRD into an implementation design. It assumes
the PRD's product decisions and does not re-argue them. Where it touches existing
code it cites real files so the migration path is concrete.

---

## 1. Scope

Make **trips** the core object, with **plans** (bookings) made of **parts**
(timeline entries) inside them, and demote the flight tracker to a per-trip and
cross-trip *view* over the same data. Flights stop being a free-standing
top-level entity and become one *type* of plan, with their tracker-specific
machinery (poller, resolver, ADS-B positions, dead-reckoning) preserved behind a
satellite table.

Delivered capabilities (per PRD): trip CRUD + sharing; the timeline; multi-method
plan capture (manual / paste / upload / email) via a generalized extractor;
owner/editor/viewer roles with per-plan privacy; tags; iCal feeds; flight alerts;
rebookings; smart hotel times; the re-scoped tracker.

Out of scope (PRD non-goals): booking/payments, mobile app + device location,
non-flight live tracking.

---

## 2. Architecture overview

Today: `flights` is top-level; `positions` hangs off it; visibility is computed
per-flight in three places in `internal/store/flights.go`
(`ListVisibleFlights`, `CanView`, `VisibleUserIDs`); the SPA has no router and is
dialog-driven over a single Zustand store; the poller broadcasts `flight.updated`
through the `sse.Hub` with a `VisibleTo` set.

Target entity stack:

```
trips
  └── plans                (the booking: type, title, confirmation, notes,
       │                     sharing/privacy, passengers, source)
       └── plan_parts       (the spine: one timeline entry; time range +
            │                start/end place + status + supersedes link)
            └── flight_details   (1:1 satellite for flight parts)
            └── positions        (re-keyed from flight_id → plan_part_id)
trip_members               (owner / editor / viewer; the sharing boundary)
trip_tags                  (label rows; group-never-grant)
plan_passengers            (people travelling on a plan)
plan_visibility            (per-plan privacy override rows)
alert_prefs                (per-user channel/threshold)
```

Rationale recap from design discussion: the **part** is the unit that lands on
the timeline; the **plan** is the unit of sharing, privacy, passengers, and
confirmation identity; the **trip** is the container and tag/visibility scope.
Flight-only columns live in `flight_details` rather than bloating `plan_parts`
with ~15 nullable columns. `positions` simply re-keys to `plan_part_id`, so the
poller/resolver keep a focused table to work against.

---

## 3. Data model

Migrations follow the existing numbered pattern in `migrations/` (next is
`0010_*`). Sketch DDL below; not final column-for-column.

### 3.1 New tables

```sql
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
    details       JSONB NOT NULL DEFAULT '{}',     -- type-specific extras (§3.2)
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX plan_parts_plan_idx ON plan_parts (plan_id);
CREATE INDEX plan_parts_starts_idx ON plan_parts (starts_at);
-- A superseded part is the row pointed at by some other part's supersedes_id.

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

CREATE TABLE plan_passengers (
    plan_id   BIGINT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    user_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (plan_id, user_id)
);

-- Per-plan privacy. Absence of a row for a plan = default "everyone on trip".
CREATE TABLE plan_visibility (
    plan_id   BIGINT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    mode      TEXT NOT NULL CHECK (mode IN ('hidden_from','only_visible_to')),
    user_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (plan_id, user_id)
);
-- mode is uniform per plan (enforced in the store layer / a trigger).

CREATE TABLE alert_prefs (
    user_id        BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    in_app         BOOLEAN NOT NULL DEFAULT TRUE,
    email          BOOLEAN NOT NULL DEFAULT TRUE,
    min_delay_min  INT NOT NULL DEFAULT 15   -- suppress changes below threshold
);
-- Viewer opt-in to a specific plan's alerts is a row in plan_alert_optin.
CREATE TABLE plan_alert_optin (
    plan_id  BIGINT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (plan_id, user_id)
);
```

The `positions` table keeps its shape but its FK becomes `plan_part_id`
referencing `plan_parts(id)` (see migration below). The `positions_flight_ts_idx`
becomes `positions_part_ts_idx`.

### 3.2 Per-type part details

Only `flight` carries enough structured, behaviour-bearing state (live tracking,
resolver throttle via `last_resolved_at`, three scheduled/estimated/actual time
pairs, the rich status enum) to justify a dedicated satellite — `flight_details`
above. The other launch types are well served by the **generic `plan_parts`
columns** (a part is fundamentally a time range with a start place and an end
place) plus a small typed **`details` JSON blob** on the part for the
type-specific extras — avoiding a sprawl of mostly-empty tables. `details` is
marshalled to/from a per-type Go struct in the store layer (validated in app
code, keyed off `plans.type`; the DB just stores JSONB). `train` graduates to its
own `train_details` satellite if and when live train tracking lands (PRD future;
§12) — at which point it follows the `flight_details` pattern.

How each launch type maps onto the spine:

| Type | Generic `plan_parts` columns | `details` JSON keys |
|------|------------------------------|---------------------|
| **flight** | starts/ends = scheduled out/in; start/end label+coords = origin/dest | *(rich data in `flight_details`)*; `seat`, `cabin` optional |
| **hotel** | starts = check-in, ends = check-out; start_label = property; coords = address | `room_type`, `guests`, `standard_checkin`, `standard_checkout`, `address`, `phone` |
| **train** | starts/ends = depart/arrive; start/end label = stations | `operator`, `service_no`, `coach`, `seat`, `class`, `platform` |
| **ground** | starts/ends = pickup/dropoff; start_label = pickup, end_label = dropoff | `provider`, `phone`, `vehicle`, `driver`, `pax` |
| **dining** | starts = reservation time; start_label = venue + coords | `party_size`, `reservation_name`, `phone` |
| **excursion** | starts/ends = activity window; start_label = meeting point | `provider`, `ticket_count`, `meeting_point` |

`standard_checkin` / `standard_checkout` feed the smart-times calc (§10); when a
confirmation doesn't state them, the 15:00 / 11:00 local defaults apply. The
extractor (§6) returns these same per-type fields, and `planops.Commit` writes
the generic columns plus either `details` or `flight_details` according to type.

### 3.3 Migration of existing data (`0010` up)

Per the PRD's "Imported flights" decision:

1. Create the new tables.
2. For each distinct `flights.created_by` user, create one trip
   `name = 'Imported flights'`, `created_by = user`, and a `trip_members` owner
   row.
3. For each existing `flights` row, create a `plans` row (`type='flight'`,
   `source='manual'`, `created_by`, `confirmation_ref=''`, `notes` carried over)
   in that user's import trip; create one `plan_parts` row (start = scheduled_out
   / origin, end = scheduled_in / dest, tz looked up via `airports.LookupTZ`);
   create the `flight_details` row from the flight's columns.
4. Repoint `positions.flight_id` → the new `plan_part_id` (join through a temp
   `flight_id → plan_part_id` map column or CTE during migration).
5. Translate `flight_passengers` → `plan_passengers`; for each, also ensure a
   `trip_members` viewer row (passenger ⇒ viewer per PRD §6.4).
6. Translate `flight_shares` → `plan_visibility(mode='only_visible_to')` rows,
   OR — simpler and truer to intent — into `trip_members` viewer rows on the
   import trip. **Decision needed** (see §12): shares were per-flight; mapping
   them to trip viewers over-shares the whole import bucket. Leaning:
   `only_visible_to` rows so the old per-flight scope is preserved.
7. `is_public = TRUE` flights → a `trip_tags`-independent broadcast. Old
   "share with all friends" semantics: seed `trip_members` viewer rows for the
   creator's accepted friends, mirroring the current predicate
   (`flights.go:522`). Acceptable one-off expansion since it matches today's
   visible set.
8. Drop `flights`, `flight_passengers`, `flight_shares` at the **end** of the
   redesign rollout — not in `0010`. Keep them through the transition (see §11)
   so a rollback is a data-restore, not a reconstruction. The down migration
   reverses table creation only.

---

## 4. Visibility & sharing model

The three-place predicate in `internal/store/flights.go` collapses to a single
reusable plan-visibility predicate. A viewer **V** can see plan **P** in trip
**T** when:

```
EXISTS trip_members(T, V)                       -- V is on the trip
AND (
     P.created_by = V                           -- always see your own
  OR EXISTS plan_passengers(P, V)               -- passenger always sees
  OR NOT EXISTS plan_visibility(P)              -- default: everyone on trip
  OR (mode='hidden_from'     AND V NOT IN plan_visibility(P).user_id)
  OR (mode='only_visible_to' AND V IN     plan_visibility(P).user_id)
)
OR T.created_by = V                             -- trip owner sees everything
```

A `plan_part` is visible iff its `plan` is visible. Superuser show-all keeps its
current bypass. This predicate is implemented once in the store
(`CanViewPlan`, `ListVisiblePlanParts`, `VisibleUserIDs(planID)` for SSE
fan-out) replacing the duplicated flight predicates.

`trip_members` roles: `owner` (full, can delete trip, manage members),
`editor` (CRUD plans/parts), `viewer` (read). Adding a `plan_passengers` row
inserts a `viewer` `trip_members` row if absent (idempotent; not removed on
passenger removal — leaves them a viewer, which matches "they were on the trip").

---

## 5. Backend: store, handlers, DTOs

### 5.1 Store layer

New files under `internal/store/`: `trips.go`, `plans.go`, `tags.go`,
`alerts.go`. `flights.go` shrinks to flight-detail helpers operating on
`flight_details` keyed by `plan_part_id`; `positions.go` helpers re-key. The
poller/tracker code (`internal/poller`, `internal/providers`) changes only where
it reads/writes flight rows — swap `FlightByID`/`ActiveFlights`/`InsertPosition`
to part-keyed equivalents (`ActiveFlightParts`, etc.). Core dead-reckoning and
resolver logic is untouched.

### 5.2 API routes

Extend `(*API).Register` in `internal/handlers/handlers.go` (Go 1.22 method
routing, `r.PathValue`). New surface:

```
GET    /api/trips                       list my trips (+ shared)
POST   /api/trips                       create
GET    /api/trips/{id}                  trip + plans + parts (timeline payload)
PATCH  /api/trips/{id}                  rename/dates
DELETE /api/trips/{id}
POST   /api/trips/{id}/members          add editor/viewer (by user id)
DELETE /api/trips/{id}/members/{userId}
PUT    /api/trips/{id}/tags             set tag labels
GET    /api/tags/suggest?q=             autocomplete over visible tags

POST   /api/trips/{id}/plans            create plan (+parts) manually
PATCH  /api/plans/{id}                  edit plan
DELETE /api/plans/{id}
POST   /api/plans/{id}/passengers       (+ auto trip viewer)
DELETE /api/plans/{id}/passengers/{userId}
PUT    /api/plans/{id}/visibility       set mode + member list
PATCH  /api/plan-parts/{id}             edit a part (time/place/status)

POST   /api/trips/{id}/ingest           paste/upload → proposed plans (no commit)
POST   /api/trips/{id}/ingest/confirm   commit confirmed/edited proposals

GET    /api/calendar/me.ics?token=      personal feed
GET    /api/calendar/trip/{id}.ics?token=
GET    /api/calendar/plan/{id}.ics?token=

GET    /api/tracker?window_before=&window_after=&tag=   convergence view data
GET    /api/alert-prefs ; PUT /api/alert-prefs
POST   /api/plans/{id}/alerts/optin ; DELETE …          viewer opt-in
```

Existing `/api/flights*` routes are retired once the SPA is cut over; the
tracker single-flight view reads `/api/trips/{id}` part data or a focused
`/api/plan-parts/{id}` with positions.

### 5.3 DTOs

`internal/api/dto.go` gains `TripDTO`, `PlanDTO`, `PlanPartDTO`, `TagDTO`. The
existing `FlightDTO` is largely reborn as `PlanPartDTO` + a nested
`FlightDetailDTO` (carrying `ident`, the three time pairs, status, `icao24`,
positions, track). The TZ-lookup convenience in `ToFlightDTO` (calling
`airports.LookupTZ`) moves to part construction. `PlanPartDTO` carries a derived
`effective_at` = `COALESCE(actual_*, estimated_*, scheduled_*)` so the front end
sorts/render every type uniformly (the rule already implicit in
`flights.go:530`).

---

## 6. Ingestion pipeline (the LLM seam)

The existing `emailingest.Extractor` already takes a prompt + binary `Document`s
and returns structured `Leg`s (`internal/emailingest/extract.go`). Generalize it:

- Rename/extend `Leg` → `ExtractedPart` with a `Type` field and per-type fields
  (lodging name + check-in/out dates, ground pickup/dropoff, dining venue/time,
  excursion title/time), keeping `Confidence` and the existing flight fields.
- Group output into `ExtractedPlan{ Type, Title, ConfirmationRef, Parts[] }` so
  one round-trip email becomes one plan with several parts (PRD §6.3).
- The extractor stays a single LLM call with the documents attached; only the
  schema/prompt in `systemPrompt` grows. Text-only retry path unchanged.

A shared `internal/planops` package (sibling to today's `internal/flightops`)
exposes `Propose(ctx, deps, userID, tripID, text, docs) ([]ExtractedPlan, …)`
and `Commit(ctx, deps, tripID, confirmed []PlanDraft)`. `flightops.Create` /
`CreateManual` fold in as the flight-type path (still calling the resolver to
enrich `ident+date`). Both the HTTP ingest endpoints and the email-ingest
`Service` (`internal/emailingest/ingest.go`) call `planops`, so all four capture
methods converge. Email ingest now needs a **target trip**: default to the
user's most recent active trip, else auto-create one (named from the
destination), surfaced for confirmation rather than silently committed.

### 6.1 Rebooking match (PRD §6.9)

On `Propose`, for flight parts, attempt to match each against existing visible
flight parts in the trip:

1. By `confirmation_ref` / PNR equality (high confidence).
2. Else by `ident` + same calendar day, or same `origin_iata/dest_iata` + date
   proximity (medium).

A match is returned as a *proposed supersession* in the proposal payload, never
auto-applied (PRD-resolved: always confirm). On confirm, insert the new part
with `supersedes_id` = matched part; set the old part's `status='cancelled'` and
mark it superseded (it stays, greyed, until tidied). The front end renders both.

---

## 7. Tracker re-scoping

The tracker becomes a read view over `plan_parts` of trackable type (`flight`
today) with `positions`, gated by the §4 predicate. Two scopes:

- **Single part:** `/api/plan-parts/{id}` → the focused one-flight view (current
  `FlightDetailPanel` + `FlightMap` for one track).
- **Convergence:** `/api/tracker?window_before=7d&window_after=7d[&tag=…]` →
  every visible trackable part whose `effective` arrival falls in the window,
  with latest positions. When `tag` is given, the default window is derived
  server-side from `min(starts_at)…max(ends_at)` over visible tagged trips
  (still overridable by the explicit window params). No leaderboard/ranking —
  the payload is just labelled parts + positions (PRD §6.5).

The window values live in the client (localStorage), keyed per-tag, exactly as
`showAll`/`showOld` are today in `web/src/state/store.ts`.

---

## 8. iCal feeds

Read-only ICS generated server-side. Each user has per-scope secret tokens
(`calendar_tokens(user_id, scope, token, created_at)`, regenerable to revoke).
The handler authenticates by token (not session cookie, since calendar clients
won't carry it) and renders **as that user**, applying the §4 predicate — so a
hidden plan never leaks. One `VEVENT` per `plan_part`, `DTSTART`/`DTEND` in the
part's tz, `SUMMARY` from plan title/type, `LOCATION` from `start_label`,
`DESCRIPTION` with confirmation ref + notes. A single-plan feed stays live so a
delayed flight updates its event on the next client refresh. No new external
deps — hand-render ICS (small, well-specified text format).

---

## 9. Alerts

The poller (`internal/poller`) already detects flight status/time changes when
it writes `flight_details`. Add a diff step: when `flight_status`,
`estimated_*`, or `actual_*` cross a meaningful threshold (delay ≥
`alert_prefs.min_delay_min`, or any cancellation/diversion/gate change), enqueue
an alert for the recipient set = plan owner + `plan_passengers` +
`plan_alert_optin` viewers, filtered by each user's `alert_prefs`.

- **In-app:** extend the open-shape `NotificationsDTO` (`dto.go:223`) with a new
  field and publish a `notifications.updated` (or new `alert.created`) SSE event
  via the hub, `VisibleTo` = recipient set, `UserPrivate` semantics as needed.
- **Email:** reuse the existing `internal/mailer` path (same one auth/email-
  ingest use). Templated "Your flight BA123 is now delayed to …".

Alert dedupe: store a per-part last-alerted signature so the same delay isn't
re-sent each poll tick.

---

## 10. Smart hotel times (PRD §6.10)

A pure function in `planops`, computed at read/render time (not stored), for a
hotel plan in a trip that also contains flight parts:

```
checkin_suggested  = max(standard_checkin (default 15:00 local),
                         inbound_arrival + 1h + airportTravel?)
checkout_suggested = min(standard_checkout (default 11:00 local),
                         outbound_departure − lead − airportTravel?)
lead = 3h if long-haul (heuristic: flight > ~6h or intercontinental) else 2h
```

`inbound_arrival` = the latest flight part arriving before the stay; `outbound_
departure` = the earliest flight part departing after it, within the same trip.
`airportTravel` is best-effort (great-circle/`internal/geo` estimate or a routing
lookup if available) and omitted when unknown. Surfaced as a *suggested* time on
the part with an "adjust" affordance; the stored check-in/out dates are
untouched. With no flanking flight, fall back to standard times.

---

## 11. Frontend

Introduce **one** routing level using **`react-router`** (resolved) for
trip-list ↔ trip-detail (and trip sub-tabs Timeline / Map); keep everything below
dialog-driven as today.

- **`TripList`** replaces the map as home: Upcoming / Happening now / Past
  groupings; "New trip" primary action (was "Add flight" in `AppShell.tsx`).
- **`TripTimeline`** (default trip view): day-grouped vertical list of parts,
  sorted by `effective_at`, local-day headers from part tz; multi-night hotels as
  a band; superseded parts greyed. **`TripMap`** as a secondary tab reusing
  `FlightMap`/MapLibre.
- **`AddToTripDialog`**: tabs Manual / Paste / Upload / Email, all hitting the
  ingest endpoints; a confirm step listing proposed plans (flagging low
  confidence and proposed supersessions).
- **Tracker** views: single-part panel (from a flight card) and the convergence
  map (from a "who's on their way" entry / a tag), with the window sliders.
- **State:** the single Zustand store (`web/src/state/store.ts`) grows
  `trips`, `currentTrip`, ingest-proposal state; the flight-centric slice is
  reworked into plan/part shapes. SSE handlers (`applyFlightUpdate` etc.) become
  `applyPlanPartUpdate` / `applyTripUpdate`.

---

## 12. Decisions and open questions

Resolved:

- **Old `flight_shares` migration:** map to per-plan `only_visible_to` rows, so
  the original per-flight share scope is preserved rather than over-sharing the
  whole import bucket.
- **Router:** use `react-router` for the single trip-list ↔ trip-detail routing
  level (§11).
- **Per-type schema:** one rich `flight_details` satellite; all other launch
  types use the generic `plan_parts` columns + a typed `details` JSON blob; a
  `train_details` satellite is added only when live train tracking is built
  (§3.2). `positions` is modelled generically (keyed on `plan_part_id`) from the
  start so that future satellite is cheap.

Still open:

- **`plan_visibility` mode uniformity:** enforce one mode per plan via a DB
  trigger vs. a store-layer invariant. Leaning store-layer.
- **Email-ingest target trip:** auto-create a draft trip vs. always ask which
  trip. Leaning auto-create a draft but require confirmation before parts
  persist.

---

## 13. Rollout / phasing

1. **Schema + read model.** `0010` migration, store layer, `/api/trips`,
   `/api/trips/{id}`; SPA trip list + timeline rendering migrated flights
   (read-only). Old `/api/flights` still live.
2. **Manual authoring + sharing.** Plan/part CRUD, members/roles, per-plan
   privacy, the §4 predicate; retire `/api/flights` writes.
3. **Ingestion.** Generalize the extractor + `planops`; paste/upload/email
   converge; rebooking match + supersession UI.
4. **Tracker re-scope + tags.** Convergence endpoint, window sliders, tag
   autocomplete; demote map to a tab.
5. **Alerts, iCal, smart hotel times.**
6. **Cleanup.** Drop `flights`/`flight_passengers`/`flight_shares` once nothing
   reads them; remove dead predicates.

Each phase ships behind the existing test conventions (`*_test.go` in each Go
package, Vitest + RTL on the web side; `internal/testsupport` fixtures extended
with trip/plan builders).
