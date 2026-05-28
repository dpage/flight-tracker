package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dpage/aerly/internal/testsupport"
)

func TestDevSyntheticIDStable(t *testing.T) {
	a := devSyntheticID("Alice")
	b := devSyntheticID("alice") // case-insensitive
	c := devSyntheticID("bob")
	if a == 0 {
		t.Errorf("synthetic id should be non-zero, got %d", a)
	}
	if a != b {
		t.Errorf("synthetic id should be case-insensitive: %d vs %d", a, b)
	}
	if a == c {
		t.Error("different logins should yield different ids")
	}
}

func TestRegisterDevLoginRoute(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterDevLogin(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/auth/dev-login?login=alice", nil))
	if w.Code != http.StatusFound {
		t.Errorf("dev-login code = %d, want 302", w.Code)
	}
}

func TestDevInfoRoute(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterDevLogin(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/auth/dev-info", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("dev-info code = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("dev-info Content-Type = %q, want application/json", got)
	}
	if body := strings.TrimSpace(w.Body.String()); body != `{"enabled":true}` {
		t.Errorf("dev-info body = %q, want %q", body, `{"enabled":true}`)
	}
}

func TestDevLoginMissingLogin(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	h.devLogin(w, httptest.NewRequest("GET", "/auth/dev-login", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", w.Code)
	}
}

func TestDevLoginBootstrapsAndSetsSession(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	h.devLogin(w, httptest.NewRequest("GET", "/auth/dev-login?login=firstuser", nil))
	res := w.Result()
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/" {
		t.Fatalf("expected redirect to /, got %d", res.StatusCode)
	}
	var sawSession bool
	for _, c := range res.Cookies() {
		if c.Name == SessionCookie && c.Value != "" {
			sawSession = true
		}
	}
	if !sawSession {
		t.Error("expected session cookie")
	}
}

func TestDevLoginOpenSignup(t *testing.T) {
	h, pool := newTestHandler(t)
	// Seed a user so the new sign-in isn't the bootstrap-superuser path.
	testsupport.InsertUser(t, pool, "existing", false, true)
	w := httptest.NewRecorder()
	h.devLogin(w, httptest.NewRequest("GET", "/auth/dev-login?login=stranger", nil))
	// Open signups: the unknown login should be accepted and a session
	// cookie issued, just like a normal first-time OAuth sign-in.
	if w.Code != http.StatusFound {
		t.Errorf("code = %d, want 302 (redirect on success)", w.Code)
	}
	var sawSession bool
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookie && c.Value != "" {
			sawSession = true
		}
	}
	if !sawSession {
		t.Error("expected session cookie")
	}
}
