package auth

import (
	"hash/fnv"
	"log/slog"
	"net/http"
	"strings"

	"github.com/dpage/flight-tracker/internal/store"
)

// RegisterDevLogin attaches GET /auth/dev-login?login=foo, which fabricates a
// GitHub identity and creates a session — bypassing OAuth entirely. It is the
// caller's responsibility to gate this on DEV_AUTH_BYPASS + localhost.
//
// Synthetic GitHub IDs are negative (real ones are positive) so dev users
// never collide with real GitHub accounts.
func (h *Handler) RegisterDevLogin(mux *http.ServeMux) {
	slog.Warn("DEV_AUTH_BYPASS enabled — /auth/dev-login active. Do not use in production.")
	mux.HandleFunc("GET /auth/dev-login", h.devLogin)
}

func (h *Handler) devLogin(w http.ResponseWriter, r *http.Request) {
	login := strings.TrimSpace(r.URL.Query().Get("login"))
	if login == "" {
		http.Error(w, "missing ?login=<github-login>", http.StatusBadRequest)
		return
	}

	profile := store.GitHubProfile{
		ID:        devSyntheticID(login),
		Login:     login,
		Name:      login,
		AvatarURL: "https://github.com/" + login + ".png",
	}
	count, err := h.Store.CountUsers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	user, err := h.Store.LinkLogin(r.Context(), profile, count == 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	SetSessionCookie(w, h.SessionKey, user.ID, h.Secure)
	http.Redirect(w, r, "/", http.StatusFound)
}

func devSyntheticID(login string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(login)))
	v := h.Sum64() & 0x7fffffffffffffff
	return -int64(v) - 1
}
