package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dpage/aerly/internal/airports"
)

const flightColumns = `id, ident, scheduled_out, scheduled_in,
	estimated_out, estimated_in, actual_out, actual_in,
	origin_iata, origin_lat, origin_lon,
	dest_iata, dest_lat, dest_lon,
	status, icao24, callsign, last_polled_at, last_resolved_at,
	created_by, notes, is_public,
	created_at, updated_at`

func scanFlight(row pgx.Row) (*Flight, error) {
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

func (s *Store) ListFlights(ctx context.Context) ([]*Flight, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+flightColumns+` FROM flights ORDER BY scheduled_out ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Flight
	for rows.Next() {
		f, err := scanFlight(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FlightsWithMissingCoords returns every flight that has at least one
// NULL coord column. Used by the periodic NULL-coord sweep to find rows
// the embedded airports table or the resolver might now be able to
// satisfy. Terminal-status flights are NOT excluded — historical routes
// still want coords for the map view.
func (s *Store) FlightsWithMissingCoords(ctx context.Context) ([]*Flight, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+flightColumns+` FROM flights
		WHERE origin_lat IS NULL OR origin_lon IS NULL
		   OR dest_lat   IS NULL OR dest_lon   IS NULL
		ORDER BY scheduled_out ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Flight
	for rows.Next() {
		f, err := scanFlight(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) FlightByID(ctx context.Context, id int64) (*Flight, error) {
	return scanFlight(s.pool.QueryRow(ctx,
		`SELECT `+flightColumns+` FROM flights WHERE id = $1`, id))
}

// ActiveFlights returns flights worth polling: anything in a non-terminal
// status whose departure is past (or within 30 minutes of being so). The
// upper bound used to be scheduled_in + 30m, but that left stale flights
// stuck Enroute if the server happened to be down during their landing —
// instead we keep returning them until the next tick promotes them to
// Arrived (via the tracker), at which point the status filter drops them
// out.
func (s *Store) ActiveFlights(ctx context.Context, now time.Time) ([]*Flight, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+flightColumns+` FROM flights
		WHERE status NOT IN ('Arrived', 'Cancelled', 'Diverted')
		  AND $1 >= scheduled_out - INTERVAL '30 minutes'
		ORDER BY scheduled_out ASC`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Flight
	for rows.Next() {
		f, err := scanFlight(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

type CreateFlightPayload struct {
	Ident        string
	ScheduledOut time.Time
	ScheduledIn  time.Time
	OriginIATA   string
	DestIATA     string
	ICAO24       string
	Notes        string
	IsPublic     bool
}

func (s *Store) CreateFlight(ctx context.Context, in CreateFlightPayload, createdBy int64) (*Flight, error) {
	ident := normalizeIdent(in.Ident)
	if ident == "" {
		return nil, errors.New("ident required")
	}
	if in.ScheduledOut.IsZero() || in.ScheduledIn.IsZero() {
		return nil, errors.New("scheduled_out and scheduled_in required")
	}
	if !in.ScheduledIn.After(in.ScheduledOut) {
		return nil, errors.New("scheduled_in must be after scheduled_out")
	}
	originIATA := strings.ToUpper(in.OriginIATA)
	destIATA := strings.ToUpper(in.DestIATA)
	originLat, originLon := lookupCoords(originIATA)
	destLat, destLon := lookupCoords(destIATA)
	icao24 := normalizeICAO24(in.ICAO24)
	// Derive status from the schedule at insert time so a freshly-added
	// flight whose scheduled_out is already past lands as Enroute rather
	// than sitting on the column default ('Scheduled') until the first
	// poll tick refreshes it.
	return scanFlight(s.pool.QueryRow(ctx, `
		INSERT INTO flights (ident, scheduled_out, scheduled_in,
			origin_iata, origin_lat, origin_lon,
			dest_iata,   dest_lat,   dest_lon,
			icao24, notes, created_by, is_public, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			CASE
				WHEN NOW() > $3 THEN 'Arrived'
				WHEN NOW() >= $2 THEN 'Enroute'
				ELSE 'Scheduled'
			END)
		RETURNING `+flightColumns,
		ident, in.ScheduledOut, in.ScheduledIn,
		originIATA, originLat, originLon,
		destIATA, destLat, destLon,
		icao24, in.Notes, createdBy, in.IsPublic))
}

// normalizeICAO24 returns a lowercase hex string with no whitespace, or nil
// if the input is empty.
func normalizeICAO24(s string) *string {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return nil
	}
	return &v
}

// normalizeIdent uppercases the ident and strips ALL whitespace (not just
// leading/trailing). AeroDataBox returns idents like "BA 286"; we want
// "BA286" so it matches the airline's display form and OpenSky callsign
// conventions, and so the user sees a consistent string in the UI.
func normalizeIdent(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}

// lookupCoords returns *float64 (nullable) so callers can pass it straight to
// pgx parameters; nil for unknown IATA codes.
func lookupCoords(iata string) (*float64, *float64) {
	lat, lon, ok := airports.Lookup(iata)
	if !ok {
		return nil, nil
	}
	return &lat, &lon
}

type UpdateFlightPayload struct {
	ScheduledOut *time.Time
	ScheduledIn  *time.Time
	OriginIATA   *string
	DestIATA     *string
	ICAO24       *string
	Notes        *string
	Status       *string
	IsPublic     *bool
}

func (s *Store) UpdateFlight(ctx context.Context, id int64, in UpdateFlightPayload) (*Flight, error) {
	originIATA := upperPtr(in.OriginIATA)
	destIATA := upperPtr(in.DestIATA)
	// When an IATA changes, re-resolve coordinates from the airport table.
	// nil-pointers mean the caller didn't supply that leg, so leave the
	// existing value untouched via CASE. When an IATA IS supplied but isn't
	// in the embedded table, lookupCoords returns nil/nil — we want to write
	// NULL to the coord columns so the handler's backfillCoordsIfNeeded
	// helper can detect the gap and fetch them from the Resolver. Using
	// COALESCE here would silently preserve stale coords from a prior IATA.
	var originLat, originLon, destLat, destLon *float64
	originIATASupplied := originIATA != nil
	destIATASupplied := destIATA != nil
	if originIATASupplied {
		originLat, originLon = lookupCoords(*originIATA)
	}
	if destIATASupplied {
		destLat, destLon = lookupCoords(*destIATA)
	}
	// When the caller does NOT supply a status, derive it from the (possibly
	// just-edited) times. Preserves terminal states the user chose explicitly
	// in a previous edit. Without this, editing the arrival time alone would
	// leave the status pill stale until the next poll tick — and if the new
	// arrival is in the past, the poller drops the row from active_flights
	// and never reconsiders it.
	// in.ICAO24 == nil means "leave alone". A pointer to the empty string
	// means "clear" — represented as a SQL NULL via NULLIF.
	var icao24Arg any
	if in.ICAO24 != nil {
		icao24Arg = normalizeICAO24(*in.ICAO24)
	}
	return scanFlight(s.pool.QueryRow(ctx, `
		UPDATE flights SET
			scheduled_out = COALESCE($2, scheduled_out),
			scheduled_in  = COALESCE($3, scheduled_in),
			origin_iata   = COALESCE($4, origin_iata),
			origin_lat    = CASE WHEN $15::boolean THEN $5 ELSE origin_lat END,
			origin_lon    = CASE WHEN $15::boolean THEN $6 ELSE origin_lon END,
			dest_iata     = COALESCE($7, dest_iata),
			dest_lat      = CASE WHEN $16::boolean THEN $8 ELSE dest_lat END,
			dest_lon      = CASE WHEN $16::boolean THEN $9 ELSE dest_lon END,
			icao24        = CASE WHEN $12::boolean THEN $13 ELSE icao24 END,
			notes         = COALESCE($10, notes),
			is_public     = COALESCE($14, is_public),
			status = COALESCE($11, CASE
				WHEN status IN ('Cancelled', 'Diverted') THEN status
				WHEN NOW() > COALESCE($3, scheduled_in)  THEN 'Arrived'
				WHEN NOW() >= COALESCE($2, scheduled_out) THEN 'Enroute'
				ELSE 'Scheduled'
			END),
			updated_at = NOW()
		WHERE id = $1
		RETURNING `+flightColumns,
		id, in.ScheduledOut, in.ScheduledIn,
		originIATA, originLat, originLon,
		destIATA, destLat, destLon,
		in.Notes, in.Status,
		in.ICAO24 != nil, icao24Arg,
		in.IsPublic,
		originIATASupplied, destIATASupplied))
}

// BackfillPayload carries optional resolver-supplied metadata for a flight
// that was created with blanks. The poller uses this to fill in airports,
// airframe, and notes the first time it sees the row in its active window.
// Empty / zero values are ignored — the SQL only touches columns whose
// current value is empty so user-typed entries are never overwritten.
type BackfillPayload struct {
	OriginIATA string
	OriginLat  float64
	OriginLon  float64
	DestIATA   string
	DestLat    float64
	DestLon    float64
	ICAO24     string
	Callsign   string
	Notes      string
}

// BackfillFlight writes any non-empty fields from in into the matching
// column on the flight row, but only when that column is currently empty
// (empty string, NULL pointer). It's the database side of opportunistic
// metadata backfill — see (*Poller).backfillMetadata.
func (s *Store) BackfillFlight(ctx context.Context, id int64, in BackfillPayload) error {
	icao24 := strings.ToLower(strings.TrimSpace(in.ICAO24))
	callsign := strings.ToUpper(strings.TrimSpace(in.Callsign))
	var originLat, originLon, destLat, destLon *float64
	if in.OriginLat != 0 || in.OriginLon != 0 {
		originLat, originLon = &in.OriginLat, &in.OriginLon
	}
	if in.DestLat != 0 || in.DestLon != 0 {
		destLat, destLon = &in.DestLat, &in.DestLon
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE flights SET
			origin_iata = CASE WHEN origin_iata = '' AND $2 <> '' THEN $2 ELSE origin_iata END,
			origin_lat  = COALESCE(origin_lat, $3),
			origin_lon  = COALESCE(origin_lon, $4),
			dest_iata   = CASE WHEN dest_iata = '' AND $5 <> '' THEN $5 ELSE dest_iata END,
			dest_lat    = COALESCE(dest_lat, $6),
			dest_lon    = COALESCE(dest_lon, $7),
			icao24      = COALESCE(icao24, NULLIF($8, '')),
			callsign    = COALESCE(callsign, NULLIF($10, '')),
			notes       = CASE WHEN notes = '' AND $9 <> '' THEN $9 ELSE notes END,
			updated_at  = NOW()
		WHERE id = $1`,
		id,
		strings.ToUpper(in.OriginIATA), originLat, originLon,
		strings.ToUpper(in.DestIATA), destLat, destLon,
		icao24, in.Notes, callsign)
	return err
}

// RefreshFlightAirframe is the day-of counterpart to BackfillFlight: it
// always bumps last_resolved_at (so the poller can throttle re-resolves),
// and overwrites icao24 / callsign when the supplied values are non-empty.
// An empty input preserves the existing column rather than blanking it —
// resolvers can legitimately omit the airframe (far-future schedules,
// transient outages) and we'd rather keep stale-but-plausible data than
// lose it.
func (s *Store) RefreshFlightAirframe(ctx context.Context, id int64, icao24, callsign string) error {
	icao24 = strings.ToLower(strings.TrimSpace(icao24))
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	_, err := s.pool.Exec(ctx, `
		UPDATE flights SET
			icao24           = COALESCE(NULLIF($2, ''), icao24),
			callsign         = COALESCE(NULLIF($3, ''), callsign),
			last_resolved_at = NOW(),
			updated_at       = NOW()
		WHERE id = $1`, id, icao24, callsign)
	return err
}

// RefreshFlightStatus re-derives status from the row's scheduled times alone,
// preserving terminal Cancelled / Diverted statuses. Called by the poller
// after writing a position so the status pill stays in lockstep with the
// schedule without us having to write extra application logic.
func (s *Store) RefreshFlightStatus(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE flights SET
			status = CASE
				WHEN status IN ('Cancelled', 'Diverted') THEN status
				WHEN NOW() > scheduled_in  THEN 'Arrived'
				WHEN NOW() >= scheduled_out THEN 'Enroute'
				ELSE 'Scheduled'
			END,
			last_polled_at = NOW(),
			updated_at = NOW()
		WHERE id = $1`, id)
	return err
}

func (s *Store) DeleteFlight(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM flights WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AddPassenger(ctx context.Context, flightID, userID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO flight_passengers (flight_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, flightID, userID)
	return err
}

func (s *Store) RemovePassenger(ctx context.Context, flightID, userID int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM flight_passengers WHERE flight_id = $1 AND user_id = $2`,
		flightID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PassengersByFlight returns a flight_id → []user_id map for the given flight IDs.
func (s *Store) PassengersByFlight(ctx context.Context, flightIDs []int64) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	if len(flightIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT flight_id, user_id FROM flight_passengers WHERE flight_id = ANY($1)`,
		flightIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var fid, uid int64
		if err := rows.Scan(&fid, &uid); err != nil {
			return nil, err
		}
		out[fid] = append(out[fid], uid)
	}
	return out, rows.Err()
}

// AddShare grants visibility on a flight to the given user. Idempotent.
func (s *Store) AddShare(ctx context.Context, flightID, userID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO flight_shares (flight_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, flightID, userID)
	return err
}

// RemoveShare revokes share-list visibility. Returns ErrNotFound if the row
// didn't exist (the caller already removed it, or it was never granted).
func (s *Store) RemoveShare(ctx context.Context, flightID, userID int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM flight_shares WHERE flight_id = $1 AND user_id = $2`,
		flightID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SharedUserIDsByFlight returns a flight_id → []user_id map of explicit
// share-list members (not creator, not passengers, not is_public viewers).
func (s *Store) SharedUserIDsByFlight(ctx context.Context, flightIDs []int64) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	if len(flightIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT flight_id, user_id FROM flight_shares WHERE flight_id = ANY($1)`,
		flightIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var fid, uid int64
		if err := rows.Scan(&fid, &uid); err != nil {
			return nil, err
		}
		out[fid] = append(out[fid], uid)
	}
	return out, rows.Err()
}

// VisibleUserIDs returns the union of {creator, passengers, share-list,
// + creator's accepted friends IFF the flight is public} for a single flight
// — the exact set of user IDs that can see the flight through any
// non-superuser-override path. Used by publishers to populate the VisibleTo
// set on SSE events before broadcasting.
//
// The shape matches ListVisibleFlights/CanView: friends only join when the
// flight is public. Callers that want the public-broadcast path explicitly
// (Hub.publishPublic) should consult Flight.IsPublic separately.
func (s *Store) VisibleUserIDs(ctx context.Context, flightID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT created_by FROM flights WHERE id = $1 AND created_by IS NOT NULL
		UNION
		SELECT user_id FROM flight_passengers WHERE flight_id = $1
		UNION
		SELECT user_id FROM flight_shares     WHERE flight_id = $1
		UNION
		SELECT CASE WHEN f.user_low = flights.created_by
		            THEN f.user_high ELSE f.user_low END
		FROM friendships f, flights
		WHERE flights.id = $1
		  AND flights.is_public = TRUE
		  AND f.status = 'accepted'
		  AND flights.created_by IN (f.user_low, f.user_high)`, flightID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}

// ListVisibleFlights returns flights the viewer is allowed to see.
// Visibility rule: created_by=viewer OR passenger OR share-list OR
// (is_public AND friend-of-creator with accepted friendship) OR
// (showAllForSuperuser AND caller is superuser). The superuser-show-all
// branch is gated by the caller — pass true only when the request
// actually originated from a superuser session that opted in.
//
// When showOld is false the result excludes flights whose effective
// arrival (COALESCE actual_in, estimated_in, scheduled_in) is more than
// 24 hours in the past. The age filter applies independently of the
// visibility branch — superusers viewing show-all still get the archive
// hidden unless they also pass showOld.
func (s *Store) ListVisibleFlights(ctx context.Context, viewerID int64, showAllForSuperuser, showOld bool) ([]*Flight, error) {
	q := `SELECT ` + flightColumns + ` FROM flights`
	args := []any{}
	conds := []string{}
	if !showAllForSuperuser {
		conds = append(conds, `(created_by = $1
		   OR EXISTS (SELECT 1 FROM flight_passengers
		              WHERE flight_id = flights.id AND user_id = $1)
		   OR EXISTS (SELECT 1 FROM flight_shares
		              WHERE flight_id = flights.id AND user_id = $1)
		   OR (is_public = TRUE
		       AND EXISTS (SELECT 1 FROM friendships f
		                   WHERE f.status = 'accepted'
		                     AND $1 IN (f.user_low, f.user_high)
		                     AND flights.created_by IN (f.user_low, f.user_high))))`)
		args = append(args, viewerID)
	}
	if !showOld {
		conds = append(conds, `COALESCE(actual_in, estimated_in, scheduled_in) >= NOW() - INTERVAL '24 hours'`)
	}
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += ` ORDER BY scheduled_out ASC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Flight
	for rows.Next() {
		f, err := scanFlight(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CanView reports whether viewerID is allowed to see flightID. The caller
// must pass showAllForSuperuser only when the viewer is in fact a superuser
// who has opted into the show-all view; this function does not check the
// superuser flag itself.
func (s *Store) CanView(ctx context.Context, flightID, viewerID int64, showAllForSuperuser bool) (bool, error) {
	if showAllForSuperuser {
		// Existence check only — keeps the API consistent (CanView on a
		// missing id returns false rather than true-for-everything).
		var n int
		err := s.pool.QueryRow(ctx,
			`SELECT 1 FROM flights WHERE id = $1`, flightID).Scan(&n)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return err == nil, err
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM flights
			WHERE id = $1
			  AND (created_by = $2
			       OR EXISTS (SELECT 1 FROM flight_passengers
			                  WHERE flight_id = $1 AND user_id = $2)
			       OR EXISTS (SELECT 1 FROM flight_shares
			                  WHERE flight_id = $1 AND user_id = $2)
			       OR (is_public = TRUE
			           AND EXISTS (SELECT 1 FROM friendships f
			                       WHERE f.status = 'accepted'
			                         AND $2 IN (f.user_low, f.user_high)
			                         AND flights.created_by IN (f.user_low, f.user_high)))))`,
		flightID, viewerID).Scan(&ok)
	return ok, err
}

// CreatorOf returns the user id that created the flight, or
// ErrNotFound if the flight doesn't exist or has no creator recorded
// (rare; legacy data).
func (s *Store) CreatorOf(ctx context.Context, flightID int64) (int64, error) {
	var creator *int64
	err := s.pool.QueryRow(ctx,
		`SELECT created_by FROM flights WHERE id = $1`, flightID).Scan(&creator)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && creator == nil) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return *creator, nil
}

// CanEdit reports whether viewerID can mutate the flight (rename, change
// schedule, add/remove passengers, add/remove shares, toggle public, or
// delete). True iff viewerID is the creator. Superuser overrides live in
// the handler layer so this function stays a pure data lookup.
func (s *Store) CanEdit(ctx context.Context, flightID, viewerID int64) (bool, error) {
	var creator *int64
	err := s.pool.QueryRow(ctx,
		`SELECT created_by FROM flights WHERE id = $1`, flightID).Scan(&creator)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return creator != nil && *creator == viewerID, nil
}

// ---------------------------------------------------------------------------
// Legacy flight_id-keyed position helpers. The Wave 1 poller/tracker re-key to
// plan_part_id (see positions.go); these stay flight_id-keyed only so the
// legacy /api/flights view keeps round-tripping its own positions through the
// transition. Wave 3 retires them with the flights table. New code must use the
// part-keyed helpers in positions.go.

// LatestPositionsByFlight returns the latest position per flight for the given
// flight IDs (legacy, flight_id-keyed).
func (s *Store) LatestPositions(ctx context.Context, flightIDs []int64) (map[int64]*Position, error) {
	out := map[int64]*Position{}
	if len(flightIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (flight_id)
			flight_id, ts, lat, lon, altitude_ft, groundspeed_kt, heading_deg, is_estimated
		FROM positions
		WHERE flight_id = ANY($1)
		ORDER BY flight_id, ts DESC`, flightIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Position
		if err := rows.Scan(&p.FlightID, &p.Ts, &p.Lat, &p.Lon,
			&p.AltitudeFt, &p.GroundspeedKt, &p.HeadingDeg, &p.IsEstimated); err != nil {
			return nil, err
		}
		pCopy := p
		out[p.FlightID] = &pCopy
	}
	return out, rows.Err()
}

// RecentTracks returns up to perFlight recent positions per flight (oldest
// first within each entry) for the given flight IDs (legacy, flight_id-keyed).
func (s *Store) RecentTracks(ctx context.Context, flightIDs []int64, perFlight int) (map[int64][]*Position, error) {
	out := map[int64][]*Position{}
	if len(flightIDs) == 0 {
		return out, nil
	}
	if perFlight <= 0 {
		perFlight = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT flight_id, ts, lat, lon, altitude_ft, groundspeed_kt, heading_deg, is_estimated
		FROM (
			SELECT *, ROW_NUMBER() OVER (PARTITION BY flight_id ORDER BY ts DESC) AS rn
			FROM positions
			WHERE flight_id = ANY($1)
		) ranked
		WHERE rn <= $2
		ORDER BY flight_id, ts ASC`, flightIDs, perFlight)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Position
		if err := rows.Scan(&p.FlightID, &p.Ts, &p.Lat, &p.Lon,
			&p.AltitudeFt, &p.GroundspeedKt, &p.HeadingDeg, &p.IsEstimated); err != nil {
			return nil, err
		}
		out[p.FlightID] = append(out[p.FlightID], &p)
	}
	return out, rows.Err()
}

// InsertPosition appends a position sample for a flight (legacy, flight_id-keyed).
func (s *Store) InsertPosition(ctx context.Context, p Position) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO positions (flight_id, ts, lat, lon, altitude_ft, groundspeed_kt, heading_deg, is_estimated)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		p.FlightID, p.Ts, p.Lat, p.Lon, p.AltitudeFt, p.GroundspeedKt, p.HeadingDeg, p.IsEstimated)
	return err
}

func upperPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.ToUpper(*s)
	return &v
}
