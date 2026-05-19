package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newADB(t *testing.T, h http.HandlerFunc) *AeroDataBox {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	a := NewAeroDataBox("apikey")
	a.BaseURL = srv.URL
	return a
}

func TestAeroDataBoxEmptyIdent(t *testing.T) {
	a := NewAeroDataBox("k")
	if _, err := a.Resolve(context.Background(), "  ", time.Now()); err == nil {
		t.Error("expected error for empty ident")
	}
}

func TestAeroDataBoxNotFound(t *testing.T) {
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := a.Resolve(context.Background(), "BA286", time.Now()); err == nil {
		t.Error("expected not-found error")
	}
}

func TestAeroDataBoxRateLimited(t *testing.T) {
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if _, err := a.Resolve(context.Background(), "BA286", time.Now()); err == nil {
		t.Error("expected rate-limit error")
	}
}

func TestAeroDataBoxServerError(t *testing.T) {
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream down"))
	})
	if _, err := a.Resolve(context.Background(), "BA286", time.Now()); err == nil {
		t.Error("expected error for 502")
	}
}

func TestAeroDataBoxBadJSON(t *testing.T) {
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	})
	if _, err := a.Resolve(context.Background(), "BA286", time.Now()); err == nil {
		t.Error("expected JSON error")
	}
}

func TestAeroDataBoxEmptyArray(t *testing.T) {
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	if _, err := a.Resolve(context.Background(), "BA286", time.Now()); err == nil {
		t.Error("expected no-flight error for empty array")
	}
}

func TestAeroDataBoxPicksOperatorAndBuilds(t *testing.T) {
	body := `[
	  {"number":"BA 999","codeshareStatus":"IsCodeshared","departure":{"airport":{"iata":"XXX"}},"arrival":{"airport":{"iata":"YYY"}}},
	  {"number":"BA 286","codeshareStatus":"IsOperator",
	   "departure":{"airport":{"iata":"LHR","location":{"lat":51.47,"lon":-0.46}},"scheduledTime":{"utc":"2026-05-19 08:30Z"}},
	   "arrival":{"airport":{"iata":"SFO","location":{"lat":37.62,"lon":-122.38}},"scheduledTime":{"utc":"2026-05-19T19:45:00Z"}},
	   "aircraft":{"modeS":" 400A1D ","model":"Boeing 777"},
	   "airline":{"name":"British Airways"}}
	]`
	a := newADB(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-RapidAPI-Key") != "apikey" {
			t.Errorf("missing rapidapi key header")
		}
		_, _ = w.Write([]byte(body))
	})
	rf, err := a.Resolve(context.Background(), "ba286", time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rf.Ident != "BA286" {
		t.Errorf("ident = %q, want BA286 (whitespace stripped)", rf.Ident)
	}
	if rf.OriginIATA != "LHR" || rf.DestIATA != "SFO" {
		t.Errorf("airports = %s/%s", rf.OriginIATA, rf.DestIATA)
	}
	if rf.OriginLat == 0 || rf.DestLon == 0 {
		t.Error("expected location coords")
	}
	if rf.ICAO24 != "400a1d" {
		t.Errorf("icao24 = %q, want lowercased/trimmed 400a1d", rf.ICAO24)
	}
	if rf.ScheduledOut.IsZero() || rf.ScheduledIn.IsZero() {
		t.Error("scheduled times not parsed")
	}
	if rf.Notes != "British Airways · Boeing 777" {
		t.Errorf("notes = %q", rf.Notes)
	}
}

func TestAeroDataBoxFirstWhenNoOperator(t *testing.T) {
	body := `[{"number":"","codeshareStatus":"Unknown","departure":{"airport":{"iata":"AAA"}},"arrival":{"airport":{"iata":"BBB"}}}]`
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	rf, err := a.Resolve(context.Background(), "ZZ1", time.Now())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// number empty → falls back to the requested ident (uppercased).
	if rf.Ident != "ZZ1" {
		t.Errorf("ident fallback = %q, want ZZ1", rf.Ident)
	}
	if rf.Notes != "" {
		t.Errorf("notes should be empty without airline/aircraft, got %q", rf.Notes)
	}
}

func TestParseADBTime(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"2026-05-19T08:30:00Z", false},      // RFC3339
		{"2026-05-19 08:30Z", false},         // space + no seconds
		{"2026-05-19T08:30Z", false},         // T + no seconds
		{"2026-05-19 08:30:00+02:00", false}, // offset form
		{"", true},
		{"garbage", true},
	}
	for _, c := range cases {
		got, err := parseADBTime(c.in)
		if c.wantErr && err == nil {
			t.Errorf("parseADBTime(%q) expected error", c.in)
		}
		if !c.wantErr {
			if err != nil {
				t.Errorf("parseADBTime(%q): %v", c.in, err)
			}
			if got.Location() != time.UTC {
				t.Errorf("parseADBTime(%q) not normalised to UTC", c.in)
			}
		}
	}
}

func TestBuildResolvedNilSubObjects(t *testing.T) {
	f := &adbFlight{Number: "AA1"}
	r := buildResolved(f, "FALLBACK")
	if r.Ident != "AA1" || r.ICAO24 != "" || r.Notes != "" {
		t.Errorf("unexpected resolved: %+v", r)
	}
	// Bad scheduled time string leaves ScheduledOut zero (parse error branch).
	f2 := &adbFlight{Number: "AA2", Departure: adbMovement{ScheduledTime: &adbTime{UTC: "bad"}}}
	r2 := buildResolved(f2, "FB")
	if !r2.ScheduledOut.IsZero() {
		t.Error("bad time should leave ScheduledOut zero")
	}
}

func TestAeroDataBoxResolverInterface(t *testing.T) {
	var _ Resolver = (*AeroDataBox)(nil)
}
