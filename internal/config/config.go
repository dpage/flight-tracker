package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	ListenAddr     string
	PublicURL      string
	DatabaseURL    string
	GitHubID       string
	GitHubSecret   string
	SessionKey     []byte
	AeroAPIKey     string
	AeroAPIBase    string
	DevAuthBypass  bool
}

func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:    getenv("LISTEN_ADDR", ":8080"),
		PublicURL:     strings.TrimRight(getenv("PUBLIC_URL", "http://localhost:8080"), "/"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		GitHubID:      os.Getenv("GITHUB_CLIENT_ID"),
		GitHubSecret:  os.Getenv("GITHUB_CLIENT_SECRET"),
		AeroAPIKey:    os.Getenv("AEROAPI_KEY"),
		AeroAPIBase:   getenv("AEROAPI_BASE_URL", "https://aeroapi.flightaware.com/aeroapi"),
		DevAuthBypass: os.Getenv("DEV_AUTH_BYPASS") == "1",
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

// StubAeroAPI reports whether to use the in-memory stub backend (no real API
// calls). Useful for local development without an AeroAPI subscription.
func (c *Config) StubAeroAPI() bool {
	return c.AeroAPIKey == ""
}

func getenv(k, dflt string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return dflt
}
