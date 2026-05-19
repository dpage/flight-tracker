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

// OpenSky is a Tracker backed by the OpenSky Network's public state-vectors
// API. Free for non-commercial use; rate-limited heavily when anonymous.
//
// Tracking is keyed on the aircraft's ICAO 24-bit address ("icao24"), six
// lowercase hex characters such as "a1b2c3" — this is the airframe ID, not
// the flight number. Flights with no icao24 yield (nil, nil) so the caller
// (typically a DeadReckoner) can decide whether to extrapolate.
type OpenSky struct {
	BaseURL  string
	Username string // optional HTTP Basic Auth (free OpenSky account)
	Password string
	HTTP     *http.Client
}

func NewOpenSky(username, password string) *OpenSky {
	return &OpenSky{
		BaseURL:  "https://opensky-network.org/api",
		Username: username,
		Password: password,
		HTTP:     &http.Client{Timeout: 15 * time.Second},
	}
}

// openSkyStates is the response shape of /api/states/all. The `states` field
// is an array of arrays — each inner array is a fixed-position vector. We
// unpack only the fields we care about; nil indicates "field absent".
type openSkyStates struct {
	Time   int64           `json:"time"`
	States [][]interface{} `json:"states"`
}

// State-vector positions defined by OpenSky:
//
//	[0] icao24            string
//	[1] callsign          string|nil
//	[2] origin_country    string
//	[3] time_position     int|nil   (seconds since epoch of last position update)
//	[4] last_contact      int
//	[5] longitude         float|nil
//	[6] latitude          float|nil
//	[7] baro_altitude     float|nil (metres)
//	[8] on_ground         bool
//	[9] velocity          float|nil (m/s)
//	[10] true_track       float|nil (degrees clockwise from north)
//	[11] vertical_rate    float|nil
//	[12] sensors          int[]|nil
//	[13] geo_altitude     float|nil
//	[14] squawk           string|nil
//	[15] spi              bool
//	[16] position_source  int

func (o *OpenSky) Track(ctx context.Context, f *store.Flight, _ time.Time) (*store.Position, error) {
	if f.ICAO24 == nil || *f.ICAO24 == "" {
		return nil, nil //nolint:nilnil // no aircraft id to query
	}
	icao := strings.ToLower(*f.ICAO24)
	q := url.Values{}
	q.Set("icao24", icao)

	req, err := http.NewRequestWithContext(ctx, "GET",
		o.BaseURL+"/states/all?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if o.Username != "" {
		req.SetBasicAuth(o.Username, o.Password)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "flight-tracker (https://github.com/dpage/flight-tracker)")

	resp, err := o.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errors.New("opensky rate limit; consider configuring OPENSKY_USERNAME/PASSWORD")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("opensky /states/all -> %d: %s", resp.StatusCode, body)
	}
	var out openSkyStates
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.States) == 0 {
		return nil, nil //nolint:nilnil // ADS-B silence — caller may dead-reckon
	}
	state := out.States[0]
	lat, latOK := stateFloat(state, 6)
	lon, lonOK := stateFloat(state, 5)
	if !latOK || !lonOK {
		return nil, nil //nolint:nilnil // partial state, no usable fix
	}
	ts := time.Unix(out.Time, 0).UTC()
	if tp, ok := stateInt(state, 3); ok {
		ts = time.Unix(tp, 0).UTC()
	}
	pos := &store.Position{
		FlightID:    f.ID,
		Ts:          ts,
		Lat:         lat,
		Lon:         lon,
		IsEstimated: false,
	}
	if v, ok := stateFloat(state, 7); ok {
		a := int32(v * 3.28084) // metres → feet
		pos.AltitudeFt = &a
	}
	if v, ok := stateFloat(state, 9); ok {
		g := int32(v * 1.94384) // m/s → knots
		pos.GroundspeedKt = &g
	}
	if v, ok := stateFloat(state, 10); ok {
		h := int16(v)
		pos.HeadingDeg = &h
	}
	return pos, nil
}

func stateFloat(state []interface{}, i int) (float64, bool) {
	if i >= len(state) || state[i] == nil {
		return 0, false
	}
	v, ok := state[i].(float64)
	return v, ok
}

func stateInt(state []interface{}, i int) (int64, bool) {
	if i >= len(state) || state[i] == nil {
		return 0, false
	}
	if v, ok := state[i].(float64); ok {
		return int64(v), true
	}
	return 0, false
}
