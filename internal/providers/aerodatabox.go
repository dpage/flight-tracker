package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
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
	// Limiter serializes outgoing requests so variant-retry bursts never
	// trip RapidAPI's per-second cap on any plan tier. The default
	// (configured in NewAeroDataBox) matches the AeroDataBox PRO plan's
	// documented 1 req/sec ceiling.
	Limiter *rate.Limiter
	// RetryWait is how long resolveOne sleeps after a 429 before retrying
	// once, used when the upstream's Retry-After header is missing or
	// zero. Default 2s; tests can shorten it to keep the suite fast.
	RetryWait time.Duration
}

func NewAeroDataBox(apiKey string) *AeroDataBox {
	return &AeroDataBox{
		APIKey:  apiKey,
		BaseURL: "https://aerodatabox.p.rapidapi.com",
		Host:    "aerodatabox.p.rapidapi.com",
		HTTP:    &http.Client{Timeout: 20 * time.Second},
		// 1 req/s — matches the AeroDataBox PRO plan's documented per-
		// second rate limit exactly. Higher-tier plans aren't faster on
		// this dimension (RapidAPI throttles ingress, not the upstream).
		// Burst 1 keeps the very first call snappy; subsequent calls
		// space by ~1s. The resolveOne path auto-retries 429 once with
		// a short wait, so any slight clock-drift miss still succeeds.
		Limiter:   rate.NewLimiter(rate.Limit(1), 1),
		RetryWait: 2 * time.Second,
	}
}

// Resolve looks up a flight on AeroDataBox. Airlines refer to the same
// flight as e.g. "BA87", "BA087", or "BA0087" interchangeably, but
// AeroDataBox keys them under one canonical form. To absorb that, on a
// "not found" response we retry with every reasonable zero-padding of the
// numeric portion (pad-length 2 → 5) before giving up.
func (a *AeroDataBox) Resolve(ctx context.Context, ident string, date time.Time) (*ResolvedFlight, error) {
	ident = strings.ToUpper(strings.TrimSpace(ident))
	if ident == "" {
		return nil, fmt.Errorf("ident required")
	}
	d := date.UTC().Format("2006-01-02")
	variants := identVariants(ident)
	for _, v := range variants {
		rf, err := a.resolveOne(ctx, v, d)
		if err == nil {
			return rf, nil
		}
		if !errors.Is(err, ErrFlightNotFound) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("no flight found for %s on %s: %w", ident, d, ErrFlightNotFound)
}

// resolveOne issues GET /flights/number/{ident}/{date} and returns the
// picked operator row, or ErrFlightNotFound if the upstream has no record
// of this exact ident on this date. On 429 it sleeps for the upstream's
// Retry-After (or a short default) and retries once — the most common
// transient throttle is hidden from the caller this way.
func (a *AeroDataBox) resolveOne(ctx context.Context, ident, date string) (*ResolvedFlight, error) {
	const maxAttempts = 2
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		rf, status, retryAfter, body, err := a.doOne(ctx, ident, date)
		if err != nil {
			return nil, err
		}
		switch status {
		case http.StatusOK:
			if rf.ScheduledOut.IsZero() || rf.ScheduledIn.IsZero() {
				// AeroDataBox knows the ident on this date but hasn't
				// returned a usable schedule for it — typically a future
				// flight whose specific-date schedule isn't published yet,
				// or a malformed time value we couldn't parse. Surface as
				// ErrFlightUnscheduled rather than letting zero times
				// reach the store.
				slog.Info("aerodatabox: flight returned with no schedule",
					"ident", ident, "date", date,
					"out_zero", rf.ScheduledOut.IsZero(),
					"in_zero", rf.ScheduledIn.IsZero(),
					"body", truncate(body, 400))
				return nil, ErrFlightUnscheduled
			}
			slog.Info("aerodatabox resolved", "ident", ident, "date", date,
				"resolved_ident", rf.Ident, "origin", rf.OriginIATA,
				"dest", rf.DestIATA, "icao24", rf.ICAO24,
				"scheduled_out", rf.ScheduledOut.Format(time.RFC3339))
			return rf, nil
		case http.StatusNotFound, http.StatusNoContent:
			slog.Info("aerodatabox not found", "ident", ident, "date", date,
				"status", status)
			return nil, ErrFlightNotFound
		case http.StatusTooManyRequests:
			msg := upstreamMessage(body)
			slog.Warn("aerodatabox rate limited",
				"ident", ident, "date", date, "attempt", attempt,
				"retry_after_sec", int(retryAfter.Seconds()),
				"upstream_message", msg, "body", truncate(body, 200))
			if attempt < maxAttempts {
				wait := retryAfter
				if wait <= 0 {
					wait = a.RetryWait
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
			if retryAfter > 0 {
				return nil, fmt.Errorf("aerodatabox rate limit — %s (retry in %ds)",
					orFallback(msg, "throttled by RapidAPI"), int(retryAfter.Seconds()))
			}
			return nil, fmt.Errorf("aerodatabox rate limit — %s",
				orFallback(msg, "throttled by RapidAPI; try again shortly"))
		default:
			slog.Warn("aerodatabox non-200", "ident", ident, "date", date,
				"status", status, "body", truncate(body, 200))
			return nil, fmt.Errorf("aerodatabox %d: %s", status, body)
		}
	}
	return nil, lastErr
}

// doOne is the single-request workhorse. It returns the parsed flight (only
// when status == 200), the HTTP status, the parsed Retry-After, the raw
// body for logging / error formatting, and any transport-level error.
func (a *AeroDataBox) doOne(ctx context.Context, ident, date string) (
	*ResolvedFlight, int, time.Duration, []byte, error,
) {
	q := url.Values{}
	q.Set("withAircraftImage", "false")
	q.Set("withLocation", "true")
	u := fmt.Sprintf("%s/flights/number/%s/%s?%s",
		a.BaseURL, url.PathEscape(ident), date, q.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	req.Header.Set("X-RapidAPI-Key", a.APIKey)
	req.Header.Set("X-RapidAPI-Host", a.Host)
	req.Header.Set("Accept", "application/json")

	if a.Limiter != nil {
		if err := a.Limiter.Wait(ctx); err != nil {
			return nil, 0, 0, nil, err
		}
	}
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<18))
	retry := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, retry, body, nil
	}

	var flights []adbFlight
	if err := json.Unmarshal(body, &flights); err != nil {
		return nil, resp.StatusCode, retry, body, fmt.Errorf("aerodatabox: bad JSON: %w", err)
	}
	if len(flights) == 0 {
		// Treat empty array as "not found"; bump the status so the caller's
		// switch maps it to ErrFlightNotFound.
		return nil, http.StatusNoContent, retry, body, nil
	}

	pick := flights[0]
	for i := range flights {
		if flights[i].CodeshareStatus == "IsOperator" {
			pick = flights[i]
			break
		}
	}
	return buildResolved(&pick, ident), resp.StatusCode, retry, body, nil
}

// upstreamMessage extracts AeroDataBox's "message" field from a JSON error
// body when present, returning "" otherwise. The shape is:
//
//	{"message":"You have exceeded the rate limit per second for your plan, PRO, by the API provider"}
func upstreamMessage(body []byte) string {
	var p struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return ""
	}
	return strings.TrimSpace(p.Message)
}

func orFallback(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// identVariants returns at most TWO candidates to try against AeroDataBox:
// the user's literal input, and the canonical 4-digit padded form (if
// different). We used to try every pad-length up to 5; that burst was a
// real driver of 429s on tighter RapidAPI plans, and in practice we never
// saw flights stored at any width other than 4. Examples:
//
//	"BA87"   → [BA87,   BA0087]
//	"BA087"  → [BA087,  BA0087]
//	"BA0087" → [BA0087]
//	"9W420"  → [9W420,  9W0420]
//	"AC1234" → [AC1234]   (already 4-digit canonical)
//
// Idents that don't match an "airline code + digits" pattern (the prefix
// must contain at least one letter) are passed through unchanged so we
// don't generate junk for pure-digit or pathological inputs.
func identVariants(ident string) []string {
	m := identRe.FindStringSubmatch(ident)
	if m == nil {
		return []string{ident}
	}
	prefix := m[1]
	if !strings.ContainsAny(prefix, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		return []string{ident}
	}
	num := strings.TrimLeft(m[2], "0")
	if num == "" {
		// e.g. "BA0000" — all zeros, weird; return as-is.
		return []string{ident}
	}
	out := []string{ident}
	// Canonical 4-digit form, only if the user didn't already type it.
	if len(num) <= 4 {
		canonical := prefix + strings.Repeat("0", 4-len(num)) + num
		if canonical != ident {
			out = append(out, canonical)
		}
	}
	return out
}

// Airline codes can start with a digit (e.g. "9W"), but they must contain
// at least one letter — enforced by the post-regex check above.
var identRe = regexp.MustCompile(`^([A-Z0-9]+?)(\d+)$`)

// parseRetryAfter understands both forms of the Retry-After header:
//   - "5"                              → 5 seconds (delta-seconds form)
//   - "Wed, 21 Oct 2015 07:28:00 GMT"  → absolute HTTP-date form
//
// Returns 0 if the header is missing or unparsable.
func parseRetryAfter(h string, now time.Time) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		d := t.Sub(now)
		if d > 0 {
			return d
		}
	}
	return 0
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

func buildResolved(f *adbFlight, fallbackIdent string) *ResolvedFlight {
	// AeroDataBox returns "BA 286" — split on whitespace then re-join so the
	// canonical "BA286" form lands in the DTO regardless of the upstream's
	// formatting.
	r := &ResolvedFlight{Ident: strings.ToUpper(strings.Join(strings.Fields(f.Number), ""))}
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
	r.OriginGate = strings.TrimSpace(f.Departure.Gate)
	r.DestGate = strings.TrimSpace(f.Arrival.Gate)
	r.OriginTerminal = strings.TrimSpace(f.Departure.Terminal)
	r.DestTerminal = strings.TrimSpace(f.Arrival.Terminal)
	if f.Aircraft != nil {
		r.ICAO24 = strings.ToLower(strings.TrimSpace(f.Aircraft.ModeS))
	}
	r.Callsign = strings.ToUpper(strings.TrimSpace(f.CallSign))
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
	// Gate / terminal are present on the departure/arrival movement for many
	// airports; absent for others (omitempty → "").
	Gate     string `json:"gate,omitempty"`
	Terminal string `json:"terminal,omitempty"`
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
