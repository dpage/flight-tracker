# Periodic NULL-Coord Sweep

## Problem

The embedded airports table is consulted at flight create/update time, and
the resolver is consulted at create time when the table misses (and during
the poller's existing 30-minute pre-departure window). Two gaps remain:

1. **New table entries don't reach existing rows.** When a deploy ships a
   new airport in `internal/airports/table.go`, flights already in the DB
   whose IATA matches the new entry are stuck with NULL coord columns.
   Re-saving in the UI only resends fields the user changed, so the table
   lookup never runs again on the existing IATA.
2. **Far-future flights don't reach the resolver until ~30 minutes before
   departure.** A flight scheduled months ahead with an IATA the table
   doesn't know stays on "no map" until either the user re-saves with an
   IATA change or the day-of poll window opens.

Concrete trigger: production flights id 43 and 44 (EZY2823 / EZY2824,
BRS↔SID, departing January 2027) had NULL SID coords after the SID table
addition deployed. Re-saving with a notes change didn't fix them because
(a) the frontend didn't resend the IATA and (b) AeroDataBox doesn't yet
publish a schedule that far in advance, so the resolver fallback failed
with `ErrFlightUnscheduled` and the helper gave up.

## Goal

Add a background sweep that:

- Re-runs `lookupCoords` against the embedded table for every row with a
  NULL coord column whose IATA is non-empty (fills any gaps the latest
  deploy's table changes have opened).
- Calls the configured `Resolver` for rows still incomplete after the
  table pass, throttled so unreachable flights cost a handful of API
  calls per day rather than hundreds.
- Publishes the resulting row over SSE so connected clients update
  without a manual reload.

## Non-goals

- No new provider interface. The existing `providers.Resolver.Resolve`
  is used as-is.
- No change to the AeroDataBox provider's `ErrFlightUnscheduled`
  behaviour. (That's a separate, deliberate decision: when the schedule
  is missing the response is rejected. The sweep gives us a clean hook
  to handle this differently in the future if we want, but we are not
  doing that here.)
- No change to the existing per-flight refresh path
  (`(*Poller).tick` / `(*Poller).refresh`).
- No new config knobs. The sweep cadence is a single hardcoded constant
  (4 hours).
- No retroactive cleanup of historical NULL-coord rows beyond what the
  sweep does on its first tick (which happens at server startup).

## Design

### Trigger and cadence

A new method `(*Poller).Sweep(ctx)` runs in two situations:

1. **At server startup**, before the main poller ticker starts. Runs
   synchronously after migrations apply, ensuring any rows the latest
   deploy's table additions can satisfy are fixed before users see the
   UI.
2. **On a `time.Ticker` every `sweepInterval`** (`= 4 * time.Hour`)
   alongside the existing poller's main ticker. The two tickers are
   independent — `Sweep` and the existing `tick` do not block each
   other.

A single `select` in `(*Poller).Run` watches both tickers and the
context's `Done()` channel. The startup sweep is one extra call before
the for-loop.

### Per-row work

`Sweep(ctx)` does:

```
flights := store.FlightsWithMissingCoords(ctx)
for each f in flights:
    needsResolver := false
    changed := false
    update := empty BackfillPayload

    // Table fast path: free, in-memory.
    if f.OriginLat == nil && f.OriginIATA != "":
        if lat, lon, ok := airports.Lookup(f.OriginIATA); ok:
            update.OriginLat = lat; update.OriginLon = lon
            update.OriginIATA = f.OriginIATA  // for the CASE in BackfillFlight
            changed = true
        else:
            needsResolver = true
    (same for dest leg)

    if needsResolver && resolver != nil && throttleAllowed(f, now):
        rf, err := resolver.Resolve(ctx, f.Ident, f.ScheduledOut)
        if err == nil:
            // Merge resolver-supplied fields into the same update payload
            // the table pass started. BackfillFlight only fills empty
            // columns, so the table-derived OriginLat (if set above)
            // survives even if rf.OriginLat is also non-zero.
            update.OriginIATA, update.DestIATA = rf.OriginIATA, rf.DestIATA
            update.OriginLat, update.OriginLon = rf.OriginLat, rf.OriginLon
            update.DestLat, update.DestLon = rf.DestLat, rf.DestLon
            update.ICAO24, update.Callsign = rf.ICAO24, rf.Callsign
            update.Notes = rf.Notes
            changed = true
        // Always bump last_resolved_at via the same RefreshFlightAirframe
        // path the existing poller uses, so the throttle works whether
        // resolve succeeded, returned ErrFlightNotFound, or errored out.
        store.RefreshFlightAirframe(ctx, f.ID,
            rf.ICAO24 if err == nil else "",
            rf.Callsign if err == nil else "")

    if changed:
        store.BackfillFlight(ctx, f.ID, update)
        fresh := store.FlightByID(ctx, f.ID)
        publish SSE event with the refreshed DTO
```

A single `BackfillFlight` call carries the union of whatever the table
pass found and (if it ran) whatever the resolver returned. The store's
existing fill-only-empty semantics make the order irrelevant.

`throttleAllowed` returns true when `f.LastResolvedAt` is nil or older
than `sweepInterval` (4 hours). That throttle reuses
`last_resolved_at`, which the existing poller already maintains — so
sweep and poller cooperate on the same per-flight cooldown.

`BackfillFlight` already enforces "only fill empty columns", so the
table-derived coord on a row where one leg is known and the other isn't
won't be overwritten by a resolver-returned mismatch.

### Where the code lives

- New file `internal/poller/sweep.go`. Contains `(*Poller).Sweep` plus
  one small unexported helper `throttleAllowed(*store.Flight, time.Time)
  bool`.
- New constant `sweepInterval = 4 * time.Hour` in the same file.
- `(*Poller).Run` in `poller.go` gets one new call before the ticker
  loop (`p.Sweep(ctx)`) and a second `time.Ticker` wired into the
  existing `select` block.
- New file `internal/poller/sweep_test.go` for the test cases listed
  below.

### Store change

One new query in `internal/store/flights.go`:

```go
func (s *Store) FlightsWithMissingCoords(ctx context.Context) ([]*Flight, error)
```

Equivalent to `ListFlights`, but with a `WHERE` clause:

```sql
WHERE origin_lat IS NULL OR origin_lon IS NULL
   OR dest_lat   IS NULL OR dest_lon   IS NULL
```

Reuses `flightColumns` and `scanFlight`. Terminal-status flights
(Arrived / Cancelled / Diverted) are NOT excluded — we still want their
coords for the historical map view.

### SSE publish

When a row changes, the sweep builds the same DTO shape as
`(*Poller).refresh` does on its publish path:

```go
pmap, _ := store.PassengersByFlight(ctx, []int64{f.ID})
smap, _ := store.SharedUserIDsByFlight(ctx, []int64{f.ID})
latest, _ := store.LatestPositions(ctx, []int64{f.ID})
tracks, _ := store.RecentTracks(ctx, []int64{f.ID}, 200)
dto := api.ToFlightDTO(fresh, pmap[f.ID], smap[f.ID], latest[f.ID], tracks[f.ID])

var visible []int64
if !fresh.IsPublic {
    visible, _ = store.VisibleUserIDs(ctx, fresh.ID)
}
hub.Publish(sse.Event{Type: "flight.updated", Data: payload, VisibleTo: visible})
```

This is duplication of the publish boilerplate from `refresh`. We
accept the duplication rather than refactoring `refresh`, because the
existing call site is doing more than just publishing (it tracks
positions, refreshes status). Extracting a shared helper would
constrain `refresh` for marginal benefit.

### Failure handling

| Failure mode | Behaviour |
|---|---|
| `FlightsWithMissingCoords` returns an error | Log ERROR, skip this sweep tick. Next tick will retry. |
| `BackfillFlight` errors on a single row | Log ERROR with the row id, continue with the next row. |
| `Resolver.Resolve` errors | Log WARN (same wording as the existing helper), still bump `last_resolved_at` to throttle, leave coords as-is. |
| `FlightByID` errors after a successful backfill | Log ERROR; skip the SSE publish for this row. The DB is correct; next client load will see the updated row. |
| SSE publish path errors | Log WARN; the DB row is correct. |

Every failure is per-row and isolated — one bad row never aborts the
sweep.

## Tests

`internal/poller/sweep_test.go`:

1. **No null rows** — sweep runs, no resolver call, no SSE event, no
   store writes.
2. **Null row, IATA in embedded table** — sweep fills via
   `lookupCoords`, no resolver call, one SSE event with the populated
   DTO, `last_resolved_at` NOT bumped (no resolver call).
3. **Null row, IATA not in table, resolver returns flight** — table
   pass is no-op, resolver fires, BackfillFlight runs, SSE event
   published, `last_resolved_at` bumped.
4. **Null row, IATA not in table, resolver returns `ErrFlightNotFound`**
   — coords stay NULL, no SSE event, `last_resolved_at` bumped, no
   exception.
5. **Null row, `LastResolvedAt` recent (within 4h)** — table pass runs
   (free), but resolver NOT called. If table fills the row, SSE event
   is published; otherwise no event.
6. **Null row, no resolver configured** — table pass runs, resolver
   step is skipped, SSE event published only if table filled the row.
7. **Mixed batch** — five rows, some table-fillable, some
   resolver-fillable, some unfillable. Verify each row is handled
   independently and one error doesn't abort the batch.

Uses the existing `fakeResolver` (with the call counter from earlier
work) and the existing test DB harness. The store query needs a unit
test too:

8. **`FlightsWithMissingCoords` query** — insert rows with various
   combinations of NULL coord columns; verify the query returns
   exactly the right subset.

## Risks

- **API quota.** Worst case: every existing flight has unfillable
  coords and `last_resolved_at` is unset. The sweep would hit the
  resolver for every one of them. After the first sweep, throttling
  spaces subsequent calls 4h apart per row. For a typical user with a
  handful of pending flights this is well under AeroDataBox's basic
  daily quota.
- **Sweep + per-flight refresh contention.** If a flight enters the
  poller's existing 30-min window between sweep ticks, both paths
  could call the resolver. The shared `last_resolved_at` throttle
  prevents back-to-back calls. Worst case: one extra API call per row
  per ~4h, which is acceptable.
- **SSE event storm at startup.** On a fresh deploy with N stuck rows,
  the startup sweep publishes N events in quick succession. Clients
  handle this fine (the existing `flight.updated` listener
  deduplicates by id), but worth knowing in case anyone watches the
  log volume.
- **Sweep delays poller startup.** The startup sweep runs synchronously
  before the poller ticker. For a typical DB it's milliseconds; for a
  large stuck-row backlog it could be a few seconds. Acceptable —
  startup is one-off and the existing tick is generous.

## Follow-ups (not part of this work)

- Relax the AeroDataBox provider's `ErrFlightUnscheduled` behaviour so
  the airport data in the response body is preserved when only the
  schedule is missing. With this in place, the sweep would self-heal
  even-further-future flights without waiting for a published
  schedule.
- A counterpart store-level regression test for `UpdateFlight` writing
  NULL coords on an unknown IATA — currently only the handler-level
  test exercises that contract.
- Frontend: send `origin_iata` / `dest_iata` in the PATCH even when
  unchanged. Combined with this sweep, the user's "edit and save"
  intuition would work as a manual trigger between sweep ticks.
