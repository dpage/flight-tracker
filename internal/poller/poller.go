// Package poller drives the periodic refresh of active flights via a
// Tracker, persists positions, refreshes the time-derived status, and
// broadcasts updates over the SSE hub. It runs as a goroutine in the same
// process as the HTTP server.
package poller

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/dpage/flight-tracker/internal/aeroapi"
	"github.com/dpage/flight-tracker/internal/api"
	"github.com/dpage/flight-tracker/internal/sse"
	"github.com/dpage/flight-tracker/internal/store"
)

type Poller struct {
	Store    *store.Store
	Tracker  aeroapi.Tracker
	Hub      *sse.Hub
	Interval time.Duration
}

func New(s *store.Store, t aeroapi.Tracker, hub *sse.Hub, interval time.Duration) *Poller {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Poller{Store: s, Tracker: t, Hub: hub, Interval: interval}
}

func (p *Poller) Run(ctx context.Context) {
	slog.Info("poller started", "interval", p.Interval)
	defer slog.Info("poller stopped")

	// Tick immediately on startup so a fresh server doesn't look stale.
	p.tick(ctx)

	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tick(ctx)
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
	latest, _ := p.Store.LatestPositions(ctx, []int64{f.ID})
	dto := api.ToFlightDTO(fresh, pmap[f.ID], latest[f.ID])
	payload, err := json.Marshal(dto)
	if err != nil {
		slog.Error("poller: marshal dto", "err", err)
		return
	}
	p.Hub.Publish(sse.Event{Type: "flight.updated", Data: payload})
}
