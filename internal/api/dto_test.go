package api

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

func TestToUserDTO(t *testing.T) {
	last := time.Now()
	u := &store.User{
		ID: 1, Username: "octocat", Name: "Octo",
		AvatarURL: "a.png", IsSuperuser: true, IsActive: true, LastLoginAt: &last,
	}
	d := ToUserDTO(u)
	if d.ID != 1 || d.Username != "octocat" || !d.IsSuperuser || !d.HasLoggedIn {
		t.Errorf("unexpected dto: %+v", d)
	}
	if d.LastLoginAt == nil || !d.LastLoginAt.Equal(last) {
		t.Errorf("LastLoginAt not propagated")
	}
}

func TestToUserDTONeverLoggedIn(t *testing.T) {
	u := &store.User{ID: 2, Username: "invitee"} // LastLoginAt nil
	d := ToUserDTO(u)
	if d.HasLoggedIn {
		t.Error("HasLoggedIn should be false when LastLoginAt is nil")
	}
}

func TestToPositionDTO(t *testing.T) {
	alt := int32(35000)
	gs := int32(450)
	hdg := int16(270)
	p := &store.Position{
		Ts: time.Unix(1700000000, 0), Lat: 1.5, Lon: -2.5,
		AltitudeFt: &alt, GroundspeedKt: &gs, HeadingDeg: &hdg, IsEstimated: true,
	}
	d := ToPositionDTO(p)
	if d.Lat != 1.5 || d.Lon != -2.5 || *d.AltitudeFt != 35000 || !d.IsEstimated {
		t.Errorf("unexpected dto: %+v", d)
	}
}

func TestToFlightDTOFull(t *testing.T) {
	lat, lon := 51.0, -0.4
	f := &store.Flight{
		ID: 7, Ident: "BA286",
		ScheduledOut: time.Unix(1700000000, 0), ScheduledIn: time.Unix(1700036000, 0),
		OriginIATA: "LHR", OriginLat: &lat, OriginLon: &lon,
		DestIATA: "SFO", Status: "Enroute", Notes: "n",
	}
	latest := &store.Position{Lat: 10, Lon: 20}
	track := []*store.Position{{Lat: 1, Lon: 2}, {Lat: 3, Lon: 4}}
	d := ToFlightDTO(f, []int64{5, 6}, []int64{8}, latest, track)
	if d.ID != 7 || d.Ident != "BA286" || d.Status != "Enroute" {
		t.Errorf("unexpected dto: %+v", d)
	}
	if len(d.PassengerIDs) != 2 || d.LatestPosition == nil || len(d.Track) != 2 {
		t.Errorf("nested fields wrong: %+v", d)
	}
	if len(d.SharedUserIDs) != 1 || d.SharedUserIDs[0] != 8 {
		t.Errorf("SharedUserIDs wrong: %v", d.SharedUserIDs)
	}
	if d.Track[1].Lat != 3 {
		t.Errorf("track order wrong")
	}
}

func TestToFlightDTONilsAndEmptyTrack(t *testing.T) {
	f := &store.Flight{ID: 1, Ident: "X1"}
	d := ToFlightDTO(f, nil, nil, nil, nil)
	if d.PassengerIDs == nil || len(d.PassengerIDs) != 0 {
		t.Errorf("nil passengers should become empty slice, got %v", d.PassengerIDs)
	}
	if d.SharedUserIDs == nil || len(d.SharedUserIDs) != 0 {
		t.Errorf("nil shared should become empty slice, got %v", d.SharedUserIDs)
	}
	if d.IsPublic {
		t.Error("IsPublic should default false")
	}
	if d.LatestPosition != nil {
		t.Error("LatestPosition should be nil")
	}
	if d.Track != nil {
		t.Error("Track should be nil when empty")
	}
}
