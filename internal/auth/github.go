package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dpage/flight-tracker/internal/store"
)

const (
	gitHubAuthURL      = "https://github.com/login/oauth/authorize"
	gitHubTokenURL     = "https://github.com/login/oauth/access_token"
	gitHubUserAPIURL   = "https://api.github.com/user"
	gitHubEmailsAPIURL = "https://api.github.com/user/emails"
)

// Handler wires the GitHub OAuth flow against a Store.
type Handler struct {
	ClientID     string
	ClientSecret string
	SessionKey   []byte
	PublicURL    string // e.g. https://flights.example.com
	Secure       bool   // mark cookies Secure
	Store        *store.Store
	HTTP         *http.Client
}

func NewHandler(clientID, clientSecret string, sessionKey []byte, publicURL string, s *store.Store) *Handler {
	return &Handler{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		SessionKey:   sessionKey,
		PublicURL:    strings.TrimRight(publicURL, "/"),
		Secure:       strings.HasPrefix(publicURL, "https://"),
		Store:        s,
		HTTP:         &http.Client{Timeout: 15 * time.Second},
	}
}

// Register attaches /auth/github/login, /auth/github/callback, /auth/logout.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/github/login", h.Login)
	mux.HandleFunc("GET /auth/github/callback", h.Callback)
	mux.HandleFunc("POST /auth/logout", h.Logout)
}

func (h *Handler) redirectURL() string {
	return h.PublicURL + "/auth/github/callback"
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	state := randomToken(24)
	// Sign the state into a short-lived cookie. We verify on callback.
	expires := time.Now().Add(StateTTL)
	stateVal := SignSession(h.SessionKey, 0, expires) + ":" + state
	http.SetCookie(w, &http.Cookie{
		Name:     StateCookie,
		Value:    stateVal,
		Path:     "/auth/github",
		Expires:  expires,
		HttpOnly: true,
		Secure:   h.Secure,
		SameSite: http.SameSiteLaxMode,
	})

	q := url.Values{}
	q.Set("client_id", h.ClientID)
	q.Set("redirect_uri", h.redirectURL())
	q.Set("scope", "read:user user:email")
	q.Set("state", state)
	http.Redirect(w, r, gitHubAuthURL+"?"+q.Encode(), http.StatusFound)
}

func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		renderLoginError(w, "GitHub returned: "+errParam)
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		renderLoginError(w, "missing code or state")
		return
	}
	c, err := r.Cookie(StateCookie)
	if err != nil {
		renderLoginError(w, "state cookie missing — try signing in again")
		return
	}
	// Clear the state cookie regardless of outcome.
	http.SetCookie(w, &http.Cookie{
		Name: StateCookie, Path: "/auth/github", MaxAge: -1,
		HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteLaxMode,
	})

	parts := strings.SplitN(c.Value, ":", 2)
	if len(parts) != 2 || parts[1] != state {
		renderLoginError(w, "state mismatch")
		return
	}
	if _, err := VerifySession(h.SessionKey, parts[0]); err != nil {
		renderLoginError(w, "state expired — try signing in again")
		return
	}

	token, err := h.exchangeCode(r.Context(), code)
	if err != nil {
		slog.Error("oauth token exchange failed", "err", err)
		renderLoginError(w, "could not complete sign-in")
		return
	}
	profile, err := h.fetchProfile(r.Context(), token)
	if err != nil {
		slog.Error("github profile fetch failed", "err", err)
		renderLoginError(w, "could not fetch GitHub profile")
		return
	}
	if email, eerr := h.fetchPrimaryEmail(r.Context(), token); eerr != nil {
		// Tolerated: sign-in continues without an email. The user just
		// can't use the forwarded-email ingest until they add one.
		slog.Warn("fetch primary email failed", "err", eerr)
	} else {
		profile.Email = email
	}

	count, err := h.Store.CountUsers(r.Context())
	if err != nil {
		slog.Error("count users failed", "err", err)
		renderLoginError(w, "database error")
		return
	}
	user, err := h.Store.LinkLogin(r.Context(), profile, count == 0)
	if errors.Is(err, store.ErrNotFound) {
		renderLoginError(w, fmt.Sprintf(
			"%s is not on the allowlist. Ask the administrator to invite you.",
			profile.Login))
		return
	}
	if err != nil {
		slog.Error("link login failed", "err", err)
		renderLoginError(w, "database error")
		return
	}

	SetSessionCookie(w, h.SessionKey, user.ID, h.Secure)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	ClearSessionCookie(w, h.Secure)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", h.ClientID)
	form.Set("client_secret", h.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", h.redirectURL())

	req, err := http.NewRequestWithContext(ctx, "POST", gitHubTokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := h.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("token endpoint %d: %s", resp.StatusCode, body)
	}
	var out struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", fmt.Errorf("%s: %s", out.Error, out.ErrorDescription)
	}
	if out.AccessToken == "" {
		return "", errors.New("empty access_token")
	}
	return out.AccessToken, nil
}

func (h *Handler) fetchProfile(ctx context.Context, token string) (store.GitHubProfile, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", gitHubUserAPIURL, nil)
	if err != nil {
		return store.GitHubProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "flight-tracker")

	resp, err := h.HTTP.Do(req)
	if err != nil {
		return store.GitHubProfile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return store.GitHubProfile{}, fmt.Errorf("/user %d: %s", resp.StatusCode, body)
	}
	var u struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return store.GitHubProfile{}, err
	}
	return store.GitHubProfile{
		ID: u.ID, Login: u.Login, Name: u.Name, AvatarURL: u.AvatarURL,
	}, nil
}

// fetchPrimaryEmail returns the user's primary verified GitHub email, or
// "" if none is set / GitHub returned an empty list. An error is returned
// only on transport or decode failures.
func (h *Handler) fetchPrimaryEmail(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", gitHubEmailsAPIURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "flight-tracker")

	resp, err := h.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("/user/emails %d: %s", resp.StatusCode, body)
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", err
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}
	return "", nil
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func renderLoginError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Sign-in failed</title>
<body style="font-family:system-ui;max-width:36rem;margin:4rem auto;padding:0 1rem">
<h1>Sign-in failed</h1><p>%s</p><p><a href="/">Back to home</a></p></body>`,
		htmlEscape(msg))
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}
