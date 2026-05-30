package handlers

import (
	"net/http"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/store"
)

// Wave 2A: the ingest pipeline. POST /api/trips/{id}/ingest proposes plans from
// pasted text / uploaded documents (the LLM seam, with a rebooking match
// against the trip's existing flights); POST /api/trips/{id}/ingest/confirm
// commits the confirmed/edited proposals. Both are editor-gated on the trip.

// ingestReq is the propose request body (matches the FE IngestInput).
type ingestReq struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

// ingestConfirmReq is the confirm request body (matches the FE
// IngestConfirmInput: {plans: ConfirmPlanInput[]}).
type ingestConfirmReq struct {
	Plans []ingestConfirmPlanReq `json:"plans"`
}

type ingestConfirmPlanReq struct {
	Type             string             `json:"type"`
	Title            string             `json:"title"`
	ConfirmationRef  string             `json:"confirmation_ref"`
	Notes            string             `json:"notes"`
	Source           string             `json:"source"`
	PassengerIDs     []int64            `json:"passenger_ids"`
	Visibility       *planVisibilityReq `json:"visibility"`
	Parts            []planPartReq      `json:"parts"`
	SupersedesPartID *int64             `json:"supersedes_part_id"`
}

// ingestTrip proposes plans from pasted text against the target trip. Nothing
// is written — the response is a set of proposals for the user to confirm.
func (a *API) ingestTrip(w http.ResponseWriter, r *http.Request) {
	tripID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripEdit(r.Context(), tripID, me, w); err != nil {
		return
	}
	if a.Extractor == nil {
		writeError(w, http.StatusServiceUnavailable, "ingest is not configured (no LLM provider)")
		return
	}
	var in ingestReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	deps := planops.Deps{Store: a.Store, Extractor: a.Extractor, Resolver: a.Resolver}
	proposals, err := planops.Propose(r.Context(), deps, me.ID, tripID, in.Text, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := api.IngestResultDTO{Proposals: make([]api.ProposedPlanDTO, 0, len(proposals))}
	for _, p := range proposals {
		out.Proposals = append(out.Proposals, toProposedPlanDTO(p))
	}
	writeJSON(w, http.StatusOK, out)
}

// ingestTripConfirm commits the confirmed/edited proposals against the trip,
// applying any rebooking supersessions (the new part links supersedes_id; the
// old part is stamped status='cancelled'). Returns the created plans.
func (a *API) ingestTripConfirm(w http.ResponseWriter, r *http.Request) {
	tripID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripEdit(r.Context(), tripID, me, w); err != nil {
		return
	}
	var in ingestConfirmReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	plans := make([]planops.ConfirmPlanInput, 0, len(in.Plans))
	for _, p := range in.Plans {
		if !validPlanTypes[p.Type] {
			writeError(w, http.StatusBadRequest, "invalid plan type")
			return
		}
		plans = append(plans, toConfirmPlanInput(p))
	}
	deps := planops.Deps{Store: a.Store, Extractor: a.Extractor, Resolver: a.Resolver}
	created, err := planops.Commit(r.Context(), deps, tripID, me.ID, plans)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out := make([]api.PlanDTO, 0, len(created))
	for _, pl := range created {
		dto, err := a.planDTO(r.Context(), pl.ID)
		if err != nil {
			handleStoreErr(w, err)
			return
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

// toConfirmPlanInput maps the request shape onto the planops commit input,
// reusing toCreatePartPayload (handlers_plans.go) to build per-type satellites.
func toConfirmPlanInput(p ingestConfirmPlanReq) planops.ConfirmPlanInput {
	out := planops.ConfirmPlanInput{
		Type:             p.Type,
		Title:            p.Title,
		ConfirmationRef:  p.ConfirmationRef,
		Notes:            p.Notes,
		Source:           p.Source,
		PassengerIDs:     p.PassengerIDs,
		SupersedesPartID: p.SupersedesPartID,
	}
	if p.Visibility != nil {
		out.Visibility = &planops.ConfirmVisibility{Mode: p.Visibility.Mode, UserIDs: p.Visibility.UserIDs}
	}
	for _, part := range p.Parts {
		cp := toCreatePartPayload(p.Type, part)
		out.Parts = append(out.Parts, planops.ConfirmPartInput{
			Type:       p.Type,
			Seq:        cp.Seq,
			StartsAt:   cp.StartsAt,
			EndsAt:     cp.EndsAt,
			StartTZ:    cp.StartTZ,
			EndTZ:      cp.EndTZ,
			StartLabel: cp.StartLabel,
			StartLat:   cp.StartLat,
			StartLon:   cp.StartLon,
			EndLabel:   cp.EndLabel,
			EndLat:     cp.EndLat,
			EndLon:     cp.EndLon,
			Status:     cp.Status,
			Flight:     cp.Flight,
			Hotel:      cp.Hotel,
			Train:      cp.Train,
			Ground:     cp.Ground,
			Dining:     cp.Dining,
			Excursion:  cp.Excursion,
		})
	}
	return out
}

// toProposedPlanDTO renders a planops.ProposedPlan as the FE ProposedPlan
// shape, projecting each part through ToPlanPartDTO (ids are 0 — these are not
// yet persisted).
func toProposedPlanDTO(p planops.ProposedPlan) api.ProposedPlanDTO {
	dto := api.ProposedPlanDTO{
		Type:             p.Type,
		Title:            p.Title,
		ConfirmationRef:  p.ConfirmationRef,
		Notes:            p.Notes,
		Confidence:       p.Confidence,
		Parts:            make([]api.PlanPartDTO, 0, len(p.Parts)),
		SupersedesPartID: p.SupersedesPartID,
	}
	for i, part := range p.Parts {
		sp := &store.PlanPart{
			Type:       part.Type,
			Seq:        i,
			StartsAt:   part.StartsAt,
			EndsAt:     part.EndsAt,
			StartTZ:    part.StartTZ,
			EndTZ:      part.EndTZ,
			StartLabel: part.StartLabel,
			EndLabel:   part.EndLabel,
			Status:     part.Status,
		}
		dto.Parts = append(dto.Parts, api.ToPlanPartDTO(sp,
			part.Flight, part.Hotel, part.Train, part.Ground, part.Dining, part.Excursion, nil, nil))
	}
	return dto
}
