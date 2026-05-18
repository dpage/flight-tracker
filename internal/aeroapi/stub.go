package aeroapi

import (
	"context"
	"math"
	"time"

	"github.com/dpage/flight-tracker/internal/store"
)

// Stub is an in-memory backend that synthesises an Update from the flight's
// schedule alone — useful for local development without an AeroAPI key.
// Coordinates are interpolated along a great-circle from origin → destination.
type Stub struct{}

func NewStub() *Stub { return &Stub{} }

func (Stub) Refresh(_ context.Context, f *store.Flight, now time.Time) (*Update, error) {
	tu := store.TrackingUpdate{}

	originLat, originLon, okO := coordsFor(f.OriginIATA, f.OriginLat, f.OriginLon)
	destLat, destLon, okD := coordsFor(f.DestIATA, f.DestLat, f.DestLon)
	if okO {
		tu.OriginIATA = f.OriginIATA
		tu.OriginLat = &originLat
		tu.OriginLon = &originLon
	}
	if okD {
		tu.DestIATA = f.DestIATA
		tu.DestLat = &destLat
		tu.DestLon = &destLon
	}

	switch {
	case now.Before(f.ScheduledOut):
		tu.Status = "Scheduled"
		return &Update{Tracking: tu}, nil
	case now.After(f.ScheduledIn):
		tu.Status = "Arrived"
		tu.ActualIn = ptrTime(f.ScheduledIn)
		tu.ActualOut = ptrTime(f.ScheduledOut)
		return &Update{Tracking: tu}, nil
	}

	tu.Status = "Enroute"
	tu.ActualOut = ptrTime(f.ScheduledOut)

	if !okO || !okD {
		// No way to interpolate; just report status.
		return &Update{Tracking: tu}, nil
	}

	total := f.ScheduledIn.Sub(f.ScheduledOut).Seconds()
	if total <= 0 {
		return &Update{Tracking: tu}, nil
	}
	frac := now.Sub(f.ScheduledOut).Seconds() / total
	if frac < 0 {
		frac = 0
	} else if frac > 1 {
		frac = 1
	}

	lat, lon := slerp(originLat, originLon, destLat, destLon, frac)
	heading := bearing(lat, lon, destLat, destLon)
	alt := cruiseAltitude(frac)
	gs := cruiseGroundspeed(originLat, originLon, destLat, destLon, time.Duration(total*float64(time.Second)))

	altI := int32(alt)
	gsI := int32(gs)
	hdgI := int16(heading)
	pos := &store.Position{
		FlightID:      f.ID,
		Ts:            now,
		Lat:           lat,
		Lon:           lon,
		AltitudeFt:    &altI,
		GroundspeedKt: &gsI,
		HeadingDeg:    &hdgI,
	}
	return &Update{Tracking: tu, Position: pos}, nil
}

// coordsFor returns the best coordinates available: explicit lat/lon if set,
// else looked up from the embedded IATA table, else (0, 0, false).
func coordsFor(iata string, lat, lon *float64) (float64, float64, bool) {
	if lat != nil && lon != nil {
		return *lat, *lon, true
	}
	if iata == "" {
		return 0, 0, false
	}
	return LookupIATA(iata)
}

func ptrTime(t time.Time) *time.Time { return &t }

// slerp interpolates along a great-circle from (lat1, lon1) to (lat2, lon2).
// Adapted from the standard spherical interpolation formula; inputs in degrees.
func slerp(lat1, lon1, lat2, lon2, f float64) (lat, lon float64) {
	φ1, λ1 := rad(lat1), rad(lon1)
	φ2, λ2 := rad(lat2), rad(lon2)

	cosΔ := math.Sin(φ1)*math.Sin(φ2) + math.Cos(φ1)*math.Cos(φ2)*math.Cos(λ2-λ1)
	if cosΔ > 1 {
		cosΔ = 1
	} else if cosΔ < -1 {
		cosΔ = -1
	}
	Δ := math.Acos(cosΔ)
	if Δ < 1e-9 {
		return lat1, lon1
	}
	a := math.Sin((1-f)*Δ) / math.Sin(Δ)
	b := math.Sin(f*Δ) / math.Sin(Δ)
	x := a*math.Cos(φ1)*math.Cos(λ1) + b*math.Cos(φ2)*math.Cos(λ2)
	y := a*math.Cos(φ1)*math.Sin(λ1) + b*math.Cos(φ2)*math.Sin(λ2)
	z := a*math.Sin(φ1) + b*math.Sin(φ2)
	return deg(math.Atan2(z, math.Sqrt(x*x+y*y))), deg(math.Atan2(y, x))
}

func bearing(lat1, lon1, lat2, lon2 float64) float64 {
	φ1, φ2 := rad(lat1), rad(lat2)
	Δλ := rad(lon2 - lon1)
	y := math.Sin(Δλ) * math.Cos(φ2)
	x := math.Cos(φ1)*math.Sin(φ2) - math.Sin(φ1)*math.Cos(φ2)*math.Cos(Δλ)
	b := deg(math.Atan2(y, x))
	if b < 0 {
		b += 360
	}
	return b
}

// haversine returns great-circle distance in nautical miles.
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const earthNM = 3440.065
	φ1, φ2 := rad(lat1), rad(lat2)
	Δφ := rad(lat2 - lat1)
	Δλ := rad(lon2 - lon1)
	a := math.Sin(Δφ/2)*math.Sin(Δφ/2) +
		math.Cos(φ1)*math.Cos(φ2)*math.Sin(Δλ/2)*math.Sin(Δλ/2)
	return 2 * earthNM * math.Asin(math.Min(1, math.Sqrt(a)))
}

func cruiseAltitude(frac float64) float64 {
	// Climb in first 10%, descend in last 10%, otherwise cruise at FL360.
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

func cruiseGroundspeed(lat1, lon1, lat2, lon2 float64, dur time.Duration) float64 {
	nm := haversine(lat1, lon1, lat2, lon2)
	hours := dur.Hours()
	if hours <= 0 {
		return 450
	}
	return nm / hours
}

func rad(d float64) float64 { return d * math.Pi / 180 }
func deg(r float64) float64 { return r * 180 / math.Pi }
