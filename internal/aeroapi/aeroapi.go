// Package aeroapi holds the flight-tracking backends — implementations of
// the Tracker interface called once per poll cycle for each active flight.
//
// The package is named "aeroapi" for historical reasons (the project started
// with a FlightAware AeroAPI integration in mind). It now houses the OpenSky
// tracker, the in-memory stub, and a dead-reckoning wrapper. A future
// AeroAPI-style live tracker would live here too.
package aeroapi

import (
	"context"
	"time"

	"github.com/dpage/flight-tracker/internal/store"
)

// Tracker fetches (or fabricates) a single positional fix for one flight at
// the given wall-clock time. Implementations should return:
//
//   - a non-nil *store.Position with IsEstimated set appropriately, OR
//   - nil, nil  if no fix is available (e.g. ADS-B silence; the caller may
//     hand the situation to a fallback such as a DeadReckoner).
//
// Trackers are NOT responsible for updating any of the flight's schedule /
// status fields — that derivation happens in SQL from the times alone.
type Tracker interface {
	Track(ctx context.Context, f *store.Flight, now time.Time) (*store.Position, error)
}
