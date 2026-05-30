package planops

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// ConfirmPartInput is one part of a confirmed/edited proposal sent back to
// commit. It mirrors the FE PlanPartInput shape.
type ConfirmPartInput struct {
	Type       string
	Seq        int
	StartsAt   time.Time
	EndsAt     *time.Time
	StartTZ    string
	EndTZ      string
	StartLabel string
	StartLat   *float64
	StartLon   *float64
	EndLabel   string
	EndLat     *float64
	EndLon     *float64
	Status     string

	Flight    *store.FlightDetail
	Hotel     *store.HotelDetail
	Train     *store.TrainDetail
	Ground    *store.GroundDetail
	Dining    *store.DiningDetail
	Excursion *store.ExcursionDetail
}

// ConfirmPlanInput is one confirmed/edited proposal. It mirrors the FE
// ConfirmPlanInput contract: a plan with its parts, passengers, visibility, and
// an optional rebooking supersession target.
type ConfirmPlanInput struct {
	Type            string
	Title           string
	ConfirmationRef string
	Notes           string
	Source          string
	PassengerIDs    []int64
	Visibility      *ConfirmVisibility

	Parts []ConfirmPartInput

	// SupersedesPartID, when set, is the existing part this plan's (single,
	// flight) part replaces. On commit the new part links to it via
	// supersedes_id and the old part is stamped status='cancelled'.
	SupersedesPartID *int64
}

// ConfirmVisibility carries a per-plan privacy override on confirm.
type ConfirmVisibility struct {
	Mode    string // ""|everyone → default; hidden_from|only_visible_to
	UserIDs []int64
}

// Commit writes the confirmed plans, their parts, and per-type satellites via
// the store, then applies any rebooking supersessions: the new part's
// supersedes_id points at the matched part and the OLD part is stamped
// status='cancelled' (the signal the front end greys on — spec §6.1). Returns
// the created plans.
func Commit(ctx context.Context, deps Deps, tripID, createdBy int64, plans []ConfirmPlanInput) ([]*store.Plan, error) {
	if deps.Store == nil {
		return nil, errors.New("planops.Commit: nil Store")
	}
	out := make([]*store.Plan, 0, len(plans))
	for _, in := range plans {
		source := in.Source
		if source == "" {
			source = "paste"
		}
		parts := make([]store.CreatePlanPartPayload, 0, len(in.Parts))
		for i, p := range in.Parts {
			seq := p.Seq
			if seq == 0 {
				seq = i
			}
			cp := store.CreatePlanPartPayload{
				Seq:        seq,
				StartsAt:   p.StartsAt,
				EndsAt:     p.EndsAt,
				StartTZ:    p.StartTZ,
				EndTZ:      p.EndTZ,
				StartLabel: p.StartLabel,
				StartLat:   p.StartLat,
				StartLon:   p.StartLon,
				EndLabel:   p.EndLabel,
				EndLat:     p.EndLat,
				EndLon:     p.EndLon,
				Status:     p.Status,
				Flight:     p.Flight,
				Hotel:      p.Hotel,
				Train:      p.Train,
				Ground:     p.Ground,
				Dining:     p.Dining,
				Excursion:  p.Excursion,
			}
			// Link the new part to the part it supersedes (rebooking). The
			// supersession is a plan-level field in the contract; it applies to
			// the plan's single flight part.
			if in.SupersedesPartID != nil && len(in.Parts) == 1 {
				cp.SupersedesID = in.SupersedesPartID
			}
			parts = append(parts, cp)
		}
		plan, err := deps.Store.CreatePlan(ctx, store.CreatePlanPayload{
			TripID:          tripID,
			Type:            in.Type,
			Title:           in.Title,
			ConfirmationRef: in.ConfirmationRef,
			Notes:           in.Notes,
			Source:          source,
			Parts:           parts,
		}, createdBy)
		if err != nil {
			return nil, fmt.Errorf("create plan %q: %w", in.Title, err)
		}
		for _, uid := range in.PassengerIDs {
			if err := deps.Store.AddPlanPassenger(ctx, plan.ID, uid); err != nil {
				return nil, fmt.Errorf("add passenger: %w", err)
			}
		}
		if in.Visibility != nil {
			mode := in.Visibility.Mode
			if mode == "everyone" {
				mode = ""
			}
			if err := deps.Store.SetPlanVisibility(ctx, plan.ID, mode, in.Visibility.UserIDs); err != nil {
				return nil, fmt.Errorf("set visibility: %w", err)
			}
		}
		// Apply the supersession: cancel the old part so the FE greys it.
		if in.SupersedesPartID != nil {
			if err := cancelSuperseded(ctx, deps, *in.SupersedesPartID); err != nil {
				return nil, fmt.Errorf("cancel superseded part %d: %w", *in.SupersedesPartID, err)
			}
		}
		out = append(out, plan)
	}
	return out, nil
}

// cancelSuperseded stamps the old part status='cancelled'. It stays on the
// timeline (greyed) until the user tidies it away via the dismiss endpoint.
func cancelSuperseded(ctx context.Context, deps Deps, partID int64) error {
	cancelled := "cancelled"
	_, err := deps.Store.UpdatePlanPart(ctx, partID, store.UpdatePlanPartPayload{Status: &cancelled})
	return err
}
