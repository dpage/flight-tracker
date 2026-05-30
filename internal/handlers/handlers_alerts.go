package handlers

import (
	"net/http"

	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/store"
)

// Wave 2B (alerts §9): per-user alert preferences and per-plan viewer opt-in.

// alertPrefsDTO is the wire shape for GET/PUT /api/alert-prefs. Matches the
// merged FE contract (web/src/api/types.ts AlertPrefs).
type alertPrefsDTO struct {
	InApp       bool `json:"in_app"`
	Email       bool `json:"email"`
	MinDelayMin int  `json:"min_delay_min"`
}

// updateAlertPrefsInput is the PUT body. All fields optional (pointer) so a
// partial patch leaves the rest unchanged — matches UpdateAlertPrefsInput.
type updateAlertPrefsInput struct {
	InApp       *bool `json:"in_app"`
	Email       *bool `json:"email"`
	MinDelayMin *int  `json:"min_delay_min"`
}

func toAlertPrefsDTO(p *store.AlertPrefs) alertPrefsDTO {
	return alertPrefsDTO{InApp: p.InApp, Email: p.Email, MinDelayMin: p.MinDelayMin}
}

func (a *API) getAlertPrefs(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	prefs, err := a.Store.AlertPrefsFor(r.Context(), me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAlertPrefsDTO(prefs))
}

func (a *API) setAlertPrefs(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	var in updateAlertPrefsInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	// Start from the current effective prefs (defaults when no row) so a
	// partial patch only touches the supplied fields.
	prefs, err := a.Store.AlertPrefsFor(r.Context(), me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if in.InApp != nil {
		prefs.InApp = *in.InApp
	}
	if in.Email != nil {
		prefs.Email = *in.Email
	}
	if in.MinDelayMin != nil {
		v := *in.MinDelayMin
		if v < 0 {
			v = 0
		}
		prefs.MinDelayMin = v
	}
	if err := a.Store.SetAlertPrefs(r.Context(), *prefs); err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAlertPrefsDTO(prefs))
}

func (a *API) addPlanAlertOptin(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	planID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid plan id")
		return
	}
	// A viewer may only opt in to a plan they can actually see (spec §4 gate);
	// otherwise opt-in would leak the plan's existence.
	ok, err := a.Store.CanViewPlan(r.Context(), planID, me.ID, me.IsSuperuser)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := a.Store.AddPlanAlertOptin(r.Context(), planID, me.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) removePlanAlertOptin(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	planID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid plan id")
		return
	}
	if err := a.Store.RemovePlanAlertOptin(r.Context(), planID, me.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
