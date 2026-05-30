package handlers

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/planops"
)

// fakeIngestExtractor returns canned plans for the ingest endpoint tests.
type fakeIngestExtractor struct {
	plans []planops.ExtractedPlan
}

func (f *fakeIngestExtractor) ExtractPlans(_ context.Context, _ string, _ []planops.Document) ([]planops.ExtractedPlan, error) {
	return f.plans, nil
}

// TestIngestPropose_NotConfigured: with no extractor wired, the endpoint 503s.
func TestIngestPropose_NotConfigured(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest", tid), map[string]any{"text": "x"}, owner)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("propose w/o extractor = %d, want 503: %s", w.Code, w.Body.String())
	}
}

// TestIngestProposeAndConfirm exercises propose → confirm for a hotel plan and
// asserts the plan is created against the trip with its satellite.
func TestIngestProposeAndConfirm(t *testing.T) {
	e := setup(t, nil, nil)
	e.api.Extractor = &fakeIngestExtractor{plans: []planops.ExtractedPlan{{
		Type: "hotel", Title: "Hotel Plaza", ConfirmationRef: "H1",
		Parts: []planops.ExtractedPart{{
			Type: "hotel", Confidence: "high",
			StartDate: "2026-06-01", EndDate: "2026-06-05",
			HotelName: "Hotel Plaza", Address: "1 Main St",
		}},
	}}}
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")

	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest", tid), map[string]any{"text": "stay", "source": "paste"}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("propose = %d: %s", w.Code, w.Body.String())
	}
	res := decodeBody[api.IngestResultDTO](t, w)
	if len(res.Proposals) != 1 || res.Proposals[0].Type != "hotel" {
		t.Fatalf("proposals = %+v", res.Proposals)
	}
	if res.Proposals[0].Confidence < 0.9 {
		t.Errorf("confidence = %v, want high", res.Proposals[0].Confidence)
	}
	if len(res.Proposals[0].Parts) != 1 || res.Proposals[0].Parts[0].Hotel == nil {
		t.Fatalf("proposed hotel part missing: %+v", res.Proposals[0].Parts)
	}

	// Confirm it.
	checkin := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	checkout := time.Date(2026, 6, 5, 11, 0, 0, 0, time.UTC)
	confirm := map[string]any{
		"plans": []map[string]any{{
			"type": "hotel", "title": "Hotel Plaza", "confirmation_ref": "H1", "source": "paste",
			"parts": []map[string]any{{
				"type": "hotel", "starts_at": checkin, "ends_at": checkout,
				"start_label": "Hotel Plaza",
				"hotel":       map[string]any{"property_name": "Hotel Plaza", "address": "1 Main St"},
			}},
		}},
	}
	w = e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest/confirm", tid), confirm, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("confirm = %d: %s", w.Code, w.Body.String())
	}
	plans := decodeBody[[]api.PlanDTO](t, w)
	if len(plans) != 1 || plans[0].Type != "hotel" || plans[0].Source != "paste" {
		t.Fatalf("created plans = %+v", plans)
	}
	if len(plans[0].Parts) != 1 || plans[0].Parts[0].Hotel == nil {
		t.Fatalf("created hotel part missing: %+v", plans[0].Parts)
	}
	if plans[0].Parts[0].Hotel.PropertyName != "Hotel Plaza" {
		t.Errorf("property = %q", plans[0].Parts[0].Hotel.PropertyName)
	}
}

// TestIngestConfirm_AppliesSupersession confirms a rebooking proposal and
// asserts the old part is cancelled and the new part links to it.
func TestIngestConfirm_AppliesSupersession(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)

	// Seed an existing flight plan.
	create := map[string]any{
		"type": "flight", "title": "BA286", "confirmation_ref": "PNR1",
		"parts": []map[string]any{{
			"type": "flight", "starts_at": out, "ends_at": in,
			"start_label": "LHR", "end_label": "JFK",
			"flight": map[string]any{
				"ident": "BA286", "scheduled_out": out, "scheduled_in": in,
				"origin_iata": "LHR", "dest_iata": "JFK",
			},
		}},
	}
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tid), create, owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed plan = %d: %s", w.Code, w.Body.String())
	}
	seeded := decodeBody[api.PlanDTO](t, w)
	oldPart := seeded.Parts[0].ID

	newOut := out.AddDate(0, 0, 1)
	newIn := in.AddDate(0, 0, 1)
	confirm := map[string]any{
		"plans": []map[string]any{{
			"type": "flight", "title": "BA286 rebooked", "confirmation_ref": "PNR1",
			"supersedes_part_id": oldPart,
			"parts": []map[string]any{{
				"type": "flight", "starts_at": newOut, "ends_at": newIn,
				"start_label": "LHR", "end_label": "JFK",
				"flight": map[string]any{
					"ident": "BA286", "scheduled_out": newOut, "scheduled_in": newIn,
					"origin_iata": "LHR", "dest_iata": "JFK",
				},
			}},
		}},
	}
	w = e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest/confirm", tid), confirm, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("confirm = %d: %s", w.Code, w.Body.String())
	}
	plans := decodeBody[[]api.PlanDTO](t, w)
	if len(plans) != 1 || plans[0].Parts[0].SupersedesID == nil || *plans[0].Parts[0].SupersedesID != oldPart {
		t.Fatalf("new part supersedes_id wrong: %+v", plans)
	}
	// Old part must now be cancelled.
	op, err := e.store.PlanPartByID(context.Background(), oldPart)
	if err != nil {
		t.Fatalf("PlanPartByID: %v", err)
	}
	if op.Status != "cancelled" {
		t.Errorf("old part status = %q, want cancelled", op.Status)
	}
}

// TestIngestPropose_Forbidden: a non-editor cannot propose against the trip.
func TestIngestPropose_Forbidden(t *testing.T) {
	e := setup(t, nil, nil)
	e.api.Extractor = &fakeIngestExtractor{}
	owner := e.user(t, "owner", false)
	stranger := e.user(t, "stranger", false)
	tid := newTrip(t, e, owner, "Trip")
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest", tid), map[string]any{"text": "x"}, stranger)
	if w.Code != http.StatusForbidden {
		t.Fatalf("stranger propose = %d, want 403: %s", w.Code, w.Body.String())
	}
}
