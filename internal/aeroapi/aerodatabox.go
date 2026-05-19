package aeroapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AeroDataBox is a Resolver backed by the AeroDataBox API on RapidAPI.
// Documentation: https://rapidapi.com/aedbx-aedbx/api/aerodatabox/
//
// Lookups go to GET /flights/number/{ident}/{date} which returns an array
// of flight legs (one per operator + codeshare entry). We prefer the
// canonical operator row.
type AeroDataBox struct {
	APIKey  string
	BaseURL string
	Host    string // RapidAPI host header
	HTTP    *http.Client
}

func NewAeroDataBox(apiKey string) *AeroDataBox {
	return &AeroDataBox{
		APIKey:  apiKey,
		BaseURL: "https://aerodatabox.p.rapidapi.com",
		Host:    "aerodatabox.p.rapidapi.com",
		HTTP:    &http.Client{Timeout: 20 * time.Second},
	}
}

func (a *AeroDataBox) Resolve(ctx context.Context, ident string, date time.Time) (*ResolvedFlight, error) {
	ident = strings.ToUpper(strings.TrimSpace(ident))
	if ident == "" {
		return nil, fmt.Errorf("ident required")
	}
	d := date.UTC().Format("2006-01-02")
	q := url.Values{}
	q.Set("withAircraftImage", "false")
	q.Set("withLocation", "true")
	u := fmt.Sprintf("%s/flights/number/%s/%s?%s",
		a.BaseURL, url.PathEscape(ident), d, q.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-RapidAPI-Key", a.APIKey)
	req.Header.Set("X-RapidAPI-Host", a.Host)
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<18))
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no flight found for %s on %s", ident, d)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("aerodatabox rate limit hit — try again shortly")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aerodatabox %d: %s", resp.StatusCode, body)
	}

	var flights []adbFlight
	if err := json.Unmarshal(body, &flights); err != nil {
		return nil, fmt.Errorf("aerodatabox: bad JSON: %w", err)
	}
	if len(flights) == 0 {
		return nil, fmt.Errorf("no flight found for %s on %s", ident, d)
	}

	pick := flights[0]
	for i := range flights {
		if flights[i].CodeshareStatus == "IsOperator" {
			pick = flights[i]
			break
		}
	}
	return buildResolved(&pick, ident), nil
}

func buildResolved(f *adbFlight, fallbackIdent string) *ResolvedFlight {
	r := &ResolvedFlight{Ident: strings.ToUpper(f.Number)}
	if r.Ident == "" {
		r.Ident = fallbackIdent
	}
	if t := f.Departure.ScheduledTime; t != nil {
		if parsed, err := parseADBTime(t.UTC); err == nil {
			r.ScheduledOut = parsed
		}
	}
	if t := f.Arrival.ScheduledTime; t != nil {
		if parsed, err := parseADBTime(t.UTC); err == nil {
			r.ScheduledIn = parsed
		}
	}
	if a := f.Departure.Airport; a.IATA != "" {
		r.OriginIATA = a.IATA
		if a.Location != nil {
			r.OriginLat = a.Location.Lat
			r.OriginLon = a.Location.Lon
		}
	}
	if a := f.Arrival.Airport; a.IATA != "" {
		r.DestIATA = a.IATA
		if a.Location != nil {
			r.DestLat = a.Location.Lat
			r.DestLon = a.Location.Lon
		}
	}
	if f.Aircraft != nil {
		r.ICAO24 = strings.ToLower(strings.TrimSpace(f.Aircraft.ModeS))
	}
	var notes []string
	if f.Airline != nil && f.Airline.Name != "" {
		notes = append(notes, f.Airline.Name)
	}
	if f.Aircraft != nil && f.Aircraft.Model != "" {
		notes = append(notes, f.Aircraft.Model)
	}
	r.Notes = strings.Join(notes, " · ")
	return r
}

// parseADBTime handles the AeroDataBox UTC time format, which uses a space
// between the date and time component instead of an ISO 'T'.
func parseADBTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	// Replace the first space with a 'T' so RFC3339 parser is happy.
	s = strings.Replace(s, " ", "T", 1)
	// AeroDataBox sometimes omits seconds (e.g. "2026-05-19T08:30Z").
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02T15:04Z", s); err == nil {
		return t.UTC(), nil
	}
	return time.Parse("2006-01-02T15:04:05Z07:00", s)
}

// AeroDataBox JSON shape (just the fields we use).

type adbFlight struct {
	Number          string       `json:"number"`
	CallSign        string       `json:"callSign"`
	Status          string       `json:"status"`
	CodeshareStatus string       `json:"codeshareStatus"`
	Departure       adbMovement  `json:"departure"`
	Arrival         adbMovement  `json:"arrival"`
	Aircraft        *adbAircraft `json:"aircraft,omitempty"`
	Airline         *adbAirline  `json:"airline,omitempty"`
}

type adbMovement struct {
	Airport       adbAirport `json:"airport"`
	ScheduledTime *adbTime   `json:"scheduledTime,omitempty"`
}

type adbTime struct {
	UTC   string `json:"utc"`
	Local string `json:"local"`
}

type adbAirport struct {
	IATA     string       `json:"iata"`
	ICAO     string       `json:"icao"`
	Name     string       `json:"name"`
	Location *adbLocation `json:"location,omitempty"`
}

type adbLocation struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type adbAircraft struct {
	Reg   string `json:"reg"`
	ModeS string `json:"modeS"`
	Model string `json:"model"`
}

type adbAirline struct {
	Name string `json:"name"`
	IATA string `json:"iata"`
	ICAO string `json:"icao"`
}

// Compile-time check that AeroDataBox satisfies Resolver.
var _ Resolver = (*AeroDataBox)(nil)
