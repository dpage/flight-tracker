package airports

import "testing"

func TestLookupKnown(t *testing.T) {
	lat, lon, ok := Lookup("LHR")
	if !ok {
		t.Fatal("LHR should be known")
	}
	if lat == 0 || lon == 0 {
		t.Errorf("LHR coords look wrong: (%v,%v)", lat, lon)
	}
}

func TestLookupCaseAndWhitespaceInsensitive(t *testing.T) {
	want, wlon, _ := Lookup("LHR")
	got, glon, ok := Lookup("  lhr ")
	if !ok || got != want || glon != wlon {
		t.Errorf("case/space-insensitive lookup failed: ok=%v (%v,%v) vs (%v,%v)",
			ok, got, glon, want, wlon)
	}
}

func TestLookupUnknown(t *testing.T) {
	lat, lon, ok := Lookup("ZZZ")
	if ok || lat != 0 || lon != 0 {
		t.Errorf("unknown code should return zeros+false, got (%v,%v,%v)", lat, lon, ok)
	}
}

func TestLookupEmpty(t *testing.T) {
	if _, _, ok := Lookup(""); ok {
		t.Error("empty code should not resolve")
	}
}

func TestTableEntriesPlausible(t *testing.T) {
	for code, e := range table {
		if len(code) != 3 {
			t.Errorf("IATA code %q is not 3 letters", code)
		}
		if e.Lat < -90 || e.Lat > 90 || e.Lon < -180 || e.Lon > 180 {
			t.Errorf("%s has out-of-range coords (%v,%v)", code, e.Lat, e.Lon)
		}
		if e.Name == "" {
			t.Errorf("%s has empty name", code)
		}
	}
}
