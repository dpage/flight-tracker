// Package auth handles GitHub OAuth sign-in and HMAC-signed session cookies.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	SessionCookie = "flight_session"
	StateCookie   = "flight_oauth_state"
	SessionTTL    = 30 * 24 * time.Hour
	StateTTL      = 10 * time.Minute
)

var ErrInvalidSession = errors.New("invalid session")

// SignSession returns a cookie value of the form v1.<uid>.<expUnix>.<sig>.
func SignSession(key []byte, userID int64, expires time.Time) string {
	body := fmt.Sprintf("v1.%d.%d", userID, expires.Unix())
	return body + "." + sign(key, body)
}

// VerifySession parses a cookie value and returns the user ID if the signature
// is valid and the expiry is in the future.
func VerifySession(key []byte, raw string) (int64, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 4 || parts[0] != "v1" {
		return 0, ErrInvalidSession
	}
	body := strings.Join(parts[:3], ".")
	if !hmac.Equal([]byte(parts[3]), []byte(sign(key, body))) {
		return 0, ErrInvalidSession
	}
	expUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, ErrInvalidSession
	}
	if time.Now().Unix() > expUnix {
		return 0, ErrInvalidSession
	}
	uid, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, ErrInvalidSession
	}
	return uid, nil
}

// SetSessionCookie writes a session cookie that expires in SessionTTL.
func SetSessionCookie(w http.ResponseWriter, key []byte, userID int64, secure bool) {
	expires := time.Now().Add(SessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    SignSession(key, userID, expires),
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie invalidates the cookie on the client.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func sign(key []byte, body string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
