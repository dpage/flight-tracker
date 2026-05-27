package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
)

func newAuthHandler(t *testing.T) (*Handler, *store.Store) {
	t.Helper()
	pool := testsupport.NewPool(t)
	s := store.New(pool)
	return NewHandler([]byte("verify-email-test-session-key!!!!!"), "http://localhost:8080", s), s
}

func TestVerifyEmail_HappyPath(t *testing.T) {
	h, s := newAuthHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)

	u, err := s.InviteUser(context.Background(), store.InvitePayload{Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := s.InsertUnverifiedEmail(context.Background(), u.ID, "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/auth/verify-email?token="+token, nil)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "verified") {
		t.Errorf("body should confirm verification, got: %s", w.Body.String())
	}
	got, _ := s.UserByVerifiedEmail(context.Background(), "alice@example.com")
	if got == nil || got.ID != u.ID {
		t.Errorf("row not verified: %+v", got)
	}
}

func TestVerifyEmail_BadToken(t *testing.T) {
	h, _ := newAuthHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/auth/verify-email?token=nope", nil)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(strings.ToLower(w.Body.String()), "expired") &&
		!strings.Contains(strings.ToLower(w.Body.String()), "invalid") {
		t.Errorf("body should mention expired/invalid, got: %s", w.Body.String())
	}
}

func TestVerifyEmail_MissingToken(t *testing.T) {
	h, _ := newAuthHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/auth/verify-email", nil)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
