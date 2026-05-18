package auth

import (
	"context"
	"net/http"

	"github.com/dpage/flight-tracker/internal/store"
)

type ctxKey int

const userCtxKey ctxKey = 1

// UserFrom returns the authenticated user attached by Require, or nil.
func UserFrom(ctx context.Context) *store.User {
	u, _ := ctx.Value(userCtxKey).(*store.User)
	return u
}

// Require wraps a handler, rejecting any request without a valid session.
// It loads the user from the database and attaches it to the request context.
func (h *Handler) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := h.userFromRequest(r)
		if u == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, u)))
	})
}

// RequireSuperuser wraps Require with an extra is_superuser check.
func (h *Handler) RequireSuperuser(next http.Handler) http.Handler {
	return h.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := UserFrom(r.Context()); u == nil || !u.IsSuperuser {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// Optional attaches the user to context if a valid session exists but does
// not reject anonymous requests.
func (h *Handler) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := h.userFromRequest(r); u != nil {
			r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) userFromRequest(r *http.Request) *store.User {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return nil
	}
	uid, err := VerifySession(h.SessionKey, c.Value)
	if err != nil {
		return nil
	}
	u, err := h.Store.UserByID(r.Context(), uid)
	if err != nil || !u.IsActive {
		return nil
	}
	return u
}
