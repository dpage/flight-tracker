package store

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/testsupport"
)

// seedFlightPart inserts a trip + flight plan + part + flight_details and
// returns (planID, partID). Mirrors the minimal shape the poller tests use.
func seedFlightPart(t *testing.T, s *Store, createdBy int64, ident string) (planID, partID int64) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	var tripID int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('t', $1) RETURNING id`, createdBy).Scan(&tripID); err != nil {
		t.Fatalf("trip: %v", err)
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`, tripID, createdBy); err != nil {
		t.Fatalf("member: %v", err)
	}
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`, tripID, createdBy).Scan(&planID); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at, ends_at, status) VALUES ($1, $2, $3, 'confirmed') RETURNING id`,
		planID, now.Add(time.Hour), now.Add(3*time.Hour)).Scan(&partID); err != nil {
		t.Fatalf("part: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO flight_details (plan_part_id, ident, scheduled_out, scheduled_in, origin_iata, dest_iata)
		VALUES ($1, $2, $3, $4, 'LHR', 'JFK')`,
		partID, ident, now.Add(time.Hour), now.Add(3*time.Hour)); err != nil {
		t.Fatalf("fd: %v", err)
	}
	return planID, partID
}

func TestAlertPrefsFor_DefaultsWhenNoRow(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)

	got, err := s.AlertPrefsFor(ctx, uid)
	if err != nil {
		t.Fatalf("AlertPrefsFor: %v", err)
	}
	if !got.InApp || !got.Email || got.MinDelayMin != 15 {
		t.Fatalf("defaults = %+v, want in_app+email on, 15", got)
	}
}

func TestSetAlertPrefs_Upsert(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)

	if err := s.SetAlertPrefs(ctx, AlertPrefs{UserID: uid, InApp: false, Email: true, MinDelayMin: 30}); err != nil {
		t.Fatalf("SetAlertPrefs insert: %v", err)
	}
	got, _ := s.AlertPrefsFor(ctx, uid)
	if got.InApp || !got.Email || got.MinDelayMin != 30 {
		t.Fatalf("after insert = %+v", got)
	}
	// Re-upsert overwrites.
	if err := s.SetAlertPrefs(ctx, AlertPrefs{UserID: uid, InApp: true, Email: false, MinDelayMin: 5}); err != nil {
		t.Fatalf("SetAlertPrefs update: %v", err)
	}
	got, _ = s.AlertPrefsFor(ctx, uid)
	if !got.InApp || got.Email || got.MinDelayMin != 5 {
		t.Fatalf("after update = %+v", got)
	}
}

func TestPlanAlertOptin_AddRemoveIdempotent(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	owner := testsupport.InsertUser(t, s.pool, "owner", false, true)
	viewer := testsupport.InsertUser(t, s.pool, "viewer", false, true)
	planID, _ := seedFlightPart(t, s, owner, "BA1")

	// Add twice (idempotent), then it shows up in recipients.
	if err := s.AddPlanAlertOptin(ctx, planID, viewer); err != nil {
		t.Fatalf("add optin: %v", err)
	}
	if err := s.AddPlanAlertOptin(ctx, planID, viewer); err != nil {
		t.Fatalf("add optin again: %v", err)
	}
	recips, err := s.AlertRecipients(ctx, planID)
	if err != nil {
		t.Fatalf("recipients: %v", err)
	}
	if !contains(recips, viewer) {
		t.Fatalf("viewer not in recipients after opt-in: %v", recips)
	}
	// Remove twice (second is a no-op).
	if err := s.RemovePlanAlertOptin(ctx, planID, viewer); err != nil {
		t.Fatalf("remove optin: %v", err)
	}
	if err := s.RemovePlanAlertOptin(ctx, planID, viewer); err != nil {
		t.Fatalf("remove optin again: %v", err)
	}
	recips, _ = s.AlertRecipients(ctx, planID)
	if contains(recips, viewer) {
		t.Fatalf("viewer still a recipient after opt-out: %v", recips)
	}
}

func TestAlertRecipientsWithPrefs_OwnerPassengerOptin(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	owner := testsupport.InsertUser(t, s.pool, "owner", false, true)
	pax := testsupport.InsertUser(t, s.pool, "pax", false, true)
	viewer := testsupport.InsertUser(t, s.pool, "viewer", false, true)
	stranger := testsupport.InsertUser(t, s.pool, "stranger", false, true)
	planID, _ := seedFlightPart(t, s, owner, "BA2")

	if _, err := s.pool.Exec(ctx, `INSERT INTO plan_passengers (plan_id, user_id) VALUES ($1, $2)`, planID, pax); err != nil {
		t.Fatalf("add pax: %v", err)
	}
	if err := s.AddPlanAlertOptin(ctx, planID, viewer); err != nil {
		t.Fatalf("optin: %v", err)
	}
	if err := s.UpsertVerifiedEmail(ctx, pax, "pax@aerly.test"); err != nil {
		t.Fatalf("pax email: %v", err)
	}
	if err := s.SetAlertPrefs(ctx, AlertPrefs{UserID: viewer, InApp: true, Email: false, MinDelayMin: 60}); err != nil {
		t.Fatalf("viewer prefs: %v", err)
	}

	recips, err := s.AlertRecipientsWithPrefs(ctx, planID)
	if err != nil {
		t.Fatalf("AlertRecipientsWithPrefs: %v", err)
	}
	byID := map[int64]AlertRecipient{}
	for _, r := range recips {
		byID[r.UserID] = r
	}
	if _, ok := byID[stranger]; ok {
		t.Fatalf("stranger should not be a recipient")
	}
	if r, ok := byID[owner]; !ok || !r.InApp || !r.Email || r.MinDelayMin != 15 {
		t.Fatalf("owner default prefs wrong: %+v ok=%v", r, ok)
	}
	if r := byID[pax]; r.EmailAddr != "pax@aerly.test" {
		t.Fatalf("pax email not folded in: %+v", r)
	}
	if r := byID[viewer]; r.Email || r.MinDelayMin != 60 {
		t.Fatalf("viewer prefs not applied: %+v", r)
	}
}

func TestFlightPartAlertSig_RoundTrip(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	owner := testsupport.InsertUser(t, s.pool, "owner", false, true)
	_, partID := seedFlightPart(t, s, owner, "BA3")

	// Never alerted: ok=false.
	if _, ok, err := s.FlightPartAlertSig(ctx, partID); err != nil || ok {
		t.Fatalf("fresh sig: ok=%v err=%v, want ok=false", ok, err)
	}
	if err := s.SetFlightPartAlertSig(ctx, partID, "delay:45"); err != nil {
		t.Fatalf("set sig: %v", err)
	}
	sig, ok, err := s.FlightPartAlertSig(ctx, partID)
	if err != nil || !ok || sig != "delay:45" {
		t.Fatalf("read sig = %q ok=%v err=%v", sig, ok, err)
	}
}

func contains(xs []int64, v int64) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
