package handlers

import "net/http"

// Wave 0a stubs for the ingest pipeline (paste/upload → proposed plans, then
// commit). Wave 2A (ingestion + rebooking) fills in the bodies here.

func (a *API) ingestTrip(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (a *API) ingestTripConfirm(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
