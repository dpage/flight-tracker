package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dpage/flight-tracker/internal/airports"
)

const flightColumns = `id, ident, scheduled_out, scheduled_in,
	estimated_out, estimated_in, actual_out, actual_in,
	origin_iata, origin_lat, origin_lon,
	dest_iata, dest_lat, dest_lon,
	status, icao24, last_polled_at, created_by, notes, created_at, updated_at`

func scanFlight(row pgx.Row) (*Flight, error) {
	var f Flight
	if err := row.Scan(
		&f.ID, &f.Ident, &f.ScheduledOut, &f.ScheduledIn,
		&f.EstimatedOut, &f.EstimatedIn, &f.ActualOut, &f.ActualIn,
		&f.OriginIATA, &f.OriginLat, &f.OriginLon,
		&f.DestIATA, &f.DestLat, &f.DestLon,
		&f.Status, &f.ICAO24, &f.LastPolledAt, &f.CreatedBy, &f.Notes, &f.CreatedAt, &f.UpdatedAt,
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
			icao24, notes, created_by, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
			CASE
				WHEN NOW() > $3 THEN 'Arrived'
				WHEN NOW() >= $2 THEN 'Enroute'
				ELSE 'Scheduled'
			END)
		RETURNING `+flightColumns,
		ident, in.ScheduledOut, in.ScheduledIn,
		originIATA, originLat, originLon,
		destIATA, destLat, destLon,
		icao24, in.Notes, createdBy))
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
}

func (s *Store) UpdateFlight(ctx context.Context, id int64, in UpdateFlightPayload) (*Flight, error) {
	originIATA := upperPtr(in.OriginIATA)
	destIATA := upperPtr(in.DestIATA)
	// When an IATA changes, re-resolve coordinates from the airport table.
	// nil-pointers leave the existing values untouched via COALESCE.
	var originLat, originLon, destLat, destLon *float64
	if originIATA != nil {
		originLat, originLon = lookupCoords(*originIATA)
	}
	if destIATA != nil {
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
			origin_lat    = COALESCE($5, origin_lat),
			origin_lon    = COALESCE($6, origin_lon),
			dest_iata     = COALESCE($7, dest_iata),
			dest_lat      = COALESCE($8, dest_lat),
			dest_lon      = COALESCE($9, dest_lon),
			icao24        = CASE WHEN $12::boolean THEN $13 ELSE icao24 END,
			notes         = COALESCE($10, notes),
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
		in.ICAO24 != nil, icao24Arg))
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

// LatestRealPosition returns the most recent position from ADS-B / airline
// data (i.e. NOT estimated) for a flight, or nil if there isn't one. The
// dead-reckoner uses this as its anchor point when extrapolating across
// coverage gaps.
func (s *Store) LatestRealPosition(ctx context.Context, flightID int64) (*Position, error) {
	var p Position
	err := s.pool.QueryRow(ctx, `
		SELECT flight_id, ts, lat, lon, altitude_ft, groundspeed_kt, heading_deg, is_estimated
		FROM positions
		WHERE flight_id = $1 AND is_estimated = FALSE
		ORDER BY ts DESC
		LIMIT 1`, flightID,
	).Scan(&p.FlightID, &p.Ts, &p.Lat, &p.Lon,
		&p.AltitudeFt, &p.GroundspeedKt, &p.HeadingDeg, &p.IsEstimated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // genuine "no data yet"
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// RecentTracks returns up to `limit` of the most recent positions per flight
// (oldest first within each entry) for the given IDs. Used to draw the
// "flown so far" polyline on the map. Estimated and real fixes are both
// included; consumers can filter if they only want hard data.
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

// LatestPositions returns the latest position per flight for the given IDs.
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

// PositionsForFlight returns all positions for a single flight, newest first.
func (s *Store) PositionsForFlight(ctx context.Context, flightID int64, limit int) ([]*Position, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx, `
		SELECT flight_id, ts, lat, lon, altitude_ft, groundspeed_kt, heading_deg, is_estimated
		FROM positions
		WHERE flight_id = $1
		ORDER BY ts DESC
		LIMIT $2`, flightID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Position
	for rows.Next() {
		var p Position
		if err := rows.Scan(&p.FlightID, &p.Ts, &p.Lat, &p.Lon,
			&p.AltitudeFt, &p.GroundspeedKt, &p.HeadingDeg, &p.IsEstimated); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// InsertPosition appends a position sample for a flight.
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
