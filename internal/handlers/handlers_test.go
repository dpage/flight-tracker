package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/config"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

var sessKey = []byte("handlers-test-session-key-32chars!!")

type fakeResolver struct {
	rf  *providers.ResolvedFlight
	err error
	// calls is incremented on every Resolve invocation so tests can
	// assert the known-IATA fast path bypasses the resolver entirely.
	calls int
}

func (f *fakeResolver) Resolve(context.Context, string, time.Time) (*providers.ResolvedFlight, error) {
	f.calls++
	return f.rf, f.err
}

type testEnv struct {
	mux   *http.ServeMux
	api   *API
	store *store.Store
	cfg   *config.Config
	hub   *sse.Hub
	pool  *pgxpool.Pool
}

func setup(t *testing.T, resolver providers.Resolver, cfg *config.Config) *testEnv {
	t.Helper()
	pool := testsupport.NewPool(t)
	s := store.New(pool)
	a := auth.NewHandler(sessKey, "http://localhost:8080", s)
	a.AddProvider(auth.NewGitHubProvider("cid", "csec"))
	hub := sse.NewHub()
	if cfg == nil {
		cfg = &config.Config{}
	}
	api := New(s, a, hub, cfg, resolver)
	mux := http.NewServeMux()
	api.Register(mux)
	return &testEnv{mux: mux, api: api, store: s, cfg: cfg, hub: hub, pool: pool}
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

func (e *testEnv) user(t *testing.T, username string, super bool) int64 {
	t.Helper()
	u, err := e.store.InviteUser(context.Background(), store.InvitePayload{
		Username: username, Name: username, IsSuperuser: super,
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
	e := setup(t, &fakeResolver{}, cfg)
	uid := e.user(t, "me", false)

	w := e.req(t, "GET", "/api/me", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/me = %d", w.Code)
	}
	me := decodeBody[map[string]any](t, w)
	if me["username"] != "me" {
		t.Errorf("unexpected me: %v", me)
	}

	w = e.req(t, "GET", "/api/config", nil, uid)
	caps := decodeBody[map[string]any](t, w)
	if caps["resolver_available"] != true {
		t.Errorf("resolver_available should be true, got %v", caps)
	}
	// The DTO grew a poll_interval_sec field; just assert it's present so
	// future shape changes are caught here. The value is whatever the test
	// fixture's Config sets — zero by default, which is fine for the wire
	// format.
	if _, ok := caps["poll_interval_sec"]; !ok {
		t.Errorf("poll_interval_sec missing from /api/config response: %v", caps)
	}
	if _, ok := caps["email_ingest_enabled"]; !ok {
		t.Errorf("email_ingest_enabled missing from /api/config response: %v", caps)
	}

	// No resolver / nil config → false. Address omitted when ingest disabled.
	e2 := setup(t, nil, &config.Config{})
	w = e2.req(t, "GET", "/api/config", nil, e2.user(t, "u", false))
	body := decodeBody[map[string]any](t, w)
	if body["resolver_available"] != false {
		t.Error("resolver_available should be false")
	}
	if _, ok := body["email_ingest_address"]; ok {
		t.Error("email_ingest_address should be omitted when ingest is disabled")
	}

	// Ingest enabled → both flags are exposed.
	e3 := setup(t, nil, &config.Config{
		EmailIngestEnabled: true,
		EmailIngestAddress: "flights@example.test",
	})
	w = e3.req(t, "GET", "/api/config", nil, e3.user(t, "u2", false))
	caps3 := decodeBody[map[string]any](t, w)
	if caps3["email_ingest_enabled"] != true {
		t.Error("email_ingest_enabled should be true when EmailIngestEnabled is set")
	}
	if got := caps3["email_ingest_address"]; got != "flights@example.test" {
		t.Errorf("email_ingest_address = %v, want flights@example.test", got)
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

// drainEvents reads every event currently buffered for the subscriber, then
// returns. We use it after each mutating request to assert that the right
// flight.updated / flight.deleted events were published.
func drainEvents(ch <-chan sse.Event) []sse.Event {
	var out []sse.Event
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		default:
			return out
		}
	}
}

func TestFlightWritesPublishSSE(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "pilot", false)
	pax := e.user(t, "pax", false)
	now := time.Now()

	ch, unsub := e.hub.Subscribe(sse.Subscription{ViewerID: uid, IsSuperuser: true, ShowAll: true})
	defer unsub()

	// Create publishes flight.updated.
	body := map[string]any{
		"ident": "BA286", "scheduled_out": now.Add(-time.Hour), "scheduled_in": now.Add(time.Hour),
		"origin_iata": "LHR", "dest_iata": "JFK",
	}
	w := e.req(t, "POST", "/api/flights", body, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	fid := int64(decodeBody[map[string]any](t, w)["id"].(float64))
	events := drainEvents(ch)
	if len(events) != 1 || events[0].Type != "flight.updated" {
		t.Fatalf("create events = %+v, want one flight.updated", events)
	}

	// Update publishes flight.updated.
	if w := e.req(t, "PATCH", fmt.Sprintf("/api/flights/%d", fid), map[string]any{"notes": "n"}, uid); w.Code != 200 {
		t.Fatalf("update = %d", w.Code)
	}
	if events := drainEvents(ch); len(events) != 1 || events[0].Type != "flight.updated" {
		t.Errorf("update events = %+v, want one flight.updated", events)
	}

	// Adding a passenger publishes flight.updated.
	if w := e.req(t, "POST", fmt.Sprintf("/api/flights/%d/passengers", fid), map[string]any{"user_id": pax}, uid); w.Code != 204 {
		t.Fatalf("addPassenger = %d", w.Code)
	}
	if events := drainEvents(ch); len(events) != 1 || events[0].Type != "flight.updated" {
		t.Errorf("addPassenger events = %+v, want one flight.updated", events)
	}

	// Removing a passenger publishes flight.updated.
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/flights/%d/passengers/%d", fid, pax), nil, uid); w.Code != 204 {
		t.Fatalf("removePassenger = %d", w.Code)
	}
	if events := drainEvents(ch); len(events) != 1 || events[0].Type != "flight.updated" {
		t.Errorf("removePassenger events = %+v, want one flight.updated", events)
	}

	// Delete publishes flight.deleted carrying the id.
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/flights/%d", fid), nil, uid); w.Code != 204 {
		t.Fatalf("delete = %d", w.Code)
	}
	events = drainEvents(ch)
	if len(events) != 1 || events[0].Type != "flight.deleted" {
		t.Fatalf("delete events = %+v, want one flight.deleted", events)
	}
	var payload struct{ ID int64 }
	if err := json.Unmarshal(events[0].Data, &payload); err != nil {
		t.Fatalf("decode delete payload: %v", err)
	}
	if payload.ID != fid {
		t.Errorf("delete payload id = %d, want %d", payload.ID, fid)
	}
}

func TestUserAdminEndpoints(t *testing.T) {
	e := setup(t, nil, nil)
	super := e.user(t, "boss", true)
	plain := e.user(t, "plain", false)

	// Non-superuser is forbidden from user mutations.
	if w := e.req(t, "POST", "/api/users", map[string]any{"username": "x"}, plain); w.Code != http.StatusForbidden {
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
	if w := e.req(t, "POST", "/api/users", map[string]any{"username": "  "}, super); w.Code != 400 {
		t.Errorf("invite empty login = %d", w.Code)
	}
	w = e.req(t, "POST", "/api/users", map[string]any{"username": "newbie", "name": "N"}, super)
	if w.Code != http.StatusCreated {
		t.Fatalf("invite = %d %s", w.Code, w.Body.String())
	}
	newbie := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	// Duplicate username should surface as 409, not the raw pg error.
	if w := e.req(t, "POST", "/api/users", map[string]any{"username": "newbie"}, super); w.Code != http.StatusConflict {
		t.Errorf("duplicate invite = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}

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
	e2 := setup(t, &fakeResolver{rf: rf}, nil)
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
	e3 := setup(t, &fakeResolver{err: errors.New("not found upstream")}, nil)
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

	// AddPassenger to a nonexistent flight: requireEdit hits CanEdit, which
	// returns ErrNotFound for an unknown id → 404 from handleStoreErr.
	if w := e.req(t, "POST", "/api/flights/888888/passengers", map[string]any{"user_id": pax}, uid); w.Code != 404 {
		t.Errorf("addPassenger to missing flight = %d, want 404", w.Code)
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

// TestVisibilityFiltering exercises the new sharing model: each user sees
// only the flights they own, are a passenger on, or are explicitly shared
// with; superusers see the same set by default, expanded with ?show_all=1.
func TestVisibilityFiltering(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	carol := e.user(t, "carol", false)
	admin := e.user(t, "admin", true)
	now := time.Now()

	// Alice creates three flights with different visibility shapes:
	//   A — private (only Alice can see it)
	//   B — explicitly shared with Bob
	//   C — public (everyone sees it)
	create := func(ident string, isPublic bool, sharedWith []int64) int64 {
		body := map[string]any{
			"ident": ident, "scheduled_out": now.Add(time.Hour), "scheduled_in": now.Add(2 * time.Hour),
			"origin_iata": "LHR", "dest_iata": "JFK",
			"is_public": isPublic, "shared_user_ids": sharedWith,
		}
		w := e.req(t, "POST", "/api/flights", body, alice)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: code=%d body=%s", ident, w.Code, w.Body.String())
		}
		return int64(decodeBody[map[string]any](t, w)["id"].(float64))
	}
	idA := create("A1", false, nil)
	idB := create("B1", false, []int64{bob})
	create("C1", true, nil)

	idents := func(uid int64, query string) []string {
		w := e.req(t, "GET", "/api/flights"+query, nil, uid)
		if w.Code != http.StatusOK {
			t.Fatalf("list as %d: code=%d", uid, w.Code)
		}
		out := decodeBody[[]map[string]any](t, w)
		got := make([]string, 0, len(out))
		for _, f := range out {
			got = append(got, f["ident"].(string))
		}
		return got
	}

	if got := idents(alice, ""); !sameStrings(got, []string{"A1", "B1", "C1"}) {
		t.Errorf("alice sees %v, want A1 B1 C1", got)
	}
	if got := idents(bob, ""); !sameStrings(got, []string{"B1", "C1"}) {
		t.Errorf("bob sees %v, want B1 C1", got)
	}
	if got := idents(carol, ""); !sameStrings(got, []string{"C1"}) {
		t.Errorf("carol sees %v, want C1 only", got)
	}
	if got := idents(admin, ""); !sameStrings(got, []string{"C1"}) {
		t.Errorf("admin (no show_all) sees %v, want C1 only", got)
	}
	if got := idents(admin, "?show_all=1"); !sameStrings(got, []string{"A1", "B1", "C1"}) {
		t.Errorf("admin show_all sees %v, want all three", got)
	}
	if got := idents(bob, "?show_all=1"); !sameStrings(got, []string{"B1", "C1"}) {
		t.Errorf("non-superuser show_all should be ignored, got %v", got)
	}

	// Carol can't read A directly (404, not 403, to avoid leaking existence).
	if w := e.req(t, "GET", apiFlightPath(idA), nil, carol); w.Code != http.StatusNotFound {
		t.Errorf("carol GET private flight = %d, want 404", w.Code)
	}
	// Bob can read his shared flight.
	if w := e.req(t, "GET", apiFlightPath(idB), nil, bob); w.Code != http.StatusOK {
		t.Errorf("bob GET shared flight = %d, want 200", w.Code)
	}
	// Carol cannot edit Bob's flight (not creator, not superuser).
	if w := e.req(t, "PATCH", apiFlightPath(idA),
		map[string]any{"notes": "no"}, carol); w.Code != http.StatusForbidden {
		t.Errorf("carol PATCH = %d, want 403", w.Code)
	}
	// Admin can edit anything regardless of show_all.
	if w := e.req(t, "PATCH", apiFlightPath(idA),
		map[string]any{"notes": "ok"}, admin); w.Code != http.StatusOK {
		t.Errorf("admin PATCH = %d, want 200", w.Code)
	}

	// Old-flight filter: seed a private flight for bob whose scheduled_in
	// is 25h in the past. CreateFlight rejects backwards scheduling, so we
	// insert via raw SQL.
	if _, err := e.pool.Exec(context.Background(), `
		INSERT INTO flights (ident, scheduled_out, scheduled_in,
			origin_iata, origin_lat, origin_lon,
			dest_iata, dest_lat, dest_lon,
			status, created_by, is_public)
		VALUES ('OLD1', NOW() - INTERVAL '48 hours', NOW() - INTERVAL '25 hours',
			'LHR', 51.4775, -0.4614, 'JFK', 40.6413, -73.7781,
			'Arrived', $1, FALSE)`, bob); err != nil {
		t.Fatalf("seed old flight: %v", err)
	}

	if got := idents(bob, ""); contains(got, "OLD1") {
		t.Errorf("bob default list should hide OLD1, got %v", got)
	}
	if got := idents(bob, "?show_old=1"); !contains(got, "OLD1") {
		t.Errorf("bob show_old=1 should include OLD1, got %v", got)
	}
	if got := idents(bob, "?show_old=true"); !contains(got, "OLD1") {
		t.Errorf("bob show_old=true should include OLD1, got %v", got)
	}
	// show_old is NOT superuser-gated: a regular user can opt in to the
	// archive of flights they're already allowed to see.
	if got := idents(carol, "?show_old=1"); contains(got, "OLD1") {
		t.Errorf("carol cannot see bob's flight even with show_old, got %v", got)
	}
	// Bogus values fall through as false.
	if got := idents(bob, "?show_old=banana"); contains(got, "OLD1") {
		t.Errorf("bob show_old=banana should be treated as false, got %v", got)
	}
}

func TestShareEndpoints(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	carol := e.user(t, "carol", false)
	now := time.Now()
	w := e.req(t, "POST", "/api/flights",
		map[string]any{
			"ident": "S1", "scheduled_out": now.Add(time.Hour), "scheduled_in": now.Add(2 * time.Hour),
			"origin_iata": "LHR", "dest_iata": "JFK",
		}, alice)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	id := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	// Bob can't add a share to Alice's flight (not creator).
	if w := e.req(t, "POST", apiFlightPath(id)+"/shares",
		map[string]any{"user_id": carol}, bob); w.Code != http.StatusForbidden {
		t.Errorf("bob add share = %d, want 403", w.Code)
	}
	// Alice shares with Bob.
	if w := e.req(t, "POST", apiFlightPath(id)+"/shares",
		map[string]any{"user_id": bob}, alice); w.Code != http.StatusNoContent {
		t.Errorf("alice add share = %d, want 204", w.Code)
	}
	// Bob now sees the flight; the DTO lists him in shared_user_ids.
	w = e.req(t, "GET", apiFlightPath(id), nil, bob)
	if w.Code != http.StatusOK {
		t.Errorf("bob GET shared = %d, want 200", w.Code)
	}
	got := decodeBody[map[string]any](t, w)
	shared, _ := got["shared_user_ids"].([]any)
	if len(shared) != 1 || int64(shared[0].(float64)) != bob {
		t.Errorf("shared_user_ids wrong: %v", got["shared_user_ids"])
	}
	// Alice un-shares.
	if w := e.req(t, "DELETE", apiFlightPath(id)+"/shares/"+itoa(bob), nil, alice); w.Code != http.StatusNoContent {
		t.Errorf("alice remove share = %d, want 204", w.Code)
	}
	// Removing twice yields 404.
	if w := e.req(t, "DELETE", apiFlightPath(id)+"/shares/"+itoa(bob), nil, alice); w.Code != http.StatusNotFound {
		t.Errorf("double remove share = %d, want 404", w.Code)
	}
}

func apiFlightPath(id int64) string {
	return "/api/flights/" + itoa(id)
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	gotSet := map[string]bool{}
	for _, s := range got {
		gotSet[s] = true
	}
	for _, s := range want {
		if !gotSet[s] {
			return false
		}
	}
	return true
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestCreateFlight_UnknownDestBackfilledFromResolver(t *testing.T) {
	cfg := &config.Config{AeroDataBoxKey: "k"}
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{
		Ident:      "BA286",
		OriginIATA: "LHR", OriginLat: 51.4775, OriginLon: -0.4614,
		DestIATA: "ZZZ", DestLat: 12.3456, DestLon: -34.5678,
		Notes: "British Airways · Boeing 777",
	}}
	e := setup(t, resolver, cfg)
	uid := e.user(t, "pilot", false)
	now := time.Now()

	body := map[string]any{
		"ident":         "BA286",
		"scheduled_out": now.Add(-time.Hour),
		"scheduled_in":  now.Add(time.Hour),
		"origin_iata":   "LHR",
		"dest_iata":     "ZZZ", // not in embedded table
	}
	w := e.req(t, "POST", "/api/flights", body, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	if resolver.calls != 1 {
		t.Errorf("resolver calls = %d, want 1", resolver.calls)
	}
	got := decodeBody[map[string]any](t, w)
	if got["dest_lat"] == nil || got["dest_lon"] == nil {
		t.Fatalf("dest_lat/lon should be populated from resolver; got %v", got)
	}
	if dl := got["dest_lat"].(float64); dl != 12.3456 {
		t.Errorf("dest_lat = %v, want 12.3456 (resolver-supplied)", dl)
	}
}

func TestUpdateFlight_UnknownDestBackfilledFromResolver(t *testing.T) {
	cfg := &config.Config{AeroDataBoxKey: "k"}
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{
		Ident:      "BA286",
		OriginIATA: "LHR", OriginLat: 51.4775, OriginLon: -0.4614,
		DestIATA: "ZZZ", DestLat: 12.3456, DestLon: -34.5678,
	}}
	e := setup(t, resolver, cfg)
	uid := e.user(t, "pilot", false)
	now := time.Now()

	// Seed with both IATAs known so the create path does NOT call the
	// resolver — that way the call count assertion below is unambiguous.
	body := map[string]any{
		"ident":         "BA286",
		"scheduled_out": now.Add(-time.Hour),
		"scheduled_in":  now.Add(time.Hour),
		"origin_iata":   "LHR",
		"dest_iata":     "JFK",
	}
	w := e.req(t, "POST", "/api/flights", body, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed create = %d %s", w.Code, w.Body.String())
	}
	created := decodeBody[map[string]any](t, w)
	fid := int64(created["id"].(float64))
	if resolver.calls != 0 {
		t.Fatalf("seed should not have called resolver (both IATAs known): calls=%d", resolver.calls)
	}

	// Patch the destination to something unknown.
	patch := map[string]any{"dest_iata": "ZZZ"}
	w = e.req(t, "PATCH", fmt.Sprintf("/api/flights/%d", fid), patch, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}
	if resolver.calls != 1 {
		t.Errorf("resolver calls after update = %d, want 1", resolver.calls)
	}
	got := decodeBody[map[string]any](t, w)
	if got["dest_lat"] == nil {
		t.Fatalf("dest_lat should be populated from resolver after update; got %v", got)
	}
	if dl := got["dest_lat"].(float64); dl != 12.3456 {
		t.Errorf("dest_lat = %v, want 12.3456", dl)
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

func TestCreateFlight_KnownIATAsBypassResolver(t *testing.T) {
	cfg := &config.Config{AeroDataBoxKey: "k"}
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{
		// Deliberately wrong — if the helper runs we'd notice via wrong
		// coord values. But it shouldn't run at all, so calls == 0.
		DestIATA: "JFK", DestLat: 999, DestLon: 999,
	}}
	e := setup(t, resolver, cfg)
	uid := e.user(t, "pilot", false)
	now := time.Now()

	body := map[string]any{
		"ident":         "BA286",
		"scheduled_out": now.Add(-time.Hour),
		"scheduled_in":  now.Add(time.Hour),
		"origin_iata":   "LHR", "dest_iata": "JFK",
	}
	w := e.req(t, "POST", "/api/flights", body, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	if resolver.calls != 0 {
		t.Errorf("known-IATA create should not call resolver; calls = %d", resolver.calls)
	}
}

func TestCreateFlight_ResolverNotFoundLeavesNullCoords(t *testing.T) {
	cfg := &config.Config{AeroDataBoxKey: "k"}
	resolver := &fakeResolver{err: providers.ErrFlightNotFound}
	e := setup(t, resolver, cfg)
	uid := e.user(t, "pilot", false)
	now := time.Now()

	body := map[string]any{
		"ident":         "XX0000",
		"scheduled_out": now.Add(-time.Hour),
		"scheduled_in":  now.Add(time.Hour),
		"origin_iata":   "LHR", "dest_iata": "ZZZ",
	}
	w := e.req(t, "POST", "/api/flights", body, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	if resolver.calls != 1 {
		t.Errorf("resolver should have been called once; calls = %d", resolver.calls)
	}
	got := decodeBody[map[string]any](t, w)
	if got["dest_lat"] != nil {
		t.Errorf("dest_lat should remain null when resolver returns not-found; got %v", got["dest_lat"])
	}
}

func TestCreateFlight_ResolverTransportErrorLeavesNullCoords(t *testing.T) {
	cfg := &config.Config{AeroDataBoxKey: "k"}
	resolver := &fakeResolver{err: errors.New("rapidapi: 502 bad gateway")}
	e := setup(t, resolver, cfg)
	uid := e.user(t, "pilot", false)
	now := time.Now()

	body := map[string]any{
		"ident":         "BA286",
		"scheduled_out": now.Add(-time.Hour),
		"scheduled_in":  now.Add(time.Hour),
		"origin_iata":   "LHR", "dest_iata": "ZZZ",
	}
	w := e.req(t, "POST", "/api/flights", body, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	got := decodeBody[map[string]any](t, w)
	if got["dest_lat"] != nil {
		t.Errorf("dest_lat should remain null on transport error; got %v", got["dest_lat"])
	}
}

func TestCreateFlight_NoResolverLeavesNullCoords(t *testing.T) {
	// nil resolver — caller has not configured AeroDataBox at all.
	e := setup(t, nil, nil)
	uid := e.user(t, "pilot", false)
	now := time.Now()

	body := map[string]any{
		"ident":         "BA286",
		"scheduled_out": now.Add(-time.Hour),
		"scheduled_in":  now.Add(time.Hour),
		"origin_iata":   "LHR", "dest_iata": "ZZZ",
	}
	w := e.req(t, "POST", "/api/flights", body, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	got := decodeBody[map[string]any](t, w)
	if got["dest_lat"] != nil {
		t.Errorf("dest_lat should remain null with no resolver; got %v", got["dest_lat"])
	}
}

func TestUpdateFlight_BothLegsUnknownToKnownSkipsResolver(t *testing.T) {
	cfg := &config.Config{AeroDataBoxKey: "k"}
	// Seed with both IATAs unknown so the create call uses the resolver
	// once. After that we PATCH both legs to known IATAs (LHR/JFK) — the
	// store's lookupCoords fills the row during UPDATE, needsCoordBackfill
	// returns false on the post-update row, and the helper must not run.
	resolver := &fakeResolver{err: providers.ErrFlightNotFound}
	e := setup(t, resolver, cfg)
	uid := e.user(t, "pilot", false)
	now := time.Now()

	seed := map[string]any{
		"ident":         "BA286",
		"scheduled_out": now.Add(-time.Hour),
		"scheduled_in":  now.Add(time.Hour),
		"origin_iata":   "QQQ", "dest_iata": "ZZZ",
	}
	w := e.req(t, "POST", "/api/flights", seed, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed = %d %s", w.Code, w.Body.String())
	}
	fid := int64(decodeBody[map[string]any](t, w)["id"].(float64))
	priorCalls := resolver.calls // 1 (the create-time fallback fired and returned not-found)

	patch := map[string]any{"origin_iata": "LHR", "dest_iata": "JFK"}
	w = e.req(t, "PATCH", fmt.Sprintf("/api/flights/%d", fid), patch, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}
	if resolver.calls != priorCalls {
		t.Errorf("resolver calls delta = %d after both-legs-now-known update, want 0",
			resolver.calls-priorCalls)
	}
	got := decodeBody[map[string]any](t, w)
	if got["origin_lat"] == nil || got["dest_lat"] == nil {
		t.Fatalf("both coords should be table-filled post-update; got %v", got)
	}
}

func TestUpdateFlight_PartiallyUnknownTriggersBackfillOfMissingLegOnly(t *testing.T) {
	cfg := &config.Config{AeroDataBoxKey: "k"}
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{
		Ident:      "BA286",
		// Deliberately a different origin lat than the table's LHR (51.4775)
		// — if the helper were to overwrite already-filled columns we'd see
		// 99.0 in the response. BackfillFlight should NOT overwrite.
		OriginIATA: "LHR", OriginLat: 99.0, OriginLon: 99.0,
		DestIATA: "ZZZ", DestLat: 12.3456, DestLon: -34.5678,
	}}
	e := setup(t, resolver, cfg)
	uid := e.user(t, "pilot", false)
	now := time.Now()

	// Seed: both IATAs unknown to force a row that starts with all coords NULL.
	// resolver.rf returns coords for the LHR/ZZZ pair above — so after the
	// create, the row will have origin coords = 99 and dest coords = 12.3456.
	// That's not what we want for the seed assertion, so we use a temp
	// resolver that returns nothing during seeding.
	resolver.rf = nil
	resolver.err = providers.ErrFlightNotFound
	seed := map[string]any{
		"ident":         "BA286",
		"scheduled_out": now.Add(-time.Hour),
		"scheduled_in":  now.Add(time.Hour),
		"origin_iata":   "QQQ", "dest_iata": "ZZZ",
	}
	w := e.req(t, "POST", "/api/flights", seed, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed create = %d %s", w.Code, w.Body.String())
	}
	created := decodeBody[map[string]any](t, w)
	fid := int64(created["id"].(float64))
	if created["origin_lat"] != nil || created["dest_lat"] != nil {
		t.Fatalf("seed should have NULL coords on both legs; got %v", created)
	}

	// Restore the resolver to the happy-path fixture for the update.
	resolver.rf = &providers.ResolvedFlight{
		Ident:      "BA286",
		OriginIATA: "LHR", OriginLat: 99.0, OriginLon: 99.0,
		DestIATA: "ZZZ", DestLat: 12.3456, DestLon: -34.5678,
	}
	resolver.err = nil
	priorCalls := resolver.calls

	// Patch origin from unknown (QQQ) to known (LHR). Dest stays unknown (ZZZ).
	patch := map[string]any{"origin_iata": "LHR"}
	w = e.req(t, "PATCH", fmt.Sprintf("/api/flights/%d", fid), patch, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}
	if resolver.calls != priorCalls+1 {
		t.Errorf("resolver calls delta = %d, want 1", resolver.calls-priorCalls)
	}
	got := decodeBody[map[string]any](t, w)

	// Origin: was just patched to LHR, table lookup fills it during UPDATE.
	// The resolver's bogus 99.0 must NOT overwrite — BackfillFlight only
	// touches empty columns.
	if got["origin_lat"] == nil {
		t.Fatalf("origin_lat should be table-filled (51.4775), got nil")
	}
	if ol := got["origin_lat"].(float64); ol != 51.4775 {
		t.Errorf("origin_lat = %v, want 51.4775 (LHR table value, NOT resolver's 99)", ol)
	}
	// Dest: still unknown, resolver supplies the value.
	if got["dest_lat"] == nil {
		t.Fatalf("dest_lat should be resolver-filled, got nil")
	}
	if dl := got["dest_lat"].(float64); dl != 12.3456 {
		t.Errorf("dest_lat = %v, want 12.3456", dl)
	}
}
