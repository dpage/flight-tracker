package geo

import (
	"math"
	"testing"
)

func approx(t *testing.T, got, want, tol float64, name string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want ~%v (tol %v)", name, got, want, tol)
	}
}

func TestRadDeg(t *testing.T) {
	approx(t, Rad(180), math.Pi, 1e-12, "Rad(180)")
	approx(t, Deg(math.Pi), 180, 1e-12, "Deg(pi)")
	approx(t, Deg(Rad(57.3)), 57.3, 1e-9, "roundtrip")
}

func TestSlerpEndpoints(t *testing.T) {
	lat, lon := Slerp(10, 20, 50, 60, 0)
	approx(t, lat, 10, 1e-9, "f=0 lat")
	approx(t, lon, 20, 1e-9, "f=0 lon")

	lat, lon = Slerp(10, 20, 50, 60, 1)
	approx(t, lat, 50, 1e-6, "f=1 lat")
	approx(t, lon, 60, 1e-6, "f=1 lon")
}

func TestSlerpMidpoint(t *testing.T) {
	// Midpoint between (0,0) and (0,90) along the equator is (0,45).
	lat, lon := Slerp(0, 0, 0, 90, 0.5)
	approx(t, lat, 0, 1e-9, "mid lat")
	approx(t, lon, 45, 1e-9, "mid lon")
}

func TestSlerpIdenticalPoints(t *testing.T) {
	// Δ < 1e-9 early-return path: identical endpoints.
	lat, lon := Slerp(12.34, 56.78, 12.34, 56.78, 0.5)
	if lat != 12.34 || lon != 56.78 {
		t.Errorf("identical points: got (%v,%v), want (12.34,56.78)", lat, lon)
	}
}

func TestSlerpClampsCos(t *testing.T) {
	// Antipodal-ish / extreme inputs should not NaN out (exercises the
	// cosΔ clamp branches).
	lat, lon := Slerp(90, 0, -90, 0, 0.5)
	if math.IsNaN(lat) || math.IsNaN(lon) {
		t.Fatalf("got NaN for pole-to-pole midpoint: (%v,%v)", lat, lon)
	}
	approx(t, lat, 0, 1e-6, "pole-to-pole mid lat")
}

func TestBearing(t *testing.T) {
	// Due east along the equator ≈ 90°.
	approx(t, Bearing(0, 0, 0, 10), 90, 1e-6, "east bearing")
	// Due north ≈ 0°.
	approx(t, Bearing(0, 0, 10, 0), 0, 1e-6, "north bearing")
	// Due west wraps into [0,360): ≈ 270°.
	b := Bearing(0, 0, 0, -10)
	approx(t, b, 270, 1e-6, "west bearing (wrapped)")
	if b < 0 || b >= 360 {
		t.Errorf("bearing out of range: %v", b)
	}
}

func TestHaversineNM(t *testing.T) {
	// Zero distance for identical points.
	approx(t, HaversineNM(40, -70, 40, -70), 0, 1e-9, "zero dist")
	// One degree of latitude ≈ 60 nautical miles.
	approx(t, HaversineNM(0, 0, 1, 0), 60, 0.5, "1° lat ≈ 60 NM")
	// LHR → JFK is roughly 3000 NM.
	d := HaversineNM(51.4775, -0.4614, 40.6413, -73.7781)
	if d < 2900 || d > 3100 {
		t.Errorf("LHR→JFK = %v NM, want ~3000", d)
	}
}
