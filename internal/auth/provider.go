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

	"github.com/dpage/aerly/internal/mailer"
	"github.com/dpage/aerly/internal/store"
)

// Provider describes one OAuth identity provider (GitHub, Google, ...).
// Each provider is a thin descriptor; the generic OAuth flow lives on
// Handler and drives all providers identically.
type Provider struct {
	// Name is the URL-safe identifier (e.g. "github", "google"). It appears
	// in the route path (/auth/{name}/login) and in user_identities.provider.
	Name string
	// Label is the human-readable name shown on the login button.
	Label string
	// ClientID / ClientSecret authenticate this server to the OAuth provider.
	ClientID     string
	ClientSecret string
	// AuthURL is the provider's authorization endpoint (the redirect target).
	AuthURL string
	// TokenURL is the provider's token-exchange endpoint.
	TokenURL string
	// Scopes is the space-separated list of scopes to request.
	Scopes string
	// FetchProfile loads the OAuth-side identity, given an access token. It
	// must populate Provider+ProviderUserID and is responsible for resolving
	// a verified email when available.
	FetchProfile func(ctx context.Context, client *http.Client, token string) (store.OAuthProfile, error)
}

// providerRouteRoot returns the path prefix this provider's routes live
// under (used for cookie Path scoping and route registration).
func (p *Provider) routeRoot() string { return "/auth/" + p.Name }

// Handler wires the OAuth flow for one or more providers.
type Handler struct {
	SessionKey []byte
	PublicURL  string // e.g. https://flights.example.com
	Secure     bool   // mark cookies Secure
	Store      *store.Store
	HTTP       *http.Client

	// Outbound mail wiring for side-channel notifications (currently:
	// "a new sign-in method was linked to your account via verified
	// email"). When MailFromAddress is empty the notification is skipped
	// with a warning log — the sign-in flow itself never blocks on email
	// delivery.
	MailFromAddress string
	SendmailPath    string

	// SendNotification dispatches an assembled RFC822 message. Defaulted
	// to mailer.Send; tests override.
	SendNotification func(ctx context.Context, sendmailPath, envelopeSender, message string) error

	// providers is keyed by Name; providerOrder preserves registration
	// order so the login UI renders buttons in a deterministic sequence.
	providers     map[string]*Provider
	providerOrder []string
}

func NewHandler(sessionKey []byte, publicURL string, s *store.Store) *Handler {
	return &Handler{
		SessionKey:       sessionKey,
		PublicURL:        strings.TrimRight(publicURL, "/"),
		Secure:           strings.HasPrefix(publicURL, "https://"),
		Store:            s,
		HTTP:             &http.Client{Timeout: 15 * time.Second},
		SendNotification: mailer.Send,
		providers:        make(map[string]*Provider),
	}
}

// AddProvider registers an OAuth provider. Providers must be added before
// Register is called.
func (h *Handler) AddProvider(p *Provider) {
	h.providers[p.Name] = p
	h.providerOrder = append(h.providerOrder, p.Name)
}

// Providers returns the registered providers, in the order they were added.
// Used by /auth/providers to render the login page buttons.
func (h *Handler) Providers() []*Provider {
	out := make([]*Provider, 0, len(h.providerOrder))
	for _, n := range h.providerOrder {
		out = append(out, h.providers[n])
	}
	return out
}

// Register attaches /auth/{provider}/login, /auth/{provider}/callback for
// every registered provider, plus the shared /auth/providers,
// /auth/verify-email and /auth/logout routes.
func (h *Handler) Register(mux *http.ServeMux) {
	for _, p := range h.providers {
		prov := p // capture
		mux.HandleFunc("GET "+prov.routeRoot()+"/login", func(w http.ResponseWriter, r *http.Request) {
			h.login(w, r, prov)
		})
		mux.HandleFunc("GET "+prov.routeRoot()+"/callback", func(w http.ResponseWriter, r *http.Request) {
			h.callback(w, r, prov)
		})
	}
	mux.HandleFunc("GET /auth/providers", h.listProviders)
	mux.HandleFunc("GET /auth/verify-email", h.VerifyEmail)
	mux.HandleFunc("POST /auth/logout", h.Logout)
}

func (h *Handler) redirectURL(p *Provider) string {
	return h.PublicURL + p.routeRoot() + "/callback"
}

type providerDTO struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

func (h *Handler) listProviders(w http.ResponseWriter, _ *http.Request) {
	ps := h.Providers()
	out := make([]providerDTO, 0, len(ps))
	for _, p := range ps {
		out = append(out, providerDTO{Name: p.Name, Label: p.Label})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string][]providerDTO{"providers": out})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request, p *Provider) {
	state := randomToken(24)
	expires := time.Now().Add(StateTTL)
	stateVal := SignSession(h.SessionKey, 0, expires) + ":" + state
	http.SetCookie(w, &http.Cookie{
		Name:     StateCookie,
		Value:    stateVal,
		Path:     p.routeRoot(),
		Expires:  expires,
		HttpOnly: true,
		Secure:   h.Secure,
		SameSite: http.SameSiteLaxMode,
	})

	q := url.Values{}
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", h.redirectURL(p))
	q.Set("scope", p.Scopes)
	q.Set("state", state)
	// Google needs response_type=code explicitly; harmless for GitHub.
	q.Set("response_type", "code")
	http.Redirect(w, r, p.AuthURL+"?"+q.Encode(), http.StatusFound)
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request, p *Provider) {
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		renderLoginError(w, p.Label+" returned: "+errParam)
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
		Name: StateCookie, Path: p.routeRoot(), MaxAge: -1,
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

	token, err := h.exchangeCode(r.Context(), p, code)
	if err != nil {
		slog.Error("oauth token exchange failed", "provider", p.Name, "err", err)
		renderLoginError(w, "could not complete sign-in")
		return
	}
	profile, err := p.FetchProfile(r.Context(), h.HTTP, token)
	if err != nil {
		slog.Error("oauth profile fetch failed", "provider", p.Name, "err", err)
		renderLoginError(w, "could not fetch "+p.Label+" profile")
		return
	}
	// Defensive: providers must set this, but if a buggy impl leaves it
	// blank we'd silently link the wrong provider on the user_identities row.
	if profile.Provider == "" {
		profile.Provider = p.Name
	}

	count, err := h.Store.CountUsers(r.Context())
	if err != nil {
		slog.Error("count users failed", "err", err)
		renderLoginError(w, "database error")
		return
	}
	user, outcome, err := h.Store.LinkLogin(r.Context(), profile, count == 0)
	if errors.Is(err, store.ErrNotFound) {
		who := profile.Username
		if who == "" {
			who = profile.Email
		}
		if who == "" {
			who = "this account"
		}
		renderLoginError(w, fmt.Sprintf(
			"%s is not on the allowlist. Ask the administrator to invite you.",
			who))
		return
	}
	if err != nil {
		slog.Error("link login failed", "err", err)
		renderLoginError(w, "database error")
		return
	}

	// Heads-up email when a new sign-in method gets attached to an
	// existing account via the verified-email match path. Best-effort:
	// failures are logged, never blocking the sign-in flow itself.
	if outcome == store.LinkOutcomeCrossProvider {
		h.notifyIdentityLinked(r.Context(), user, p, profile.Email)
	}

	SetSessionCookie(w, h.SessionKey, user.ID, h.Secure)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (h *Handler) Logout(w http.ResponseWriter, _ *http.Request) {
	ClearSessionCookie(w, h.Secure)
	w.WriteHeader(http.StatusNoContent)
}

// exchangeCode trades an OAuth authorization code for an access token. Works
// against both GitHub (returns JSON when Accept is set) and Google.
func (h *Handler) exchangeCode(ctx context.Context, p *Provider, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", p.ClientID)
	form.Set("client_secret", p.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", h.redirectURL(p))
	form.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, "POST", p.TokenURL,
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
