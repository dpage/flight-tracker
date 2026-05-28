// Package poller drives the periodic refresh of active flights via a
// Tracker, persists positions, refreshes the time-derived status, and
// broadcasts updates over the SSE hub. It runs as a goroutine in the same
// process as the HTTP server.
package poller

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

type Poller struct {
	Store    *store.Store
	Tracker  providers.Tracker
	Resolver providers.Resolver // optional; when set, backfills missing metadata
	Hub      *sse.Hub
	Interval time.Duration
}

func New(s *store.Store, t providers.Tracker, hub *sse.Hub, interval time.Duration) *Poller {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Poller{Store: s, Tracker: t, Hub: hub, Interval: interval}
}

func (p *Poller) Run(ctx context.Context) {
	slog.Info("poller started", "interval", p.Interval)
	defer slog.Info("poller stopped")

	// Startup sweep: fill any NULL coord columns the latest deploy's
	// airports table can now satisfy, before the main poll loop starts.
	p.Sweep(ctx)

	// Tick immediately on startup so a fresh server doesn't look stale.
	p.tick(ctx)

	mainT := time.NewTicker(p.Interval)
	defer mainT.Stop()
	sweepT := time.NewTicker(sweepInterval)
	defer sweepT.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-mainT.C:
			p.tick(ctx)
		case <-sweepT.C:
			p.Sweep(ctx)
		}
	}
}

// minPollAge returns how long to wait between polls for a given flight.
// Enroute flights are polled at the base interval; all other active statuses
// (Scheduled, etc.) are polled at 5× the base interval since they change
// infrequently before departure.
func (p *Poller) minPollAge(status string) time.Duration {
	if status == "Enroute" {
		return p.Interval
	}
	return p.Interval * 5
}

func (p *Poller) tick(ctx context.Context) {
	now := time.Now()
	flights, err := p.Store.ActiveFlights(ctx, now)
	if err != nil {
		slog.Error("poller: list active flights", "err", err)
		return
	}
	for _, f := range flights {
		if ctx.Err() != nil {
			return
		}
		if f.LastPolledAt != nil && now.Sub(*f.LastPolledAt) < p.minPollAge(f.Status) {
			continue
		}
		p.refresh(ctx, f, now)
	}
}

func (p *Poller) refresh(ctx context.Context, f *store.Flight, now time.Time) {
	// Resolver work, two overlapping triggers:
	//   - needsBackfill: airports / airframe are blank (manual add, never
	//     resolved), so we want to fill them in once.
	//   - needsLateRefresh: the flight is close to departure (or enroute)
	//     and last_resolved_at is stale. AeroDataBox only firms up the
	//     operating airframe within ~24h of departure, and airlines swap
	//     metal on the day; without this, we'd keep polling OpenSky for
	//     the airframe that was scheduled at booking time, not the one
	//     actually in the air.
	// last_resolved_at is bumped on every resolve attempt — success,
	// not-found, or transport error — so a doomed lookup doesn't burn
	// quota on every tick.
	if p.Resolver != nil && (needsBackfill(f) || needsLateRefresh(f, now)) {
		if fresh, err := p.resolveAndUpdate(ctx, f, now); err == nil && fresh != nil {
			f = fresh
		}
	}

	pos, err := p.Tracker.Track(ctx, f, now)
	if err != nil {
		slog.Warn("poller: track failed", "flight", f.Ident, "id", f.ID, "err", err)
	}
	if pos != nil {
		if err := p.Store.InsertPosition(ctx, *pos); err != nil {
			slog.Error("poller: insert position", "id", f.ID, "err", err)
		}
	}
	// Always refresh the status from the schedule; preserves Cancelled /
	// Diverted, otherwise derives Scheduled / Enroute / Arrived from times.
	if err := p.Store.RefreshFlightStatus(ctx, f.ID); err != nil {
		slog.Error("poller: refresh status", "id", f.ID, "err", err)
	}

	fresh, err := p.Store.FlightByID(ctx, f.ID)
	if err != nil {
		slog.Error("poller: refetch flight", "id", f.ID, "err", err)
		return
	}
	pmap, _ := p.Store.PassengersByFlight(ctx, []int64{f.ID})
	smap, _ := p.Store.SharedUserIDsByFlight(ctx, []int64{f.ID})
	latest, _ := p.Store.LatestPositions(ctx, []int64{f.ID})
	tracks, _ := p.Store.RecentTracks(ctx, []int64{f.ID}, 200)
	dto := api.ToFlightDTO(fresh, pmap[f.ID], smap[f.ID], latest[f.ID], tracks[f.ID])
	payload, err := json.Marshal(dto)
	if err != nil {
		slog.Error("poller: marshal dto", "err", err)
		return
	}
	// Scope the broadcast to the flight's visibility set. VisibleUserIDs
	// already includes the creator's accepted friends when is_public is
	// true, so this always returns the correct set regardless of is_public.
	visible, err := p.Store.VisibleUserIDs(ctx, fresh.ID)
	if err != nil {
		slog.Warn("poller: visibility lookup failed", "id", fresh.ID, "err", err)
	}
	p.Hub.Publish(sse.Event{Type: "flight.updated", Data: payload, VisibleTo: visible})
}

// needsBackfill is true when the resolver could meaningfully fill in at
// least one of the metadata fields that the rest of the system needs.
func needsBackfill(f *store.Flight) bool {
	return f.OriginIATA == "" || f.DestIATA == "" || f.ICAO24 == nil
}

// lateRefreshWindow is how close to scheduled departure we start re-asking
// the resolver about the operating airframe. AeroDataBox doesn't reliably
// publish modeS / callSign until ~24h out, but airlines also swap metal
// closer in than that, so the cheap thing is to keep poking from here.
const lateRefreshWindow = 12 * time.Hour

// lateRefreshInterval throttles how often we re-resolve while inside the
// window — covers the "every tick for an enroute flight" case. AeroDataBox
// BASIC tier allows a few hundred calls/day; one call per active flight
// per ~4h is well under that.
const lateRefreshInterval = 4 * time.Hour

// needsLateRefresh is true when the flight is in (or close to) its active
// window and we haven't asked the resolver recently. It complements
// needsBackfill: backfill cares about *which fields are empty*, this
// cares about *how stale the data is*.
func needsLateRefresh(f *store.Flight, now time.Time) bool {
	if now.Before(f.ScheduledOut.Add(-lateRefreshWindow)) {
		return false
	}
	if f.Status == "Arrived" || f.Status == "Cancelled" || f.Status == "Diverted" {
		return false
	}
	if f.LastResolvedAt == nil {
		return true
	}
	return now.Sub(*f.LastResolvedAt) >= lateRefreshInterval
}

// resolveAndUpdate calls the Resolver and persists the result through both
// the empty-fill path (BackfillFlight, which protects user-typed values)
// and the day-of overwrite path (RefreshFlightAirframe, which catches
// airframe swaps and bumps last_resolved_at). On error or not-found we
// still bump last_resolved_at via an empty Refresh so the next tick
// throttles instead of retrying immediately.
func (p *Poller) resolveAndUpdate(ctx context.Context, f *store.Flight, now time.Time) (*store.Flight, error) {
	rf, err := p.Resolver.Resolve(ctx, f.Ident, f.ScheduledOut)
	if err != nil {
		if !errors.Is(err, providers.ErrFlightNotFound) {
			slog.Warn("poller: resolve failed",
				"ident", f.Ident, "id", f.ID, "err", err)
		}
		if touchErr := p.Store.RefreshFlightAirframe(ctx, f.ID, "", ""); touchErr != nil {
			slog.Error("poller: stamp last_resolved_at failed", "id", f.ID, "err", touchErr)
		}
		return nil, err
	}
	if err := p.Store.BackfillFlight(ctx, f.ID, store.BackfillPayload{
		OriginIATA: rf.OriginIATA, OriginLat: rf.OriginLat, OriginLon: rf.OriginLon,
		DestIATA: rf.DestIATA, DestLat: rf.DestLat, DestLon: rf.DestLon,
		ICAO24: rf.ICAO24, Callsign: rf.Callsign,
		Notes: rf.Notes,
	}); err != nil {
		slog.Error("poller: backfill write failed", "id", f.ID, "err", err)
		return nil, err
	}
	if err := p.Store.RefreshFlightAirframe(ctx, f.ID, rf.ICAO24, rf.Callsign); err != nil {
		slog.Error("poller: refresh airframe failed", "id", f.ID, "err", err)
		return nil, err
	}
	slog.Info("poller: resolved",
		"ident", f.Ident, "id", f.ID,
		"origin", rf.OriginIATA, "dest", rf.DestIATA,
		"icao24", rf.ICAO24, "callsign", rf.Callsign)
	return p.Store.FlightByID(ctx, f.ID)
}
