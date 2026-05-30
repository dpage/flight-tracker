package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/store"
)

// Read-only iCal feeds (spec §8) and their token management (spec §5.2/§8).
//
// The .ics feeds authenticate by the ?token= query param (NOT the session
// cookie, since calendar clients won't carry it). The token resolves to its
// owning user, and the feed is rendered AS that user with the §4 visibility
// predicate applied in the store query — so a plan hidden from the token owner
// never appears, and another user's token can never surface the owner's
// private plans.
//
// The token-management endpoints are session-authed and let a logged-in user
// list / issue (regenerate) / revoke their own per-scope tokens.

// --- Feed handlers (token-authed, no session) ---

func (a *API) calendarMe(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.calendarTokenUser(w, r)
	if !ok {
		return
	}
	events, err := a.Store.CalendarEventsForUser(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeICS(w, "Aerly", events)
}

func (a *API) calendarTrip(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.calendarTokenUser(w, r)
	if !ok {
		return
	}
	id, ok := parseICSPathID(r.URL.Path, "/api/calendar/trip/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	events, err := a.Store.CalendarEventsForTrip(r.Context(), uid, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeICS(w, "Aerly Trip", events)
}

func (a *API) calendarPlan(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.calendarTokenUser(w, r)
	if !ok {
		return
	}
	id, ok := parseICSPathID(r.URL.Path, "/api/calendar/plan/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	events, err := a.Store.CalendarEventsForPlan(r.Context(), uid, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeICS(w, "Aerly Plan", events)
}

// calendarTokenUser resolves the ?token= query param to its owning user id,
// writing a 401 and returning ok=false when absent or unknown.
func (a *API) calendarTokenUser(w http.ResponseWriter, r *http.Request) (int64, bool) {
	tok := strings.TrimSpace(r.URL.Query().Get("token"))
	if tok == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return 0, false
	}
	uid, err := a.Store.UserByCalendarToken(r.Context(), tok)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return 0, false
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return 0, false
	}
	return uid, true
}

// parseICSPathID extracts the {id} from a "<prefix>{id}.ics" request path. The
// trip/plan feeds are registered as prefix patterns (Go 1.22 ServeMux can't
// match a wildcard mid-segment), so we parse the trailing segment here.
func parseICSPathID(path, prefix string) (int64, bool) {
	rest := strings.TrimPrefix(path, prefix)
	if rest == path { // prefix not present
		return 0, false
	}
	rest = strings.TrimSuffix(rest, ".ics")
	if rest == "" || strings.Contains(rest, "/") {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func writeICS(w http.ResponseWriter, calName string, events []*store.CalendarEvent) {
	body := renderICS(calName, events)
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// --- Token-management handlers (session-authed) ---
//
// Shapes match the already-merged frontend contract in web/src/api/client.ts +
// types.ts:
//   GET    /api/calendar/tokens            -> CalendarToken[]
//   POST   /api/calendar/tokens {scope,id} -> CalendarToken   (issue/regenerate)
//   DELETE /api/calendar/tokens/{token}    -> 204
// where CalendarToken = { scope, token, url, created_at }.

type calendarTokenDTO struct {
	Scope     string `json:"scope"`
	Token     string `json:"token"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

type issueCalendarTokenInput struct {
	Scope string `json:"scope"`
	// ID is the trip or plan id for scope=="trip"/"plan"; it is folded into the
	// returned feed URL. The token itself is per-(user,scope) — the URL carries
	// the id — so the FE's optional id only shapes the URL, never the token row.
	ID int64 `json:"id"`
}

func (a *API) listCalendarTokens(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	toks, err := a.Store.ListCalendarTokens(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]calendarTokenDTO, 0, len(toks))
	for _, t := range toks {
		out = append(out, a.calendarTokenDTO(t, 0))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) issueCalendarToken(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	var in issueCalendarTokenInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	scope := strings.TrimSpace(in.Scope)
	switch scope {
	case "me", "trip", "plan":
	default:
		writeError(w, http.StatusBadRequest, "invalid scope")
		return
	}
	if (scope == "trip" || scope == "plan") && in.ID <= 0 {
		writeError(w, http.StatusBadRequest, "id required for trip/plan scope")
		return
	}
	// Issue (regenerate, revoking any prior token for this scope).
	tok, err := a.Store.RegenerateCalendarToken(r.Context(), u.ID, scope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.calendarTokenDTO(tok, in.ID))
}

func (a *API) revokeCalendarToken(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	token := r.PathValue("token")
	if token == "" {
		http.NotFound(w, r)
		return
	}
	if err := a.Store.RevokeCalendarToken(r.Context(), u.ID, token); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// calendarTokenDTO builds the wire shape, deriving the ready-to-use feed URL
// from the public base URL, scope, and (for trip/plan) the id.
func (a *API) calendarTokenDTO(t *store.CalendarToken, id int64) calendarTokenDTO {
	return calendarTokenDTO{
		Scope:     t.Scope,
		Token:     t.Token,
		URL:       a.calendarFeedURL(t.Scope, id, t.Token),
		CreatedAt: t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func (a *API) calendarFeedURL(scope string, id int64, token string) string {
	base := ""
	if a.Config != nil {
		base = strings.TrimRight(a.Config.PublicURL, "/")
	}
	var path string
	switch scope {
	case "trip":
		path = "/api/calendar/trip/" + strconv.FormatInt(id, 10) + ".ics"
	case "plan":
		path = "/api/calendar/plan/" + strconv.FormatInt(id, 10) + ".ics"
	default:
		path = "/api/calendar/me.ics"
	}
	return base + path + "?token=" + token
}
