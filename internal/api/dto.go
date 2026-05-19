// Package api holds the JSON DTOs shared by the HTTP handlers and the poller's
// SSE broadcasts. Keeping them out of the handlers package avoids the poller
// having to import handlers (a cyclic-ish dependency in spirit).
package api

import (
	"time"

	"github.com/dpage/flight-tracker/internal/store"
)

type UserDTO struct {
	ID          int64      `json:"id"`
	GitHubLogin string     `json:"github_login"`
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
		GitHubLogin: u.GitHubLogin,
		Name:        u.Name,
		AvatarURL:   u.AvatarURL,
		IsSuperuser: u.IsSuperuser,
		IsActive:    u.IsActive,
		HasLoggedIn: u.GitHubID != nil,
		LastLoginAt: u.LastLoginAt,
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
	ID             int64        `json:"id"`
	Ident          string       `json:"ident"`
	ICAO24         *string      `json:"icao24,omitempty"`
	ScheduledOut   time.Time    `json:"scheduled_out"`
	ScheduledIn    time.Time    `json:"scheduled_in"`
	EstimatedOut   *time.Time   `json:"estimated_out,omitempty"`
	EstimatedIn    *time.Time   `json:"estimated_in,omitempty"`
	ActualOut      *time.Time   `json:"actual_out,omitempty"`
	ActualIn       *time.Time   `json:"actual_in,omitempty"`
	OriginIATA     string       `json:"origin_iata"`
	OriginLat      *float64     `json:"origin_lat,omitempty"`
	OriginLon      *float64     `json:"origin_lon,omitempty"`
	DestIATA       string       `json:"dest_iata"`
	DestLat        *float64     `json:"dest_lat,omitempty"`
	DestLon        *float64     `json:"dest_lon,omitempty"`
	Status         string       `json:"status"`
	Notes          string       `json:"notes"`
	LastPolledAt   *time.Time   `json:"last_polled_at,omitempty"`
	CreatedBy      *int64       `json:"created_by,omitempty"`
	PassengerIDs   []int64      `json:"passenger_ids"`
	LatestPosition *PositionDTO `json:"latest_position,omitempty"`
}

func ToFlightDTO(f *store.Flight, passengerIDs []int64, latest *store.Position) FlightDTO {
	if passengerIDs == nil {
		passengerIDs = []int64{}
	}
	dto := FlightDTO{
		ID:           f.ID,
		Ident:        f.Ident,
		ICAO24:       f.ICAO24,
		ScheduledOut: f.ScheduledOut,
		ScheduledIn:  f.ScheduledIn,
		EstimatedOut: f.EstimatedOut,
		EstimatedIn:  f.EstimatedIn,
		ActualOut:    f.ActualOut,
		ActualIn:     f.ActualIn,
		OriginIATA:   f.OriginIATA,
		OriginLat:    f.OriginLat,
		OriginLon:    f.OriginLon,
		DestIATA:     f.DestIATA,
		DestLat:      f.DestLat,
		DestLon:      f.DestLon,
		Status:       f.Status,
		Notes:        f.Notes,
		LastPolledAt: f.LastPolledAt,
		CreatedBy:    f.CreatedBy,
		PassengerIDs: passengerIDs,
	}
	if latest != nil {
		p := ToPositionDTO(latest)
		dto.LatestPosition = &p
	}
	return dto
}
