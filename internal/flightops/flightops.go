// Package flightops contains shared business logic for creating flights,
// usable from both HTTP handlers and the email-ingest pipeline.
package flightops

import (
	"context"
	"errors"
	"fmt"
	"time"

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
