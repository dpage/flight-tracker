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
//
// Ident/Date/Confidence are the core fields used to look the flight up
// against the airline-data provider. The remaining fields are the raw
// schedule details the LLM was able to pull from the email body itself;
// they let the ingest path fall back to a manual add when the provider
// has no record of the flight yet. All of OriginIATA, DestIATA,
// DepartTimeLocal, ArriveDate, ArriveTimeLocal must be set for the
// manual fallback to fire — partial data is ignored.
type Leg struct {
	Ident           string
	Date            string // YYYY-MM-DD (departure date)
	Confidence      string // high | medium | low
	OriginIATA      string // 3-letter IATA, uppercase
	DestIATA        string // 3-letter IATA, uppercase
	DepartTimeLocal string // HH:MM, 24h, in origin airport's local time
	ArriveDate      string // YYYY-MM-DD (arrival local calendar day)
	ArriveTimeLocal string // HH:MM, 24h, in dest airport's local time
}

// HasManualDetails returns true when every field needed to insert the
// flight without provider data is populated.
func (l Leg) HasManualDetails() bool {
	return l.OriginIATA != "" && l.DestIATA != "" &&
		l.DepartTimeLocal != "" && l.ArriveDate != "" && l.ArriveTimeLocal != ""
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
  "flights": [{
    "ident": "<airline+number, uppercase, e.g. LH441>",
    "date": "YYYY-MM-DD (local departure)",
    "confidence": "high"|"medium"|"low",
    "origin_iata": "<3-letter IATA, uppercase, e.g. LHR>",
    "dest_iata":   "<3-letter IATA, uppercase, e.g. JFK>",
    "depart_time": "HH:MM (24h, in the origin airport's local time)",
    "arrive_date": "YYYY-MM-DD (in the destination airport's local calendar day)",
    "arrive_time": "HH:MM (24h, in the destination airport's local time)"
  }],
  "notes": "optional short note"
}

If a leg's ident or date is ambiguous, set confidence to "low" and the caller will skip it. Use the date the passenger physically departs, in the airport's local calendar day. The origin/destination/time fields are optional but you SHOULD fill them in whenever the email contains them — they let us add the flight even when the airline hasn't published its schedule yet. Leave a field empty ("") only when the email genuinely doesn't say. Today is %s.`

var identRe = regexp.MustCompile(`^[A-Z0-9]{2,3}[0-9]{1,4}[A-Z]?$`)
var dateRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
var iataRe = regexp.MustCompile(`^[A-Z]{3}$`)
var timeRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

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
			OriginIATA string `json:"origin_iata"`
			DestIATA   string `json:"dest_iata"`
			DepartTime string `json:"depart_time"`
			ArriveDate string `json:"arrive_date"`
			ArriveTime string `json:"arrive_time"`
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
		leg := Leg{Ident: f.Ident, Date: f.Date, Confidence: f.Confidence}
		// Manual-fallback fields. Each is validated independently and only
		// retained if well-formed — partial / garbled data is dropped so
		// the manual-add path won't fire on it.
		origin := strings.ToUpper(strings.TrimSpace(f.OriginIATA))
		dest := strings.ToUpper(strings.TrimSpace(f.DestIATA))
		if iataRe.MatchString(origin) {
			leg.OriginIATA = origin
		}
		if iataRe.MatchString(dest) {
			leg.DestIATA = dest
		}
		if timeRe.MatchString(f.DepartTime) {
			leg.DepartTimeLocal = f.DepartTime
		}
		if timeRe.MatchString(f.ArriveTime) {
			leg.ArriveTimeLocal = f.ArriveTime
		}
		if dateRe.MatchString(f.ArriveDate) {
			if ad, err := time.Parse("2006-01-02", f.ArriveDate); err == nil &&
				!ad.Before(now.AddDate(-2, 0, 0)) && !ad.After(now.AddDate(2, 0, 0)) {
				leg.ArriveDate = f.ArriveDate
			}
		}
		out = append(out, leg)
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
