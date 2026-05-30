package handlers

import "net/http"

// Wave 0a stubs for the read-only iCal feeds. These authenticate by the
// ?token= query param (not the session cookie, since calendar clients won't
// carry it) and render as the token's owner with the §4 predicate applied.
// Wave 1D (iCal feeds) fills in the bodies here.

func (a *API) calendarMe(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (a *API) calendarTrip(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (a *API) calendarPlan(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
