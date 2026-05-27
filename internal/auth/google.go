package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/dpage/aerly/internal/store"
)

const (
	googleAuthURL    = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL   = "https://oauth2.googleapis.com/token"
	googleUserAPIURL = "https://openidconnect.googleapis.com/v1/userinfo"
)

// NewGoogleProvider returns a Provider configured for Google's OAuth 2.0 /
// OpenID Connect flow. The "openid email profile" scope is the minimum that
// gets us a stable subject ID, a verified email, and the user's display name.
func NewGoogleProvider(clientID, clientSecret string) *Provider {
	return &Provider{
		Name:         "google",
		Label:        "Google",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AuthURL:      googleAuthURL,
		TokenURL:     googleTokenURL,
		Scopes:       "openid email profile",
		FetchProfile: fetchGoogleProfile,
	}
}

func fetchGoogleProfile(ctx context.Context, client *http.Client, token string) (store.OAuthProfile, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", googleUserAPIURL, nil)
	if err != nil {
		return store.OAuthProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return store.OAuthProfile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return store.OAuthProfile{}, fmt.Errorf("/userinfo %d: %s", resp.StatusCode, body)
	}
	// The OpenID Connect userinfo endpoint returns the standard claims.
	// "sub" is the stable per-user identifier; "email_verified" tells us
	// whether to trust the email (we drop it if not).
	var u struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return store.OAuthProfile{}, err
	}
	if u.Sub == "" {
		return store.OAuthProfile{}, fmt.Errorf("/userinfo returned empty sub")
	}
	p := store.OAuthProfile{
		Provider:       "google",
		ProviderUserID: u.Sub,
		// Google doesn't expose a stable username/handle, so we leave
		// Username empty. LinkLogin falls back to the email local-part
		// when bootstrapping the first user.
		Name:      u.Name,
		AvatarURL: u.Picture,
	}
	if u.EmailVerified {
		p.Email = u.Email
	}
	return p, nil
}
