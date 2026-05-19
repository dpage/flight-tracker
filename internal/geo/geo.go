// Package geo holds the great-circle math shared by the stub backend, the
// dead-reckoner, and any future tracker that needs to interpolate between two
// points on the sphere. All angles are in degrees; distance is nautical miles.
package geo

import "math"

const earthNM = 3440.065 // Earth's mean radius in nautical miles

// Slerp samples a point along the great-circle from (lat1, lon1) to (lat2, lon2)
// at parameter f ∈ [0, 1]. f=0 returns the first point, f=1 the second.
func Slerp(lat1, lon1, lat2, lon2, f float64) (lat, lon float64) {
	φ1, λ1 := Rad(lat1), Rad(lon1)
	φ2, λ2 := Rad(lat2), Rad(lon2)

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
	return Deg(math.Atan2(z, math.Sqrt(x*x+y*y))), Deg(math.Atan2(y, x))
}

// Bearing returns the initial bearing in degrees [0, 360) from (lat1, lon1)
// toward (lat2, lon2) along the great-circle.
func Bearing(lat1, lon1, lat2, lon2 float64) float64 {
	φ1, φ2 := Rad(lat1), Rad(lat2)
	Δλ := Rad(lon2 - lon1)
	y := math.Sin(Δλ) * math.Cos(φ2)
	x := math.Cos(φ1)*math.Sin(φ2) - math.Sin(φ1)*math.Cos(φ2)*math.Cos(Δλ)
	b := Deg(math.Atan2(y, x))
	if b < 0 {
		b += 360
	}
	return b
}

// HaversineNM returns the great-circle distance in nautical miles between two
// points on the sphere.
func HaversineNM(lat1, lon1, lat2, lon2 float64) float64 {
	φ1, φ2 := Rad(lat1), Rad(lat2)
	Δφ := Rad(lat2 - lat1)
	Δλ := Rad(lon2 - lon1)
	a := math.Sin(Δφ/2)*math.Sin(Δφ/2) +
		math.Cos(φ1)*math.Cos(φ2)*math.Sin(Δλ/2)*math.Sin(Δλ/2)
	return 2 * earthNM * math.Asin(math.Min(1, math.Sqrt(a)))
}

func Rad(d float64) float64 { return d * math.Pi / 180 }
func Deg(r float64) float64 { return r * 180 / math.Pi }
