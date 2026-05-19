package handlers

import (
	"net/http"
	"time"

	"github.com/dpage/flight-tracker/internal/api"
	"github.com/dpage/flight-tracker/internal/auth"
	"github.com/dpage/flight-tracker/internal/store"
)

type createFlightReq struct {
	Ident        string    `json:"ident"`
	ScheduledOut time.Time `json:"scheduled_out"`
	ScheduledIn  time.Time `json:"scheduled_in"`
	OriginIATA   string    `json:"origin_iata"`
	DestIATA     string    `json:"dest_iata"`
	ICAO24       string    `json:"icao24"`
	Notes        string    `json:"notes"`
	PassengerIDs []int64   `json:"passenger_ids"`
}

type updateFlightReq struct {
	ScheduledOut *time.Time `json:"scheduled_out,omitempty"`
	ScheduledIn  *time.Time `json:"scheduled_in,omitempty"`
	OriginIATA   *string    `json:"origin_iata,omitempty"`
	DestIATA     *string    `json:"dest_iata,omitempty"`
	ICAO24       *string    `json:"icao24,omitempty"`
	Notes        *string    `json:"notes,omitempty"`
	Status       *string    `json:"status,omitempty"`
}

type addPassengerReq struct {
	UserID int64 `json:"user_id"`
}

func (a *API) listFlights(w http.ResponseWriter, r *http.Request) {
	flights, err := a.Store.ListFlights(r.Context())
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	ids := make([]int64, 0, len(flights))
	for _, f := range flights {
		ids = append(ids, f.ID)
	}
	passengers, err := a.Store.PassengersByFlight(r.Context(), ids)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	latest, err := a.Store.LatestPositions(r.Context(), ids)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.FlightDTO, 0, len(flights))
	for _, f := range flights {
		out = append(out, api.ToFlightDTO(f, passengers[f.ID], latest[f.ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) getFlight(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	f, err := a.Store.FlightByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	passengers, _ := a.Store.PassengersByFlight(r.Context(), []int64{id})
	latest, _ := a.Store.LatestPositions(r.Context(), []int64{id})
	writeJSON(w, http.StatusOK, api.ToFlightDTO(f, passengers[id], latest[id]))
}

func (a *API) createFlight(w http.ResponseWriter, r *http.Request) {
	var in createFlightReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	me := auth.UserFrom(r.Context())
	f, err := a.Store.CreateFlight(r.Context(), store.CreateFlightPayload{
		Ident:        in.Ident,
		ScheduledOut: in.ScheduledOut,
		ScheduledIn:  in.ScheduledIn,
		OriginIATA:   in.OriginIATA,
		DestIATA:     in.DestIATA,
		ICAO24:       in.ICAO24,
		Notes:        in.Notes,
	}, me.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, uid := range in.PassengerIDs {
		if err := a.Store.AddPassenger(r.Context(), f.ID, uid); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	passengers, _ := a.Store.PassengersByFlight(r.Context(), []int64{f.ID})
	writeJSON(w, http.StatusCreated, api.ToFlightDTO(f, passengers[f.ID], nil))
}

func (a *API) updateFlight(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var in updateFlightReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f, err := a.Store.UpdateFlight(r.Context(), id, store.UpdateFlightPayload{
		ScheduledOut: in.ScheduledOut,
		ScheduledIn:  in.ScheduledIn,
		OriginIATA:   in.OriginIATA,
		DestIATA:     in.DestIATA,
		ICAO24:       in.ICAO24,
		Notes:        in.Notes,
		Status:       in.Status,
	})
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	passengers, _ := a.Store.PassengersByFlight(r.Context(), []int64{id})
	latest, _ := a.Store.LatestPositions(r.Context(), []int64{id})
	writeJSON(w, http.StatusOK, api.ToFlightDTO(f, passengers[id], latest[id]))
}

func (a *API) deleteFlight(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := a.Store.DeleteFlight(r.Context(), id); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) addPassenger(w http.ResponseWriter, r *http.Request) {
	fid, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var in addPassengerReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.UserID == 0 {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	if err := a.Store.AddPassenger(r.Context(), fid, in.UserID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) removePassenger(w http.ResponseWriter, r *http.Request) {
	fid, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	uid, err := pathID(r, "userId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad userId")
		return
	}
	if err := a.Store.RemovePassenger(r.Context(), fid, uid); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
