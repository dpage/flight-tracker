package handlers

import "net/http"

// Wave 0a stub for the convergence tracker view. Wave 1C (tracker re-scope)
// fills in the body here.

func (a *API) getTracker(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
