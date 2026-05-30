package store

import "context"

// FlightForStats is one of the viewer's flight bookings projected for the
// Statistics rollup: the flight carrier (read from the plan model — a
// flight-type plan_part + its flight_details) plus the owning plan's passenger
// list. The legacy flights table is gone, but the Statistics dialog still
// consumes the FlightDTO wire shape, so we rebuild it from plan_parts here.
type FlightForStats struct {
	Flight
	PassengerIDs []int64
}

// MyFlights returns the flight bookings the viewer is a passenger on, for the
// Statistics rollup, newest scheduled-departure first. Scoped to passenger
// membership to mirror the pre-cut-over behaviour (the dialog filtered the
// flight list to flights the user was a passenger on). Dismissed parts are
// excluded; cancelled/diverted are kept so the dialog can report them.
func (s *Store) MyFlights(ctx context.Context, viewerID int64) ([]FlightForStats, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+flightPartColumns+`,
		       ARRAY(SELECT pp2.user_id FROM plan_passengers pp2
		             WHERE pp2.plan_id = pl.id ORDER BY pp2.user_id) AS passenger_ids
		`+flightPartFrom+`
		WHERE part.dismissed_at IS NULL
		  AND EXISTS (SELECT 1 FROM plan_passengers pp
		              WHERE pp.plan_id = pl.id AND pp.user_id = $1)
		ORDER BY fd.scheduled_out DESC`, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FlightForStats
	for rows.Next() {
		var f Flight
		var pax []int64
		if err := rows.Scan(
			&f.ID, &f.Ident, &f.ScheduledOut, &f.ScheduledIn,
			&f.EstimatedOut, &f.EstimatedIn, &f.ActualOut, &f.ActualIn,
			&f.OriginIATA, &f.OriginLat, &f.OriginLon,
			&f.DestIATA, &f.DestLat, &f.DestLon,
			&f.Status, &f.ICAO24, &f.Callsign, &f.LastPolledAt, &f.LastResolvedAt,
			&f.CreatedBy, &f.Notes,
			&f.OriginGate, &f.DestGate, &f.OriginTerminal, &f.DestTerminal,
			&f.IsPublic,
			&f.CreatedAt, &f.UpdatedAt,
			&pax,
		); err != nil {
			return nil, err
		}
		if pax == nil {
			pax = []int64{}
		}
		out = append(out, FlightForStats{Flight: f, PassengerIDs: pax})
	}
	return out, rows.Err()
}
