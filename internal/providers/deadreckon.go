package providers

import (
	"context"
	"time"

	"github.com/dpage/flight-tracker/internal/geo"
	"github.com/dpage/flight-tracker/internal/store"
)

// DeadReckoner wraps an inner Tracker and synthesises a position when the
// inner tracker returns no fresh fix — useful for ADS-B coverage gaps
// (oceans, polar regions) where OpenSky goes quiet for hours, and for
// flights that never report a fix at all (some carriers don't broadcast,
// or the icao24 mapping is off).
//
// Strategy, in order:
//  1. If the inner tracker returns a fix, use it.
//  2. Else, if there's a recent real (non-estimated) position in the DB,
//     extrapolate along the great-circle from that anchor toward the
//     destination. Map remaining wall-clock time onto remaining route
//     fraction. Carry heading/groundspeed from the anchor.
//  3. Else, if origin/destination coords + schedule are known and the
//     flight is in its air-time window, fall back to a schedule-based
//     interpolation from origin → destination (same math the Stub uses).
//     This handles flights that never get a real fix.
//  4. Else nil — the poller skips emitting a position.
//
// Synthesised positions are flagged is_estimated=true so the frontend can
// render them with reduced opacity / dashed outline, and so a later real
// fix is identified correctly by [Store.LatestRealPosition].
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

	// Step 2: extrapolate from the last REAL fix toward the destination.
	if d.Anchor != nil && f.DestLat != nil && f.DestLon != nil {
		if anchor, err := d.Anchor.LatestRealPosition(ctx, f.ID); err == nil && anchor != nil && now.After(anchor.Ts) {
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
			return &store.Position{
				FlightID:      f.ID,
				Ts:            now,
				Lat:           lat,
				Lon:           lon,
				HeadingDeg:    &hdg,
				AltitudeFt:    anchor.AltitudeFt,
				GroundspeedKt: anchor.GroundspeedKt,
				IsEstimated:   true,
			}, nil
		}
	}

	// Step 3: no real fix has ever landed for this flight, but we have
	// origin + destination + schedule. Interpolate along the great-circle
	// from origin toward destination using the elapsed fraction of the
	// scheduled flight time. Same math the Stub uses.
	if f.OriginLat == nil || f.OriginLon == nil || f.DestLat == nil || f.DestLon == nil {
		return nil, nil //nolint:nilnil // no geometry to interpolate along
	}
	if now.Before(f.ScheduledOut) || now.After(f.ScheduledIn) {
		return nil, nil //nolint:nilnil // outside the flight's air-time window
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
	hdg := int16(geo.Bearing(lat, lon, *f.DestLat, *f.DestLon))
	return &store.Position{
		FlightID:    f.ID,
		Ts:          now,
		Lat:         lat,
		Lon:         lon,
		HeadingDeg:  &hdg,
		IsEstimated: true,
	}, nil
}
