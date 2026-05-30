package poller

import (
	"context"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/airports"
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
	flights, err := p.Store.FlightPartsWithMissingCoords(ctx)
	if err != nil {
		slog.Error("sweep: list flight parts with missing coords", "err", err)
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

// sweepOne runs the table + resolver passes for a single flight row.
// Extracted so a failure on one row doesn't unwind the whole loop.
// The now parameter feeds the resolver throttle: a recent
// last_resolved_at suppresses repeat API calls for rows that the
// resolver couldn't satisfy last time.
func (p *Poller) sweepOne(ctx context.Context, f *store.Flight, now time.Time) {
	var update store.BackfillPayload
	changed := false
	var originNeedsResolver, destNeedsResolver bool

	// Table fast path.
	if f.OriginLat == nil && f.OriginIATA != "" {
		if lat, lon, ok := airports.Lookup(f.OriginIATA); ok {
			update.OriginIATA, update.OriginLat, update.OriginLon = f.OriginIATA, lat, lon
			changed = true
		} else {
			originNeedsResolver = true
		}
	}
	if f.DestLat == nil && f.DestIATA != "" {
		if lat, lon, ok := airports.Lookup(f.DestIATA); ok {
			update.DestIATA, update.DestLat, update.DestLon = f.DestIATA, lat, lon
			changed = true
		} else {
			destNeedsResolver = true
		}
	}

	// Resolver slow path — only when something the table can't satisfy
	// remains, a resolver is configured, and the throttle allows it.
	if (originNeedsResolver || destNeedsResolver) && p.Resolver != nil && throttleAllowed(f, now) {
		rf, rerr := p.Resolver.Resolve(ctx, f.Ident, f.ScheduledOut)
		if rerr == nil {
			// Merge only the legs the table couldn't fill. The
			// table-derived coord on a satisfied leg must NOT be
			// clobbered — BackfillFlight's "only fill empty columns"
			// rule is enforced at the DB layer, but it also short-
			// circuits the write entirely if BOTH lat and lon for a
			// leg are zero in the payload. If the resolver returns
			// zero coords for a table-known leg we'd lose the table
			// value entirely.
			if originNeedsResolver {
				update.OriginIATA = rf.OriginIATA
				update.OriginLat, update.OriginLon = rf.OriginLat, rf.OriginLon
			}
			if destNeedsResolver {
				update.DestIATA = rf.DestIATA
				update.DestLat, update.DestLon = rf.DestLat, rf.DestLon
			}
			update.ICAO24, update.Callsign = rf.ICAO24, rf.Callsign
			update.Notes = rf.Notes
			changed = true
		} else {
			slog.Warn("sweep: resolve failed", "ident", f.Ident, "id", f.ID, "err", rerr)
		}
		// Always bump last_resolved_at so unreachable flights don't burn
		// API quota on every sweep tick. On error we still want the
		// throttle — empty strings here mean "don't overwrite airframe".
		icao24, callsign := "", ""
		if rerr == nil {
			icao24, callsign = rf.ICAO24, rf.Callsign
		}
		if terr := p.Store.RefreshFlightPartAirframe(ctx, f.ID, icao24, callsign); terr != nil {
			slog.Error("sweep: bump last_resolved_at", "id", f.ID, "err", terr)
		}
	}

	if !changed {
		return
	}
	if err := p.Store.BackfillFlightPart(ctx, f.ID, update); err != nil {
		slog.Error("sweep: backfill", "id", f.ID, "err", err)
		return
	}
	p.publishPartChange(ctx, f.ID)
}

// throttleAllowed reports whether enough time has passed since the last
// resolver attempt for this flight to merit another one. nil means the
// row has never been resolved, so the answer is yes.
func throttleAllowed(f *store.Flight, now time.Time) bool {
	if f.LastResolvedAt == nil {
		return true
	}
	return now.Sub(*f.LastResolvedAt) >= sweepInterval
}
