package aeroapi

import (
	"context"
	"time"

	"github.com/dpage/flight-tracker/internal/geo"
	"github.com/dpage/flight-tracker/internal/store"
)

// DeadReckoner wraps an inner Tracker and synthesises a position when the
// inner tracker returns no fresh fix — useful for ADS-B coverage gaps
// (oceans, polar regions) where OpenSky goes quiet for hours.
//
// Strategy:
//  1. If the inner tracker returns a fix, use it.
//  2. Else, if there's a recent real (non-estimated) position in the DB,
//     extrapolate along the great-circle from that anchor toward the
//     destination. Map remaining wall-clock time onto remaining route
//     fraction. Carry heading/groundspeed from the anchor.
//  3. Else fall back to the inner tracker's nil — the poller will skip
//     emitting a position.
//
// The estimated marker is flagged is_estimated=true so the frontend can
// render it with reduced opacity / dashed outline, and so a later real fix
// is identified correctly by [Store.LatestRealPosition].
type DeadReckoner struct {
	Inner          Tracker
	FreshThreshold time.Duration // a real fix older than this is treated as stale
	Anchor         RealPositionFetcher
}

// RealPositionFetcher abstracts the DB call so DeadReckoner stays decoupled
// from *store.Store directly. [store.Store.LatestRealPosition] satisfies it.
type RealPositionFetcher interface {
	LatestRealPosition(ctx context.Context, flightID int64) (*store.Position, error)
}

// NewDeadReckoner returns a DeadReckoner with a default freshness threshold
// of five minutes — i.e. a real fix is considered "fresh enough" for that
// long after its timestamp before we start filling the gap with estimates.
func NewDeadReckoner(inner Tracker, anchor RealPositionFetcher) *DeadReckoner {
	return &DeadReckoner{
		Inner:          inner,
		FreshThreshold: 5 * time.Minute,
		Anchor:         anchor,
	}
}

func (d *DeadReckoner) Track(ctx context.Context, f *store.Flight, now time.Time) (*store.Position, error) {
	if pos, err := d.Inner.Track(ctx, f, now); err == nil && pos != nil {
		return pos, nil
	} else if err != nil {
		// Note the error but don't surface it — fall back to dead-reckoning.
		// (Logging happens one layer up in the poller.)
		_ = err
	}

	if d.Anchor == nil {
		return nil, nil //nolint:nilnil
	}
	anchor, err := d.Anchor.LatestRealPosition(ctx, f.ID)
	if err != nil || anchor == nil {
		return nil, nil //nolint:nilnil // no anchor → nothing to extrapolate from
	}
	if f.DestLat == nil || f.DestLon == nil {
		return nil, nil //nolint:nilnil // no destination geometry
	}
	if !now.After(anchor.Ts) {
		// Anchor is in the future; can't extrapolate sensibly.
		return nil, nil //nolint:nilnil
	}
	// Map elapsed-since-anchor onto remaining-flight-time. If we're already
	// past the scheduled arrival, clamp the fraction to 1.
	totalRemaining := f.ScheduledIn.Sub(anchor.Ts).Seconds()
	elapsed := now.Sub(anchor.Ts).Seconds()
	var frac float64
	switch {
	case totalRemaining <= 0:
		frac = 1
	case elapsed >= totalRemaining:
		frac = 1
	default:
		frac = elapsed / totalRemaining
	}

	lat, lon := geo.Slerp(anchor.Lat, anchor.Lon, *f.DestLat, *f.DestLon, frac)
	hdg := int16(geo.Bearing(lat, lon, *f.DestLat, *f.DestLon))

	pos := &store.Position{
		FlightID:      f.ID,
		Ts:            now,
		Lat:           lat,
		Lon:           lon,
		HeadingDeg:    &hdg,
		AltitudeFt:    anchor.AltitudeFt,    // assume cruise; if we want we could descend
		GroundspeedKt: anchor.GroundspeedKt, // carry from anchor
		IsEstimated:   true,
	}
	return pos, nil
}
