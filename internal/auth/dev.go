package auth

import (
	"encoding/json"
	"hash/fnv"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/dpage/aerly/internal/store"
)

// RegisterDevLogin attaches GET /auth/dev-login?login=foo, which fabricates a
// synthetic identity and creates a session — bypassing OAuth entirely. It is
// the caller's responsibility to gate this on DEV_AUTH_BYPASS + localhost.
//
// Also attaches GET /auth/dev-info — an unauthenticated probe the SPA's login
// page uses to decide whether to render the dev-login form. When dev bypass is
// off the route isn't registered and the probe 404s.
//
// Synthetic identity rows use the "dev" provider, so they can never collide
// with real GitHub or Google identities.
func (h *Handler) RegisterDevLogin(mux *http.ServeMux) {
	slog.Warn("DEV_AUTH_BYPASS enabled — /auth/dev-login active. Do not use in production.")
	mux.HandleFunc("GET /auth/dev-login", h.devLogin)
	mux.HandleFunc("GET /auth/dev-info", h.devInfo)
}

func (h *Handler) devInfo(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"enabled": true})
}

func (h *Handler) devLogin(w http.ResponseWriter, r *http.Request) {
	login := strings.TrimSpace(r.URL.Query().Get("login"))
	if login == "" {
		http.Error(w, "missing ?login=<username>", http.StatusBadRequest)
		return
	}

	profile := store.OAuthProfile{
		Provider:       "dev",
		ProviderUserID: strconv.FormatUint(devSyntheticID(login), 10),
		Username:       login,
		Name:           login,
		AvatarURL:      "https://github.com/" + login + ".png",
	}
	count, err := h.Store.CountUsers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	user, _, err := h.Store.LinkLogin(r.Context(), profile, count == 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	SetSessionCookie(w, h.SessionKey, user.ID, h.Secure)
	http.Redirect(w, r, "/", http.StatusFound)
}

// devSyntheticID hashes the login into a stable identifier so the same dev
// login always maps to the same user_identities row across server restarts.
func devSyntheticID(login string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(login)))
	return h.Sum64()
}
