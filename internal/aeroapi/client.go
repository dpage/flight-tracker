package aeroapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dpage/flight-tracker/internal/store"
)

// Live is a real AeroAPI client (FlightAware AeroAPI v4).
// See https://www.flightaware.com/aeroapi/portal/documentation.
type Live struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client
}

func NewLive(apiKey, baseURL string) *Live {
	return &Live{
		APIKey:  apiKey,
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 20 * time.Second},
	}
}

type aeroFlightsResp struct {
	Flights []aeroFlight `json:"flights"`
}

type aeroFlight struct {
	FAFlightID   string         `json:"fa_flight_id"`
	Ident        string         `json:"ident"`
	Status       string         `json:"status"`
	ScheduledOut *time.Time     `json:"scheduled_out"`
	EstimatedOut *time.Time     `json:"estimated_out"`
	ActualOut    *time.Time     `json:"actual_out"`
	ScheduledIn  *time.Time     `json:"scheduled_in"`
	EstimatedIn  *time.Time     `json:"estimated_in"`
	ActualIn     *time.Time     `json:"actual_in"`
	Origin       *aeroAirport   `json:"origin"`
	Destination  *aeroAirport   `json:"destination"`
	LastPosition *aeroLastPos   `json:"last_position"`
	Cancelled    bool           `json:"cancelled"`
}

type aeroAirport struct {
	CodeIATA  string  `json:"code_iata"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type aeroLastPos struct {
	Timestamp     time.Time `json:"timestamp"`
	Latitude      float64   `json:"latitude"`
	Longitude     float64   `json:"longitude"`
	AltitudeFt    *int32    `json:"altitude"`
	Groundspeed   *int32    `json:"groundspeed"`
	Heading       *int16    `json:"heading"`
}

// Refresh looks up the flight either by fa_flight_id (preferred, set after
// first successful resolve) or by ident scoped to the scheduled date.
func (c *Live) Refresh(ctx context.Context, f *store.Flight, now time.Time) (*Update, error) {
	var resp aeroFlightsResp
	if f.AeroAPIID != nil && *f.AeroAPIID != "" {
		var single struct {
			Flights []aeroFlight `json:"flights"`
		}
		if err := c.get(ctx, "/flights/"+url.PathEscape(*f.AeroAPIID), nil, &single); err != nil {
			return nil, err
		}
		resp.Flights = single.Flights
	} else {
		q := url.Values{}
		// scope to a 24-hour window around the scheduled date in UTC.
		start := f.ScheduledOut.Add(-12 * time.Hour).UTC().Format(time.RFC3339)
		end := f.ScheduledOut.Add(36 * time.Hour).UTC().Format(time.RFC3339)
		q.Set("start", start)
		q.Set("end", end)
		if err := c.get(ctx, "/flights/"+url.PathEscape(f.Ident), q, &resp); err != nil {
			return nil, err
		}
	}
	pick := pickFlight(resp.Flights, f.ScheduledOut)
	if pick == nil {
		return nil, fmt.Errorf("no matching AeroAPI flight for %s near %s", f.Ident, f.ScheduledOut.Format(time.RFC3339))
	}
	return buildUpdate(pick, f.ID), nil
}

func (c *Live) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.BaseURL + path
	if q != nil {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-apikey", c.APIKey)
	req.Header.Set("Accept", "application/json; charset=UTF-8")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("aeroapi GET %s -> %d: %s", path, resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// pickFlight returns the flight whose scheduled_out is closest to want.
func pickFlight(flights []aeroFlight, want time.Time) *aeroFlight {
	var best *aeroFlight
	var bestDelta time.Duration = 365 * 24 * time.Hour
	for i := range flights {
		f := &flights[i]
		if f.ScheduledOut == nil {
			continue
		}
		d := want.Sub(*f.ScheduledOut)
		if d < 0 {
			d = -d
		}
		if d < bestDelta {
			bestDelta = d
			best = f
		}
	}
	return best
}

func buildUpdate(af *aeroFlight, flightID int64) *Update {
	tu := store.TrackingUpdate{
		Status:       af.Status,
		EstimatedOut: af.EstimatedOut,
		EstimatedIn:  af.EstimatedIn,
		ActualOut:    af.ActualOut,
		ActualIn:     af.ActualIn,
	}
	if af.Cancelled {
		tu.Status = "Cancelled"
	}
	if af.Origin != nil {
		tu.OriginIATA = af.Origin.CodeIATA
		lat, lon := af.Origin.Latitude, af.Origin.Longitude
		tu.OriginLat, tu.OriginLon = &lat, &lon
	}
	if af.Destination != nil {
		tu.DestIATA = af.Destination.CodeIATA
		lat, lon := af.Destination.Latitude, af.Destination.Longitude
		tu.DestLat, tu.DestLon = &lat, &lon
	}
	up := &Update{Tracking: tu}
	if af.LastPosition != nil {
		up.Position = &store.Position{
			FlightID:      flightID,
			Ts:            af.LastPosition.Timestamp,
			Lat:           af.LastPosition.Latitude,
			Lon:           af.LastPosition.Longitude,
			AltitudeFt:    af.LastPosition.AltitudeFt,
			GroundspeedKt: af.LastPosition.Groundspeed,
			HeadingDeg:    af.LastPosition.Heading,
		}
	}
	return up
}

// Compile-time check that both implementations satisfy Client.
var _ Client = (*Live)(nil)
var _ Client = Stub{}

// Suppress staticcheck error for unused haversine when import order changes.
var _ = errors.New
