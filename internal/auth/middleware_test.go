package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dpage/flight-tracker/internal/store"
	"github.com/dpage/flight-tracker/internal/testsupport"
)

func newTestHandler(t *testing.T) (*Handler, *pgxpool.Pool) {
	t.Helper()
	pool := testsupport.NewPool(t)
	s := store.New(pool)
	h := NewHandler("cid", "csecret", key, "http://localhost:8080", s)
	return h, pool
}

func sessionReq(t *testing.T, uid int64) *http.Request {
	t.Helper()
	r := httptest.NewRequest("GET", "/x", nil)
	r.AddCookie(&http.Cookie{
		Name:  SessionCookie,
		Value: SignSession(key, uid, time.Now().Add(time.Hour)),
	})
	return r
}

func TestUserFromEmpty(t *testing.T) {
	if u := UserFrom(context.Background()); u != nil {
		t.Errorf("expected nil user, got %+v", u)
	}
}

func TestRequireNoCookie(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	h.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next should not be called")
	})).ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", w.Code)
	}
}

func TestRequireBadCookie(t *testing.T) {
	h, _ := newTestHandler(t)
	r := httptest.NewRequest("GET", "/x", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "garbage"})
	w := httptest.NewRecorder()
	h.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", w.Code)
	}
}

func TestRequireInactiveUserRejected(t *testing.T) {
	h, pool := newTestHandler(t)
	id := testsupport.InsertUser(t, pool, "inactive", false, false)
	w := httptest.NewRecorder()
	h.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("inactive user must not pass")
	})).ServeHTTP(w, sessionReq(t, id))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", w.Code)
	}
}

func TestRequireValidUserPasses(t *testing.T) {
	h, pool := newTestHandler(t)
	id := testsupport.InsertUser(t, pool, "alice", false, true)
	var gotID int64
	w := httptest.NewRecorder()
	h.Require(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotID = UserFrom(r.Context()).ID
	})).ServeHTTP(w, sessionReq(t, id))
	if w.Code != http.StatusOK || gotID != id {
		t.Errorf("code=%d gotID=%d want 200/%d", w.Code, gotID, id)
	}
}

func TestRequireSuperuser(t *testing.T) {
	h, pool := newTestHandler(t)
	plain := testsupport.InsertUser(t, pool, "plain", false, true)
	super := testsupport.InsertUser(t, pool, "boss", true, true)

	w := httptest.NewRecorder()
	h.RequireSuperuser(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("non-superuser must be blocked")
	})).ServeHTTP(w, sessionReq(t, plain))
	if w.Code != http.StatusForbidden {
		t.Errorf("plain user: code=%d want 403", w.Code)
	}

	w = httptest.NewRecorder()
	called := false
	h.RequireSuperuser(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, sessionReq(t, super))
	if w.Code != http.StatusOK || !called {
		t.Errorf("superuser: code=%d called=%v", w.Code, called)
	}
}

func TestOptional(t *testing.T) {
	h, pool := newTestHandler(t)
	id := testsupport.InsertUser(t, pool, "opt", false, true)

	// Anonymous: passes through, no user.
	w := httptest.NewRecorder()
	sawUser := true
	h.Optional(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		sawUser = UserFrom(r.Context()) != nil
	})).ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	if w.Code != http.StatusOK || sawUser {
		t.Errorf("anon optional: code=%d sawUser=%v", w.Code, sawUser)
	}

	// Authenticated: user attached.
	w = httptest.NewRecorder()
	h.Optional(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if UserFrom(r.Context()) == nil {
			t.Error("expected user in context")
		}
	})).ServeHTTP(w, sessionReq(t, id))
}
