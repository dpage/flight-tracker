package auth

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/dpage/aerly/internal/store"
)

// captureSender records every message sent through Handler.SendNotification
// so tests can assert that the cross-provider notification fires (and what
// it contains) without spawning a real sendmail.
type captureSender struct {
	mu       sync.Mutex
	calls    int
	lastTo   string
	lastFrom string
	lastBody string
}

func (c *captureSender) send(_ context.Context, _, envelopeSender, message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.lastFrom = envelopeSender
	c.lastBody = message
	return nil
}

func TestNotifyIdentityLinked_SkippedWhenFromAddressUnset(t *testing.T) {
	h, _ := newTestHandler(t)
	cap := &captureSender{}
	h.SendNotification = cap.send
	// MailFromAddress is intentionally left empty.
	user := &store.User{ID: 42, Username: "alice", Name: "Alice"}
	prov := h.providers["github"]
	h.notifyIdentityLinked(context.Background(), user, prov, "alice@example.com")
	if cap.calls != 0 {
		t.Errorf("expected 0 sends when MAIL_FROM_ADDRESS is unset, got %d", cap.calls)
	}
}

func TestNotifyIdentityLinked_SkippedWhenMatchEmailEmpty(t *testing.T) {
	h, _ := newTestHandler(t)
	cap := &captureSender{}
	h.SendNotification = cap.send
	h.MailFromAddress = "noreply@example.com"
	user := &store.User{ID: 42, Username: "alice"}
	prov := h.providers["github"]
	h.notifyIdentityLinked(context.Background(), user, prov, "")
	if cap.calls != 0 {
		t.Errorf("expected 0 sends when match email is empty, got %d", cap.calls)
	}
}

func TestNotifyIdentityLinked_SendsMessage(t *testing.T) {
	h, _ := newTestHandler(t)
	cap := &captureSender{}
	h.SendNotification = cap.send
	h.MailFromAddress = "noreply@aerly.test"
	h.SendmailPath = "/bin/true"
	user := &store.User{ID: 42, Username: "alice", Name: "Alice Example"}
	prov := h.providers["github"]
	h.notifyIdentityLinked(context.Background(), user, prov, "alice@example.com")
	if cap.calls != 1 {
		t.Fatalf("expected 1 send, got %d", cap.calls)
	}
	if cap.lastFrom != "noreply@aerly.test" {
		t.Errorf("envelope sender = %q, want noreply@aerly.test", cap.lastFrom)
	}
	body := cap.lastBody
	for _, want := range []string{
		"To: alice@example.com",
		"From: noreply@aerly.test",
		"Subject:",
		"sign-in method",
		"alice@example.com",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("message missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestBuildIdentityLinkedEmail_RendersBothBodies(t *testing.T) {
	msg := buildIdentityLinkedEmail(identityLinkedInput{
		FromAddr:      "noreply@aerly.test",
		ToAddr:        "alice@example.com",
		PublicURL:     "https://flights.example.com",
		UserName:      "",
		UserUsername:  "alice",
		ProviderLabel: "Google",
		MatchEmail:    "alice@example.com",
	})
	// Falls back to username when display name is empty.
	if !strings.Contains(msg, "Hi alice,") {
		t.Errorf("expected greeting to fall back to username; body:\n%s", msg)
	}
	// Both alternatives present.
	if !strings.Contains(msg, "Content-Type: text/plain") || !strings.Contains(msg, "Content-Type: text/html") {
		t.Errorf("expected both plain and html alternatives; body:\n%s", msg)
	}
	// Mentions the linked provider in the body.
	if !strings.Contains(msg, "Google") {
		t.Errorf("expected provider label in body; body:\n%s", msg)
	}
}

func TestGreetName(t *testing.T) {
	if got := greetName("Alice", "alice"); got != "Alice" {
		t.Errorf("greetName(Alice, alice) = %q, want Alice", got)
	}
	if got := greetName("   ", "alice"); got != "alice" {
		t.Errorf("greetName(whitespace, alice) = %q, want alice", got)
	}
}

func TestValidateHeaderAddress(t *testing.T) {
	for _, c := range []struct {
		in      string
		wantErr bool
	}{
		{"alice@example.com", false},
		{"Alice <alice@example.com>", false},
		{"", true},
		{"alice@example.com\r\nBcc: attacker@evil", true},
		{"alice@example.com\nX-Injected: bad", true},
		{"not an address", true},
	} {
		err := validateHeaderAddress(c.in)
		if c.wantErr && err == nil {
			t.Errorf("validateHeaderAddress(%q) = nil, want error", c.in)
		}
		if !c.wantErr && err != nil {
			t.Errorf("validateHeaderAddress(%q) = %v, want nil", c.in, err)
		}
	}
}

func TestNotifyIdentityLinked_SkippedOnInvalidFromAddress(t *testing.T) {
	h, _ := newTestHandler(t)
	cap := &captureSender{}
	h.SendNotification = cap.send
	// Header-injection attempt — must be rejected before send.
	h.MailFromAddress = "noreply@aerly.test\r\nBcc: attacker@evil"
	user := &store.User{ID: 42, Username: "alice"}
	prov := h.providers["github"]
	h.notifyIdentityLinked(context.Background(), user, prov, "alice@example.com")
	if cap.calls != 0 {
		t.Errorf("expected 0 sends when From address is malformed, got %d", cap.calls)
	}
}

func TestNotifyIdentityLinked_SkippedOnInvalidRecipientAddress(t *testing.T) {
	h, _ := newTestHandler(t)
	cap := &captureSender{}
	h.SendNotification = cap.send
	h.MailFromAddress = "noreply@aerly.test"
	user := &store.User{ID: 42, Username: "alice"}
	prov := h.providers["github"]
	h.notifyIdentityLinked(context.Background(), user, prov, "alice@example.com\r\nBcc: attacker@evil")
	if cap.calls != 0 {
		t.Errorf("expected 0 sends when recipient is malformed, got %d", cap.calls)
	}
}
