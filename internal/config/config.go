package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	ListenAddr      string
	PublicURL       string
	DatabaseURL     string
	GitHubID        string
	GitHubSecret    string
	SessionKey      []byte
	OpenSkyUsername string
	OpenSkyPassword string
	OpenSkyEnabled  bool // true if we should query OpenSky even without creds
	PollInterval    time.Duration
	DevAuthBypass   bool
}

func Load() (*Config, error) {
	pollInterval, pollErr := time.ParseDuration(getenv("POLL_INTERVAL", "60s"))
	if pollErr != nil {
		return nil, fmt.Errorf("POLL_INTERVAL must be a positive duration (e.g. 60s, 5m): %w", pollErr)
	}
	if pollInterval <= 0 {
		return nil, fmt.Errorf("POLL_INTERVAL must be a positive duration (e.g. 60s, 5m)")
	}

	cfg := &Config{
		ListenAddr:      getenv("LISTEN_ADDR", ":8080"),
		PublicURL:       strings.TrimRight(getenv("PUBLIC_URL", "http://localhost:8080"), "/"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		GitHubID:        os.Getenv("GITHUB_CLIENT_ID"),
		GitHubSecret:    os.Getenv("GITHUB_CLIENT_SECRET"),
		OpenSkyUsername: os.Getenv("OPENSKY_USERNAME"),
		OpenSkyPassword: os.Getenv("OPENSKY_PASSWORD"),
		OpenSkyEnabled:  os.Getenv("OPENSKY_ENABLED") == "1",
		PollInterval:    pollInterval,
		DevAuthBypass:   os.Getenv("DEV_AUTH_BYPASS") == "1",
	}

	sessKey := os.Getenv("SESSION_KEY")
	if len(sessKey) < 32 {
		return nil, fmt.Errorf("SESSION_KEY must be set to at least 32 chars (got %d)", len(sessKey))
	}
	cfg.SessionKey = []byte(sessKey)

	var missing []string
	if cfg.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if !cfg.DevAuthBypass {
		if cfg.GitHubID == "" {
			missing = append(missing, "GITHUB_CLIENT_ID")
		}
		if cfg.GitHubSecret == "" {
			missing = append(missing, "GITHUB_CLIENT_SECRET")
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}
	if cfg.DevAuthBypass && !strings.HasPrefix(cfg.PublicURL, "http://localhost") &&
		!strings.HasPrefix(cfg.PublicURL, "http://127.0.0.1") {
		return nil, fmt.Errorf("DEV_AUTH_BYPASS may only be used with a localhost PUBLIC_URL (got %q)", cfg.PublicURL)
	}
	return cfg, nil
}

// UseOpenSky reports whether the OpenSky tracker should be used. We turn it
// on whenever OpenSky credentials are configured, or whenever the operator
// explicitly opts into anonymous OpenSky (heavily rate-limited).
func (c *Config) UseOpenSky() bool {
	return c.OpenSkyUsername != "" || c.OpenSkyEnabled
}

// ResolverAvailable reports whether a Resolver is wired — i.e. whether the
// frontend can offer the minimal "ident + date" Add Flight dialog. No
// Resolver is implemented yet, so this is always false until one lands.
func (c *Config) ResolverAvailable() bool {
	return false
}

func getenv(k, dflt string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return dflt
}
