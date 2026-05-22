package config

import (
	"strings"
	"testing"
	"time"
)

const goodKey = "0123456789abcdef0123456789abcdef" // 32 chars

// base sets a minimal valid environment; individual tests override.
func base(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("SESSION_KEY", goodKey)
	t.Setenv("GITHUB_CLIENT_ID", "id")
	t.Setenv("GITHUB_CLIENT_SECRET", "secret")
	t.Setenv("PUBLIC_URL", "https://flights.example.com/")
	t.Setenv("POLL_INTERVAL", "")
	t.Setenv("LISTEN_ADDR", "")
	t.Setenv("OPENSKY_USERNAME", "")
	t.Setenv("OPENSKY_PASSWORD", "")
	t.Setenv("OPENSKY_ENABLED", "")
	t.Setenv("AERODATABOX_RAPIDAPI_KEY", "")
	t.Setenv("DEV_AUTH_BYPASS", "")
}

func TestLoadSuccessDefaults(t *testing.T) {
	base(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("default ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.PublicURL != "https://flights.example.com" {
		t.Errorf("PublicURL trailing slash not trimmed: %q", cfg.PublicURL)
	}
	if cfg.PollInterval != 60*time.Second {
		t.Errorf("default PollInterval = %v", cfg.PollInterval)
	}
	if string(cfg.SessionKey) != goodKey {
		t.Errorf("SessionKey not set")
	}
}

func TestLoadCustomPollInterval(t *testing.T) {
	base(t)
	t.Setenv("POLL_INTERVAL", "5m")
	t.Setenv("LISTEN_ADDR", "127.0.0.1:9999")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PollInterval != 5*time.Minute {
		t.Errorf("PollInterval = %v", cfg.PollInterval)
	}
	if cfg.ListenAddr != "127.0.0.1:9999" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
}

func TestLoadBadPollInterval(t *testing.T) {
	base(t)
	t.Setenv("POLL_INTERVAL", "not-a-duration")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "POLL_INTERVAL") {
		t.Fatalf("expected POLL_INTERVAL parse error, got %v", err)
	}
}

func TestLoadNonPositivePollInterval(t *testing.T) {
	base(t)
	t.Setenv("POLL_INTERVAL", "0s")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("expected non-positive error, got %v", err)
	}
}

func TestLoadShortSessionKey(t *testing.T) {
	base(t)
	t.Setenv("SESSION_KEY", "tooshort")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SESSION_KEY") {
		t.Fatalf("expected SESSION_KEY error, got %v", err)
	}
}

func TestLoadMissingDatabaseURL(t *testing.T) {
	base(t)
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL missing error, got %v", err)
	}
}

func TestLoadMissingGitHubCreds(t *testing.T) {
	base(t)
	t.Setenv("GITHUB_CLIENT_ID", "")
	t.Setenv("GITHUB_CLIENT_SECRET", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "GITHUB_CLIENT_ID") ||
		!strings.Contains(err.Error(), "GITHUB_CLIENT_SECRET") {
		t.Fatalf("expected both GitHub creds missing, got %v", err)
	}
}

func TestLoadDevBypassSkipsGitHubCreds(t *testing.T) {
	base(t)
	t.Setenv("GITHUB_CLIENT_ID", "")
	t.Setenv("GITHUB_CLIENT_SECRET", "")
	t.Setenv("DEV_AUTH_BYPASS", "1")
	t.Setenv("PUBLIC_URL", "http://localhost:8080")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with dev bypass: %v", err)
	}
	if !cfg.DevAuthBypass {
		t.Error("DevAuthBypass should be true")
	}
}

func TestLoadDevBypassRequiresLocalhost(t *testing.T) {
	base(t)
	t.Setenv("DEV_AUTH_BYPASS", "1")
	t.Setenv("PUBLIC_URL", "https://prod.example.com")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "DEV_AUTH_BYPASS") {
		t.Fatalf("expected localhost guard error, got %v", err)
	}
}

func TestLoadDevBypassLoopbackIP(t *testing.T) {
	base(t)
	t.Setenv("DEV_AUTH_BYPASS", "1")
	t.Setenv("PUBLIC_URL", "http://127.0.0.1:8080")
	if _, err := Load(); err != nil {
		t.Fatalf("127.0.0.1 should be allowed for dev bypass: %v", err)
	}
}

func TestUseOpenSky(t *testing.T) {
	base(t)
	cfg, _ := Load()
	if cfg.UseOpenSky() {
		t.Error("UseOpenSky should be false with no creds and not enabled")
	}

	base(t)
	t.Setenv("OPENSKY_USERNAME", "user")
	cfg, _ = Load()
	if !cfg.UseOpenSky() {
		t.Error("UseOpenSky true when username set")
	}

	base(t)
	t.Setenv("OPENSKY_ENABLED", "1")
	cfg, _ = Load()
	if !cfg.UseOpenSky() {
		t.Error("UseOpenSky true when explicitly enabled")
	}
}

func TestResolverAvailable(t *testing.T) {
	base(t)
	cfg, _ := Load()
	if cfg.ResolverAvailable() {
		t.Error("ResolverAvailable should be false without key")
	}
	base(t)
	t.Setenv("AERODATABOX_RAPIDAPI_KEY", "k")
	cfg, _ = Load()
	if !cfg.ResolverAvailable() {
		t.Error("ResolverAvailable should be true with key")
	}
}

func emailIngestBase(t *testing.T) {
	t.Helper()
	base(t)
	t.Setenv("EMAIL_INGEST_ENABLED", "1")
	t.Setenv("EMAIL_INGEST_MAILDIR", "/var/spool/flight-tracker/Maildir")
	t.Setenv("EMAIL_INGEST_ADDRESS", "flights@flights.example")
	t.Setenv("LLM_API_KEY", "sk-test")
}

func TestLoadEmailIngestDisabled(t *testing.T) {
	base(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EmailIngestEnabled {
		t.Error("expected EmailIngestEnabled false by default")
	}
}

func TestLoadEmailIngestEnabledDefaults(t *testing.T) {
	emailIngestBase(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.EmailIngestEnabled {
		t.Error("expected EmailIngestEnabled true")
	}
	if cfg.EmailIngestPollInterval != 30*time.Second {
		t.Errorf("default poll = %v", cfg.EmailIngestPollInterval)
	}
	if !cfg.EmailIngestRequireDKIM {
		t.Error("RequireDKIM should default to true")
	}
	if cfg.EmailIngestMaxBodyBytes != 1<<20 {
		t.Errorf("default max body = %d", cfg.EmailIngestMaxBodyBytes)
	}
	if cfg.EmailIngestSendmail != "/usr/sbin/sendmail" {
		t.Errorf("default sendmail = %q", cfg.EmailIngestSendmail)
	}
	if cfg.LLMProvider != "anthropic" || cfg.LLMModel != "claude-haiku-4-5" {
		t.Errorf("default llm = %s/%s", cfg.LLMProvider, cfg.LLMModel)
	}
}

func TestLoadEmailIngestMissingRequired(t *testing.T) {
	emailIngestBase(t)
	t.Setenv("EMAIL_INGEST_MAILDIR", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EMAIL_INGEST_MAILDIR") {
		t.Errorf("expected MAILDIR required error, got %v", err)
	}
}

func TestLoadEmailIngestBadPollInterval(t *testing.T) {
	emailIngestBase(t)
	t.Setenv("EMAIL_INGEST_POLL_INTERVAL", "garbage")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EMAIL_INGEST_POLL_INTERVAL") {
		t.Errorf("expected POLL_INTERVAL error, got %v", err)
	}
}

func TestLoadEmailIngestBadMaxBytes(t *testing.T) {
	emailIngestBase(t)
	t.Setenv("EMAIL_INGEST_MAX_BODY_BYTES", "0")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MAX_BODY_BYTES") {
		t.Errorf("expected MAX_BODY_BYTES error, got %v", err)
	}
}

func TestLoadEmailIngestRequiresLLMKey(t *testing.T) {
	emailIngestBase(t)
	t.Setenv("LLM_API_KEY", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "LLM_API_KEY") {
		t.Errorf("expected LLM_API_KEY error, got %v", err)
	}
}

func TestLoadEmailIngestOllamaSkipsAPIKey(t *testing.T) {
	emailIngestBase(t)
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_PROVIDER", "ollama")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected ollama to skip API key check, got %v", err)
	}
	if cfg.LLMProvider != "ollama" {
		t.Errorf("LLMProvider = %q", cfg.LLMProvider)
	}
}

func TestLoadEmailIngestCustomMaxBytes(t *testing.T) {
	emailIngestBase(t)
	t.Setenv("EMAIL_INGEST_MAX_BODY_BYTES", "65536")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EmailIngestMaxBodyBytes != 65536 {
		t.Errorf("MaxBodyBytes = %d", cfg.EmailIngestMaxBodyBytes)
	}
}

func TestLoadEmailIngestRequireDKIMOff(t *testing.T) {
	emailIngestBase(t)
	t.Setenv("EMAIL_INGEST_REQUIRE_DKIM", "0")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EmailIngestRequireDKIM {
		t.Error("RequireDKIM should be false when env=0")
	}
}
