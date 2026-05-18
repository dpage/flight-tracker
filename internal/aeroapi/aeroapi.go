// Package aeroapi contains the FlightAware AeroAPI client used by the poller,
// plus a Stub backend that synthesises plausible-looking updates for local
// development without a paid API key.
package aeroapi

import (
	"context"
	"time"

	"github.com/dpage/flight-tracker/internal/store"
)

// Update captures everything the poller writes back per tick.
type Update struct {
	Tracking store.TrackingUpdate
	Position *store.Position // optional; nil if no in-air position yet
}

// Client is what the poller depends on. Both Live and Stub satisfy it.
type Client interface {
	Refresh(ctx context.Context, f *store.Flight, now time.Time) (*Update, error)
}
