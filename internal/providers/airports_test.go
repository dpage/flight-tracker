package providers

import "testing"

func TestLookupIATAPassthrough(t *testing.T) {
	lat, lon, ok := LookupIATA("lhr")
	if !ok || lat == 0 || lon == 0 {
		t.Errorf("LookupIATA(LHR) = %v,%v,%v", lat, lon, ok)
	}
	if _, _, ok := LookupIATA("nope"); ok {
		t.Error("unknown code should be !ok")
	}
}
