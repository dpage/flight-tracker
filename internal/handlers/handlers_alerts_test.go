package handlers

import (
	"context"
	"net/http"
	"testing"
)

func TestGetAlertPrefs_Defaults(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "alice", false)

	w := e.req(t, "GET", "/api/alert-prefs", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	got := decodeBody[alertPrefsDTO](t, w)
	if !got.InApp || !got.Email || got.MinDelayMin != 15 {
		t.Fatalf("defaults = %+v", got)
	}
}

func TestPutAlertPrefs_PartialPatch(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "alice", false)

	// Patch only min_delay_min; in_app/email keep their defaults.
	w := e.req(t, "PUT", "/api/alert-prefs", map[string]any{"min_delay_min": 30}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	got := decodeBody[alertPrefsDTO](t, w)
	if !got.InApp || !got.Email || got.MinDelayMin != 30 {
		t.Fatalf("after patch = %+v", got)
	}

	// Patch email off; min_delay_min persists.
	w = e.req(t, "PUT", "/api/alert-prefs", map[string]any{"email": false}, uid)
	got = decodeBody[alertPrefsDTO](t, w)
	if !got.InApp || got.Email || got.MinDelayMin != 30 {
		t.Fatalf("after second patch = %+v", got)
	}
}

func TestPlanAlertOptin_RequiresVisibility(t *testing.T) {
	e := setup(t, nil, nil)
	ctx := context.Background()
	owner := e.user(t, "owner", false)
	outsider := e.user(t, "outsider", false)

	// Owner-created flight plan the outsider can't see.
	var tripID, planID int64
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('t', $1) RETURNING id`, owner).Scan(&tripID); err != nil {
		t.Fatalf("trip: %v", err)
	}
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`, tripID, owner); err != nil {
		t.Fatalf("member: %v", err)
	}
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`, tripID, owner).Scan(&planID); err != nil {
		t.Fatalf("plan: %v", err)
	}

	// Outsider can't see it → opt-in is 404.
	w := e.req(t, "POST", "/api/plans/"+itoa(planID)+"/alerts/optin", nil, outsider)
	if w.Code != http.StatusNotFound {
		t.Fatalf("outsider opt-in status = %d, want 404", w.Code)
	}

	// Owner can opt in → 204, and shows up as a recipient.
	w = e.req(t, "POST", "/api/plans/"+itoa(planID)+"/alerts/optin", nil, owner)
	if w.Code != http.StatusNoContent {
		t.Fatalf("owner opt-in status = %d, body=%s", w.Code, w.Body.String())
	}

	// Opt-out → 204 (idempotent).
	w = e.req(t, "DELETE", "/api/plans/"+itoa(planID)+"/alerts/optin", nil, owner)
	if w.Code != http.StatusNoContent {
		t.Fatalf("opt-out status = %d", w.Code)
	}
	w = e.req(t, "DELETE", "/api/plans/"+itoa(planID)+"/alerts/optin", nil, owner)
	if w.Code != http.StatusNoContent {
		t.Fatalf("repeat opt-out status = %d", w.Code)
	}
}
