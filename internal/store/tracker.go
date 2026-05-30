package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Tracker re-scope (spec §7) + poller re-key (plan §5 1C).
//
// The poller and the providers (Tracker / Resolver / DeadReckoner / SpeedGate)
// all consume a *Flight value and key positions on its ID. Wave 1C re-keys that
// machinery from flights.id to plan_parts.id with ZERO behavioural change: we
// keep the exact same *Flight carrier struct the providers already use, but
// populate it from the flight_details + plan_parts + plans join (so Flight.ID
// now carries the plan_part_id) and target the write helpers at flight_details
// / plan_parts. Positions key on plan_part_id. The dead-reckoning, the resolver
// throttle, and the status derivation are unchanged — only the source/target
// tables moved.
//
// Coordinate columns live on plan_parts (start_lat/lon, end_lat/lon); schedule,
// airframe, the rich status enum, and the poll/resolve timestamps live on
// flight_details. notes/created_by come from the owning plan.

// flightPartColumns projects a flight-type plan_part + its flight_details + the
// owning plan into the legacy Flight shape. part.id becomes Flight.ID, so every
// downstream call (Track, LatestRealPosition, InsertPosition, …) keys on the
// plan_part_id without the providers knowing the difference.
const flightPartColumns = `part.id, fd.ident, fd.scheduled_out, fd.scheduled_in,
	fd.estimated_out, fd.estimated_in, fd.actual_out, fd.actual_in,
	fd.origin_iata, part.start_lat, part.start_lon,
	fd.dest_iata, part.end_lat, part.end_lon,
	fd.flight_status, fd.icao24, fd.callsign, fd.last_polled_at, fd.last_resolved_at,
	pl.created_by, pl.notes, FALSE,
	part.created_at, part.updated_at`

const flightPartFrom = `FROM plan_parts part
	JOIN flight_details fd ON fd.plan_part_id = part.id
	JOIN plans pl ON pl.id = part.plan_id`

// scanFlightPart reads the flightPartColumns projection into a *Flight. The
// scan order matches scanFlight in flights.go exactly, so the carrier struct is
// byte-for-byte equivalent to the legacy one — IsPublic is always false (the
// concept moved to plan_visibility), and CreatedBy/Notes come from the plan.
func scanFlightPart(row pgx.Row) (*Flight, error) {
	var f Flight
	if err := row.Scan(
		&f.ID, &f.Ident, &f.ScheduledOut, &f.ScheduledIn,
		&f.EstimatedOut, &f.EstimatedIn, &f.ActualOut, &f.ActualIn,
		&f.OriginIATA, &f.OriginLat, &f.OriginLon,
		&f.DestIATA, &f.DestLat, &f.DestLon,
		&f.Status, &f.ICAO24, &f.Callsign, &f.LastPolledAt, &f.LastResolvedAt,
		&f.CreatedBy, &f.Notes, &f.IsPublic,
		&f.CreatedAt, &f.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &f, nil
}

// ActiveFlightParts is the part-keyed replacement for ActiveFlights: flight
// plan_parts worth polling — non-terminal flight_status whose departure is past
// (or within 30 minutes of being so). The status filter and the departure
// bound are identical to ActiveFlights; only the table moved from flights to
// flight_details + plan_parts. Dismissed (tidied-away) parts are excluded.
func (s *Store) ActiveFlightParts(ctx context.Context, now time.Time) ([]*Flight, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+flightPartColumns+` `+flightPartFrom+`
		WHERE fd.flight_status NOT IN ('Arrived', 'Cancelled', 'Diverted')
		  AND part.dismissed_at IS NULL
		  AND $1 >= fd.scheduled_out - INTERVAL '30 minutes'
		ORDER BY fd.scheduled_out ASC`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Flight
	for rows.Next() {
		f, err := scanFlightPart(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FlightPartByID returns the flight carrier for a single plan_part_id, or
// ErrNotFound if the part has no flight_details (not a flight part) or doesn't
// exist. Part-keyed counterpart of FlightByID.
func (s *Store) FlightPartByID(ctx context.Context, partID int64) (*Flight, error) {
	return scanFlightPart(s.pool.QueryRow(ctx,
		`SELECT `+flightPartColumns+` `+flightPartFrom+` WHERE part.id = $1`, partID))
}

// FlightPartsWithMissingCoords returns every flight part with at least one NULL
// coord column on its plan_part. Part-keyed counterpart of
// FlightsWithMissingCoords used by the periodic sweep. Terminal-status parts are
// NOT excluded — historical routes still want coords for the map view.
func (s *Store) FlightPartsWithMissingCoords(ctx context.Context) ([]*Flight, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+flightPartColumns+` `+flightPartFrom+`
		WHERE part.start_lat IS NULL OR part.start_lon IS NULL
		   OR part.end_lat   IS NULL OR part.end_lon   IS NULL
		ORDER BY fd.scheduled_out ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Flight
	for rows.Next() {
		f, err := scanFlightPart(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// BackfillFlightPart is the part-keyed counterpart of BackfillFlight. It writes
// resolver-supplied metadata only into columns that are currently empty so
// user-typed values are never overwritten — identical rule to BackfillFlight,
// split across the two tables the data now lives in: IATA/airframe on
// flight_details, coords on plan_parts, notes on the owning plan.
func (s *Store) BackfillFlightPart(ctx context.Context, partID int64, in BackfillPayload) error {
	icao24 := strings.ToLower(strings.TrimSpace(in.ICAO24))
	callsign := strings.ToUpper(strings.TrimSpace(in.Callsign))
	var originLat, originLon, destLat, destLon *float64
	if in.OriginLat != 0 || in.OriginLon != 0 {
		originLat, originLon = &in.OriginLat, &in.OriginLon
	}
	if in.DestLat != 0 || in.DestLon != 0 {
		destLat, destLon = &in.DestLat, &in.DestLon
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // best-effort rollback on early return

	// flight_details: IATA + airframe (only-fill-empty), mirroring BackfillFlight.
	if _, err := tx.Exec(ctx, `
		UPDATE flight_details SET
			origin_iata = CASE WHEN origin_iata = '' AND $2 <> '' THEN $2 ELSE origin_iata END,
			dest_iata   = CASE WHEN dest_iata = '' AND $3 <> '' THEN $3 ELSE dest_iata END,
			icao24      = COALESCE(icao24, NULLIF($4, '')),
			callsign    = COALESCE(callsign, NULLIF($5, ''))
		WHERE plan_part_id = $1`,
		partID, strings.ToUpper(in.OriginIATA), strings.ToUpper(in.DestIATA),
		icao24, callsign); err != nil {
		return err
	}

	// plan_parts: coords (only-fill-empty) + the same start/end labels the
	// migration seeded from origin/dest IATA + notes lands on the plan.
	if _, err := tx.Exec(ctx, `
		UPDATE plan_parts SET
			start_lat   = COALESCE(start_lat, $2),
			start_lon   = COALESCE(start_lon, $3),
			end_lat     = COALESCE(end_lat, $4),
			end_lon     = COALESCE(end_lon, $5),
			updated_at  = NOW()
		WHERE id = $1`,
		partID, originLat, originLon, destLat, destLon); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE plans SET notes = CASE WHEN notes = '' AND $2 <> '' THEN $2 ELSE notes END,
			updated_at = NOW()
		WHERE id = (SELECT plan_id FROM plan_parts WHERE id = $1)`,
		partID, in.Notes); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RefreshFlightPartAirframe is the part-keyed counterpart of
// RefreshFlightAirframe: it always bumps last_resolved_at (the resolver
// throttle), and overwrites icao24 / callsign when the supplied values are
// non-empty (empty preserves the existing column). Identical logic, on
// flight_details.
func (s *Store) RefreshFlightPartAirframe(ctx context.Context, partID int64, icao24, callsign string) error {
	icao24 = strings.ToLower(strings.TrimSpace(icao24))
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	_, err := s.pool.Exec(ctx, `
		UPDATE flight_details SET
			icao24           = COALESCE(NULLIF($2, ''), icao24),
			callsign         = COALESCE(NULLIF($3, ''), callsign),
			last_resolved_at = NOW()
		WHERE plan_part_id = $1`, partID, icao24, callsign)
	return err
}

// RefreshFlightPartStatus is the part-keyed counterpart of RefreshFlightStatus:
// re-derives flight_status from the scheduled times alone, preserving terminal
// Cancelled / Diverted, and bumps last_polled_at. Identical CASE logic, on
// flight_details.
func (s *Store) RefreshFlightPartStatus(ctx context.Context, partID int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE flight_details SET
			flight_status = CASE
				WHEN flight_status IN ('Cancelled', 'Diverted') THEN flight_status
				WHEN NOW() > scheduled_in  THEN 'Arrived'
				WHEN NOW() >= scheduled_out THEN 'Enroute'
				ELSE 'Scheduled'
			END,
			last_polled_at = NOW()
		WHERE plan_part_id = $1`, partID)
	return err
}

// ----- Convergence view (spec §7) -----

// TrackerPart is one labelled trackable plan_part for the convergence view. It
// is intentionally flat — the tracker is a read view with no leaderboard /
// ranking, just the fields the front end needs to plot a labelled marker. The
// latest position is attached by the caller via LatestPositions(part_id).
type TrackerPart struct {
	PlanPartID  int64
	PlanID      int64
	TripID      int64
	OwnerID     *int64
	Title       string
	Status      string // the rich flight_status enum
	EffectiveAt time.Time
	Ident       string
	DestIATA    string
}

// trackerVisible is the spec §4 plan-visibility predicate, inlined for the
// tracker queries. $1 = viewerID; it correlates pl/t to the outer row, matching
// the form used by ListVisiblePlanParts so the gate stays identical.
const trackerVisible = `(
	t.created_by = $1
 OR (
	  EXISTS (SELECT 1 FROM trip_members tm
	          WHERE tm.trip_id = pl.trip_id AND tm.user_id = $1)
	  AND (
	       pl.created_by = $1
	    OR EXISTS (SELECT 1 FROM plan_passengers pp
	               WHERE pp.plan_id = pl.id AND pp.user_id = $1)
	    OR NOT EXISTS (SELECT 1 FROM plan_visibility pv WHERE pv.plan_id = pl.id)
	    OR EXISTS (SELECT 1 FROM plan_visibility pv
	               WHERE pv.plan_id = pl.id AND pv.mode = 'hidden_from'
	                 AND NOT EXISTS (SELECT 1 FROM plan_visibility_members m
	                                 WHERE m.plan_id = pl.id AND m.user_id = $1))
	    OR EXISTS (SELECT 1 FROM plan_visibility pv
	               JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
	               WHERE pv.plan_id = pl.id AND pv.mode = 'only_visible_to'
	                 AND m.user_id = $1)
	  )
	)
)`

// effectiveArrival is the SQL for a flight part's effective arrival, mirroring
// FlightDetail.EffectiveIn / flights.go's COALESCE(actual, estimated, scheduled).
const effectiveArrival = `COALESCE(fd.actual_in, fd.estimated_in, fd.scheduled_in)`

// effectiveDeparture mirrors FlightDetail.EffectiveOut; it's what the tracker
// reports as the part's effective_at so the front end sorts uniformly.
const effectiveDeparture = `COALESCE(fd.actual_out, fd.estimated_out, fd.scheduled_out)`

// ConvergenceParts returns every flight part the viewer may see (spec §4 gate)
// whose effective arrival falls within [from, to], newest-arriving last. When
// tag is non-empty, results are restricted to trips carrying that (normalised)
// tag. No ranking — the front end plots the markers itself (spec §7). Dismissed
// parts are excluded. The caller attaches latest positions via LatestPositions.
func (s *Store) ConvergenceParts(ctx context.Context, viewerID int64, from, to time.Time, tag string) ([]*TrackerPart, error) {
	args := []any{viewerID, from, to}
	q := `SELECT part.id, pl.id, pl.trip_id, pl.created_by,
		COALESCE(NULLIF(pl.title, ''), fd.ident) AS title,
		fd.flight_status, ` + effectiveDeparture + `, fd.ident, fd.dest_iata
		FROM plan_parts part
		JOIN flight_details fd ON fd.plan_part_id = part.id
		JOIN plans pl ON pl.id = part.plan_id
		JOIN trips t ON t.id = pl.trip_id
		WHERE pl.type = 'flight'
		  AND part.dismissed_at IS NULL
		  AND ` + effectiveArrival + ` BETWEEN $2 AND $3
		  AND ` + trackerVisible
	if norm := normalizeTag(tag); norm != "" {
		args = append(args, norm)
		q += ` AND EXISTS (SELECT 1 FROM trip_tags tt
		                   WHERE tt.trip_id = pl.trip_id AND tt.label_norm = $4)`
	}
	q += ` ORDER BY ` + effectiveArrival + ` ASC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TrackerPart
	for rows.Next() {
		var tp TrackerPart
		if err := rows.Scan(&tp.PlanPartID, &tp.PlanID, &tp.TripID, &tp.OwnerID,
			&tp.Title, &tp.Status, &tp.EffectiveAt, &tp.Ident, &tp.DestIATA); err != nil {
			return nil, err
		}
		out = append(out, &tp)
	}
	return out, rows.Err()
}

// TrackerPartByID returns the convergence row for a single visible flight part,
// or ErrNotFound when the viewer can't see it / it isn't a flight part. Backs
// the focused single-flight view and the poller's SSE publish.
func (s *Store) TrackerPartByID(ctx context.Context, viewerID, partID int64) (*TrackerPart, error) {
	var tp TrackerPart
	err := s.pool.QueryRow(ctx, `
		SELECT part.id, pl.id, pl.trip_id, pl.created_by,
			COALESCE(NULLIF(pl.title, ''), fd.ident) AS title,
			fd.flight_status, `+effectiveDeparture+`, fd.ident, fd.dest_iata
		FROM plan_parts part
		JOIN flight_details fd ON fd.plan_part_id = part.id
		JOIN plans pl ON pl.id = part.plan_id
		JOIN trips t ON t.id = pl.trip_id
		WHERE part.id = $2 AND pl.type = 'flight' AND `+trackerVisible,
		viewerID, partID).Scan(&tp.PlanPartID, &tp.PlanID, &tp.TripID, &tp.OwnerID,
		&tp.Title, &tp.Status, &tp.EffectiveAt, &tp.Ident, &tp.DestIATA)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &tp, nil
}

// TrackerPartRow returns the convergence row for a flight part WITHOUT a
// visibility gate — for trusted server-side callers (the poller) that apply
// per-recipient visibility separately on the broadcast. ErrNotFound when the
// part is missing or isn't a flight part.
func (s *Store) TrackerPartRow(ctx context.Context, partID int64) (*TrackerPart, error) {
	var tp TrackerPart
	err := s.pool.QueryRow(ctx, `
		SELECT part.id, pl.id, pl.trip_id, pl.created_by,
			COALESCE(NULLIF(pl.title, ''), fd.ident) AS title,
			fd.flight_status, `+effectiveDeparture+`, fd.ident, fd.dest_iata
		FROM plan_parts part
		JOIN flight_details fd ON fd.plan_part_id = part.id
		JOIN plans pl ON pl.id = part.plan_id
		WHERE part.id = $1 AND pl.type = 'flight'`,
		partID).Scan(&tp.PlanPartID, &tp.PlanID, &tp.TripID, &tp.OwnerID,
		&tp.Title, &tp.Status, &tp.EffectiveAt, &tp.Ident, &tp.DestIATA)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &tp, nil
}

// TaggedTripSpan returns the [min(starts_at), max(ends_at)] span across the
// parts of the visible trips carrying tag (spec §7: the tag-derived default
// convergence window). ok is false when the viewer can see no such parts, so
// the caller falls back to the explicit/default window.
func (s *Store) TaggedTripSpan(ctx context.Context, viewerID int64, tag string) (from, to time.Time, ok bool, err error) {
	norm := normalizeTag(tag)
	if norm == "" {
		return time.Time{}, time.Time{}, false, nil
	}
	var minStart, maxEnd *time.Time
	err = s.pool.QueryRow(ctx, `
		SELECT MIN(part.starts_at), MAX(COALESCE(part.ends_at, part.starts_at))
		FROM plan_parts part
		JOIN plans pl ON pl.id = part.plan_id
		JOIN trips t ON t.id = pl.trip_id
		WHERE part.dismissed_at IS NULL
		  AND EXISTS (SELECT 1 FROM trip_tags tt
		              WHERE tt.trip_id = pl.trip_id AND tt.label_norm = $2)
		  AND `+trackerVisible,
		viewerID, norm).Scan(&minStart, &maxEnd)
	if err != nil {
		return time.Time{}, time.Time{}, false, err
	}
	if minStart == nil || maxEnd == nil {
		return time.Time{}, time.Time{}, false, nil
	}
	return *minStart, *maxEnd, true, nil
}

// normalizeTag lowercases and trims a tag label for label_norm matching,
// mirroring the trip_tags.label_norm convention (spec §3.1).
func normalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}
