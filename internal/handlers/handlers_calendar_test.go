package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dpage/aerly/internal/config"
)

// rawGet issues an anonymous GET (no session cookie) and returns the recorder —
// used for the token-authed .ics feeds.
func rawGet(e *testEnv, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return w
}

// seedTrip / seedPlan / seedPart insert fixtures via raw SQL, since the plan
// CRUD store methods are stubbed in Wave 1B and the calendar feed only needs
// rows in place.
func seedTrip(t *testing.T, e *testEnv, owner int64) int64 {
	t.Helper()
	var id int64
	if err := e.pool.QueryRow(context.Background(),
		`INSERT INTO trips (name, created_by) VALUES ('T', $1) RETURNING id`, owner).Scan(&id); err != nil {
		t.Fatalf("seed trip: %v", err)
	}
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1,$2,'owner')`, id, owner); err != nil {
		t.Fatalf("seed owner member: %v", err)
	}
	return id
}

func seedMember(t *testing.T, e *testEnv, trip, user int64) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1,$2,'viewer')`, trip, user); err != nil {
		t.Fatalf("seed member: %v", err)
	}
}

func seedPlan(t *testing.T, e *testEnv, trip, owner int64, title string) int64 {
	t.Helper()
	var id int64
	if err := e.pool.QueryRow(context.Background(),
		`INSERT INTO plans (trip_id, type, title, created_by) VALUES ($1,'flight',$2,$3) RETURNING id`,
		trip, title, owner).Scan(&id); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	return id
}

func seedPart(t *testing.T, e *testEnv, plan int64) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_tz, end_tz, start_label)
		 VALUES ($1, NOW(), NOW() + INTERVAL '2 hours', 'Europe/London', 'America/New_York', 'LHR')`,
		plan); err != nil {
		t.Fatalf("seed part: %v", err)
	}
}

func hidePlanFrom(t *testing.T, e *testEnv, plan, user int64) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO plan_visibility (plan_id, mode) VALUES ($1,'hidden_from')`, plan); err != nil {
		t.Fatalf("set visibility: %v", err)
	}
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO plan_visibility_members (plan_id, user_id) VALUES ($1,$2)`, plan, user); err != nil {
		t.Fatalf("set visibility member: %v", err)
	}
}

func calCfg() *config.Config {
	return &config.Config{PublicURL: "https://aerly.test"}
}

// TestCalendarTokenManagementEndpoints exercises the FE-contract shapes:
// GET/POST /api/calendar/tokens and DELETE /api/calendar/tokens/{token}.
func TestCalendarTokenManagementEndpoints(t *testing.T) {
	e := setup(t, nil, calCfg())
	uid := e.user(t, "cal-user", false)

	// Empty list initially.
	w := e.req(t, "GET", "/api/calendar/tokens", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d %s", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Errorf("empty list = %q, want []", got)
	}

	// Issue a "me" token.
	w = e.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "me"}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("issue me = %d %s", w.Code, w.Body.String())
	}
	tok := decodeBody[map[string]any](t, w)
	if tok["scope"] != "me" || tok["token"] == "" {
		t.Fatalf("issue me bad shape: %v", tok)
	}
	meURL, _ := tok["url"].(string)
	if !strings.HasPrefix(meURL, "https://aerly.test/api/calendar/me.ics?token=") {
		t.Errorf("me url = %q, want me.ics feed url", meURL)
	}
	if _, ok := tok["created_at"]; !ok {
		t.Error("token missing created_at")
	}

	// Issue a "trip" token with an id → url carries that id.
	w = e.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "trip", "id": 42}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("issue trip = %d %s", w.Code, w.Body.String())
	}
	tripTok := decodeBody[map[string]any](t, w)
	if u, _ := tripTok["url"].(string); !strings.Contains(u, "/api/calendar/trip/42.ics?token=") {
		t.Errorf("trip url = %q, want trip/42.ics", u)
	}

	// trip/plan scope without id → 400.
	if w := e.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "plan"}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("plan scope without id = %d, want 400", w.Code)
	}
	// bad scope → 400.
	if w := e.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "x"}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("bad scope = %d, want 400", w.Code)
	}

	// List now has 2 tokens.
	w = e.req(t, "GET", "/api/calendar/tokens", nil, uid)
	list := decodeBody[[]map[string]any](t, w)
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}

	// Revoke the me token.
	meToken := tok["token"].(string)
	if w := e.req(t, "DELETE", "/api/calendar/tokens/"+meToken, nil, uid); w.Code != http.StatusNoContent {
		t.Errorf("revoke = %d, want 204", w.Code)
	}
	// Revoke again → 404.
	if w := e.req(t, "DELETE", "/api/calendar/tokens/"+meToken, nil, uid); w.Code != http.StatusNotFound {
		t.Errorf("double revoke = %d, want 404", w.Code)
	}

	// Token management requires a session.
	if w := e.req(t, "GET", "/api/calendar/tokens", nil, 0); w.Code != http.StatusUnauthorized {
		t.Errorf("anon list = %d, want 401", w.Code)
	}
}

// TestCalendarFeedTokenAuthAndVisibility: the .ics feeds are token-authed (no
// session) and render as the token owner with the §4 predicate, so a plan
// hidden from the owner is absent and another user's token can't see private
// plans.
func TestCalendarFeedTokenAuthAndVisibility(t *testing.T) {
	e := setup(t, nil, calCfg())
	owner := e.user(t, "owner", false)
	member := e.user(t, "member", false)

	trip := seedTrip(t, e, owner)
	seedMember(t, e, trip, member)
	pub := seedPlan(t, e, trip, owner, "Public Flight")
	seedPart(t, e, pub)
	hid := seedPlan(t, e, trip, owner, "Hidden Flight")
	seedPart(t, e, hid)
	hidePlanFrom(t, e, hid, member)

	// Issue per-user tokens.
	ownerTok, err := e.store.CalendarToken(context.Background(), owner, "me")
	if err != nil {
		t.Fatalf("owner token: %v", err)
	}
	memberTok, err := e.store.CalendarToken(context.Background(), member, "me")
	if err != nil {
		t.Fatalf("member token: %v", err)
	}

	feed := func(path string) string {
		w := rawGet(e, path)
		if w.Code != http.StatusOK {
			t.Fatalf("feed %s = %d %s", path, w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/calendar") {
			t.Errorf("feed content-type = %q, want text/calendar", ct)
		}
		return w.Body.String()
	}

	// Owner's me feed has both plans.
	ownerFeed := feed("/api/calendar/me.ics?token=" + ownerTok.Token)
	if !strings.Contains(ownerFeed, "Public Flight") || !strings.Contains(ownerFeed, "Hidden Flight") {
		t.Errorf("owner feed missing a plan:\n%s", ownerFeed)
	}

	// Member's me feed has only the public plan — the hidden one must not leak.
	memberFeed := feed("/api/calendar/me.ics?token=" + memberTok.Token)
	if !strings.Contains(memberFeed, "Public Flight") {
		t.Errorf("member feed missing public plan:\n%s", memberFeed)
	}
	if strings.Contains(memberFeed, "Hidden Flight") {
		t.Errorf("member feed LEAKED hidden plan:\n%s", memberFeed)
	}

	// Trip feed for member: same — hidden absent.
	tripFeed := feed("/api/calendar/trip/" + itoa(trip) + ".ics?token=" + memberTok.Token)
	if strings.Contains(tripFeed, "Hidden Flight") {
		t.Errorf("member trip feed LEAKED hidden plan:\n%s", tripFeed)
	}

	// Single-plan feed for the hidden plan via the member's token → empty
	// (no VEVENT), because the member can't see it.
	planFeed := feed("/api/calendar/plan/" + itoa(hid) + ".ics?token=" + memberTok.Token)
	if strings.Contains(planFeed, "BEGIN:VEVENT") {
		t.Errorf("member single-plan feed for hidden plan should have no events:\n%s", planFeed)
	}

	// Missing token → 401.
	if w := rawGet(e, "/api/calendar/me.ics"); w.Code != http.StatusUnauthorized {
		t.Errorf("no-token feed = %d, want 401", w.Code)
	}
	// Bad token → 401.
	if w := rawGet(e, "/api/calendar/me.ics?token=garbage"); w.Code != http.StatusUnauthorized {
		t.Errorf("bad-token feed = %d, want 401", w.Code)
	}
	// Bad trip id segment → 404.
	if w := rawGet(e, "/api/calendar/trip/abc.ics?token="+ownerTok.Token); w.Code != http.StatusNotFound {
		t.Errorf("bad trip id = %d, want 404", w.Code)
	}
}

// TestCalendarFeedUpdatesReflectPartChanges: a delayed part re-renders (the
// single-plan feed stays live — re-fetch shows the new time and LAST-MODIFIED).
func TestCalendarFeedUpdatesReflectPartChanges(t *testing.T) {
	e := setup(t, nil, calCfg())
	owner := e.user(t, "owner-live", false)
	trip := seedTrip(t, e, owner)
	plan := seedPlan(t, e, trip, owner, "Live Flight")
	seedPart(t, e, plan)
	tok, _ := e.store.CalendarToken(context.Background(), owner, "me")

	first := rawGet(e, "/api/calendar/plan/"+itoa(plan)+".ics?token="+tok.Token).Body.String()
	if !strings.Contains(first, "BEGIN:VEVENT") {
		t.Fatalf("expected a VEVENT:\n%s", first)
	}

	// Push the part 90 minutes later (simulated delay).
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE plan_parts SET starts_at = starts_at + INTERVAL '90 minutes', updated_at = NOW()
		   WHERE plan_id = $1`, plan); err != nil {
		t.Fatalf("delay part: %v", err)
	}
	second := rawGet(e, "/api/calendar/plan/"+itoa(plan)+".ics?token="+tok.Token).Body.String()
	if first == second {
		t.Error("feed did not change after the part time moved")
	}
}
