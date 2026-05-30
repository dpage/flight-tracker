package mailer

import (
	"strings"
	"testing"
	"time"
)

func TestFlightAlertSubject(t *testing.T) {
	cases := map[string]string{
		"delayed":   "Your flight BA123 is now delayed",
		"cancelled": "Your flight BA123 has been cancelled",
		"diverted":  "Your flight BA123 has been diverted",
		"":          "Your flight BA123 is now delayed", // unknown kind → delayed
	}
	for kind, want := range cases {
		if got := FlightAlertSubject("BA123", kind); got != want {
			t.Errorf("kind %q: got %q, want %q", kind, got, want)
		}
	}
}

func TestBuildFlightAlertEmail_DelayHeadlineAndStructure(t *testing.T) {
	msg := BuildFlightAlertEmail(FlightAlertInput{
		FromAddr:  "alerts@aerly.test",
		ToAddr:    "owner@aerly.test",
		PublicURL: "http://localhost:8080",
		Ident:     "BA123",
		Kind:      "delayed",
		Detail:    "now departing 14:35 UTC",
		When:      time.Now(),
	})
	for _, want := range []string{
		"From: alerts@aerly.test",
		"To: owner@aerly.test",
		"MIME-Version: 1.0",
		"multipart/alternative",
		"Your flight BA123 is now delayed",
		"now departing 14:35 UTC",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q\n---\n%s", want, msg)
		}
	}
	// Subject is RFC-encoded but the plain-text part carries the headline.
	if !strings.Contains(msg, "Subject:") {
		t.Errorf("no Subject header")
	}
}

func TestBuildFlightAlertEmail_CancellationNoDetail(t *testing.T) {
	msg := BuildFlightAlertEmail(FlightAlertInput{
		FromAddr:  "a@x",
		ToAddr:    "b@x",
		PublicURL: "http://localhost:8080",
		Ident:     "LH9",
		Kind:      "cancelled",
	})
	if !strings.Contains(msg, "Your flight LH9 has been cancelled") {
		t.Errorf("cancellation headline missing:\n%s", msg)
	}
}
