package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

func TestEscapeText(t *testing.T) {
	cases := map[string]string{
		`a,b;c\d`:    `a\,b\;c\\d`,
		"line1\nl2":  `line1\nl2`,
		"crlf\r\nx":  `crlf\nx`,
		"plain text": "plain text",
	}
	for in, want := range cases {
		if got := escapeText(in); got != want {
			t.Errorf("escapeText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatTZOffset(t *testing.T) {
	cases := map[int]string{
		0:      "+0000",
		3600:   "+0100",
		-18000: "-0500",
		19800:  "+0530",
	}
	for secs, want := range cases {
		if got := formatTZOffset(secs); got != want {
			t.Errorf("formatTZOffset(%d) = %q, want %q", secs, got, want)
		}
	}
}

func TestWriteLineFolding(t *testing.T) {
	var b strings.Builder
	long := "SUMMARY:" + strings.Repeat("x", 200)
	writeLine(&b, long)
	out := b.String()
	for _, line := range strings.Split(strings.TrimRight(out, "\r\n"), "\r\n") {
		// Continuation lines start with a space; none may exceed 75 octets.
		if len(line) > 75 {
			t.Errorf("folded line exceeds 75 octets: %d %q", len(line), line)
		}
	}
	// Unfolding (strip CRLF + leading space) must restore the original.
	unfolded := strings.ReplaceAll(out, "\r\n ", "")
	unfolded = strings.TrimRight(unfolded, "\r\n")
	if unfolded != long {
		t.Errorf("unfold mismatch:\n got %q\nwant %q", unfolded, long)
	}
}

func TestRenderICSStructure(t *testing.T) {
	end := time.Date(2026, 7, 1, 14, 30, 0, 0, time.UTC)
	events := []*store.CalendarEvent{
		{
			PartID:          7,
			PlanID:          3,
			Type:            "flight",
			Title:           "BA286",
			ConfirmationRef: "ABC123",
			Notes:           "window; seat",
			StartsAt:        time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
			EndsAt:          &end,
			StartTZ:         "Europe/London",
			EndTZ:           "America/New_York",
			StartLabel:      "LHR",
			Status:          "confirmed",
			UpdatedAt:       time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	out := renderICS("Aerly", events)

	mustContain := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:" + icsProdID,
		"BEGIN:VTIMEZONE",
		"TZID:Europe/London",
		"TZID:America/New_York",
		"BEGIN:VEVENT",
		"UID:plan-part-7@aerly",
		"DTSTART;TZID=Europe/London:20260701T100000",  // BST = UTC+1
		"DTEND;TZID=America/New_York:20260701T103000", // EDT = UTC-4
		"SUMMARY:BA286 (Flight)",
		"LOCATION:LHR",
		"STATUS:CONFIRMED",
		"END:VEVENT",
		"END:VCALENDAR",
	}
	for _, m := range mustContain {
		if !strings.Contains(out, m) {
			t.Errorf("ICS output missing %q\n---\n%s", m, out)
		}
	}
	// Confirmation + notes folded into DESCRIPTION, with escaping.
	if !strings.Contains(out, "DESCRIPTION:Confirmation: ABC123\\nwindow\\; seat") {
		t.Errorf("DESCRIPTION wrong/unescaped:\n%s", out)
	}
	// CRLF line endings.
	if !strings.Contains(out, "\r\n") {
		t.Error("ICS must use CRLF line endings")
	}
}

func TestRenderICSNoTZFallsBackToUTC(t *testing.T) {
	events := []*store.CalendarEvent{{
		PartID:    1,
		Type:      "dining",
		Title:     "Dinner",
		StartsAt:  time.Date(2026, 7, 1, 19, 0, 0, 0, time.UTC),
		UpdatedAt: time.Now(),
	}}
	out := renderICS("Aerly", events)
	if !strings.Contains(out, "DTSTART:20260701T190000Z") {
		t.Errorf("expected UTC DTSTART fallback when tz empty:\n%s", out)
	}
}
