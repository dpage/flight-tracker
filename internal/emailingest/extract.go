package emailingest

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/planops"
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

// The generalized multi-type extraction result types (ExtractedPart /
// ExtractedPlan) live in internal/planops so the planops capture path can own
// them without a dependency cycle (emailingest depends on planops, never the
// reverse). ExtractPlans below returns those planops types.

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

// plansSystemPrompt is the generalized multi-type extraction prompt used by
// ExtractPlans (the planops capture path). It groups every booking in the
// email into plans of any type — flight, hotel, train, ground, dining,
// excursion — each with one or more parts. The flight schema is identical to
// the flights-only prompt so the resolver-backed enrich + manual fallback
// still apply to flight parts.
const plansSystemPrompt = `You receive the body of a forwarded travel email (and possibly attached tickets). Extract every booking the traveller has made and group them into plans. One booking = one plan; a round-trip or multi-leg booking is one plan with several parts. Return JSON only, no prose, matching this schema:

{
  "plans": [{
    "type": "flight"|"hotel"|"train"|"ground"|"dining"|"excursion",
    "title": "<short human label, e.g. 'BA to JFK' or 'Hotel Plaza'>",
    "confirmation_ref": "<booking reference / PNR if present, else ''>",
    "parts": [{
      "type": "<same as the plan's type>",
      "confidence": "high"|"medium"|"low",

      "flight": {
        "ident": "<airline+number, uppercase, e.g. LH441>",
        "date": "YYYY-MM-DD (local departure)",
        "origin_iata": "<3-letter IATA, uppercase>",
        "dest_iata": "<3-letter IATA, uppercase>",
        "depart_time": "HH:MM (24h, origin local)",
        "arrive_date": "YYYY-MM-DD (dest local calendar day)",
        "arrive_time": "HH:MM (24h, dest local)"
      },

      "start_date": "YYYY-MM-DD (check-in / pickup / reservation / start day)",
      "start_time": "HH:MM (24h, local; optional)",
      "end_date":   "YYYY-MM-DD (check-out / dropoff / end day; optional)",
      "end_time":   "HH:MM (24h, local; optional)",
      "start_label": "<place name, e.g. station, restaurant, pickup point>",
      "end_label":   "<destination place name, when relevant>",

      "hotel":     { "property_name": "", "address": "", "phone": "", "room_type": "" },
      "train":     { "operator": "", "service_no": "", "class": "" },
      "ground":    { "provider": "", "vehicle": "" },
      "dining":    { "reservation_name": "" },
      "excursion": { "title": "" }
    }]
  }],
  "notes": "optional short note"
}

Only populate the per-type detail object that matches the part's type; leave the others absent or empty. For flight parts fill the "flight" object exactly as for a flights-only extraction (ident + date are required; origin/dest/times are strongly preferred). For every non-flight part fill start_date (required) and as many of the generic + per-type fields as the email states. Set a part's confidence to "low" when its core identity (flight ident+date, or a non-flight start_date) is ambiguous and the caller will skip it. Leave any field empty ("") when the email genuinely doesn't say. Today is %s.`

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

// ExtractPlans is the generalized capture: it calls the LLM with the
// multi-type plansSystemPrompt, parses the {"plans":[…]} response, validates
// each part by type, and groups them into ExtractedPlans. Low-confidence and
// malformed parts are dropped; plans left with no parts are omitted. Flight
// parts populate the embedded Leg (so callers can reuse the resolver-backed
// flight path); other parts populate their type-specific fields.
func (x *Extractor) ExtractPlans(ctx context.Context, body string, docs []planops.Document) ([]planops.ExtractedPlan, error) {
	emDocs := make([]Document, 0, len(docs))
	for _, d := range docs {
		emDocs = append(emDocs, Document{Data: d.Data, MediaType: d.MediaType, Filename: d.Filename})
	}
	prompt := fmt.Sprintf(plansSystemPrompt, x.Now().UTC().Format(time.RFC3339)) + "\n\n---\n\n" + body
	raw, err := x.LLM.Complete(ctx, prompt, emDocs)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}
	cleaned := stripCodeFence(raw)
	var resp struct {
		Plans []struct {
			Type            string `json:"type"`
			Title           string `json:"title"`
			ConfirmationRef string `json:"confirmation_ref"`
			Parts           []struct {
				Type       string `json:"type"`
				Confidence string `json:"confidence"`
				Flight     struct {
					Ident      string `json:"ident"`
					Date       string `json:"date"`
					OriginIATA string `json:"origin_iata"`
					DestIATA   string `json:"dest_iata"`
					DepartTime string `json:"depart_time"`
					ArriveDate string `json:"arrive_date"`
					ArriveTime string `json:"arrive_time"`
				} `json:"flight"`
				StartDate  string `json:"start_date"`
				StartTime  string `json:"start_time"`
				EndDate    string `json:"end_date"`
				EndTime    string `json:"end_time"`
				StartLabel string `json:"start_label"`
				EndLabel   string `json:"end_label"`
				Hotel      struct {
					PropertyName string `json:"property_name"`
					Address      string `json:"address"`
					Phone        string `json:"phone"`
					RoomType     string `json:"room_type"`
				} `json:"hotel"`
				Train struct {
					Operator  string `json:"operator"`
					ServiceNo string `json:"service_no"`
					Class     string `json:"class"`
				} `json:"train"`
				Ground struct {
					Provider string `json:"provider"`
					Vehicle  string `json:"vehicle"`
				} `json:"ground"`
				Dining struct {
					ReservationName string `json:"reservation_name"`
				} `json:"dining"`
				Excursion struct {
					Title string `json:"title"`
				} `json:"excursion"`
			} `json:"parts"`
		} `json:"plans"`
	}
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return nil, fmt.Errorf("llm json: %w (got %q)", err, cleaned)
	}
	out := make([]planops.ExtractedPlan, 0, len(resp.Plans))
	for _, pl := range resp.Plans {
		planType := strings.ToLower(strings.TrimSpace(pl.Type))
		ep := planops.ExtractedPlan{Type: planType, Title: pl.Title, ConfirmationRef: pl.ConfirmationRef}
		for _, p := range pl.Parts {
			partType := strings.ToLower(strings.TrimSpace(p.Type))
			if partType == "" {
				partType = planType
			}
			if !validExtractType[partType] {
				continue
			}
			if strings.EqualFold(p.Confidence, "low") {
				continue
			}
			part := planops.ExtractedPart{
				Type:       partType,
				Confidence: p.Confidence,
				StartDate:  p.StartDate,
				EndDate:    p.EndDate,
				StartLabel: strings.TrimSpace(p.StartLabel),
				EndLabel:   strings.TrimSpace(p.EndLabel),
			}
			if timeRe.MatchString(p.StartTime) {
				part.StartTime = p.StartTime
			}
			if timeRe.MatchString(p.EndTime) {
				part.EndTime = p.EndTime
			}
			if partType == "flight" {
				leg, ok := x.validateFlightPart(p.Flight.Ident, p.Flight.Date, p.Confidence,
					p.Flight.OriginIATA, p.Flight.DestIATA, p.Flight.DepartTime,
					p.Flight.ArriveDate, p.Flight.ArriveTime)
				if !ok {
					continue
				}
				part.Flight = planops.FlightFields{
					Ident:           leg.Ident,
					Date:            leg.Date,
					OriginIATA:      leg.OriginIATA,
					DestIATA:        leg.DestIATA,
					DepartTimeLocal: leg.DepartTimeLocal,
					ArriveDate:      leg.ArriveDate,
					ArriveTimeLocal: leg.ArriveTimeLocal,
				}
				part.StartDate = leg.Date
			} else if !dateRe.MatchString(part.StartDate) {
				// Non-flight parts need a well-formed start day to land on a
				// timeline; otherwise drop them.
				continue
			}
			switch partType {
			case "hotel":
				part.HotelName = p.Hotel.PropertyName
				part.Address = p.Hotel.Address
				part.Phone = p.Hotel.Phone
				part.RoomType = p.Hotel.RoomType
			case "train":
				part.Operator = p.Train.Operator
				part.ServiceNo = p.Train.ServiceNo
				part.Class = p.Train.Class
			case "ground":
				part.Provider = p.Ground.Provider
				part.Vehicle = p.Ground.Vehicle
			case "dining":
				part.ReservationName = p.Dining.ReservationName
			case "excursion":
				part.ExcursionTitle = p.Excursion.Title
			}
			ep.Parts = append(ep.Parts, part)
		}
		if len(ep.Parts) == 0 {
			continue
		}
		if ep.Type == "" {
			ep.Type = ep.Parts[0].Type
		}
		out = append(out, ep)
	}
	return out, nil
}

var validExtractType = map[string]bool{
	"flight": true, "train": true, "hotel": true,
	"ground": true, "dining": true, "excursion": true,
}

// validateFlightPart applies the same regex/sanity gates as Extract to a single
// flight part and returns the populated Leg. ok is false when the core
// ident+date fails validation (the caller drops the part).
func (x *Extractor) validateFlightPart(ident, date, confidence, originIATA, destIATA, departTime, arriveDate, arriveTime string) (Leg, bool) {
	if !identRe.MatchString(ident) || !dateRe.MatchString(date) {
		return Leg{}, false
	}
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return Leg{}, false
	}
	now := x.Now()
	if d.Before(now.AddDate(-2, 0, 0)) || d.After(now.AddDate(2, 0, 0)) {
		return Leg{}, false
	}
	leg := Leg{Ident: ident, Date: date, Confidence: confidence}
	if origin := strings.ToUpper(strings.TrimSpace(originIATA)); iataRe.MatchString(origin) {
		leg.OriginIATA = origin
	}
	if dest := strings.ToUpper(strings.TrimSpace(destIATA)); iataRe.MatchString(dest) {
		leg.DestIATA = dest
	}
	if timeRe.MatchString(departTime) {
		leg.DepartTimeLocal = departTime
	}
	if timeRe.MatchString(arriveTime) {
		leg.ArriveTimeLocal = arriveTime
	}
	if dateRe.MatchString(arriveDate) {
		if ad, err := time.Parse("2006-01-02", arriveDate); err == nil &&
			!ad.Before(now.AddDate(-2, 0, 0)) && !ad.After(now.AddDate(2, 0, 0)) {
			leg.ArriveDate = arriveDate
		}
	}
	return leg, true
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
