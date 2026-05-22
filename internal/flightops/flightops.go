// Package flightops contains shared business logic for creating flights,
// usable from both HTTP handlers and the email-ingest pipeline.
package flightops

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dpage/flight-tracker/internal/airports"
	"github.com/dpage/flight-tracker/internal/providers"
	"github.com/dpage/flight-tracker/internal/store"
)

// Deps bundles the collaborators Create needs. Pass nil Resolver to disable
// the create path entirely (it'll return an error).
type Deps struct {
	Store    *store.Store
	Resolver providers.Resolver
}

// Create resolves the given ident+date pair via the configured resolver,
// inserts the resulting flight with createdBy=userID, and attaches userID
// as the sole passenger. Returns the created flight.
//
// date is expected in YYYY-MM-DD; any other format is rejected.
func Create(ctx context.Context, deps Deps, userID int64, ident, date string) (*store.Flight, error) {
	if deps.Store == nil {
		return nil, errors.New("flightops.Create: nil Store")
	}
	if deps.Resolver == nil {
		return nil, errors.New("flightops.Create: no resolver configured")
	}
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, fmt.Errorf("date must be YYYY-MM-DD: %w", err)
	}
	rf, err := deps.Resolver.Resolve(ctx, ident, d)
	if err != nil {
		return nil, fmt.Errorf("resolve %s on %s: %w", ident, date, err)
	}
	f, err := deps.Store.CreateFlight(ctx, store.CreateFlightPayload{
		Ident:        rf.Ident,
		ScheduledOut: rf.ScheduledOut,
		ScheduledIn:  rf.ScheduledIn,
		OriginIATA:   rf.OriginIATA,
		DestIATA:     rf.DestIATA,
		ICAO24:       rf.ICAO24,
		Notes:        rf.Notes,
	}, userID)
	if err != nil {
		return nil, err
	}
	if err := deps.Store.AddPassenger(ctx, f.ID, userID); err != nil {
		return nil, err
	}
	return f, nil
}

// ManualCreatePayload is everything CreateManual needs to insert a flight
// without consulting the schedule resolver. All fields are required.
// Dates are YYYY-MM-DD in each airport's local calendar; times are HH:MM
// (24h) in each airport's local time. The airport timezones are looked up
// in the embedded airports table; if either airport is missing from the
// table, the corresponding local time is treated as UTC (best-effort
// fallback — the user can edit later).
type ManualCreatePayload struct {
	Ident           string
	DepartDate      string
	DepartTimeLocal string
	ArriveDate      string
	ArriveTimeLocal string
	OriginIATA      string
	DestIATA        string
	Notes           string
}

// CreateManual inserts a flight using the caller-supplied schedule details
// instead of asking the resolver. Used by the email-ingest path when the
// upstream provider has no record of the flight yet but the email itself
// contains enough detail to add it.
func CreateManual(ctx context.Context, deps Deps, userID int64, in ManualCreatePayload) (*store.Flight, error) {
	if deps.Store == nil {
		return nil, errors.New("flightops.CreateManual: nil Store")
	}
	out, err := parseLocalDateTime(in.DepartDate, in.DepartTimeLocal, in.OriginIATA)
	if err != nil {
		return nil, fmt.Errorf("departure: %w", err)
	}
	inT, err := parseLocalDateTime(in.ArriveDate, in.ArriveTimeLocal, in.DestIATA)
	if err != nil {
		return nil, fmt.Errorf("arrival: %w", err)
	}
	if !inT.After(out) {
		return nil, fmt.Errorf("scheduled arrival (%s) must be after departure (%s)", inT.Format(time.RFC3339), out.Format(time.RFC3339))
	}
	f, err := deps.Store.CreateFlight(ctx, store.CreateFlightPayload{
		Ident:        in.Ident,
		ScheduledOut: out,
		ScheduledIn:  inT,
		OriginIATA:   in.OriginIATA,
		DestIATA:     in.DestIATA,
		Notes:        in.Notes,
	}, userID)
	if err != nil {
		return nil, err
	}
	if err := deps.Store.AddPassenger(ctx, f.ID, userID); err != nil {
		return nil, err
	}
	return f, nil
}

// parseLocalDateTime combines a YYYY-MM-DD date and HH:MM time and
// interprets them in the IANA timezone of the given IATA. Falls back to
// UTC when either the airport isn't in the embedded table or its tz
// fails to load. Returns the resulting instant in UTC.
func parseLocalDateTime(date, hhmm, iata string) (time.Time, error) {
	combined := date + "T" + hhmm + ":00"
	loc := time.UTC
	if tzName, ok := airports.LookupTZ(iata); ok {
		if l, err := time.LoadLocation(tzName); err == nil {
			loc = l
		}
	}
	t, err := time.ParseInLocation("2006-01-02T15:04:05", combined, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad date/time %q: %w", combined, err)
	}
	return t.UTC(), nil
}
