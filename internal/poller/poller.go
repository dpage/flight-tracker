// Package poller drives the periodic refresh of active flights via the
// AeroAPI client, persists the results, and broadcasts updates over the SSE
// hub. It runs as a goroutine in the same process as the HTTP server.
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
	Client   aeroapi.Client
	Hub      *sse.Hub
	Interval time.Duration
}

func New(s *store.Store, c aeroapi.Client, hub *sse.Hub, interval time.Duration) *Poller {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Poller{Store: s, Client: c, Hub: hub, Interval: interval}
}

// Run polls until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	slog.Info("poller started", "interval", p.Interval)
	defer slog.Info("poller stopped")

	// First tick immediately so a fresh server doesn't appear stale.
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
		p.refresh(ctx, f, now)
	}
}

func (p *Poller) refresh(ctx context.Context, f *store.Flight, now time.Time) {
	up, err := p.Client.Refresh(ctx, f, now)
	if err != nil {
		slog.Warn("poller: refresh failed", "flight", f.Ident, "id", f.ID, "err", err)
		return
	}
	if err := p.Store.UpdateFlightTracking(ctx, f.ID, up.Tracking); err != nil {
		slog.Error("poller: update tracking", "id", f.ID, "err", err)
		return
	}
	if up.Position != nil {
		if err := p.Store.InsertPosition(ctx, *up.Position); err != nil {
			slog.Error("poller: insert position", "id", f.ID, "err", err)
		}
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
