package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/dpage/aerly/internal/store"
)

const (
	gitHubAuthURL      = "https://github.com/login/oauth/authorize"
	gitHubTokenURL     = "https://github.com/login/oauth/access_token"
	gitHubUserAPIURL   = "https://api.github.com/user"
	gitHubEmailsAPIURL = "https://api.github.com/user/emails"
)

// NewGitHubProvider returns a Provider configured for GitHub. The
// "user:email" scope is what unlocks the /user/emails endpoint we use to
// match forwarded mail to the signed-in user.
func NewGitHubProvider(clientID, clientSecret string) *Provider {
	return &Provider{
		Name:         "github",
		Label:        "GitHub",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AuthURL:      gitHubAuthURL,
		TokenURL:     gitHubTokenURL,
		Scopes:       "read:user user:email",
		FetchProfile: fetchGitHubProfile,
	}
}

func fetchGitHubProfile(ctx context.Context, client *http.Client, token string) (store.OAuthProfile, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", gitHubUserAPIURL, nil)
	if err != nil {
		return store.OAuthProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "aerly")

	resp, err := client.Do(req)
	if err != nil {
		return store.OAuthProfile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return store.OAuthProfile{}, fmt.Errorf("/user %d: %s", resp.StatusCode, body)
	}
	var u struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return store.OAuthProfile{}, err
	}
	p := store.OAuthProfile{
		Provider:       "github",
		ProviderUserID: strconv.FormatInt(u.ID, 10),
		Username:       u.Login,
		Name:           u.Name,
		AvatarURL:      u.AvatarURL,
	}
	// Best-effort email fetch. Tolerated on failure: the user can sign in
	// without an email, they just won't be able to use the forwarded-email
	// ingest until they add one manually.
	if email, eerr := fetchGitHubPrimaryEmail(ctx, client, token); eerr != nil {
		slog.Warn("fetch github primary email failed", "err", eerr)
	} else {
		p.Email = email
	}
	return p, nil
}

// fetchGitHubPrimaryEmail returns the user's primary verified GitHub email,
// or "" if none is set / GitHub returned an empty list. An error is returned
// only on transport or decode failures.
func fetchGitHubPrimaryEmail(ctx context.Context, client *http.Client, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", gitHubEmailsAPIURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "aerly")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("/user/emails %d: %s", resp.StatusCode, body)
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", err
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}
	return "", nil
}
