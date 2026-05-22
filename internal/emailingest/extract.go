package emailingest

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Leg is one extracted flight from an email.
type Leg struct {
	Ident      string
	Date       string // YYYY-MM-DD
	Confidence string // high | medium | low
}

// Document is one binary attachment passed to the LLM alongside the
// prompt — typically a PDF airline ticket. MediaType is the MIME type
// (e.g. "application/pdf"); Filename is informational only.
type Document struct {
	Data      []byte
	MediaType string
	Filename  string
}

// LLM is the minimal interface Extractor needs. The real implementation
// wraps pgedge-go-llm-lib; tests pass in a fake. Documents may be empty
// (text-only emails) and providers that don't support documents may
// receive a text-only retry — see RealLLM.Complete.
type LLM interface {
	Complete(ctx context.Context, prompt string, docs []Document) (string, error)
}

// Extractor calls an LLM and parses its JSON response into legs.
type Extractor struct {
	LLM   LLM
	Model string
	Now   func() time.Time
}

// NewExtractor returns an Extractor backed by the given LLM client.
func NewExtractor(l LLM, model string) *Extractor {
	return &Extractor{LLM: l, Model: model, Now: time.Now}
}

const systemPrompt = `You receive the body of a forwarded airline or travel agent email. Extract every flight leg the user has booked. Return JSON only, no prose, matching this schema:

{
  "flights": [{ "ident": "<airline+number, uppercase, e.g. LH441>", "date": "YYYY-MM-DD (local departure)", "confidence": "high"|"medium"|"low" }],
  "notes": "optional short note"
}

If a leg's ident or date is ambiguous, set confidence to "low" and the caller will skip it. Use the date the passenger physically departs, in the airport's local calendar day. Today is %s.`

var identRe = regexp.MustCompile(`^[A-Z0-9]{2,3}[0-9]{1,4}[A-Z]?$`)
var dateRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// Extract calls the LLM with the body and any document attachments,
// parses the JSON response, drops any leg that's low-confidence or fails
// regex / sanity validation, and returns the rest.
func (x *Extractor) Extract(ctx context.Context, body string, docs []Document) ([]Leg, error) {
	prompt := fmt.Sprintf(systemPrompt, x.Now().UTC().Format(time.RFC3339)) + "\n\n---\n\n" + body
	raw, err := x.LLM.Complete(ctx, prompt, docs)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}
	cleaned := stripCodeFence(raw)
	var resp struct {
		Flights []struct {
			Ident      string `json:"ident"`
			Date       string `json:"date"`
			Confidence string `json:"confidence"`
		} `json:"flights"`
	}
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return nil, fmt.Errorf("llm json: %w (got %q)", err, cleaned)
	}
	now := x.Now()
	out := make([]Leg, 0, len(resp.Flights))
	for _, f := range resp.Flights {
		if strings.EqualFold(f.Confidence, "low") {
			continue
		}
		if !identRe.MatchString(f.Ident) || !dateRe.MatchString(f.Date) {
			continue
		}
		d, err := time.Parse("2006-01-02", f.Date)
		if err != nil {
			continue
		}
		// Reject obviously wrong dates: more than 2 years in either direction.
		if d.Before(now.AddDate(-2, 0, 0)) || d.After(now.AddDate(2, 0, 0)) {
			continue
		}
		out = append(out, Leg{Ident: f.Ident, Date: f.Date, Confidence: f.Confidence})
	}
	return out, nil
}

// stripCodeFence removes ```...``` wrappers around an LLM response, if present.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}
