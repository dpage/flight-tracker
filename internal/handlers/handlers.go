// Package handlers wires the JSON HTTP API endpoints.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/config"
	"github.com/dpage/aerly/internal/emailingest"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

type API struct {
	Store    *store.Store
	Auth     *auth.Handler
	Hub      *sse.Hub
	Config   *config.Config
	Resolver providers.Resolver // may be nil if no resolver is configured

	// SendVerifyEmail dispatches the verification message. Defaulted in
	// New() to the real sendmail pipe; tests can override.
	SendVerifyEmail func(ctx context.Context, to, token string) error
}

func New(s *store.Store, a *auth.Handler, hub *sse.Hub, cfg *config.Config, r providers.Resolver) *API {
	api := &API{Store: s, Auth: a, Hub: hub, Config: cfg, Resolver: r}
	api.SendVerifyEmail = api.defaultSendVerifyEmail
	return api
}

func (a *API) defaultSendVerifyEmail(ctx context.Context, to, token string) error {
	msg := emailingest.BuildVerifyEmail(emailingest.VerifyInput{
		FromAddr:  a.Config.EmailIngestAddress,
		ToAddr:    to,
		PublicURL: a.Config.PublicURL,
		Token:     token,
	})
	return emailingest.Send(ctx, a.Config.EmailIngestSendmail, a.Config.EmailIngestAddress, msg)
}

// Register attaches every /api/* route. All routes require an authenticated
// session; routes that mutate the user table additionally require superuser.
func (a *API) Register(mux *http.ServeMux) {
	req := a.Auth.Require
	sup := a.Auth.RequireSuperuser

	mux.Handle("GET /api/me", req(http.HandlerFunc(a.getMe)))
	mux.Handle("GET /api/config", req(http.HandlerFunc(a.getConfig)))
	mux.Handle("GET /api/events", req(http.HandlerFunc(a.events)))
	mux.Handle("GET /api/me/emails", req(http.HandlerFunc(a.listMyEmails)))
	mux.Handle("POST /api/me/emails", req(http.HandlerFunc(a.addMyEmail)))
	mux.Handle("POST /api/me/emails/{id}/resend", req(http.HandlerFunc(a.resendMyEmail)))
	mux.Handle("DELETE /api/me/emails/{id}", req(http.HandlerFunc(a.deleteMyEmail)))

	mux.Handle("GET /api/flights", req(http.HandlerFunc(a.listFlights)))
	mux.Handle("POST /api/flights", req(http.HandlerFunc(a.createFlight)))
	mux.Handle("POST /api/flights/resolve", req(http.HandlerFunc(a.resolveFlight)))
	mux.Handle("GET /api/flights/{id}", req(http.HandlerFunc(a.getFlight)))
	mux.Handle("PATCH /api/flights/{id}", req(http.HandlerFunc(a.updateFlight)))
	mux.Handle("DELETE /api/flights/{id}", req(http.HandlerFunc(a.deleteFlight)))
	mux.Handle("POST /api/flights/{id}/passengers", req(http.HandlerFunc(a.addPassenger)))
	mux.Handle("DELETE /api/flights/{id}/passengers/{userId}", req(http.HandlerFunc(a.removePassenger)))
	mux.Handle("POST /api/flights/{id}/shares", req(http.HandlerFunc(a.addShare)))
	mux.Handle("DELETE /api/flights/{id}/shares/{userId}", req(http.HandlerFunc(a.removeShare)))

	mux.Handle("GET /api/friends", req(http.HandlerFunc(a.listFriends)))
	mux.Handle("POST /api/friends/invite", req(http.HandlerFunc(a.inviteFriend)))
	mux.Handle("POST /api/friends/{userId}/accept", req(http.HandlerFunc(a.acceptFriend)))
	mux.Handle("DELETE /api/friends/{userId}", req(http.HandlerFunc(a.removeFriend)))

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
