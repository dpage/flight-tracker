package emailingest

import (
	"context"
	"strings"
	"testing"
)

func TestBuildReply_AllAdded(t *testing.T) {
	in := ReplyInput{
		FromAddr:  "flights@flights.example",
		ToAddr:    "devrim@example.com",
		InReplyTo: "<msg1@example.com>",
		Subject:   "Fwd: TK1980 confirmation",
		Added:     []ReplyLeg{{Ident: "TK1980", Date: "2026-06-12"}},
		PublicURL: "https://flights.example",
	}
	body := BuildReply(in)
	for _, want := range []string{
		"From: flights@flights.example",
		"To: devrim@example.com",
		"In-Reply-To: <msg1@example.com>",
		"References: <msg1@example.com>",
		"Subject: Re: Fwd: TK1980 confirmation",
		"TK1980 on 2026-06-12",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n%s", want, body)
		}
	}
}

func TestBuildReply_PartialFailure(t *testing.T) {
	in := ReplyInput{
		FromAddr: "flights@flights.example", ToAddr: "u@x", PublicURL: "https://flights.example/",
		Added:  []ReplyLeg{{Ident: "TK1980", Date: "2026-06-12"}},
		Failed: []ReplyFailure{{Ident: "XX9999", Date: "2026-06-13", Reason: "no schedule"}},
	}
	body := BuildReply(in)
	if !strings.Contains(body, "TK1980 on 2026-06-12") {
		t.Error("missing success line")
	}
	if !strings.Contains(body, "XX9999 on 2026-06-13 — no schedule") {
		t.Error("missing failure line")
	}
	// Trailing slash on PublicURL must not be doubled.
	if strings.Contains(body, "flights.example//") {
		t.Error("PublicURL trailing slash doubled")
	}
}

func TestBuildReply_AllFailed(t *testing.T) {
	in := ReplyInput{
		FromAddr: "x", ToAddr: "y", PublicURL: "https://flights.example",
		Failed: []ReplyFailure{{Ident: "XX9", Date: "2026-06-13", Reason: "nope"}},
	}
	body := BuildReply(in)
	if !strings.Contains(body, "couldn't add any of the flights") {
		t.Errorf("missing all-failed lead-in: %s", body)
	}
}

func TestBuildReply_NothingFound(t *testing.T) {
	in := ReplyInput{
		FromAddr: "flights@flights.example", ToAddr: "u@x", PublicURL: "https://flights.example",
	}
	body := BuildReply(in)
	if !strings.Contains(body, "couldn't find any flight") {
		t.Errorf("missing fallback copy, got: %s", body)
	}
}

func TestBuildReply_SubjectAlreadyHasRe(t *testing.T) {
	in := ReplyInput{FromAddr: "x@y", ToAddr: "u@x", Subject: "Re: hello"}
	body := BuildReply(in)
	if strings.Contains(body, "Re: Re: hello") {
		t.Error("subject double-prefixed")
	}
	if !strings.Contains(body, "Subject: Re: hello") {
		t.Error("expected single Re: prefix")
	}
}

func TestBuildReply_EmptySubject(t *testing.T) {
	in := ReplyInput{FromAddr: "x@y", ToAddr: "u@x"}
	body := BuildReply(in)
	if !strings.Contains(body, "Subject: Re: Your forwarded flight email") {
		t.Errorf("missing fallback subject: %s", body)
	}
}

func TestSend_BinaryDoesNotExist(t *testing.T) {
	err := Send(context.Background(), "/tmp/does-not-exist-flight-tracker", "From: a\r\n\r\n")
	if err == nil {
		t.Error("expected error when sendmail binary doesn't exist")
	}
}
