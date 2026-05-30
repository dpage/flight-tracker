package store

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/testsupport"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return New(testsupport.NewPool(t))
}

var ctx = context.Background()

var loginSeq atomic.Int64

// mkUser inserts a fresh user (unique login) and returns its id; flights need
// a valid created_by FK.
func mkUser(t *testing.T, s *Store) int64 {
	t.Helper()
	return testsupport.InsertUser(t, s.pool,
		fmt.Sprintf("creator%d", loginSeq.Add(1)), false, true)
}

func mkFlight(t *testing.T, s *Store, ident string, out, in time.Time) *Flight {
	t.Helper()
	f, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: ident, ScheduledOut: out, ScheduledIn: in,
		OriginIATA: "LHR", DestIATA: "JFK",
	}, mkUser(t, s))
	if err != nil {
		t.Fatalf("CreateFlight: %v", err)
	}
	return f
}

func TestNormalizeIdent(t *testing.T) {
	if got := normalizeIdent("  ba 286 "); got != "BA286" {
		t.Errorf("normalizeIdent = %q, want BA286", got)
	}
	if got := normalizeIdent(""); got != "" {
		t.Errorf("empty → %q", got)
	}
}

func TestNormalizeICAO24(t *testing.T) {
	if normalizeICAO24("  ") != nil {
		t.Error("blank icao24 should be nil")
	}
	v := normalizeICAO24(" 400A1D ")
	if v == nil || *v != "400a1d" {
		t.Errorf("normalizeICAO24 = %v, want 400a1d", v)
	}
}

func TestUpperPtr(t *testing.T) {
	if upperPtr(nil) != nil {
		t.Error("nil → nil")
	}
	s := "abc"
	if got := upperPtr(&s); *got != "ABC" {
		t.Errorf("upperPtr = %q", *got)
	}
}

func TestLookupCoords(t *testing.T) {
	la, lo := lookupCoords("LHR")
	if la == nil || lo == nil {
		t.Error("LHR should resolve coords")
	}
	if la2, lo2 := lookupCoords("ZZZ"); la2 != nil || lo2 != nil {
		t.Error("unknown IATA → nil coords")
	}
}

func TestCreateFlightValidation(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	_, err := s.CreateFlight(ctx, CreateFlightPayload{Ident: "  "}, 0)
	if err == nil {
		t.Error("empty ident should error")
	}
	_, err = s.CreateFlight(ctx, CreateFlightPayload{Ident: "X1"}, 0)
	if err == nil {
		t.Error("zero times should error")
	}
	_, err = s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "X1", ScheduledOut: now, ScheduledIn: now.Add(-time.Hour),
	}, 0)
	if err == nil {
		t.Error("scheduled_in before out should error")
	}
}

func TestCreateFlightStatusDerivation(t *testing.T) {
	s := newStore(t)
	now := time.Now()

	future := mkFlight(t, s, "FUT1", now.Add(time.Hour), now.Add(3*time.Hour))
	if future.Status != "Scheduled" {
		t.Errorf("future flight status = %q, want Scheduled", future.Status)
	}
	enroute := mkFlight(t, s, "ENR1", now.Add(-time.Hour), now.Add(time.Hour))
	if enroute.Status != "Enroute" {
		t.Errorf("in-air flight status = %q, want Enroute", enroute.Status)
	}
	arrived := mkFlight(t, s, "ARR1", now.Add(-3*time.Hour), now.Add(-time.Hour))
	if arrived.Status != "Arrived" {
		t.Errorf("past flight status = %q, want Arrived", arrived.Status)
	}
	// Coords resolved from the airport table at write time.
	if future.OriginLat == nil || future.DestLat == nil {
		t.Error("expected origin/dest coords resolved from IATA")
	}
}

func TestCreateFlightWithICAO24(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	f, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "IC1", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "lhr", DestIATA: "jfk", ICAO24: " 400A1D ", Notes: "hello",
	}, mkUser(t, s))
	if err != nil {
		t.Fatalf("CreateFlight: %v", err)
	}
	if f.ICAO24 == nil || *f.ICAO24 != "400a1d" {
		t.Errorf("icao24 = %v", f.ICAO24)
	}
	if f.Notes != "hello" || f.OriginIATA != "LHR" {
		t.Errorf("unexpected flight %+v", f)
	}
}

func TestListAndGetFlight(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	if fs, err := s.ListFlights(ctx); err != nil || len(fs) != 0 {
		t.Fatalf("empty list: %v %v", fs, err)
	}
	b := mkFlight(t, s, "B2", now.Add(2*time.Hour), now.Add(4*time.Hour))
	a := mkFlight(t, s, "A1", now.Add(time.Hour), now.Add(3*time.Hour))
	list, err := s.ListFlights(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v %v", list, err)
	}
	// Ordered by scheduled_out ASC → A1 (earlier) first.
	if list[0].ID != a.ID || list[1].ID != b.ID {
		t.Errorf("ordering wrong: %d,%d", list[0].ID, list[1].ID)
	}
	got, err := s.FlightByID(ctx, a.ID)
	if err != nil || got.Ident != "A1" {
		t.Fatalf("FlightByID: %v %v", got, err)
	}
	if _, err := s.FlightByID(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing flight should be ErrNotFound, got %v", err)
	}
}

func TestActiveFlights(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	enr := mkFlight(t, s, "ENR", now.Add(-time.Hour), now.Add(time.Hour))
	mkFlight(t, s, "FARFUTURE", now.Add(48*time.Hour), now.Add(50*time.Hour))
	arrived := mkFlight(t, s, "DONE", now.Add(-5*time.Hour), now.Add(-3*time.Hour))

	act, err := s.ActiveFlights(ctx, now)
	if err != nil {
		t.Fatalf("ActiveFlights: %v", err)
	}
	ids := map[int64]bool{}
	for _, f := range act {
		ids[f.ID] = true
	}
	if !ids[enr.ID] {
		t.Error("enroute flight should be active")
	}
	if ids[arrived.ID] {
		t.Error("arrived flight should not be active")
	}
}

func TestUpdateFlight(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	f := mkFlight(t, s, "UPD1", now.Add(-time.Hour), now.Add(time.Hour))

	// Partial update: only notes; everything else preserved via COALESCE.
	notes := "updated notes"
	upd, err := s.UpdateFlight(ctx, f.ID, UpdateFlightPayload{Notes: &notes})
	if err != nil {
		t.Fatalf("UpdateFlight: %v", err)
	}
	if upd.Notes != notes || upd.OriginIATA != "LHR" {
		t.Errorf("partial update lost fields: %+v", upd)
	}

	// Change origin IATA → coords re-resolved.
	newOrigin := "cdg"
	upd, err = s.UpdateFlight(ctx, f.ID, UpdateFlightPayload{OriginIATA: &newOrigin})
	if err != nil {
		t.Fatalf("UpdateFlight origin: %v", err)
	}
	if upd.OriginIATA != "CDG" || upd.OriginLat == nil {
		t.Errorf("origin change didn't re-resolve coords: %+v", upd)
	}

	// Explicit status wins over derivation.
	st := "Cancelled"
	upd, _ = s.UpdateFlight(ctx, f.ID, UpdateFlightPayload{Status: &st})
	if upd.Status != "Cancelled" {
		t.Errorf("explicit status not applied: %q", upd.Status)
	}
	// With Cancelled preserved, a no-status update keeps it.
	upd, _ = s.UpdateFlight(ctx, f.ID, UpdateFlightPayload{Notes: &notes})
	if upd.Status != "Cancelled" {
		t.Errorf("terminal status not preserved: %q", upd.Status)
	}

	// Set then clear icao24.
	ic := "400a1d"
	upd, _ = s.UpdateFlight(ctx, f.ID, UpdateFlightPayload{ICAO24: &ic})
	if upd.ICAO24 == nil || *upd.ICAO24 != "400a1d" {
		t.Errorf("icao24 not set: %v", upd.ICAO24)
	}
	empty := ""
	upd, _ = s.UpdateFlight(ctx, f.ID, UpdateFlightPayload{ICAO24: &empty})
	if upd.ICAO24 != nil {
		t.Errorf("empty icao24 should clear to NULL, got %v", upd.ICAO24)
	}

	if _, err := s.UpdateFlight(ctx, 99999, UpdateFlightPayload{Notes: &notes}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update missing → ErrNotFound, got %v", err)
	}
}

func TestUpdateFlightStatusReDerived(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	f := mkFlight(t, s, "RED1", now.Add(time.Hour), now.Add(2*time.Hour)) // Scheduled
	// Move schedule into the past with no explicit status → derive Arrived.
	past := now.Add(-3 * time.Hour)
	pastIn := now.Add(-time.Hour)
	upd, err := s.UpdateFlight(ctx, f.ID, UpdateFlightPayload{
		ScheduledOut: &past, ScheduledIn: &pastIn,
	})
	if err != nil {
		t.Fatalf("UpdateFlight: %v", err)
	}
	if upd.Status != "Arrived" {
		t.Errorf("status should re-derive to Arrived, got %q", upd.Status)
	}
}

func TestRefreshFlightStatus(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	f := mkFlight(t, s, "RFS1", now.Add(time.Hour), now.Add(2*time.Hour))
	if err := s.RefreshFlightStatus(ctx, f.ID); err != nil {
		t.Fatalf("RefreshFlightStatus: %v", err)
	}
	got, _ := s.FlightByID(ctx, f.ID)
	if got.LastPolledAt == nil {
		t.Error("last_polled_at should be set")
	}
	if got.Status != "Scheduled" {
		t.Errorf("status = %q, want Scheduled", got.Status)
	}

	// Cancelled is preserved by RefreshFlightStatus.
	st := "Cancelled"
	_, _ = s.UpdateFlight(ctx, f.ID, UpdateFlightPayload{Status: &st})
	_ = s.RefreshFlightStatus(ctx, f.ID)
	got, _ = s.FlightByID(ctx, f.ID)
	if got.Status != "Cancelled" {
		t.Errorf("Cancelled not preserved by refresh: %q", got.Status)
	}
}

func TestDeleteFlight(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	f := mkFlight(t, s, "DEL1", now, now.Add(time.Hour))
	if err := s.DeleteFlight(ctx, f.ID); err != nil {
		t.Fatalf("DeleteFlight: %v", err)
	}
	if err := s.DeleteFlight(ctx, f.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete → ErrNotFound, got %v", err)
	}
}

func TestPassengers(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	f := mkFlight(t, s, "PAX1", now, now.Add(time.Hour))
	u1 := testsupport.InsertUser(t, s.pool, "u1", false, true)
	u2 := testsupport.InsertUser(t, s.pool, "u2", false, true)

	if m, err := s.PassengersByFlight(ctx, nil); err != nil || len(m) != 0 {
		t.Fatalf("empty ids → empty map: %v %v", m, err)
	}

	if err := s.AddPassenger(ctx, f.ID, u1); err != nil {
		t.Fatalf("AddPassenger: %v", err)
	}
	// Idempotent (ON CONFLICT DO NOTHING).
	if err := s.AddPassenger(ctx, f.ID, u1); err != nil {
		t.Fatalf("AddPassenger idempotent: %v", err)
	}
	_ = s.AddPassenger(ctx, f.ID, u2)

	m, err := s.PassengersByFlight(ctx, []int64{f.ID})
	if err != nil || len(m[f.ID]) != 2 {
		t.Fatalf("PassengersByFlight = %v %v", m, err)
	}

	if err := s.RemovePassenger(ctx, f.ID, u1); err != nil {
		t.Fatalf("RemovePassenger: %v", err)
	}
	if err := s.RemovePassenger(ctx, f.ID, u1); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing absent passenger → ErrNotFound, got %v", err)
	}
}

// BackfillFlight should populate callsign the same way it populates
// icao24 — only when the column is currently NULL — and a refreshed row
// must expose Callsign + LastResolvedAt through Flight.
func TestBackfillFillsCallsign(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	f := mkFlight(t, s, "LH493", now, now.Add(time.Hour))
	if err := s.BackfillFlight(ctx, f.ID, BackfillPayload{
		ICAO24:   "3C4A8C",
		Callsign: "DLH493",
	}); err != nil {
		t.Fatalf("BackfillFlight: %v", err)
	}
	got, _ := s.FlightByID(ctx, f.ID)
	if got.ICAO24 == nil || *got.ICAO24 != "3c4a8c" {
		t.Errorf("icao24 not backfilled: %v", got.ICAO24)
	}
	if got.Callsign == nil || *got.Callsign != "DLH493" {
		t.Errorf("callsign not backfilled: %v", got.Callsign)
	}
	if got.LastResolvedAt != nil {
		t.Errorf("BackfillFlight should NOT bump last_resolved_at (that's RefreshFlightAirframe's job); got %v", got.LastResolvedAt)
	}
}

// Existing values must not be overwritten by BackfillFlight — same
// "first-wins" semantics as the other backfilled columns.
func TestBackfillCallsignPreservesExisting(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	f := mkFlight(t, s, "LH493", now, now.Add(time.Hour))
	if err := s.BackfillFlight(ctx, f.ID, BackfillPayload{Callsign: "ORIGINAL"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.BackfillFlight(ctx, f.ID, BackfillPayload{Callsign: "REPLACED"}); err != nil {
		t.Fatalf("BackfillFlight: %v", err)
	}
	got, _ := s.FlightByID(ctx, f.ID)
	if got.Callsign == nil || *got.Callsign != "ORIGINAL" {
		t.Errorf("callsign should not be overwritten by second backfill: %v", got.Callsign)
	}
}

// RefreshFlightAirframe is the day-of refresh hook the poller calls when
// it's close to departure: it OVERWRITES icao24 and callsign (unlike
// BackfillFlight) so airframe swaps land in the DB, and it bumps
// last_resolved_at unconditionally so the poller can throttle itself.
func TestRefreshFlightAirframeOverwritesAndStamps(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	f := mkFlight(t, s, "LH493", now, now.Add(time.Hour))
	// Seed: pretend the initial backfill set these from a far-out schedule.
	if err := s.BackfillFlight(ctx, f.ID, BackfillPayload{
		ICAO24: "3C4A8B", Callsign: "DLH493",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Day-of: AeroDataBox now reports a different airframe.
	if err := s.RefreshFlightAirframe(ctx, f.ID, "3C4A8C", "DLH493"); err != nil {
		t.Fatalf("RefreshFlightAirframe: %v", err)
	}
	got, _ := s.FlightByID(ctx, f.ID)
	if got.ICAO24 == nil || *got.ICAO24 != "3c4a8c" {
		t.Errorf("icao24 not overwritten: %v", got.ICAO24)
	}
	if got.Callsign == nil || *got.Callsign != "DLH493" {
		t.Errorf("callsign = %v", got.Callsign)
	}
	if got.LastResolvedAt == nil {
		t.Fatal("last_resolved_at should be set")
	}
	if time.Since(*got.LastResolvedAt) > 5*time.Second {
		t.Errorf("last_resolved_at not bumped to NOW(): %v ago", time.Since(*got.LastResolvedAt))
	}
}

// Empty inputs to RefreshFlightAirframe still bump last_resolved_at so the
// poller doesn't thrash retrying a resolver that has nothing new to say,
// but they must NOT blank out an existing icao24/callsign.
func TestRefreshFlightAirframeEmptyValuesTouchOnly(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	f := mkFlight(t, s, "LH493", now, now.Add(time.Hour))
	if err := s.BackfillFlight(ctx, f.ID, BackfillPayload{
		ICAO24: "3C4A8B", Callsign: "DLH493",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.RefreshFlightAirframe(ctx, f.ID, "", ""); err != nil {
		t.Fatalf("RefreshFlightAirframe: %v", err)
	}
	got, _ := s.FlightByID(ctx, f.ID)
	if got.ICAO24 == nil || *got.ICAO24 != "3c4a8b" {
		t.Errorf("empty icao24 should preserve existing: %v", got.ICAO24)
	}
	if got.Callsign == nil || *got.Callsign != "DLH493" {
		t.Errorf("empty callsign should preserve existing: %v", got.Callsign)
	}
	if got.LastResolvedAt == nil {
		t.Error("last_resolved_at should still be bumped even when nothing changed")
	}
}

func TestLatestPositionNoRows(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	f := mkFlight(t, s, "LP1", now, now.Add(time.Hour))
	got, err := s.LatestPosition(ctx, f.ID)
	if err != nil || got != nil {
		t.Errorf("no rows → (nil, nil); got %+v %v", got, err)
	}
}

func TestShares(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	f := mkFlight(t, s, "SH1", now, now.Add(time.Hour))
	u1 := testsupport.InsertUser(t, s.pool, "s1", false, true)
	u2 := testsupport.InsertUser(t, s.pool, "s2", false, true)

	if m, err := s.SharedUserIDsByFlight(ctx, nil); err != nil || len(m) != 0 {
		t.Fatalf("empty ids → empty map: %v %v", m, err)
	}
	if err := s.AddShare(ctx, f.ID, u1); err != nil {
		t.Fatalf("AddShare: %v", err)
	}
	// Idempotent.
	if err := s.AddShare(ctx, f.ID, u1); err != nil {
		t.Fatalf("AddShare idempotent: %v", err)
	}
	_ = s.AddShare(ctx, f.ID, u2)

	m, err := s.SharedUserIDsByFlight(ctx, []int64{f.ID})
	if err != nil || len(m[f.ID]) != 2 {
		t.Fatalf("SharedUserIDsByFlight = %v %v", m, err)
	}
	if err := s.RemoveShare(ctx, f.ID, u1); err != nil {
		t.Fatalf("RemoveShare: %v", err)
	}
	if err := s.RemoveShare(ctx, f.ID, u1); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing absent share → ErrNotFound, got %v", err)
	}
}

func TestVisibilityHelpers(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	alice := testsupport.InsertUser(t, s.pool, "alice-v", false, true)
	bob := testsupport.InsertUser(t, s.pool, "bob-v", false, true)
	carol := testsupport.InsertUser(t, s.pool, "carol-v", false, true)

	private, _ := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "PV", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, alice)
	shared, _ := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "SV", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, alice)
	if err := s.AddShare(ctx, shared.ID, bob); err != nil {
		t.Fatalf("AddShare: %v", err)
	}
	public, _ := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "PU", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
		IsPublic: true,
	}, alice)

	listIdents := func(uid int64, showAll bool) []string {
		fs, err := s.ListVisibleFlights(ctx, uid, showAll, false)
		if err != nil {
			t.Fatalf("ListVisibleFlights: %v", err)
		}
		out := make([]string, 0, len(fs))
		for _, f := range fs {
			out = append(out, f.Ident)
		}
		return out
	}
	want := func(have, expected []string) bool {
		if len(have) != len(expected) {
			return false
		}
		set := map[string]bool{}
		for _, s := range have {
			set[s] = true
		}
		for _, s := range expected {
			if !set[s] {
				return false
			}
		}
		return true
	}

	if got := listIdents(alice, false); !want(got, []string{"PV", "SV", "PU"}) {
		t.Errorf("alice list = %v, want all three", got)
	}
	// Bob has an explicit share on SV but is not alice's friend, so he only
	// sees SV — is_public alone no longer grants access to non-friends.
	if got := listIdents(bob, false); !want(got, []string{"SV"}) {
		t.Errorf("bob list = %v, want SV only (not a friend)", got)
	}
	// Carol is a stranger (not alice's friend), so she sees nothing — is_public
	// alone no longer grants access to non-friends.
	if got := listIdents(carol, false); !want(got, []string{}) {
		t.Errorf("carol list = %v, want nothing (stranger)", got)
	}
	if got := listIdents(carol, true); !want(got, []string{"PV", "SV", "PU"}) {
		t.Errorf("carol show-all list = %v, want all three", got)
	}

	ok, err := s.CanView(ctx, private.ID, carol, false)
	if err != nil || ok {
		t.Errorf("carol CanView private = %v %v, want false nil", ok, err)
	}
	ok, _ = s.CanView(ctx, private.ID, carol, true)
	if !ok {
		t.Errorf("carol CanView private with show-all should be true")
	}
	ok, _ = s.CanView(ctx, shared.ID, bob, false)
	if !ok {
		t.Errorf("bob CanView shared should be true")
	}
	// Carol is not alice's friend, so she cannot view the public flight.
	ok, _ = s.CanView(ctx, public.ID, carol, false)
	if ok {
		t.Errorf("carol CanView public should be false (not a friend)")
	}

	if ok, _ := s.CanEdit(ctx, private.ID, alice); !ok {
		t.Errorf("alice CanEdit own flight should be true")
	}
	if ok, _ := s.CanEdit(ctx, private.ID, bob); ok {
		t.Errorf("bob CanEdit alice's flight should be false")
	}
	if _, err := s.CanEdit(ctx, 999999, alice); !errors.Is(err, ErrNotFound) {
		t.Errorf("CanEdit missing id should be ErrNotFound, got %v", err)
	}

	// Friend-of-creator visibility: turning carol into alice's friend grants
	// carol access to public flights (is_public AND friend-of-creator), but
	// NOT to private flights — the friend branch no longer bypasses is_public.
	if _, err := s.RequestFriendship(ctx, alice, carol, ""); err != nil {
		t.Fatalf("alice→carol request: %v", err)
	}
	if _, err := s.AcceptFriendship(ctx, carol, alice); err != nil {
		t.Fatalf("carol accepts: %v", err)
	}
	if got := listIdents(carol, false); !want(got, []string{"PU"}) {
		t.Errorf("carol after friending alice = %v, want PU only (public+friend; private still hidden)", got)
	}
	// Private flight is still hidden from carol even after friendship.
	ok, _ = s.CanView(ctx, private.ID, carol, false)
	if ok {
		t.Errorf("carol CanView private after friending alice should be false (is_public=false)")
	}
	// VisibleUserIDs for a private flight does NOT include friends —
	// the friend branch only fires when is_public = true.
	pvIDs, err := s.VisibleUserIDs(ctx, private.ID)
	if err != nil {
		t.Fatalf("VisibleUserIDs(PV): %v", err)
	}
	pvSet := map[int64]bool{}
	for _, id := range pvIDs {
		pvSet[id] = true
	}
	if pvSet[carol] {
		t.Errorf("VisibleUserIDs(PV) should not contain friend carol for private flight: %v", pvIDs)
	}

	// VisibleUserIDs: shared flight (private) returns only {alice, bob} —
	// carol is alice's friend but SV is not public, so the friend branch
	// does not fire. The explicit share for bob still applies.
	ids, err := s.VisibleUserIDs(ctx, shared.ID)
	if err != nil {
		t.Fatalf("VisibleUserIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("VisibleUserIDs len = %d, want 2 (alice+bob only): %v", len(ids), ids)
	}
}

// TestListVisibleFlights_ShowOldFilter verifies that the showOld parameter
// gates flights whose effective arrival (COALESCE actual_in, estimated_in,
// scheduled_in) is more than 24h in the past. The boundary uses >= so a
// flight that arrived exactly 24h ago is still visible.
func TestListVisibleFlights_ShowOldFilter(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	alice := testsupport.InsertUser(t, s.pool, "old-alice", false, true)

	// Create flights owned by alice, then backdate their arrival timestamps
	// directly so we can test each fallback in the COALESCE.
	_, _ = s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "FRESH", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, alice)
	_, _ = s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "STALE", ScheduledOut: now.Add(-48 * time.Hour), ScheduledIn: now.Add(-25 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, alice)
	// "boundary" has scheduled_in 1 minute inside the 24h window so it
	// stays visible even accounting for the small gap between Go's now and
	// the Postgres NOW() evaluated when the query runs.
	_, _ = s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "BOUND", ScheduledOut: now.Add(-48 * time.Hour), ScheduledIn: now.Add(-24*time.Hour + time.Minute),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, alice)
	// Stale by actual_in even though scheduled_in is recent — exercises the
	// COALESCE picking actual_in first.
	actualStale, _ := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "ACTSTL", ScheduledOut: now.Add(-30 * time.Hour), ScheduledIn: now.Add(-time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, alice)
	if _, err := s.pool.Exec(ctx,
		`UPDATE flights SET actual_in = $1 WHERE id = $2`,
		now.Add(-25*time.Hour), actualStale.ID); err != nil {
		t.Fatalf("backdate actual_in: %v", err)
	}
	// Stale by estimated_in even though actual_in is NULL and scheduled_in is
	// recent — exercises the middle COALESCE branch.
	estStale, _ := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "ESTSTL", ScheduledOut: now.Add(-30 * time.Hour), ScheduledIn: now.Add(-time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, alice)
	if _, err := s.pool.Exec(ctx,
		`UPDATE flights SET estimated_in = $1 WHERE id = $2`,
		now.Add(-25*time.Hour), estStale.ID); err != nil {
		t.Fatalf("backdate estimated_in: %v", err)
	}

	idents := func(showOld bool) map[string]bool {
		fs, err := s.ListVisibleFlights(ctx, alice, false, showOld)
		if err != nil {
			t.Fatalf("ListVisibleFlights: %v", err)
		}
		out := map[string]bool{}
		for _, f := range fs {
			out[f.Ident] = true
		}
		return out
	}

	got := idents(false)
	if !got["FRESH"] {
		t.Errorf("showOld=false: FRESH should be visible, got %v", got)
	}
	if got["STALE"] {
		t.Errorf("showOld=false: STALE should be hidden, got %v", got)
	}
	if !got["BOUND"] {
		t.Errorf("showOld=false: BOUND (exactly 24h) should be visible, got %v", got)
	}
	if got["ACTSTL"] {
		t.Errorf("showOld=false: ACTSTL (actual_in 25h ago) should be hidden, got %v", got)
	}
	if got["ESTSTL"] {
		t.Errorf("showOld=false: ESTSTL (estimated_in 25h ago) should be hidden, got %v", got)
	}

	got = idents(true)
	for _, id := range []string{"FRESH", "STALE", "BOUND", "ACTSTL", "ESTSTL"} {
		if !got[id] {
			t.Errorf("showOld=true: %s should be visible, got %v", id, got)
		}
	}
}

func TestListVisibleFlights_FriendGatedVisibility(t *testing.T) {
	s := newStore(t)
	now := time.Now()

	creator := mkUser(t, s)
	friend := mkUser(t, s)
	stranger := mkUser(t, s)
	pending := mkUser(t, s)

	if _, err := s.RequestFriendship(ctx, creator, friend, ""); err != nil {
		t.Fatalf("RequestFriendship friend: %v", err)
	}
	if _, err := s.AcceptFriendship(ctx, friend, creator); err != nil {
		t.Fatalf("AcceptFriendship friend: %v", err)
	}
	if _, err := s.RequestFriendship(ctx, creator, pending, ""); err != nil {
		t.Fatalf("RequestFriendship pending: %v", err)
	}

	private, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "PR1", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", IsPublic: false,
	}, creator)
	if err != nil {
		t.Fatalf("CreateFlight private: %v", err)
	}

	public, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "PU1", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", IsPublic: true,
	}, creator)
	if err != nil {
		t.Fatalf("CreateFlight public: %v", err)
	}

	contains := func(list []*Flight, id int64) bool {
		for _, f := range list {
			if f.ID == id {
				return true
			}
		}
		return false
	}
	mustList := func(viewer int64) []*Flight {
		t.Helper()
		got, err := s.ListVisibleFlights(ctx, viewer, false, true)
		if err != nil {
			t.Fatalf("ListVisibleFlights: %v", err)
		}
		return got
	}

	// Creator sees both.
	got := mustList(creator)
	if !contains(got, private.ID) || !contains(got, public.ID) {
		t.Errorf("creator: got %v, want both flights", got)
	}

	// Friend sees ONLY the public one (the change — used to see both via
	// standalone friend-of-creator branch).
	got = mustList(friend)
	if contains(got, private.ID) {
		t.Errorf("friend should not see private flight")
	}
	if !contains(got, public.ID) {
		t.Errorf("friend should see public flight")
	}

	// Stranger sees neither (the change — used to see public via is_public).
	got = mustList(stranger)
	if contains(got, private.ID) || contains(got, public.ID) {
		t.Errorf("stranger should see nothing, got %v", got)
	}

	// Pending friend sees nothing — pending is not enough.
	got = mustList(pending)
	if contains(got, private.ID) || contains(got, public.ID) {
		t.Errorf("pending should see nothing, got %v", got)
	}
}

func TestCanView_FriendGatedVisibility(t *testing.T) {
	s := newStore(t)
	now := time.Now()

	creator := mkUser(t, s)
	friend := mkUser(t, s)
	stranger := mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, creator, friend, ""); err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	if _, err := s.AcceptFriendship(ctx, friend, creator); err != nil {
		t.Fatalf("AcceptFriendship: %v", err)
	}

	private, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "PR2", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", IsPublic: false,
	}, creator)
	if err != nil {
		t.Fatalf("CreateFlight: %v", err)
	}
	public, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "PU2", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", IsPublic: true,
	}, creator)
	if err != nil {
		t.Fatalf("CreateFlight: %v", err)
	}

	cases := []struct {
		name   string
		fid    int64
		viewer int64
		wantOK bool
	}{
		{"creator/private", private.ID, creator, true},
		{"creator/public", public.ID, creator, true},
		{"friend/private", private.ID, friend, false},
		{"friend/public", public.ID, friend, true},
		{"stranger/private", private.ID, stranger, false},
		{"stranger/public", public.ID, stranger, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := s.CanView(ctx, tc.fid, tc.viewer, false)
			if err != nil {
				t.Errorf("err=%v", err)
			}
			if ok != tc.wantOK {
				t.Errorf("ok=%v want %v", ok, tc.wantOK)
			}
		})
	}
}

func TestListVisibleFlights_ExplicitShareSurvivesNonFriend(t *testing.T) {
	s := newStore(t)
	now := time.Now()

	creator := mkUser(t, s)
	nonFriend := mkUser(t, s)

	f, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "EX1", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", IsPublic: false,
	}, creator)
	if err != nil {
		t.Fatalf("CreateFlight: %v", err)
	}
	if err := s.AddShare(ctx, f.ID, nonFriend); err != nil {
		t.Fatalf("AddShare: %v", err)
	}

	got, err := s.ListVisibleFlights(ctx, nonFriend, false, true)
	if err != nil {
		t.Fatalf("ListVisibleFlights: %v", err)
	}
	found := false
	for _, x := range got {
		if x.ID == f.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("non-friend with explicit share should still see flight")
	}
}

func TestFlightsWithMissingCoords(t *testing.T) {
	s := newStore(t)
	uid := mkUser(t, s)
	now := time.Now()

	// (a) Fully filled: LHR / JFK (both in embedded airports table).
	a, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "AAA1", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)
	if err != nil {
		t.Fatalf("create a: %v", err)
	}

	// (b) Dest IATA unknown to the table → dest_lat / dest_lon NULL.
	b, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "BBB1", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "ZZZ",
	}, uid)
	if err != nil {
		t.Fatalf("create b: %v", err)
	}

	// (c) Both IATAs unknown → all four coord columns NULL.
	c, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "CCC1", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "QQQ", DestIATA: "ZZZ",
	}, uid)
	if err != nil {
		t.Fatalf("create c: %v", err)
	}

	got, err := s.FlightsWithMissingCoords(ctx)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	gotIDs := map[int64]bool{}
	for _, f := range got {
		gotIDs[f.ID] = true
	}
	if gotIDs[a.ID] {
		t.Errorf("fully-filled flight %d should NOT be returned", a.ID)
	}
	if !gotIDs[b.ID] {
		t.Errorf("dest-NULL flight %d should be returned", b.ID)
	}
	if !gotIDs[c.ID] {
		t.Errorf("both-NULL flight %d should be returned", c.ID)
	}
}

func TestVisibleUserIDs_FriendGated(t *testing.T) {
	s := newStore(t)
	now := time.Now()

	creator := mkUser(t, s)
	friend := mkUser(t, s)
	stranger := mkUser(t, s)
	pendingUser := mkUser(t, s)
	passenger := mkUser(t, s)
	sharedUser := mkUser(t, s)

	if _, err := s.RequestFriendship(ctx, creator, friend, ""); err != nil {
		t.Fatalf("req friend: %v", err)
	}
	if _, err := s.AcceptFriendship(ctx, friend, creator); err != nil {
		t.Fatalf("accept friend: %v", err)
	}
	if _, err := s.RequestFriendship(ctx, creator, pendingUser, ""); err != nil {
		t.Fatalf("req pending: %v", err)
	}

	// Private flight: only creator + explicit passenger + explicit share.
	priv, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "VU1", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", IsPublic: false,
	}, creator)
	if err != nil {
		t.Fatalf("CreateFlight private: %v", err)
	}
	if err := s.AddPassenger(ctx, priv.ID, passenger); err != nil {
		t.Fatalf("AddPassenger: %v", err)
	}
	if err := s.AddShare(ctx, priv.ID, sharedUser); err != nil {
		t.Fatalf("AddShare: %v", err)
	}

	uids, err := s.VisibleUserIDs(ctx, priv.ID)
	if err != nil {
		t.Fatalf("VisibleUserIDs: %v", err)
	}
	want := map[int64]bool{creator: true, passenger: true, sharedUser: true}
	got := map[int64]bool{}
	for _, u := range uids {
		got[u] = true
	}
	for u := range want {
		if !got[u] {
			t.Errorf("missing %d in viewers", u)
		}
	}
	if got[friend] || got[stranger] || got[pendingUser] {
		t.Errorf("non-passenger/non-share viewers leaked: friend=%v stranger=%v pending=%v",
			got[friend], got[stranger], got[pendingUser])
	}

	// Public flight: creator's accepted friends join; strangers and pending do not.
	pub, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "VU2", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", IsPublic: true,
	}, creator)
	if err != nil {
		t.Fatalf("CreateFlight public: %v", err)
	}
	uids, err = s.VisibleUserIDs(ctx, pub.ID)
	if err != nil {
		t.Fatalf("VisibleUserIDs public: %v", err)
	}
	got = map[int64]bool{}
	for _, u := range uids {
		got[u] = true
	}
	if !got[creator] || !got[friend] {
		t.Errorf("public flight should include creator and friend; got %v", got)
	}
	if got[stranger] || got[pendingUser] {
		t.Errorf("public flight should not include stranger/pending; got %v", got)
	}
}
