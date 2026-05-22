package auth

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/dpage/flight-tracker/internal/store"
)

// VerifyEmail consumes a verification token from the query string and
// renders a small HTML success or error page. Public route — no session
// required, by design: the click comes from the user's mail client.
func (h *Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		renderVerifyEmailError(w, "Missing token in the link.")
		return
	}
	_, err := h.Store.VerifyEmailByToken(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		renderVerifyEmailError(w, "This verification link is invalid or has expired.")
		return
	}
	if err != nil {
		slog.Error("verify email failed", "err", err)
		renderVerifyEmailError(w, "Something went wrong. Please try again.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><title>Email verified</title>
<body style="font-family:system-ui;max-width:36rem;margin:4rem auto;padding:0 1rem">
<h1>Email verified</h1>
<p>Your address is now registered with flight-tracker. You can close this tab.</p>
<p><a href="/">Back to home</a></p></body>`)
}

func renderVerifyEmailError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Email verification failed</title>
<body style="font-family:system-ui;max-width:36rem;margin:4rem auto;padding:0 1rem">
<h1>Email verification failed</h1><p>%s</p>
<p><a href="/">Back to home</a></p></body>`, htmlEscape(msg))
}
