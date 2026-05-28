package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type googleServerOpts struct {
	userStatus int
	userBody   string
}

func googleServer(t *testing.T, o googleServerOpts) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/userinfo", func(w http.ResponseWriter, _ *http.Request) {
		if o.userStatus == 0 {
			o.userStatus = 200
		}
		w.WriteHeader(o.userStatus)
		if o.userBody == "" {
			o.userBody = `{"sub":"g-12345","email":"alice@example.com",
				"email_verified":true,"name":"Alice","picture":"pic.png"}`
		}
		_, _ = w.Write([]byte(o.userBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// wireHTTPForGoogle rewrites outbound requests to the test server so
// fetchGoogleProfile's hard-coded URL hits our handler.
func wireHTTPForGoogle(t *testing.T, h *Handler, srv *httptest.Server) {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	h.HTTP = &http.Client{Transport: rewriteTransport{base: u}, Timeout: 5 * time.Second}
}

func TestNewGoogleProviderShape(t *testing.T) {
	p := NewGoogleProvider("gid", "gsec")
	if p.Name != "google" || p.Label != "Google" {
		t.Errorf("unexpected name/label: %+v", p)
	}
	if !strings.Contains(p.Scopes, "openid") || !strings.Contains(p.Scopes, "email") {
		t.Errorf("scopes should include openid + email, got %q", p.Scopes)
	}
	if p.FetchProfile == nil {
		t.Error("FetchProfile must be set")
	}
}

func TestFetchGoogleProfileSuccess(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTPForGoogle(t, h, googleServer(t, googleServerOpts{}))
	p, err := fetchGoogleProfile(context.Background(), h.HTTP, "tok")
	if err != nil {
		t.Fatalf("fetchGoogleProfile: %v", err)
	}
	if p.Provider != "google" || p.ProviderUserID != "g-12345" {
		t.Errorf("unexpected identity: %+v", p)
	}
	if p.Email != "alice@example.com" || p.Name != "Alice" || p.AvatarURL != "pic.png" {
		t.Errorf("unexpected profile fields: %+v", p)
	}
	if p.Username != "" {
		t.Errorf("Google does not expose a username, got %q", p.Username)
	}
}

// Google asserts email_verified=false for unconfirmed addresses (e.g. a
// G Suite admin removed the verification). We must drop the email so the
// cross-provider email-match in LinkLogin doesn't trust it.
func TestFetchGoogleProfile_UnverifiedEmailDropped(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTPForGoogle(t, h, googleServer(t, googleServerOpts{
		userBody: `{"sub":"g-1","email":"unverified@example.com","email_verified":false,"name":"X"}`,
	}))
	p, err := fetchGoogleProfile(context.Background(), h.HTTP, "tok")
	if err != nil {
		t.Fatalf("fetchGoogleProfile: %v", err)
	}
	if p.Email != "" {
		t.Errorf("unverified email should be dropped, got %q", p.Email)
	}
}

func TestFetchGoogleProfile_EmptySubRejected(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTPForGoogle(t, h, googleServer(t, googleServerOpts{
		userBody: `{"sub":"","email":"x@y.z","email_verified":true}`,
	}))
	if _, err := fetchGoogleProfile(context.Background(), h.HTTP, "tok"); err == nil {
		t.Error("expected error for empty sub")
	}
}

func TestFetchGoogleProfile_HTTPError(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTPForGoogle(t, h, googleServer(t, googleServerOpts{
		userStatus: 401, userBody: "nope",
	}))
	if _, err := fetchGoogleProfile(context.Background(), h.HTTP, "tok"); err == nil {
		t.Error("expected error for 401")
	}
}

func TestFetchGoogleProfile_BadJSON(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTPForGoogle(t, h, googleServer(t, googleServerOpts{userBody: "not json"}))
	if _, err := fetchGoogleProfile(context.Background(), h.HTTP, "tok"); err == nil {
		t.Error("expected JSON decode error")
	}
}

func TestFetchGoogleProfile_TransportError(t *testing.T) {
	h, _ := newTestHandler(t)
	h.HTTP = &http.Client{Transport: errTransport{}}
	if _, err := fetchGoogleProfile(context.Background(), h.HTTP, "tok"); err == nil {
		t.Error("expected transport error")
	}
}
