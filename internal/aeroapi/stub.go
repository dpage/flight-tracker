package aeroapi

import (
	"context"
	"time"

	"github.com/dpage/flight-tracker/internal/geo"
	"github.com/dpage/flight-tracker/internal/store"
)

// Stub is an in-memory Tracker that synthesises a plausible position from the
// flight's schedule and the airport-table coordinates. Useful for local
// development when no real flight-data provider is configured.
//
// The Stub does NOT mark its positions as estimated — they aren't a fallback
// from a stale real fix, they're the only source we have in stub mode and
// the UI shouldn't suggest uncertainty for normal stub operation.
type Stub struct{}

func NewStub() *Stub { return &Stub{} }

func (Stub) Track(_ context.Context, f *store.Flight, now time.Time) (*store.Position, error) {
	if now.Before(f.ScheduledOut) || now.After(f.ScheduledIn) {
		return nil, nil //nolint:nilnil // outside the flight's air time
	}
	if f.OriginLat == nil || f.OriginLon == nil || f.DestLat == nil || f.DestLon == nil {
		return nil, nil //nolint:nilnil // no geometry to interpolate along
	}

	total := f.ScheduledIn.Sub(f.ScheduledOut).Seconds()
	if total <= 0 {
		return nil, nil //nolint:nilnil
	}
	frac := now.Sub(f.ScheduledOut).Seconds() / total
	if frac < 0 {
		frac = 0
	} else if frac > 1 {
		frac = 1
	}

	lat, lon := geo.Slerp(*f.OriginLat, *f.OriginLon, *f.DestLat, *f.DestLon, frac)
	heading := geo.Bearing(lat, lon, *f.DestLat, *f.DestLon)
	alt := cruiseAltitude(frac)
	nm := geo.HaversineNM(*f.OriginLat, *f.OriginLon, *f.DestLat, *f.DestLon)
	hours := time.Duration(total * float64(time.Second)).Hours()
	gs := 450.0
	if hours > 0 {
		gs = nm / hours
	}

	altI := int32(alt)
	gsI := int32(gs)
	hdgI := int16(heading)
	return &store.Position{
		FlightID:      f.ID,
		Ts:            now,
		Lat:           lat,
		Lon:           lon,
		AltitudeFt:    &altI,
		GroundspeedKt: &gsI,
		HeadingDeg:    &hdgI,
		IsEstimated:   false,
	}, nil
}

func cruiseAltitude(frac float64) float64 {
	const cruise = 36000.0
	switch {
	case frac < 0.10:
		return cruise * (frac / 0.10)
	case frac > 0.90:
		return cruise * ((1 - frac) / 0.10)
	default:
		return cruise
	}
}
