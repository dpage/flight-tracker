# Implementation plan: Trips as the core of Aerly

**Status:** Draft for review
**Date:** 2026-05-29
**PRD:** `docs/prd/2026-05-28-trip-planning-core-redesign.md`
**Spec:** `docs/spec/2026-05-29-trip-planning-core-redesign.md`
**Audience:** Engineering / the agent orchestrator

This plan decomposes the spec into units sized for **parallel sub-agents**, each
working in its **own git worktree** where isolation helps. It is organised into
**waves**: everything in a wave can run concurrently; a wave starts once its
dependencies have merged.

---

## 1. Strategy: contract-first, scaffold-first

The enemy of parallel agents is three shared hot-spot files everyone wants to
touch: `internal/handlers/handlers.go` (`Register`), `internal/api/dto.go`, and
`web/src/state/store.ts`. We neutralise them up front:

1. **One foundation wave** lands the `0010` migration, the store contract
   (types + method signatures + the visibility predicate), **all** DTOs, and
   **all** routes — but each route is wired to a *stub handler in a per-area
   file* returning `501`. Feature agents then implement bodies in **their own
   file** without editing `Register` or `dto.go`.
2. The Zustand store is **split into per-domain slice files** in the same wave,
   so frontend feature agents each own a slice rather than fighting over one
   file.
3. Backend feature work is organised **one Go file (and one `_test.go`) per
   agent per area**, in packages that already isolate concerns
   (`internal/store`, `internal/handlers`, `internal/planops`, `internal/poller`).

Result: after Wave 0, feature agents touch disjoint files and merge without
conflict. Integration is "merge in any order, re-run the suite."

---

## 2. Worktree & branch protocol

- **Integration trunk:** `claude/trip-planning-core-redesign-bcUYe`. Waves merge
  here; promote to `main` at the end of each green wave.
- Each parallel task runs in its **own worktree** branched off the current trunk
  (`isolation: worktree`), on a branch like `trip/<area>` (e.g.
  `trip/store-foundation`, `trip/plans-api`, `trip/fe-timeline`).
- A task is **done** when: its files compile, its package tests pass, `go vet` /
  `gofmt` and the web lint/typecheck are clean, and it merges into the trunk with
  the **full suite green** (`make test` or the repo's equivalent;
  `internal/testsupport` fixtures extended with trip/plan builders).
- Shared-file edits are confined to Wave 0. If a later task *must* touch a
  hot-spot file, it appends only (new route line, new DTO) to minimise conflict,
  and that task is **not** run concurrently with another that edits the same
  file.

---

## 3. Dependency graph (waves)

```
Wave 0  (foundation, 2 agents in parallel)
  0a backend contract: 0010 migration + store skeleton + DTOs + stub routes + planops pkg
  0b frontend scaffold: react-router + TS types + api client + sliced store + stub pages
        │
        ▼  (both merged → trunk green)
Wave 1  (6 agents in parallel)
  1A trips & sharing API        (store/trips.go, store/tags.go, handlers_trips.go)
  1B plans & parts API          (store/plans.go, handlers_plans.go)
  1C tracker re-scope + poller  (handlers_tracker.go, poller, store/positions.go, providers)
  1D iCal feeds                 (store/calendar.go, handlers_calendar.go, ics renderer)
  1E smart hotel times          (planops/hoteltimes.go)
  1F FE: trip list + timeline   (fe slices/pages: trips, timeline, map tab)
        │
        ▼  (1A,1B merged are the gate for Wave 2 backend; 0b+1F gate FE)
Wave 2  (5 agents in parallel)
  2A ingestion + rebooking      (planops/propose.go, planops/commit.go, emailingest, handlers_ingest.go)
  2B alerts                     (poller diff step, store/alerts.go, mailer template, handlers_alerts.go)
  2C FE: add-to-trip + confirm  (AddToTripDialog, ingest proposal UI)
  2D FE: sharing & privacy UI   (members, roles, per-plan "who can see this")
  2E FE: tracker + tags + cal   (convergence map, window sliders, tag autocomplete, subscribe links)
        │
        ▼
Wave 3  (cut-over, sequential, 1 agent)
  retire /api/flights writes → reads → drop flights/flight_passengers/flight_shares;
  remove dead predicates; final main promotion.
```

Maps onto spec §13 phases: Wave 0+1 ≈ phases 1–2, Wave 2 ≈ phases 3–5, Wave 3 ≈
phase 6.

---

## 4. Wave 0 — foundation (parallel pair)

### 0a — Backend contract  *(worktree: `trip/backend-foundation`)*
- **Migration `migrations/0010_*.{up,down}.sql`:** all new tables from spec §3.1
  + the five new `*_details` satellites §3.2 (`hotel/train/ground/dining/
  excursion`), `plan_visibility` (parent) + `plan_visibility_members`,
  `plan_passengers`, `alert_prefs`, `plan_alert_optin`, `calendar_tokens`,
  `trip_tags`. Re-key `positions.flight_id → plan_part_id`. **Triggers:**
  passenger⇒viewer `AFTER INSERT` on `plan_passengers` (spec §4). **Data
  migration:** existing flights → per-user "Imported flights" trips; passengers →
  `plan_passengers`; `flight_shares` → `only_visible_to`; `is_public` → friend
  viewer rows (spec §3.3). *Do not drop legacy tables* (Wave 3).
- **Store skeleton (`internal/store/`):** Go types for Trip, Plan, PlanPart, the
  detail structs, Tag; method signatures (bodies = `ErrNotImplemented` for
  feature waves) **except** the visibility predicate, which is implemented now
  (`CanViewPlan`, `ListVisiblePlanParts`, `VisibleUserIDs(planID)`) since every
  area depends on it. `effective_at = COALESCE(actual,estimated,scheduled)`
  helper.
- **DTOs (`internal/api/dto.go`):** `TripDTO`, `PlanDTO`, `PlanPartDTO`,
  per-type detail DTOs, `TagDTO`, calendar/tracker payloads.
- **Routes:** add every route from spec §5.2 to `Register`, each pointing at a
  stub in a new per-area file (`handlers_trips.go`, `handlers_plans.go`,
  `handlers_ingest.go`, `handlers_tracker.go`, `handlers_calendar.go`,
  `handlers_alerts.go`) returning `501`.
- **`internal/planops` package** created (empty + doc) so Waves 1E/2A extend it
  in separate files.
- **Tests:** migration up/down test (extend `migrations/migrations_test.go`
  pattern); predicate unit tests with fixtures.

### 0b — Frontend scaffold  *(worktree: `trip/frontend-foundation`)*
- Add `react-router`; routes `/` (trip list), `/trips/:id` (timeline),
  `/trips/:id/map`, plus tracker route. Keep dialogs below routing.
- **TS types** (`web/src/api/types.ts`) mirroring the 0a DTOs; **api client**
  methods (`web/src/api/client.ts`) for every endpoint.
- **Split the Zustand store** (`web/src/state/`) into slices: `tripsSlice`,
  `plansSlice`, `trackerSlice`, `ingestSlice`, `alertsSlice` — each exporting
  stub actions, composed into the existing store. This is the key FE
  conflict-avoidance step.
- Stub pages/components so the app builds and routes render placeholders.
- **Contract is authoritative here:** 0a and 0b agree DTO/route shapes from spec
  §5.2–§5.3 *before* either starts; they run concurrently against that contract.

---

## 5. Wave 1 — independent feature areas (6 parallel agents)

Each in its own worktree off the post-Wave-0 trunk; each owns the files listed.

- **1A Trips & sharing API** — implement trip CRUD, `trip_members` add/remove +
  roles, tag set + `GET /api/tags/suggest` (visibility-gated autocomplete).
  Files: `store/trips.go`, `store/tags.go`, `handlers_trips.go` (+tests).
- **1B Plans & parts API** — plan CRUD (writing the right satellite by type),
  parts edit, `plan_passengers` (+ trigger relied upon), `PUT …/visibility`
  (writes parent+members atomically, owner/editor only), `POST …/move`,
  `POST …/dismiss`. Files: `store/plans.go`, `handlers_plans.go` (+tests).
  **Gates Wave 2A/2B** (they write/read plans).
- **1C Tracker re-scope + poller** — re-key poller/tracker reads & writes to
  `plan_part_id`; `GET /api/tracker` convergence query (window + tag-derived
  default span, visibility-gated, no ranking); single-part endpoint. Files:
  `handlers_tracker.go`, `internal/poller/*`, `store/positions.go`,
  `internal/providers/*` (key swap only). **Gates 2B** (alerts hook the poller).
- **1D iCal feeds** — `calendar_tokens` issue/regenerate; ICS rendering (one
  `VEVENT` per part, tz-correct, privacy via the §4 predicate, token-auth not
  cookie). Files: `store/calendar.go`, `handlers_calendar.go`, an `ics` helper
  (+tests). Depends only on the read model + predicate → safe in Wave 1.
- **1E Smart hotel times** — pure function `planops/hoteltimes.go` implementing
  the min/max formula (§10), consumed when assembling hotel `PlanPartDTO`.
  Coordinates with 1B only on the DTO field name (defined in 0a). Small.
- **1F FE: trip list + timeline** — `TripList` (Upcoming/Now/Past from effective
  span), `TripTimeline` (day grouping, local-tz headers, linked parts, hotel
  band, greyed/dismissed), `TripMap` tab. Owns `tripsSlice`/`plansSlice` actions
  + those components. Works against 0b's client (can mock until 1A/1B land).

---

## 6. Wave 2 — dependent features (5 parallel agents)

- **2A Ingestion + rebooking** — generalise `emailingest.Extractor`
  (`Leg → ExtractedPart` + `ExtractedPlan`); `planops/propose.go` +
  `planops/commit.go`; rebooking match (ref → traveller+route+date) returning
  proposed supersessions; email date-proximity trip selection; wire
  `handlers_ingest.go` + the email `Service`. Dep: **1B**.
- **2B Alerts** — poller diff step (delay≥threshold / cancel / divert / gate),
  dedupe signature, recipient set (owner+passengers+optin), `alert_prefs` +
  `plan_alert_optin` store, mailer template, SSE event. Files: `store/alerts.go`,
  `handlers_alerts.go`, poller hook, mailer. Deps: **1B, 1C**.
- **2C FE: add-to-trip + confirm** — `AddToTripDialog` (Manual/Paste/Upload/
  Email tabs), the proposal/confirm step (low-confidence flags, proposed
  supersessions, dismiss). Owns `ingestSlice`. Dep: **2A** contract.
- **2D FE: sharing & privacy UI** — member/role management, the per-plan "Who can
  see this?" control (everyone/hidden-from/only-visible-to), passenger picker.
  Owns sharing components. Dep: **1A, 1B**.
- **2E FE: tracker + tags + calendar** — convergence map + window sliders
  (localStorage per-tag), tag autocomplete input, single-flight panel,
  subscribe-to-calendar links. Owns `trackerSlice`. Deps: **1C, 1D**.

---

## 7. Wave 3 — cut-over (sequential, single agent)

Only once everything reads/writes the new model and the FE is fully migrated:
retire `/api/flights*` (writes first, then reads), delete the legacy
`flights` / `flight_passengers` / `flight_shares` tables in `0011_*`, remove the
old triplicated predicate and dead Zustand flight code. Final promotion to
`main`. Not parallelisable — it removes the compatibility layer the other waves
leant on.

---

## 8. Parallelism summary (what to launch together)

| Wave | Launch concurrently | Gate to start |
|------|---------------------|---------------|
| 0 | 0a, 0b | — (agree contract first) |
| 1 | 1A, 1B, 1C, 1D, 1E, 1F | Wave 0 merged |
| 2 | 2A, 2B, 2C, 2D, 2E | 1A+1B+1C+1D merged (per each task's deps) |
| 3 | (single) | all of Wave 2 merged + FE cut over |

Backend feature agents (1A–1E, 2A–2B) are mutually file-disjoint by design.
Frontend agents (1F, 2C–2E) are slice-disjoint by design. Re-run the full suite
on every merge into the trunk; a red suite blocks the next merge, not the
in-flight worktrees.

---

## 9. Cross-cutting requirements for every task

- Tests alongside code (Go `*_test.go` per package; Vitest + RTL on the web
  side). The visibility predicate and ICS privacy get explicit
  "hidden plan must not leak" tests.
- No business invariant in app-only code (spec §2): roles/types/statuses are
  `CHECK`s, passenger⇒viewer and one-mode-per-plan are DB-enforced.
- SSE: new `trip.updated` / `plan_part.updated` events carry a correct
  `VisibleTo` set derived from `VisibleUserIDs`.
- Keep `gofmt`/`go vet` and web lint clean; match surrounding style.

---

## 10. Risks

- **Migration of live data** (`0010` step) is the riskiest single unit — it must
  be idempotent-safe and fully reversible (down restores the pre-state because
  legacy tables survive until Wave 3). Give it the most test attention.
- **Poller re-key (1C)** touches the most behavioural code (dead-reckoning,
  resolver throttle). Keep the change a mechanical `flight_id → plan_part_id`
  swap; no logic changes in the same PR.
- **Contract drift** between 0a and 0b: lock DTO/route shapes in this plan's
  sign-off before Wave 0 starts; treat the spec §5.2/§5.3 as the source of truth.
