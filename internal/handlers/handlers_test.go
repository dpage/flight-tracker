package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dpage/flight-tracker/internal/auth"
	"github.com/dpage/flight-tracker/internal/config"
	"github.com/dpage/flight-tracker/internal/providers"
	"github.com/dpage/flight-tracker/internal/sse"
	"github.com/dpage/flight-tracker/internal/store"
	"github.com/dpage/flight-tracker/internal/testsupport"
)

var sessKey = []byte("handlers-test-session-key-32chars!!")

type fakeResolver struct {
	rf  *providers.ResolvedFlight
	err error
}

func (f fakeResolver) Resolve(context.Context, string, time.Time) (*providers.ResolvedFlight, error) {
	return f.rf, f.err
}

type testEnv struct {
	mux   *http.ServeMux
	store *store.Store
	cfg   *config.Config
}

func setup(t *testing.T, resolver providers.Resolver, cfg *config.Config) *testEnv {
	t.Helper()
	pool := testsupport.NewPool(t)
	s := store.New(pool)
	a := auth.NewHandler("cid", "csec", sessKey, "http://localhost:8080", s)
	hub := sse.NewHub()
	if cfg == nil {
		cfg = &config.Config{}
	}
	api := New(s, a, hub, cfg, resolver)
	mux := http.NewServeMux()
	api.Register(mux)
	return &testEnv{mux: mux, store: s, cfg: cfg}
}

func (e *testEnv) req(t *testing.T, method, path string, body any, asUser int64) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, path, rdr)
	if asUser != 0 {
		r.AddCookie(&http.Cookie{
			Name:  auth.SessionCookie,
			Value: auth.SignSession(sessKey, asUser, time.Now().Add(time.Hour)),
		})
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return w
}

func (e *testEnv) user(t *testing.T, login string, super bool) int64 {
	t.Helper()
	u, err := e.store.InviteUser(context.Background(), store.InvitePayload{
		GitHubLogin: login, Name: login, IsSuperuser: super,
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u.ID
}

func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	return v
}

func TestRequiresAuth(t *testing.T) {
	e := setup(t, nil, nil)
	if w := e.req(t, "GET", "/api/flights", nil, 0); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous /api/flights = %d, want 401", w.Code)
	}
}

func TestGetMeAndConfig(t *testing.T) {
	cfg := &config.Config{AeroDataBoxKey: "k"} // ResolverAvailable → true
	e := setup(t, fakeResolver{}, cfg)
	uid := e.user(t, "me", false)

	w := e.req(t, "GET", "/api/me", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/me = %d", w.Code)
	}
	me := decodeBody[map[string]any](t, w)
	if me["github_login"] != "me" {
		t.Errorf("unexpected me: %v", me)
	}

	w = e.req(t, "GET", "/api/config", nil, uid)
	cap := decodeBody[map[string]bool](t, w)
	if !cap["resolver_available"] {
		t.Errorf("resolver_available should be true, got %v", cap)
	}

	// No resolver / nil config → false.
	e2 := setup(t, nil, &config.Config{})
	w = e2.req(t, "GET", "/api/config", nil, e2.user(t, "u", false))
	if decodeBody[map[string]bool](t, w)["resolver_available"] {
		t.Error("resolver_available should be false")
	}
}

func TestFlightCRUD(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "pilot", false)
	pax := e.user(t, "pax", false)
	now := time.Now()

	// Empty list.
	w := e.req(t, "GET", "/api/flights", nil, uid)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("empty list = %d %q", w.Code, w.Body.String())
	}

	// Create (bad body).
	if w := e.req(t, "POST", "/api/flights", "not-json", uid); w.Code != 400 {
		// "not-json" marshals to a JSON string; decode into struct fails → 400
		t.Errorf("bad create body = %d, want 400", w.Code)
	}
	// Create (store validation: missing ident).
	bad := map[string]any{"scheduled_out": now, "scheduled_in": now.Add(time.Hour)}
	if w := e.req(t, "POST", "/api/flights", bad, uid); w.Code != 400 {
		t.Errorf("invalid create = %d, want 400", w.Code)
	}
	// Create OK with a passenger.
	body := map[string]any{
		"ident": "BA286", "scheduled_out": now.Add(-time.Hour), "scheduled_in": now.Add(time.Hour),
		"origin_iata": "LHR", "dest_iata": "JFK", "passenger_ids": []int64{pax},
	}
	w = e.req(t, "POST", "/api/flights", body, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	created := decodeBody[map[string]any](t, w)
	fid := int64(created["id"].(float64))

	// Create with a bad passenger id → AddPassenger FK error → 400.
	body2 := map[string]any{
		"ident": "BA287", "scheduled_out": now, "scheduled_in": now.Add(time.Hour),
		"origin_iata": "LHR", "dest_iata": "JFK", "passenger_ids": []int64{999999},
	}
	if w := e.req(t, "POST", "/api/flights", body2, uid); w.Code != 400 {
		t.Errorf("create w/ bad passenger = %d, want 400", w.Code)
	}

	// Get (bad id, not found, ok).
	if w := e.req(t, "GET", "/api/flights/abc", nil, uid); w.Code != 400 {
		t.Errorf("bad id = %d, want 400", w.Code)
	}
	if w := e.req(t, "GET", "/api/flights/999999", nil, uid); w.Code != 404 {
		t.Errorf("missing flight = %d, want 404", w.Code)
	}
	w = e.req(t, "GET", fmt.Sprintf("/api/flights/%d", fid), nil, uid)
	if w.Code != 200 {
		t.Fatalf("get flight = %d", w.Code)
	}

	// Update (bad id, bad body, not found, ok).
	if w := e.req(t, "PATCH", "/api/flights/x", map[string]any{}, uid); w.Code != 400 {
		t.Errorf("update bad id = %d", w.Code)
	}
	if w := e.req(t, "PATCH", fmt.Sprintf("/api/flights/%d", fid), "??", uid); w.Code != 400 {
		// JSON string into struct → decode error
		t.Errorf("update bad body = %d", w.Code)
	}
	if w := e.req(t, "PATCH", "/api/flights/999999", map[string]any{"notes": "x"}, uid); w.Code != 404 {
		t.Errorf("update missing = %d, want 404", w.Code)
	}
	notes := "updated"
	w = e.req(t, "PATCH", fmt.Sprintf("/api/flights/%d", fid), map[string]any{"notes": notes}, uid)
	if w.Code != 200 || decodeBody[map[string]any](t, w)["notes"] != notes {
		t.Errorf("update = %d %s", w.Code, w.Body.String())
	}

	// Passenger add/remove.
	if w := e.req(t, "POST", "/api/flights/bad/passengers", map[string]any{"user_id": pax}, uid); w.Code != 400 {
		t.Errorf("addPassenger bad id = %d", w.Code)
	}
	if w := e.req(t, "POST", fmt.Sprintf("/api/flights/%d/passengers", fid), "x", uid); w.Code != 400 {
		t.Errorf("addPassenger bad body = %d", w.Code)
	}
	if w := e.req(t, "POST", fmt.Sprintf("/api/flights/%d/passengers", fid), map[string]any{"user_id": 0}, uid); w.Code != 400 {
		t.Errorf("addPassenger user_id 0 = %d", w.Code)
	}
	if w := e.req(t, "POST", fmt.Sprintf("/api/flights/%d/passengers", fid), map[string]any{"user_id": pax}, uid); w.Code != 204 {
		t.Errorf("addPassenger = %d, want 204", w.Code)
	}
	if w := e.req(t, "DELETE", "/api/flights/x/passengers/1", nil, uid); w.Code != 400 {
		t.Errorf("removePassenger bad fid = %d", w.Code)
	}
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/flights/%d/passengers/y", fid), nil, uid); w.Code != 400 {
		t.Errorf("removePassenger bad uid = %d", w.Code)
	}
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/flights/%d/passengers/%d", fid, pax), nil, uid); w.Code != 204 {
		t.Errorf("removePassenger = %d, want 204", w.Code)
	}
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/flights/%d/passengers/%d", fid, pax), nil, uid); w.Code != 404 {
		t.Errorf("removePassenger absent = %d, want 404", w.Code)
	}

	// Delete (bad id, ok, not found).
	if w := e.req(t, "DELETE", "/api/flights/zzz", nil, uid); w.Code != 400 {
		t.Errorf("delete bad id = %d", w.Code)
	}
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/flights/%d", fid), nil, uid); w.Code != 204 {
		t.Errorf("delete = %d, want 204", w.Code)
	}
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/flights/%d", fid), nil, uid); w.Code != 404 {
		t.Errorf("delete again = %d, want 404", w.Code)
	}
}

func TestUserAdminEndpoints(t *testing.T) {
	e := setup(t, nil, nil)
	super := e.user(t, "boss", true)
	plain := e.user(t, "plain", false)

	// Non-superuser is forbidden from user mutations.
	if w := e.req(t, "POST", "/api/users", map[string]any{"github_login": "x"}, plain); w.Code != http.StatusForbidden {
		t.Errorf("non-super invite = %d, want 403", w.Code)
	}

	// listUsers (any authed user).
	w := e.req(t, "GET", "/api/users", nil, plain)
	if w.Code != 200 || len(decodeBody[[]map[string]any](t, w)) != 2 {
		t.Errorf("listUsers = %d %s", w.Code, w.Body.String())
	}

	// invite: bad body, store error (empty login), success.
	if w := e.req(t, "POST", "/api/users", "??", super); w.Code != 400 {
		t.Errorf("invite bad body = %d", w.Code)
	}
	if w := e.req(t, "POST", "/api/users", map[string]any{"github_login": "  "}, super); w.Code != 400 {
		t.Errorf("invite empty login = %d", w.Code)
	}
	w = e.req(t, "POST", "/api/users", map[string]any{"github_login": "newbie", "name": "N"}, super)
	if w.Code != http.StatusCreated {
		t.Fatalf("invite = %d %s", w.Code, w.Body.String())
	}
	newbie := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	// update: bad id, bad body, not found, self-guards, success.
	if w := e.req(t, "PATCH", "/api/users/x", map[string]any{}, super); w.Code != 400 {
		t.Errorf("update bad id = %d", w.Code)
	}
	if w := e.req(t, "PATCH", fmt.Sprintf("/api/users/%d", newbie), "??", super); w.Code != 400 {
		t.Errorf("update bad body = %d", w.Code)
	}
	if w := e.req(t, "PATCH", "/api/users/999999", map[string]any{"name": "z"}, super); w.Code != 404 {
		t.Errorf("update missing = %d, want 404", w.Code)
	}
	no := false
	if w := e.req(t, "PATCH", fmt.Sprintf("/api/users/%d", super), map[string]any{"is_superuser": no}, super); w.Code != 400 {
		t.Errorf("self-demote should be 400, got %d", w.Code)
	}
	if w := e.req(t, "PATCH", fmt.Sprintf("/api/users/%d", super), map[string]any{"is_active": no}, super); w.Code != 400 {
		t.Errorf("self-deactivate should be 400, got %d", w.Code)
	}
	nm := "Renamed"
	w = e.req(t, "PATCH", fmt.Sprintf("/api/users/%d", newbie), map[string]any{"name": nm}, super)
	if w.Code != 200 || decodeBody[map[string]any](t, w)["name"] != nm {
		t.Errorf("update = %d %s", w.Code, w.Body.String())
	}

	// delete: bad id, self-guard, not found, success.
	if w := e.req(t, "DELETE", "/api/users/x", nil, super); w.Code != 400 {
		t.Errorf("delete bad id = %d", w.Code)
	}
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/users/%d", super), nil, super); w.Code != 400 {
		t.Errorf("self-delete should be 400, got %d", w.Code)
	}
	if w := e.req(t, "DELETE", "/api/users/999999", nil, super); w.Code != 404 {
		t.Errorf("delete missing = %d, want 404", w.Code)
	}
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/users/%d", newbie), nil, super); w.Code != 204 {
		t.Errorf("delete = %d, want 204", w.Code)
	}
}

func TestResolveFlight(t *testing.T) {
	// No resolver configured → 501.
	e := setup(t, nil, nil)
	uid := e.user(t, "u", false)
	if w := e.req(t, "POST", "/api/flights/resolve", map[string]any{"ident": "BA1", "date": "2026-05-19"}, uid); w.Code != http.StatusNotImplemented {
		t.Errorf("no resolver = %d, want 501", w.Code)
	}

	rf := &providers.ResolvedFlight{
		Ident: "BA286", OriginIATA: "LHR", DestIATA: "SFO",
		ScheduledOut: time.Now(), ScheduledIn: time.Now().Add(11 * time.Hour),
	}
	e2 := setup(t, fakeResolver{rf: rf}, nil)
	u2 := e2.user(t, "u2", false)

	if w := e2.req(t, "POST", "/api/flights/resolve", "??", u2); w.Code != 400 {
		t.Errorf("resolve bad body = %d", w.Code)
	}
	if w := e2.req(t, "POST", "/api/flights/resolve", map[string]any{"ident": "", "date": ""}, u2); w.Code != 400 {
		t.Errorf("resolve missing fields = %d", w.Code)
	}
	if w := e2.req(t, "POST", "/api/flights/resolve", map[string]any{"ident": "BA286", "date": "19/05/2026"}, u2); w.Code != 400 {
		t.Errorf("resolve bad date = %d", w.Code)
	}
	w := e2.req(t, "POST", "/api/flights/resolve", map[string]any{"ident": "BA286", "date": "2026-05-19"}, u2)
	if w.Code != 200 || decodeBody[map[string]any](t, w)["ident"] != "BA286" {
		t.Errorf("resolve = %d %s", w.Code, w.Body.String())
	}

	// Resolver returns an error → 422.
	e3 := setup(t, fakeResolver{err: errors.New("not found upstream")}, nil)
	u3 := e3.user(t, "u3", false)
	if w := e3.req(t, "POST", "/api/flights/resolve", map[string]any{"ident": "ZZ9", "date": "2026-05-19"}, u3); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("resolver error = %d, want 422", w.Code)
	}
}

func TestListFlightsWithData(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "owner", false)
	pax := e.user(t, "rider", false)
	now := time.Now()

	f, err := e.store.CreateFlight(context.Background(), store.CreateFlightPayload{
		Ident: "LD1", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)
	if err != nil {
		t.Fatalf("seed flight: %v", err)
	}
	if err := e.store.AddPassenger(context.Background(), f.ID, pax); err != nil {
		t.Fatalf("seed passenger: %v", err)
	}
	hdg := int16(90)
	_ = e.store.InsertPosition(context.Background(), store.Position{
		FlightID: f.ID, Ts: now.Add(-30 * time.Minute), Lat: 50, Lon: -10, HeadingDeg: &hdg,
	})

	w := e.req(t, "GET", "/api/flights", nil, uid)
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body.String())
	}
	out := decodeBody[[]map[string]any](t, w)
	if len(out) != 1 {
		t.Fatalf("expected 1 flight, got %d", len(out))
	}
	if pids, _ := out[0]["passenger_ids"].([]any); len(pids) != 1 {
		t.Errorf("expected 1 passenger in DTO, got %v", out[0]["passenger_ids"])
	}
	if out[0]["latest_position"] == nil {
		t.Error("expected latest_position in DTO")
	}

	// AddPassenger to a nonexistent flight → FK error → 400.
	if w := e.req(t, "POST", "/api/flights/888888/passengers", map[string]any{"user_id": pax}, uid); w.Code != 400 {
		t.Errorf("addPassenger to missing flight = %d, want 400", w.Code)
	}
}

func TestListFlightsStoreError(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "cancely", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/api/flights", nil).WithContext(ctx)
	r.AddCookie(&http.Cookie{
		Name:  auth.SessionCookie,
		Value: auth.SignSession(sessKey, uid, time.Now().Add(time.Hour)),
	})
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	// Auth middleware loads the user with the (cancelled) ctx; that itself
	// fails → 401. Either way the handler/middleware error path is exercised.
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusUnauthorized {
		t.Errorf("cancelled list = %d, want 500 or 401", w.Code)
	}
}

func TestWriteHelpers(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusTeapot, map[string]int{"a": 1})
	if w.Code != http.StatusTeapot || w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("writeJSON wrong: %d %s", w.Code, w.Header().Get("Content-Type"))
	}

	w = httptest.NewRecorder()
	handleStoreErr(w, store.ErrNotFound)
	if w.Code != http.StatusNotFound {
		t.Errorf("ErrNotFound → %d, want 404", w.Code)
	}
	w = httptest.NewRecorder()
	handleStoreErr(w, errors.New("boom"))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("generic err → %d, want 500", w.Code)
	}
}
