// Package providers holds the external flight-data integrations: position
// trackers (Tracker) and schedule resolvers (Resolver), together with a
// dead-reckoning wrapper that fills coverage gaps.
//
// Concrete implementations:
//
//   - Stub        — in-memory; interpolates positions from the schedule alone.
//   - OpenSky     — ADS-B state vectors from opensky-network.org.
//   - DeadReckoner — wraps any Tracker and synthesises a position when the
//     inner tracker returns no fresh fix.
//   - AeroDataBox — schedule + airport + airframe lookups via RapidAPI.
package providers

import (
	"context"
	"errors"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// ErrFlightNotFound is returned by Resolver.Resolve (and helpers) when the
// upstream provider has no record of the requested flight. Callers can use
// it to drive fallback behaviour — e.g. AeroDataBox.Resolve tries several
// pad-length variants of the same ident before giving up.
var ErrFlightNotFound = errors.New("flight not found")

// ErrFlightUnscheduled is returned when the upstream knows the flight
// number for the requested date but has not published a schedule for it
// yet (or returned schedule fields we can't parse). Distinct from
// ErrFlightNotFound so the caller can surface a clearer
// "schedule not available" message than the store's bare
// "scheduled_out required".
var ErrFlightUnscheduled = errors.New("flight has no published schedule for that date yet")

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

// ResolvedFlight is the airline-data-source view of a single scheduled
// flight, used to autofill the Add Flight dialog from just an ident + date.
type ResolvedFlight struct {
	Ident        string
	ScheduledOut time.Time
	ScheduledIn  time.Time
	OriginIATA   string
	OriginLat    float64
	OriginLon    float64
	DestIATA     string
	DestLat      float64
	DestLon      float64
	ICAO24       string // 24-bit Mode-S hex address (lowercase) when known
	Callsign     string // ICAO radio callsign (e.g. "DLH493"); empty when not yet assigned
	Notes        string // free-text summary — typically airline + aircraft model
	// Gate / terminal as reported on the departure/arrival movement. Many
	// airports populate these; absent → empty string. Gate changes are what
	// the gate-change alert detects, so the resolver surfaces the live value
	// on every resolve (not just first-fill).
	OriginGate     string
	DestGate       string
	OriginTerminal string
	DestTerminal   string
}

// Resolver maps a flight number + departure date to a ResolvedFlight. The
// concrete implementation is whatever airline-data provider the operator
// has configured (AeroDataBox today; AviationStack / FlightStats / similar
// could slot in here too).
type Resolver interface {
	Resolve(ctx context.Context, ident string, date time.Time) (*ResolvedFlight, error)
}
