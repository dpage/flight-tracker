// Package api holds the JSON DTOs shared by the HTTP handlers and the poller's
// SSE broadcasts. Keeping them out of the handlers package avoids the poller
// having to import handlers (a cyclic-ish dependency in spirit).
package api

import (
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/store"
)

type UserDTO struct {
	ID          int64      `json:"id"`
	Username    string     `json:"username"`
	Name        string     `json:"name"`
	AvatarURL   string     `json:"avatar_url"`
	IsSuperuser bool       `json:"is_superuser"`
	IsActive    bool       `json:"is_active"`
	HasLoggedIn bool       `json:"has_logged_in"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

func ToUserDTO(u *store.User) UserDTO {
	return UserDTO{
		ID:          u.ID,
		Username:    u.Username,
		Name:        u.Name,
		AvatarURL:   u.AvatarURL,
		IsSuperuser: u.IsSuperuser,
		IsActive:    u.IsActive,
		// A user has "logged in" once any provider has linked an identity
		// to them, which last_login_at tracks.
		HasLoggedIn: u.LastLoginAt != nil,
		LastLoginAt: u.LastLoginAt,
	}
}

// FriendshipDTO describes one row in /api/friends, oriented from the
// viewer's perspective. FriendID is the *other* user in the pair, never
// the viewer themselves. Direction is "outgoing" when the viewer initiated
// a pending request, "incoming" when the viewer needs to act on someone
// else's pending request, and "" (empty) for accepted friendships.
type FriendshipDTO struct {
	FriendID    int64      `json:"friend_id"`
	Status      string     `json:"status"` // "pending" | "accepted"
	Direction   string     `json:"direction,omitempty"`
	RequestedAt time.Time  `json:"requested_at"`
	AcceptedAt  *time.Time `json:"accepted_at,omitempty"`
}

// ToFriendshipDTO orients a *store.Friendship around viewerID and renders
// it for the wire. Callers must ensure viewerID is one of the pair.
func ToFriendshipDTO(f *store.Friendship, viewerID int64) FriendshipDTO {
	dto := FriendshipDTO{
		FriendID:    f.FriendID(viewerID),
		Status:      f.Status,
		RequestedAt: f.RequestedAt,
		AcceptedAt:  f.AcceptedAt,
	}
	if f.Status == "pending" {
		if f.RequestedBy == viewerID {
			dto.Direction = "outgoing"
		} else {
			dto.Direction = "incoming"
		}
	}
	return dto
}

type UserEmailDTO struct {
	ID         int64      `json:"id"`
	Address    string     `json:"address"`
	Verified   bool       `json:"verified"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func ToUserEmailDTO(e *store.UserEmail) UserEmailDTO {
	return UserEmailDTO{
		ID:         e.ID,
		Address:    e.Address,
		Verified:   e.Verified,
		VerifiedAt: e.VerifiedAt,
		CreatedAt:  e.CreatedAt,
	}
}

type PositionDTO struct {
	Ts            time.Time `json:"ts"`
	Lat           float64   `json:"lat"`
	Lon           float64   `json:"lon"`
	AltitudeFt    *int32    `json:"altitude_ft,omitempty"`
	GroundspeedKt *int32    `json:"groundspeed_kt,omitempty"`
	HeadingDeg    *int16    `json:"heading_deg,omitempty"`
	IsEstimated   bool      `json:"is_estimated"`
}

func ToPositionDTO(p *store.Position) PositionDTO {
	return PositionDTO{
		Ts: p.Ts, Lat: p.Lat, Lon: p.Lon,
		AltitudeFt: p.AltitudeFt, GroundspeedKt: p.GroundspeedKt, HeadingDeg: p.HeadingDeg,
		IsEstimated: p.IsEstimated,
	}
}

type FlightDTO struct {
	ID           int64      `json:"id"`
	Ident        string     `json:"ident"`
	ICAO24       *string    `json:"icao24,omitempty"`
	ScheduledOut time.Time  `json:"scheduled_out"`
	ScheduledIn  time.Time  `json:"scheduled_in"`
	EstimatedOut *time.Time `json:"estimated_out,omitempty"`
	EstimatedIn  *time.Time `json:"estimated_in,omitempty"`
	ActualOut    *time.Time `json:"actual_out,omitempty"`
	ActualIn     *time.Time `json:"actual_in,omitempty"`
	OriginIATA   string     `json:"origin_iata"`
	OriginLat    *float64   `json:"origin_lat,omitempty"`
	OriginLon    *float64   `json:"origin_lon,omitempty"`
	// OriginTZ / DestTZ are IANA timezone strings looked up from the
	// embedded airports table; empty when the IATA is unknown. The
	// frontend uses them to render scheduled times in airport-local
	// time on both ends of the trip.
	OriginTZ     string       `json:"origin_tz,omitempty"`
	DestIATA     string       `json:"dest_iata"`
	DestLat      *float64     `json:"dest_lat,omitempty"`
	DestLon      *float64     `json:"dest_lon,omitempty"`
	DestTZ       string       `json:"dest_tz,omitempty"`
	Status       string       `json:"status"`
	Notes        string       `json:"notes"`
	LastPolledAt *time.Time   `json:"last_polled_at,omitempty"`
	CreatedBy    *int64       `json:"created_by,omitempty"`
	PassengerIDs []int64      `json:"passenger_ids"`
	// IsPublic flips the flight to "visible to every authenticated user".
	IsPublic bool `json:"is_public"`
	// SharedUserIDs lists explicit share-list members. Always non-nil
	// (empty slice when nobody has been explicitly shared with).
	SharedUserIDs  []int64      `json:"shared_user_ids"`
	LatestPosition *PositionDTO `json:"latest_position,omitempty"`
	// Recent positions, oldest → newest, used to draw the flown track on the
	// map. nil when there is no track yet.
	Track []PositionDTO `json:"track,omitempty"`
}

func ToFlightDTO(
	f *store.Flight,
	passengerIDs []int64,
	sharedUserIDs []int64,
	latest *store.Position,
	track []*store.Position,
) FlightDTO {
	if passengerIDs == nil {
		passengerIDs = []int64{}
	}
	if sharedUserIDs == nil {
		sharedUserIDs = []int64{}
	}
	originTZ, _ := airports.LookupTZ(f.OriginIATA)
	destTZ, _ := airports.LookupTZ(f.DestIATA)
	dto := FlightDTO{
		ID:            f.ID,
		Ident:         f.Ident,
		ICAO24:        f.ICAO24,
		ScheduledOut:  f.ScheduledOut,
		ScheduledIn:   f.ScheduledIn,
		EstimatedOut:  f.EstimatedOut,
		EstimatedIn:   f.EstimatedIn,
		ActualOut:     f.ActualOut,
		ActualIn:      f.ActualIn,
		OriginIATA:    f.OriginIATA,
		OriginLat:     f.OriginLat,
		OriginLon:     f.OriginLon,
		OriginTZ:      originTZ,
		DestIATA:      f.DestIATA,
		DestLat:       f.DestLat,
		DestLon:       f.DestLon,
		DestTZ:        destTZ,
		Status:        f.Status,
		Notes:         f.Notes,
		LastPolledAt:  f.LastPolledAt,
		CreatedBy:     f.CreatedBy,
		PassengerIDs:  passengerIDs,
		IsPublic:      f.IsPublic,
		SharedUserIDs: sharedUserIDs,
	}
	if latest != nil {
		p := ToPositionDTO(latest)
		dto.LatestPosition = &p
	}
	if len(track) > 0 {
		dto.Track = make([]PositionDTO, len(track))
		for i, p := range track {
			dto.Track[i] = ToPositionDTO(p)
		}
	}
	return dto
}
