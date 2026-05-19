// Package handlers wires the JSON HTTP API endpoints.
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/dpage/flight-tracker/internal/aeroapi"
	"github.com/dpage/flight-tracker/internal/auth"
	"github.com/dpage/flight-tracker/internal/config"
	"github.com/dpage/flight-tracker/internal/sse"
	"github.com/dpage/flight-tracker/internal/store"
)

type API struct {
	Store    *store.Store
	Auth     *auth.Handler
	Hub      *sse.Hub
	Config   *config.Config
	Resolver aeroapi.Resolver // may be nil if no resolver is configured
}

func New(s *store.Store, a *auth.Handler, hub *sse.Hub, cfg *config.Config, r aeroapi.Resolver) *API {
	return &API{Store: s, Auth: a, Hub: hub, Config: cfg, Resolver: r}
}

// Register attaches every /api/* route. All routes require an authenticated
// session; routes that mutate the user table additionally require superuser.
func (a *API) Register(mux *http.ServeMux) {
	req := a.Auth.Require
	sup := a.Auth.RequireSuperuser

	mux.Handle("GET /api/me", req(http.HandlerFunc(a.getMe)))
	mux.Handle("GET /api/config", req(http.HandlerFunc(a.getConfig)))
	mux.Handle("GET /api/events", req(http.HandlerFunc(a.Hub.Handle)))

	mux.Handle("GET /api/flights", req(http.HandlerFunc(a.listFlights)))
	mux.Handle("POST /api/flights", req(http.HandlerFunc(a.createFlight)))
	mux.Handle("POST /api/flights/resolve", req(http.HandlerFunc(a.resolveFlight)))
	mux.Handle("GET /api/flights/{id}", req(http.HandlerFunc(a.getFlight)))
	mux.Handle("PATCH /api/flights/{id}", req(http.HandlerFunc(a.updateFlight)))
	mux.Handle("DELETE /api/flights/{id}", req(http.HandlerFunc(a.deleteFlight)))
	mux.Handle("POST /api/flights/{id}/passengers", req(http.HandlerFunc(a.addPassenger)))
	mux.Handle("DELETE /api/flights/{id}/passengers/{userId}", req(http.HandlerFunc(a.removePassenger)))

	mux.Handle("GET /api/users", req(http.HandlerFunc(a.listUsers)))
	mux.Handle("POST /api/users", sup(http.HandlerFunc(a.inviteUser)))
	mux.Handle("PATCH /api/users/{id}", sup(http.HandlerFunc(a.updateUser)))
	mux.Handle("DELETE /api/users/{id}", sup(http.HandlerFunc(a.deleteUser)))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func handleStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func pathID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

