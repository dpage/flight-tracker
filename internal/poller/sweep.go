package poller

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

// sweepInterval is the cadence of the periodic NULL-coord sweep. The
// embedded airports table only changes between deploys, so a longer
// interval would also be defensible; 4h is a compromise that also lets
// the resolver step inside the sweep eventually self-heal far-future
// flights once their schedule is published, without burning more than
// ~6 API calls per stuck row per day.
const sweepInterval = 4 * time.Hour

// Sweep finds every flight with at least one NULL coord column and tries
// to fill the missing legs — first from the embedded airports table
// (free, in-memory), then from the configured Resolver if anything is
// still missing and the row's last_resolved_at throttle allows it. Rows
// whose coords actually changed get republished over SSE so connected
// clients update without a reload.
//
// Per-row failures are logged and isolated; one bad row never aborts
// the sweep.
func (p *Poller) Sweep(ctx context.Context) {
	flights, err := p.Store.FlightsWithMissingCoords(ctx)
	if err != nil {
		slog.Error("sweep: list flights with missing coords", "err", err)
		return
	}
	if len(flights) == 0 {
		return
	}
	slog.Info("sweep: starting", "candidate_rows", len(flights))
	now := time.Now()
	for _, f := range flights {
		if ctx.Err() != nil {
			return
		}
		p.sweepOne(ctx, f, now)
	}
}

// sweepOne runs the table + (later) resolver passes for a single flight
// row. Extracted so a failure on one row doesn't unwind the whole loop.
// The now parameter is unused in this commit; it's reserved for the
// upcoming resolver throttle check that compares against f.LastResolvedAt.
func (p *Poller) sweepOne(ctx context.Context, f *store.Flight, now time.Time) {
	var update store.BackfillPayload
	changed := false

	// Table fast path.
	if f.OriginLat == nil && f.OriginIATA != "" {
		if lat, lon, ok := airports.Lookup(f.OriginIATA); ok {
			update.OriginIATA, update.OriginLat, update.OriginLon = f.OriginIATA, lat, lon
			changed = true
		}
	}
	if f.DestLat == nil && f.DestIATA != "" {
		if lat, lon, ok := airports.Lookup(f.DestIATA); ok {
			update.DestIATA, update.DestLat, update.DestLon = f.DestIATA, lat, lon
			changed = true
		}
	}

	if !changed {
		return
	}
	if err := p.Store.BackfillFlight(ctx, f.ID, update); err != nil {
		slog.Error("sweep: backfill", "id", f.ID, "err", err)
		return
	}
	p.publishFlightChange(ctx, f.ID)
}

// publishFlightChange rebuilds the full FlightDTO for a row that just
// had its coords updated and publishes it via the hub. Mirrors the
// publish boilerplate in (*Poller).refresh; we duplicate rather than
// extract a shared helper because refresh does other work (tracking,
// status refresh) on the same call.
func (p *Poller) publishFlightChange(ctx context.Context, id int64) {
	fresh, err := p.Store.FlightByID(ctx, id)
	if err != nil {
		slog.Error("sweep: refetch", "id", id, "err", err)
		return
	}
	pmap, _ := p.Store.PassengersByFlight(ctx, []int64{id})
	smap, _ := p.Store.SharedUserIDsByFlight(ctx, []int64{id})
	latest, _ := p.Store.LatestPositions(ctx, []int64{id})
	tracks, _ := p.Store.RecentTracks(ctx, []int64{id}, 200)
	dto := api.ToFlightDTO(fresh, pmap[id], smap[id], latest[id], tracks[id])
	payload, err := json.Marshal(dto)
	if err != nil {
		slog.Error("sweep: marshal dto", "id", id, "err", err)
		return
	}
	var visible []int64
	if !fresh.IsPublic {
		v, err := p.Store.VisibleUserIDs(ctx, fresh.ID)
		if err != nil {
			slog.Warn("sweep: visibility lookup failed", "id", fresh.ID, "err", err)
		}
		visible = v
	}
	p.Hub.Publish(sse.Event{Type: "flight.updated", Data: payload, VisibleTo: visible})
}
