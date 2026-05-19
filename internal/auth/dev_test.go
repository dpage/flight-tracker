package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dpage/flight-tracker/internal/testsupport"
)

func TestDevSyntheticIDNegativeAndStable(t *testing.T) {
	a := devSyntheticID("Alice")
	b := devSyntheticID("alice") // case-insensitive
	c := devSyntheticID("bob")
	if a >= 0 {
		t.Errorf("synthetic id should be negative, got %d", a)
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

func TestDevLoginNotInvitedForbidden(t *testing.T) {
	h, pool := newTestHandler(t)
	// Seed a user so this is no longer the bootstrap (count > 0); an
	// uninvited login is then rejected with 403.
	testsupport.InsertUser(t, pool, "existing", false, true)
	w := httptest.NewRecorder()
	h.devLogin(w, httptest.NewRequest("GET", "/auth/dev-login?login=stranger", nil))
	if w.Code != http.StatusForbidden {
		t.Errorf("code = %d, want 403", w.Code)
	}
}
