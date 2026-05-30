package handlers

import "net/http"

// Wave 0a stubs for alert preferences and per-plan viewer opt-in. Wave 2B
// (alerts) fills in the bodies here.

func (a *API) getAlertPrefs(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (a *API) setAlertPrefs(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (a *API) addPlanAlertOptin(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (a *API) removePlanAlertOptin(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
