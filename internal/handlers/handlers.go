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
	"github.com/dpage/aerly/internal/planops"
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

	// Extractor backs the paste/upload ingest endpoints (the LLM seam). May
	// be nil when no LLM provider is configured — the ingest endpoints then
	// return 503.
	Extractor planops.Extractor

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

	mux.Handle("GET /api/notifications", req(http.HandlerFunc(a.getNotifications)))

	mux.Handle("GET /api/friends", req(http.HandlerFunc(a.listFriends)))
	mux.Handle("POST /api/friends/invite", req(http.HandlerFunc(a.inviteFriend)))
	mux.Handle("DELETE /api/friends/outgoing", req(http.HandlerFunc(a.cancelOutgoingInvite)))
	mux.Handle("POST /api/friends/accept-token", req(http.HandlerFunc(a.acceptFriendToken)))
	mux.Handle("POST /api/friends/{userId}/accept", req(http.HandlerFunc(a.acceptFriend)))
	mux.Handle("DELETE /api/friends/{userId}", req(http.HandlerFunc(a.removeFriend)))

	mux.Handle("GET /api/users", req(http.HandlerFunc(a.listUsers)))
	mux.Handle("POST /api/users", sup(http.HandlerFunc(a.inviteUser)))
	mux.Handle("PATCH /api/users/{id}", sup(http.HandlerFunc(a.updateUser)))
	mux.Handle("DELETE /api/users/{id}", sup(http.HandlerFunc(a.deleteUser)))

	// --- Trip-planning core redesign (spec §5.2). Bodies are filled in by
	// the Wave 1/2 feature agents in their per-area handler files. ---

	// Trips, members, tags (Wave 1A).
	mux.Handle("GET /api/trips", req(http.HandlerFunc(a.listTrips)))
	mux.Handle("POST /api/trips", req(http.HandlerFunc(a.createTrip)))
	mux.Handle("GET /api/trips/{id}", req(http.HandlerFunc(a.getTrip)))
	mux.Handle("PATCH /api/trips/{id}", req(http.HandlerFunc(a.updateTrip)))
	mux.Handle("DELETE /api/trips/{id}", req(http.HandlerFunc(a.deleteTrip)))
	mux.Handle("POST /api/trips/{id}/members", req(http.HandlerFunc(a.addTripMember)))
	mux.Handle("DELETE /api/trips/{id}/members/{userId}", req(http.HandlerFunc(a.removeTripMember)))
	mux.Handle("PUT /api/trips/{id}/tags", req(http.HandlerFunc(a.setTripTags)))
	mux.Handle("GET /api/tags/suggest", req(http.HandlerFunc(a.suggestTags)))

	// Plans, parts, passengers, visibility, move (Wave 1B).
	mux.Handle("POST /api/trips/{id}/plans", req(http.HandlerFunc(a.createPlan)))
	mux.Handle("PATCH /api/plans/{id}", req(http.HandlerFunc(a.updatePlan)))
	mux.Handle("DELETE /api/plans/{id}", req(http.HandlerFunc(a.deletePlan)))
	mux.Handle("POST /api/plans/{id}/passengers", req(http.HandlerFunc(a.addPlanPassenger)))
	mux.Handle("DELETE /api/plans/{id}/passengers/{userId}", req(http.HandlerFunc(a.removePlanPassenger)))
	mux.Handle("PUT /api/plans/{id}/visibility", req(http.HandlerFunc(a.setPlanVisibility)))
	mux.Handle("POST /api/plans/{id}/move", req(http.HandlerFunc(a.movePlan)))
	mux.Handle("PATCH /api/plan-parts/{id}", req(http.HandlerFunc(a.updatePlanPart)))
	mux.Handle("POST /api/plan-parts/{id}/dismiss", req(http.HandlerFunc(a.dismissPlanPart)))

	// Ingest (Wave 2A).
	mux.Handle("POST /api/trips/{id}/ingest", req(http.HandlerFunc(a.ingestTrip)))
	mux.Handle("POST /api/trips/{id}/ingest/confirm", req(http.HandlerFunc(a.ingestTripConfirm)))

	// iCal feeds (Wave 1D). Token-authed via ?token=, NOT the session cookie,
	// so they are registered without the req() session guard.
	//
	// Go 1.22 ServeMux can't express a wildcard that doesn't span a whole path
	// segment (e.g. "{id}.ics"), so the trip/plan feeds are registered as
	// prefix patterns and the handler parses the trailing "{id}.ics". The
	// public URLs stay exactly /api/calendar/{trip,plan}/{id}.ics.
	mux.Handle("GET /api/calendar/me.ics", http.HandlerFunc(a.calendarMe))
	mux.Handle("GET /api/calendar/trip/", http.HandlerFunc(a.calendarTrip))
	mux.Handle("GET /api/calendar/plan/", http.HandlerFunc(a.calendarPlan))

	// Calendar token management (Wave 1D) — session-authed, matching the FE
	// contract in web/src/api/client.ts (list/issue/revoke per-scope tokens).
	mux.Handle("GET /api/calendar/tokens", req(http.HandlerFunc(a.listCalendarTokens)))
	mux.Handle("POST /api/calendar/tokens", req(http.HandlerFunc(a.issueCalendarToken)))
	mux.Handle("DELETE /api/calendar/tokens/{token}", req(http.HandlerFunc(a.revokeCalendarToken)))

	// Tracker (Wave 1C).
	mux.Handle("GET /api/tracker", req(http.HandlerFunc(a.getTracker)))

	// Alerts (Wave 2B).
	mux.Handle("GET /api/alert-prefs", req(http.HandlerFunc(a.getAlertPrefs)))
	mux.Handle("PUT /api/alert-prefs", req(http.HandlerFunc(a.setAlertPrefs)))
	mux.Handle("POST /api/plans/{id}/alerts/optin", req(http.HandlerFunc(a.addPlanAlertOptin)))
	mux.Handle("DELETE /api/plans/{id}/alerts/optin", req(http.HandlerFunc(a.removePlanAlertOptin)))
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
