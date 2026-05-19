package store

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dpage/flight-tracker/internal/testsupport"
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

func TestPositions(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	f := mkFlight(t, s, "POS1", now, now.Add(time.Hour))

	if p, err := s.LatestRealPosition(ctx, f.ID); err != nil || p != nil {
		t.Fatalf("no positions yet → nil: %v %v", p, err)
	}
	if m, _ := s.LatestPositions(ctx, nil); len(m) != 0 {
		t.Error("empty ids → empty map")
	}
	if m, _ := s.RecentTracks(ctx, nil, 0); len(m) != 0 {
		t.Error("empty ids → empty map")
	}

	t0 := now.Add(-30 * time.Minute)
	hdr := int16(90)
	for i := 0; i < 3; i++ {
		err := s.InsertPosition(ctx, Position{
			FlightID: f.ID, Ts: t0.Add(time.Duration(i) * time.Minute),
			Lat: float64(i), Lon: float64(-i), HeadingDeg: &hdr, IsEstimated: i == 2,
		})
		if err != nil {
			t.Fatalf("InsertPosition: %v", err)
		}
	}

	real, err := s.LatestRealPosition(ctx, f.ID)
	if err != nil || real == nil || real.IsEstimated {
		t.Fatalf("LatestRealPosition should skip estimated: %v %v", real, err)
	}
	if real.Lat != 1 {
		t.Errorf("expected latest real (i=1) lat=1, got %v", real.Lat)
	}

	latest, _ := s.LatestPositions(ctx, []int64{f.ID})
	if latest[f.ID] == nil || latest[f.ID].Lat != 2 {
		t.Errorf("LatestPositions should pick newest (i=2): %+v", latest[f.ID])
	}

	tracks, _ := s.RecentTracks(ctx, []int64{f.ID}, 0) // 0 → default limit
	if len(tracks[f.ID]) != 3 {
		t.Errorf("RecentTracks count = %d, want 3", len(tracks[f.ID]))
	}
	// Oldest-first within the track.
	if tracks[f.ID][0].Lat != 0 || tracks[f.ID][2].Lat != 2 {
		t.Errorf("RecentTracks order wrong: %+v", tracks[f.ID])
	}

	pf, _ := s.PositionsForFlight(ctx, f.ID, 0) // 0 → default limit
	if len(pf) != 3 || pf[0].Lat != 2 {
		t.Errorf("PositionsForFlight newest-first wrong: %+v", pf)
	}
	pf2, _ := s.PositionsForFlight(ctx, f.ID, 1)
	if len(pf2) != 1 {
		t.Errorf("PositionsForFlight limit not applied: %d", len(pf2))
	}
}
