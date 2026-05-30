package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// newADB builds an AeroDataBox client pointed at a httptest server and
// disables the production 1-req/sec limiter + shortens the 429 retry
// wait, so individual unit tests don't each pay seconds of real wall
// time on rate-limit paths. Tests that need to verify the limiter or
// retry-wait timing can override either field.
func newADB(t *testing.T, h http.HandlerFunc) *AeroDataBox {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	a := NewAeroDataBox("apikey")
	a.BaseURL = srv.URL
	a.Limiter = rate.NewLimiter(rate.Inf, 1)
	a.RetryWait = 5 * time.Millisecond
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

// AeroDataBox answers a well-formed lookup that simply has no matching
// schedule with 204 No Content (empty body) rather than 404. It must read
// as a clean not-found, never leak a raw "aerodatabox 204:" status.
func TestAeroDataBoxNoContent(t *testing.T) {
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	_, err := a.Resolve(context.Background(), "BA087", time.Now())
	if err == nil {
		t.Fatal("expected an error for a 204 response")
	}
	if strings.Contains(err.Error(), "204") {
		t.Fatalf("204 status leaked into the error message: %q", err)
	}
	if !strings.Contains(err.Error(), "no flight found") {
		t.Fatalf("want a friendly not-found message, got %q", err)
	}
}

func TestAeroDataBoxRateLimited(t *testing.T) {
	var calls atomic.Int32
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if _, err := a.Resolve(context.Background(), "BA286", time.Now()); err == nil {
		t.Error("expected rate-limit error")
	}
	// resolveOne retries 429 once before giving up.
	if got := calls.Load(); got != 2 {
		t.Errorf("server saw %d calls, want 2 (initial + one retry)", got)
	}
}

// A 429 followed by a 200 on retry returns the success cleanly — the
// retry path hides the transient throttle from the caller.
func TestAeroDataBoxRetryHidesTransient429(t *testing.T) {
	var calls atomic.Int32
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`[{"number":"BA286","codeshareStatus":"IsOperator",
			"departure":{"airport":{"iata":"LHR"},"scheduledTime":{"utc":"2026-05-19T08:30Z"}},
			"arrival":{"airport":{"iata":"SFO"},"scheduledTime":{"utc":"2026-05-19T19:45Z"}}}]`))
	})
	rf, err := a.Resolve(context.Background(), "BA286", time.Now())
	if err != nil {
		t.Fatalf("Resolve after retry: %v", err)
	}
	if rf.OriginIATA != "LHR" || rf.DestIATA != "SFO" {
		t.Errorf("got %+v", rf)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("server saw %d calls, want 2", got)
	}
}

// On 429, the upstream "message" field (when AeroDataBox supplies one)
// surfaces in the final error so the UI shows something specific.
func TestAeroDataBoxRateLimitSurfacesUpstreamMessage(t *testing.T) {
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"You have exceeded the rate limit per second for your plan, PRO, by the API provider"}`))
	})
	_, err := a.Resolve(context.Background(), "BA286", time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "PRO") {
		t.Errorf("upstream message should appear in error: %v", err)
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

// AeroDataBox sometimes returns a flight record for a known ident on a
// known route, but with no scheduledTime — typical for future flights
// whose specific-date schedule hasn't been published by the airline yet.
// We must surface that as ErrFlightUnscheduled, not let zero times reach
// the store (where they trigger the cryptic "scheduled_out and
// scheduled_in required" error).
// AeroDataBox returns the ICAO radio callsign as `callSign` (e.g. "DLH493"
// for Lufthansa LH493). The resolver should pass it through, trimming
// whitespace; far-future flights return without a callSign and that's fine.
func TestAeroDataBoxCarriesCallsign(t *testing.T) {
	body := `[{"number":"LH 493","codeshareStatus":"IsOperator","callSign":" DLH493 ",
	  "departure":{"airport":{"iata":"YVR"},"scheduledTime":{"utc":"2026-05-25 23:15Z"}},
	  "arrival":{"airport":{"iata":"FRA"},"scheduledTime":{"utc":"2026-05-26T09:00Z"}},
	  "aircraft":{"modeS":"3C4A8C","model":"Boeing 747"}}]`
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	rf, err := a.Resolve(context.Background(), "LH493", time.Now())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rf.Callsign != "DLH493" {
		t.Errorf("callsign = %q, want DLH493 (trimmed)", rf.Callsign)
	}
}

func TestAeroDataBoxFlightWithoutSchedule(t *testing.T) {
	body := `[{"number":"EZY2824","codeshareStatus":"IsOperator",
	  "departure":{"airport":{"iata":"BRS"}},
	  "arrival":{"airport":{"iata":"PMI"}}}]`
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	_, err := a.Resolve(context.Background(), "EZY2824", time.Now())
	if err == nil {
		t.Fatal("expected error when AeroDataBox returns a flight without schedule")
	}
	if !errors.Is(err, ErrFlightUnscheduled) {
		t.Errorf("err = %v; want errors.Is(ErrFlightUnscheduled)", err)
	}
}

// One leg of a return journey may be scheduled while the other isn't.
// We must catch the half-scheduled case the same way.
func TestAeroDataBoxFlightWithOnlyDepartureTime(t *testing.T) {
	body := `[{"number":"EZY2824","codeshareStatus":"IsOperator",
	  "departure":{"airport":{"iata":"BRS"},"scheduledTime":{"utc":"2027-01-25T08:00Z"}},
	  "arrival":{"airport":{"iata":"PMI"}}}]`
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	_, err := a.Resolve(context.Background(), "EZY2824", time.Now())
	if !errors.Is(err, ErrFlightUnscheduled) {
		t.Errorf("err = %v; want errors.Is(ErrFlightUnscheduled)", err)
	}
}

func TestAeroDataBoxFirstWhenNoOperator(t *testing.T) {
	body := `[{"number":"","codeshareStatus":"Unknown",
		"departure":{"airport":{"iata":"AAA"},"scheduledTime":{"utc":"2026-05-19T08:30Z"}},
		"arrival":{"airport":{"iata":"BBB"},"scheduledTime":{"utc":"2026-05-19T09:30Z"}}}]`
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

// AeroDataBox returns gate + terminal on the departure/arrival movement for
// many airports; the resolver must surface them (trimmed). Absent fields → "".
func TestAeroDataBoxCarriesGateAndTerminal(t *testing.T) {
	body := `[{"number":"BA 286","codeshareStatus":"IsOperator",
	  "departure":{"airport":{"iata":"LHR"},"scheduledTime":{"utc":"2026-05-19 08:30Z"},"gate":" B32 ","terminal":"5"},
	  "arrival":{"airport":{"iata":"SFO"},"scheduledTime":{"utc":"2026-05-19T19:45Z"},"gate":"A12"}}]`
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	rf, err := a.Resolve(context.Background(), "BA286", time.Now())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rf.OriginGate != "B32" {
		t.Errorf("origin gate = %q, want B32 (trimmed)", rf.OriginGate)
	}
	if rf.DestGate != "A12" {
		t.Errorf("dest gate = %q, want A12", rf.DestGate)
	}
	if rf.OriginTerminal != "5" {
		t.Errorf("origin terminal = %q, want 5", rf.OriginTerminal)
	}
	if rf.DestTerminal != "" {
		t.Errorf("dest terminal = %q, want empty (absent)", rf.DestTerminal)
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

func TestIdentVariants(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		// At most two candidates: the user's literal input, and the canonical
		// 4-digit form (only if different). Wider zero-paddings were dropped
		// because they were a real driver of 429 bursts on tighter plans.
		{"BA87", []string{"BA87", "BA0087"}},
		{"BA087", []string{"BA087", "BA0087"}},
		{"BA0087", []string{"BA0087"}}, // already canonical → single try
		{"BA00087", []string{"BA00087", "BA0087"}},

		// Airline codes can include digits ("9W" = Jet Airways). The regex
		// is non-greedy on the prefix so the trailing run of digits is what
		// gets re-padded, not the airline-code digit.
		{"9W420", []string{"9W420", "9W0420"}},

		// 4-digit ident with no leading zero — already canonical width.
		{"AC1234", []string{"AC1234"}},
		// More than 4 digits — we don't generate any 4-pad form.
		{"AC12345", []string{"AC12345"}},

		// Pathological inputs pass through unchanged.
		{"", []string{""}},
		{"GIBBERISH", []string{"GIBBERISH"}},
		{"BA0000", []string{"BA0000"}},
		{"BA", []string{"BA"}},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := identVariants(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("identVariants(%q) = %v; want %v", c.in, got, c.want)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 5, 19, 14, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"  ", 0},
		{"5", 5 * time.Second},
		{"0", 0},
		{"-1", 0},
		{"junk", 0},
		// HTTP-date form, 90s in the future.
		{now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second},
		// HTTP-date form in the past → 0 (don't return a negative wait).
		{now.Add(-time.Minute).Format(http.TimeFormat), 0},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := parseRetryAfter(c.in, now)
			// Accept ±1s slop on HTTP-date round-tripping (second-precision).
			if got < c.want-time.Second || got > c.want+time.Second {
				t.Errorf("parseRetryAfter(%q) = %v; want ~%v", c.in, got, c.want)
			}
		})
	}
}

// 429 with a Retry-After header surfaces the wait time in the error
// message so the UI can render something useful.
func TestAeroDataBoxRateLimitedWithRetryAfter(t *testing.T) {
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := a.Resolve(context.Background(), "BA0087", time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "42") {
		t.Errorf("error should mention the retry-after seconds: %v", err)
	}
}

// Verifies that AeroDataBox.Resolve retries with zero-padded variants of the
// requested ident on a not-found response, and stops as soon as one variant
// hits. Airlines refer to BA87 / BA087 / BA0087 interchangeably; we want any
// of those to find the record AeroDataBox stores under just one of them.
func TestAeroDataBoxResolveTriesPaddedVariants(t *testing.T) {
	var calls atomic.Int32
	a := newADB(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// The handler simulates AeroDataBox storing this flight only under
		// the 4-digit padded form. Anything else is a 204.
		switch {
		case strings.Contains(r.URL.Path, "/BA0087/"):
			_, _ = w.Write([]byte(`[{"number":"BA0087","codeshareStatus":"IsOperator",
				"departure":{"airport":{"iata":"LHR"},"scheduledTime":{"utc":"2026-05-19T08:30Z"}},
				"arrival":{"airport":{"iata":"YVR"},"scheduledTime":{"utc":"2026-05-19T19:45Z"}}}]`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	rf, err := a.Resolve(context.Background(), "BA87", time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rf.Ident != "BA0087" {
		t.Errorf("got ident %q, want BA0087", rf.Ident)
	}
	// Tried BA87 (204) → BA0087 (hit). With the variant cap at 2 the loop
	// stops after the canonical form regardless of the result.
	if got := calls.Load(); got != 2 {
		t.Errorf("server saw %d calls, want 2", got)
	}
}

// When every variant returns not-found, Resolve surfaces ErrFlightNotFound
// so callers can distinguish it from network / quota failures.
func TestAeroDataBoxResolveAllVariantsMiss(t *testing.T) {
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	_, err := a.Resolve(context.Background(), "BA87", time.Now())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrFlightNotFound) {
		t.Errorf("err = %v; want errors.Is(ErrFlightNotFound)", err)
	}
}

// On a hard transport-level error, Resolve must NOT keep retrying — that
// would burn API quota and mask the real problem.
func TestAeroDataBoxResolveDoesNotRetryHardErrors(t *testing.T) {
	var calls atomic.Int32
	a := newADB(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("oh no"))
	})
	if _, err := a.Resolve(context.Background(), "BA87", time.Now()); err == nil {
		t.Fatal("expected an error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server saw %d calls, want exactly 1", got)
	}
}
