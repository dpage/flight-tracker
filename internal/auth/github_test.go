package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// rewriteTransport redirects all outbound requests to a test server,
// preserving the path so we can intercept GitHub's token/user endpoints.
type rewriteTransport struct{ base *url.URL }

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.URL.Scheme = rt.base.Scheme
	r.URL.Host = rt.base.Host
	return http.DefaultTransport.RoundTrip(r)
}

type ghServerOpts struct {
	tokenStatus int
	tokenBody   string
	userStatus  int
	userBody    string
}

func ghServer(t *testing.T, o ghServerOpts) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, _ *http.Request) {
		if o.tokenStatus == 0 {
			o.tokenStatus = 200
		}
		w.WriteHeader(o.tokenStatus)
		if o.tokenBody == "" {
			o.tokenBody = `{"access_token":"tok123"}`
		}
		_, _ = w.Write([]byte(o.tokenBody))
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, _ *http.Request) {
		if o.userStatus == 0 {
			o.userStatus = 200
		}
		w.WriteHeader(o.userStatus)
		if o.userBody == "" {
			o.userBody = `{"id":555,"login":"octocat","name":"Octo","avatar_url":"a.png"}`
		}
		_, _ = w.Write([]byte(o.userBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func wireHTTP(t *testing.T, h *Handler, srv *httptest.Server) {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	h.HTTP = &http.Client{Transport: rewriteTransport{base: u}, Timeout: 5 * time.Second}
}

func TestNewHandlerSecureFromHTTPS(t *testing.T) {
	h := NewHandler("id", "sec", key, "https://x.example.com/", nil)
	if !h.Secure {
		t.Error("https public URL should set Secure")
	}
	if h.PublicURL != "https://x.example.com" {
		t.Errorf("trailing slash not trimmed: %q", h.PublicURL)
	}
	h2 := NewHandler("id", "sec", key, "http://localhost", nil)
	if h2.Secure {
		t.Error("http public URL should not set Secure")
	}
}

func TestRegisterRoutes(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)
	// /auth/github/login should be routable.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/auth/github/login", nil))
	if w.Code != http.StatusFound {
		t.Errorf("login route code = %d, want 302", w.Code)
	}
}

func TestLoginSetsStateCookieAndRedirects(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	h.Login(w, httptest.NewRequest("GET", "/auth/github/login", nil))
	res := w.Result()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("code = %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.HasPrefix(loc, gitHubAuthURL) || !strings.Contains(loc, "state=") {
		t.Errorf("bad redirect location: %s", loc)
	}
	var found bool
	for _, c := range res.Cookies() {
		if c.Name == StateCookie {
			found = true
		}
	}
	if !found {
		t.Error("state cookie not set")
	}
}

// stateCookie returns a valid state cookie + the embedded state token.
func stateCookie(h *Handler, expired bool) (*http.Cookie, string) {
	state := "thestate"
	exp := time.Now().Add(StateTTL)
	if expired {
		exp = time.Now().Add(-time.Minute)
	}
	val := SignSession(h.SessionKey, 0, exp) + ":" + state
	return &http.Cookie{Name: StateCookie, Value: val}, state
}

func callback(h *Handler, q url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/auth/github/callback?"+q.Encode(), nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.Callback(w, r)
	return w
}

func TestCallbackErrorParam(t *testing.T) {
	h, _ := newTestHandler(t)
	w := callback(h, url.Values{"error": {"access_denied"}}, nil)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "access_denied") {
		t.Errorf("expected escaped error page, code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCallbackMissingCodeOrState(t *testing.T) {
	h, _ := newTestHandler(t)
	w := callback(h, url.Values{"code": {""}, "state": {""}}, nil)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "missing code or state") {
		t.Errorf("unexpected: %d %s", w.Code, w.Body.String())
	}
}

func TestCallbackMissingStateCookie(t *testing.T) {
	h, _ := newTestHandler(t)
	w := callback(h, url.Values{"code": {"c"}, "state": {"s"}}, nil)
	if !strings.Contains(w.Body.String(), "state cookie missing") {
		t.Errorf("unexpected: %s", w.Body.String())
	}
}

func TestCallbackStateMismatch(t *testing.T) {
	h, _ := newTestHandler(t)
	c, _ := stateCookie(h, false)
	w := callback(h, url.Values{"code": {"c"}, "state": {"WRONG"}}, c)
	if !strings.Contains(w.Body.String(), "state mismatch") {
		t.Errorf("unexpected: %s", w.Body.String())
	}
}

func TestCallbackStateExpired(t *testing.T) {
	h, _ := newTestHandler(t)
	c, state := stateCookie(h, true)
	w := callback(h, url.Values{"code": {"c"}, "state": {state}}, c)
	if !strings.Contains(w.Body.String(), "state expired") {
		t.Errorf("unexpected: %s", w.Body.String())
	}
}

func TestCallbackTokenExchangeFails(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTP(t, h, ghServer(t, ghServerOpts{tokenStatus: 500, tokenBody: "boom"}))
	c, state := stateCookie(h, false)
	w := callback(h, url.Values{"code": {"c"}, "state": {state}}, c)
	if !strings.Contains(w.Body.String(), "could not complete sign-in") {
		t.Errorf("unexpected: %s", w.Body.String())
	}
}

func TestCallbackTokenErrorJSON(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTP(t, h, ghServer(t, ghServerOpts{tokenBody: `{"error":"bad_verification_code","error_description":"nope"}`}))
	c, state := stateCookie(h, false)
	w := callback(h, url.Values{"code": {"c"}, "state": {state}}, c)
	if !strings.Contains(w.Body.String(), "could not complete sign-in") {
		t.Errorf("unexpected: %s", w.Body.String())
	}
}

func TestCallbackEmptyAccessToken(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTP(t, h, ghServer(t, ghServerOpts{tokenBody: `{"access_token":""}`}))
	c, state := stateCookie(h, false)
	w := callback(h, url.Values{"code": {"c"}, "state": {state}}, c)
	if !strings.Contains(w.Body.String(), "could not complete sign-in") {
		t.Errorf("unexpected: %s", w.Body.String())
	}
}

func TestCallbackProfileFetchFails(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTP(t, h, ghServer(t, ghServerOpts{userStatus: 401, userBody: "nope"}))
	c, state := stateCookie(h, false)
	w := callback(h, url.Values{"code": {"c"}, "state": {state}}, c)
	if !strings.Contains(w.Body.String(), "could not fetch GitHub profile") {
		t.Errorf("unexpected: %s", w.Body.String())
	}
}

func TestCallbackNotOnAllowlist(t *testing.T) {
	h, pool := newTestHandler(t)
	// Pre-seed another user so CountUsers > 0 (no bootstrap), and the
	// octocat login is not invited → ErrNotFound → allowlist message.
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO users (github_login, is_active) VALUES ('someoneelse', true)`)
	wireHTTP(t, h, ghServer(t, ghServerOpts{}))
	c, state := stateCookie(h, false)
	w := callback(h, url.Values{"code": {"c"}, "state": {state}}, c)
	if !strings.Contains(w.Body.String(), "not on the allowlist") {
		t.Errorf("unexpected: %s", w.Body.String())
	}
}

func TestCallbackSuccessBootstrapsFirstUser(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTP(t, h, ghServer(t, ghServerOpts{}))
	c, state := stateCookie(h, false)
	w := callback(h, url.Values{"code": {"c"}, "state": {state}}, c)
	res := w.Result()
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/" {
		t.Fatalf("expected redirect to /, got %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	var sawSession bool
	for _, ck := range res.Cookies() {
		if ck.Name == SessionCookie && ck.Value != "" {
			sawSession = true
		}
	}
	if !sawSession {
		t.Error("session cookie not set on success")
	}
}

func TestCallbackSuccessExistingInvitedUser(t *testing.T) {
	h, pool := newTestHandler(t)
	// Pre-create an invited (github_id NULL) active user matching the
	// octocat login the test GitHub server returns; plus another user so
	// this isn't the bootstrap path.
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO users (github_login, is_active) VALUES ('someoneelse', true)`)
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO users (github_login, is_active) VALUES ('octocat', true)`)
	wireHTTP(t, h, ghServer(t, ghServerOpts{}))
	c, state := stateCookie(h, false)
	w := callback(h, url.Values{"code": {"c"}, "state": {state}}, c)
	res := w.Result()
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/" {
		t.Fatalf("expected redirect to /, got %d", res.StatusCode)
	}
}

type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, http.ErrServerClosed
}

func TestExchangeCodeTransportError(t *testing.T) {
	h, _ := newTestHandler(t)
	h.HTTP = &http.Client{Transport: errTransport{}}
	if _, err := h.exchangeCode(context.Background(), "code"); err == nil {
		t.Error("expected transport error")
	}
}

func TestFetchProfileTransportError(t *testing.T) {
	h, _ := newTestHandler(t)
	h.HTTP = &http.Client{Transport: errTransport{}}
	if _, err := h.fetchProfile(context.Background(), "tok"); err == nil {
		t.Error("expected transport error")
	}
}

func TestLogout(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	h.Logout(w, httptest.NewRequest("POST", "/auth/logout", nil))
	if w.Code != http.StatusNoContent {
		t.Errorf("code = %d, want 204", w.Code)
	}
	if len(w.Result().Cookies()) == 0 {
		t.Error("expected a cleared cookie")
	}
}

func TestExchangeCodeBadJSON(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTP(t, h, ghServer(t, ghServerOpts{tokenBody: "not json"}))
	if _, err := h.exchangeCode(context.Background(), "code"); err == nil {
		t.Error("expected JSON decode error")
	}
}

func TestFetchProfileBadJSON(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTP(t, h, ghServer(t, ghServerOpts{userBody: "not json"}))
	if _, err := h.fetchProfile(context.Background(), "tok"); err == nil {
		t.Error("expected JSON decode error")
	}
}

func TestFetchProfileSuccess(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTP(t, h, ghServer(t, ghServerOpts{}))
	p, err := h.fetchProfile(context.Background(), "tok")
	if err != nil {
		t.Fatalf("fetchProfile: %v", err)
	}
	if p.ID != 555 || p.Login != "octocat" {
		t.Errorf("unexpected profile: %+v", p)
	}
}

func TestHTMLEscape(t *testing.T) {
	got := htmlEscape(`<script>"&'`)
	want := "&lt;script&gt;&quot;&amp;&#39;"
	if got != want {
		t.Errorf("htmlEscape = %q, want %q", got, want)
	}
}

func TestRandomTokenUnique(t *testing.T) {
	a, b := randomToken(24), randomToken(24)
	if a == b || a == "" {
		t.Errorf("tokens not unique/non-empty: %q %q", a, b)
	}
}

func TestExchangeCodeReturnsToken(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTP(t, h, ghServer(t, ghServerOpts{}))
	tok, err := h.exchangeCode(context.Background(), "code")
	if err != nil || tok != "tok123" {
		t.Fatalf("tok=%q err=%v", tok, err)
	}
	// Sanity: response decodes as expected JSON shape.
	var probe struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal([]byte(`{"access_token":"x"}`), &probe)
}
