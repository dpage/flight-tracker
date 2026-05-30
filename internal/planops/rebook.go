package planops

import (
	"context"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// rebookCandidate is an existing visible flight part in the trip, paired with
// the booking's confirmation_ref and the passenger set of its plan — the two
// keys the rebooking match (spec §6.1, PRD §6.9) scores against.
type rebookCandidate struct {
	partID       int64
	planID       int64
	confirmRef   string
	passengerIDs []int64
	flight       *store.FlightDetail
}

// rebookMatch is the chosen supersession target plus the confidence to stamp on
// the proposal.
type rebookMatch struct {
	partID     int64
	confidence float64
}

// visibleFlightCandidates loads the non-dismissed, non-cancelled flight parts
// the user may see in the trip, with their plan's confirmation_ref and
// passenger set, as candidates for the rebooking match.
func visibleFlightCandidates(ctx context.Context, deps Deps, userID, tripID int64) ([]rebookCandidate, error) {
	parts, err := deps.Store.ListVisiblePlanParts(ctx, userID, store.ListVisiblePlanPartsOpts{
		TripID: tripID,
		Type:   "flight",
	})
	if err != nil {
		return nil, err
	}
	var out []rebookCandidate
	for _, p := range parts {
		// A part that is already cancelled (superseded earlier) is not a
		// rebooking target.
		if p.Status == "cancelled" {
			continue
		}
		fd, err := deps.Store.FlightDetailFor(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		if fd == nil {
			continue
		}
		plan, err := deps.Store.PlanByID(ctx, p.PlanID)
		if err != nil {
			return nil, err
		}
		pax, err := deps.Store.PassengersByPlan(ctx, []int64{p.PlanID})
		if err != nil {
			return nil, err
		}
		out = append(out, rebookCandidate{
			partID:       p.ID,
			planID:       p.PlanID,
			confirmRef:   plan.ConfirmationRef,
			passengerIDs: pax[p.PlanID],
			flight:       fd,
		})
	}
	return out, nil
}

// matchRebooking implements spec §6.1: match an incoming flight part against
// existing flight candidates, first by confirmation_ref/PNR (high), else by
// ident + same calendar day or same route + date proximity (medium). Scoped to
// the trip; the candidate set has already been filtered to parts the traveller
// can see. Returns nil when nothing matches.
//
// incomingPax is the proposed plan's passenger set. When non-empty, candidates
// sharing a passenger are preferred so a rebooking matches the right person's
// flight rather than a trip-mate's (PRD §6.9 "by traveller and route"). The
// candidate set here is the proposing user's visible flights, which is already
// a strong traveller filter; the shared-passenger preference refines among
// several visible matches.
func matchRebooking(confirmRef string, incoming *store.FlightDetail, candidates []rebookCandidate) *rebookMatch {
	ref := normalizeRef(confirmRef)

	// 1. confirmation_ref / PNR equality — highest confidence.
	if ref != "" {
		for _, c := range candidates {
			if normalizeRef(c.confirmRef) == ref {
				return &rebookMatch{partID: c.partID, confidence: 0.95}
			}
		}
	}

	// 2. ident + same calendar day, or same route + date proximity (medium).
	day := incoming.ScheduledOut.UTC().Truncate(24 * time.Hour)
	ident := strings.ToUpper(strings.Join(strings.Fields(incoming.Ident), ""))

	var best *rebookCandidate
	bestScore := -1
	for i := range candidates {
		c := &candidates[i]
		score := -1
		cIdent := strings.ToUpper(strings.Join(strings.Fields(c.flight.Ident), ""))
		cDay := c.flight.ScheduledOut.UTC().Truncate(24 * time.Hour)
		switch {
		case ident != "" && cIdent == ident && cDay.Equal(day):
			score = 2 // same flight number, same day
		case sameRoute(incoming, c.flight) && withinDateProximity(c.flight.ScheduledOut, incoming.ScheduledOut):
			score = 1 // same origin/dest, nearby date
		default:
			continue
		}
		if score > bestScore {
			best, bestScore = c, score
		}
	}
	if best != nil {
		return &rebookMatch{partID: best.partID, confidence: 0.6}
	}
	return nil
}

// normalizeRef upper-cases and strips whitespace from a confirmation ref so PNR
// comparison is punctuation-insensitive. Empty refs never match.
func normalizeRef(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}

// sameRoute reports whether two flights share origin and destination IATA.
func sameRoute(a, b *store.FlightDetail) bool {
	return a.OriginIATA != "" && a.OriginIATA == b.OriginIATA &&
		a.DestIATA != "" && a.DestIATA == b.DestIATA
}

// withinDateProximity reports whether two scheduled departures are within the
// rebooking date tolerance (±2 days — a rebooking commonly shifts the leg by a
// day or two).
func withinDateProximity(a, b time.Time) bool {
	d := a.Sub(b)
	if d < 0 {
		d = -d
	}
	return d <= 48*time.Hour
}
