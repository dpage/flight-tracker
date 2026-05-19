package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dpage/flight-tracker/internal/store"
)

func osFlight(icao string) *store.Flight {
	f := &store.Flight{ID: 1}
	if icao != "" {
		f.ICAO24 = &icao
	}
	return f
}

func newOpenSky(t *testing.T, h http.HandlerFunc) *OpenSky {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	o := NewOpenSky("", "")
	o.BaseURL = srv.URL
	return o
}

func TestOpenSkyNoICAO(t *testing.T) {
	o := NewOpenSky("", "")
	if p, err := o.Track(context.Background(), osFlight(""), time.Now()); p != nil || err != nil {
		t.Errorf("no icao24 → (nil,nil), got %v %v", p, err)
	}
}

func TestOpenSkyRateLimited(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err == nil {
		t.Error("expected rate-limit error")
	}
}

func TestOpenSkyNon200(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	})
	if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err == nil {
		t.Error("expected error for non-200")
	}
}

func TestOpenSkyBadJSON(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	})
	if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err == nil {
		t.Error("expected JSON decode error")
	}
}

func TestOpenSkyEmptyStates(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"time":1,"states":[]}`))
	})
	if p, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); p != nil || err != nil {
		t.Errorf("empty states → (nil,nil), got %v %v", p, err)
	}
}

func TestOpenSkyPartialStateNoLatLon(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		// lon (idx5) / lat (idx6) are null.
		_, _ = w.Write([]byte(`{"time":100,"states":[["abc123","CALL","UK",100,100,null,null,null,false]]}`))
	})
	if p, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); p != nil || err != nil {
		t.Errorf("missing lat/lon → (nil,nil), got %v %v", p, err)
	}
}

func TestOpenSkyFullState(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("icao24") != "abc123" {
			t.Errorf("expected lowercased icao24 query, got %q", r.URL.RawQuery)
		}
		// idx: 0 icao,1 call,2 country,3 time_position,4 last_contact,
		// 5 lon,6 lat,7 baro_alt(m),8 on_ground,9 velocity(m/s),10 track
		_, _ = w.Write([]byte(`{"time":1700000000,"states":[["abc123","BA286 ","UK",1700000111,1700000111,-30.5,45.2,10668,false,231.5,87.0]]}`))
	})
	p, err := o.Track(context.Background(), osFlight("ABC123"), time.Now())
	if err != nil || p == nil {
		t.Fatalf("expected a position, got %v %v", p, err)
	}
	if p.Lat != 45.2 || p.Lon != -30.5 {
		t.Errorf("lat/lon = (%v,%v)", p.Lat, p.Lon)
	}
	// time_position present → ts from that field.
	if p.Ts.Unix() != 1700000111 {
		t.Errorf("ts = %v, want time_position", p.Ts.Unix())
	}
	if p.AltitudeFt == nil || *p.AltitudeFt < 30000 {
		t.Errorf("altitude conversion wrong: %v", p.AltitudeFt)
	}
	if p.GroundspeedKt == nil || *p.GroundspeedKt < 400 {
		t.Errorf("groundspeed conversion wrong: %v", p.GroundspeedKt)
	}
	if p.HeadingDeg == nil || *p.HeadingDeg != 87 {
		t.Errorf("heading = %v, want 87", p.HeadingDeg)
	}
	if p.IsEstimated {
		t.Error("OpenSky fixes are real, not estimated")
	}
}

func TestOpenSkyNoTimePositionUsesGlobalTime(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		// time_position (idx3) null → fall back to top-level "time".
		_, _ = w.Write([]byte(`{"time":1700000000,"states":[["abc123","C","UK",null,1,10.0,20.0]]}`))
	})
	p, err := o.Track(context.Background(), osFlight("abc123"), time.Now())
	if err != nil || p == nil {
		t.Fatalf("got %v %v", p, err)
	}
	if p.Ts.Unix() != 1700000000 {
		t.Errorf("ts = %d, want global time 1700000000", p.Ts.Unix())
	}
}

func TestOpenSkyAuthedRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "user" || p != "pass" {
			t.Errorf("expected basic auth user/pass, got ok=%v u=%q", ok, u)
		}
		_, _ = w.Write([]byte(`{"time":1,"states":[]}`))
	}))
	t.Cleanup(srv.Close)
	o := NewOpenSky("user", "pass")
	o.BaseURL = srv.URL
	if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err != nil {
		t.Fatalf("authed request failed: %v", err)
	}
}

func TestStateHelpers(t *testing.T) {
	st := []interface{}{"a", nil, 3.0}
	if _, ok := stateFloat(st, 99); ok {
		t.Error("out-of-range index should be !ok")
	}
	if _, ok := stateFloat(st, 1); ok {
		t.Error("nil element should be !ok")
	}
	if _, ok := stateFloat(st, 0); ok {
		t.Error("non-float element should be !ok")
	}
	v, ok := stateFloat(st, 2)
	if !ok || v != 3.0 {
		t.Errorf("stateFloat = %v,%v", v, ok)
	}
	if _, ok := stateInt(st, 99); ok {
		t.Error("out-of-range int should be !ok")
	}
	if _, ok := stateInt(st, 1); ok {
		t.Error("nil int should be !ok")
	}
	if _, ok := stateInt(st, 0); ok {
		t.Error("non-numeric int should be !ok")
	}
	n, ok := stateInt(st, 2)
	if !ok || n != 3 {
		t.Errorf("stateInt = %v,%v", n, ok)
	}
}
