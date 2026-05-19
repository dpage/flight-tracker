package handlers

import (
	"net/http"
	"time"
)

type resolveReq struct {
	Ident string `json:"ident"`
	Date  string `json:"date"` // YYYY-MM-DD (UTC)
}

type resolvedFlightDTO struct {
	Ident        string    `json:"ident"`
	ScheduledOut time.Time `json:"scheduled_out"`
	ScheduledIn  time.Time `json:"scheduled_in"`
	OriginIATA   string    `json:"origin_iata"`
	OriginLat    float64   `json:"origin_lat"`
	OriginLon    float64   `json:"origin_lon"`
	DestIATA     string    `json:"dest_iata"`
	DestLat      float64   `json:"dest_lat"`
	DestLon      float64   `json:"dest_lon"`
	ICAO24       string    `json:"icao24"`
	Notes        string    `json:"notes"`
}

func (a *API) resolveFlight(w http.ResponseWriter, r *http.Request) {
	if a.Resolver == nil {
		writeError(w, http.StatusNotImplemented,
			"no flight resolver is configured on this server")
		return
	}
	var in resolveReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Ident == "" || in.Date == "" {
		writeError(w, http.StatusBadRequest, "ident and date required")
		return
	}
	date, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	rf, err := a.Resolver.Resolve(r.Context(), in.Ident, date)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resolvedFlightDTO{
		Ident:        rf.Ident,
		ScheduledOut: rf.ScheduledOut,
		ScheduledIn:  rf.ScheduledIn,
		OriginIATA:   rf.OriginIATA,
		OriginLat:    rf.OriginLat,
		OriginLon:    rf.OriginLon,
		DestIATA:     rf.DestIATA,
		DestLat:      rf.DestLat,
		DestLon:      rf.DestLon,
		ICAO24:       rf.ICAO24,
		Notes:        rf.Notes,
	})
}
