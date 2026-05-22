package config

import (
	"fmt"
	"os"
	"strconv"
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
	AeroDataBoxKey  string
	PollInterval    time.Duration
	DevAuthBypass   bool

	// Email ingest (optional). All EmailIngest* fields are zero when
	// EmailIngestEnabled is false. When enabled, the rest are populated
	// from env vars with the defaults documented in README.
	EmailIngestEnabled      bool
	EmailIngestMaildir      string
	EmailIngestAddress      string
	EmailIngestPollInterval time.Duration
	EmailIngestRequireDKIM  bool
	EmailIngestMaxBodyBytes int
	EmailIngestSendmail     string
	LLMProvider             string
	LLMModel                string
	LLMAPIKey               string
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
		AeroDataBoxKey:  os.Getenv("AERODATABOX_RAPIDAPI_KEY"),
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

	cfg.EmailIngestEnabled = os.Getenv("EMAIL_INGEST_ENABLED") == "1"
	if cfg.EmailIngestEnabled {
		cfg.EmailIngestMaildir = os.Getenv("EMAIL_INGEST_MAILDIR")
		cfg.EmailIngestAddress = os.Getenv("EMAIL_INGEST_ADDRESS")
		if cfg.EmailIngestMaildir == "" || cfg.EmailIngestAddress == "" {
			return nil, fmt.Errorf("EMAIL_INGEST_ENABLED=1 requires EMAIL_INGEST_MAILDIR and EMAIL_INGEST_ADDRESS")
		}
		pi, err := time.ParseDuration(getenv("EMAIL_INGEST_POLL_INTERVAL", "30s"))
		if err != nil || pi <= 0 {
			return nil, fmt.Errorf("EMAIL_INGEST_POLL_INTERVAL must be a positive duration")
		}
		cfg.EmailIngestPollInterval = pi
		cfg.EmailIngestRequireDKIM = getenv("EMAIL_INGEST_REQUIRE_DKIM", "1") == "1"
		cfg.EmailIngestMaxBodyBytes = 1 << 20
		if v := os.Getenv("EMAIL_INGEST_MAX_BODY_BYTES"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("EMAIL_INGEST_MAX_BODY_BYTES must be a positive integer")
			}
			cfg.EmailIngestMaxBodyBytes = n
		}
		cfg.EmailIngestSendmail = getenv("EMAIL_INGEST_SENDMAIL", "/usr/sbin/sendmail")
		cfg.LLMProvider = getenv("LLM_PROVIDER", "anthropic")
		cfg.LLMModel = getenv("LLM_MODEL", "claude-haiku-4-5")
		cfg.LLMAPIKey = os.Getenv("LLM_API_KEY")
		if cfg.LLMProvider != "ollama" && cfg.LLMAPIKey == "" {
			return nil, fmt.Errorf("LLM_API_KEY required when EMAIL_INGEST_ENABLED=1 and LLM_PROVIDER != ollama")
		}
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
// frontend can offer the minimal "ident + date" Add Flight dialog.
func (c *Config) ResolverAvailable() bool {
	return c.AeroDataBoxKey != ""
}

func getenv(k, dflt string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return dflt
}
