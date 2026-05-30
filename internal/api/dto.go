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
// viewer's perspective. Direction is "outgoing" when the viewer initiated
// a pending request, "incoming" when the viewer needs to act on someone
// else's pending request, and "" (empty) for accepted friendships.
type FriendshipDTO struct {
	// FriendID is the *other* user in the pair. Omitted (zero on the wire)
	// for outgoing pending invites — those expose only the typed email so
	// the inviter never learns whether the target is a registered user.
	FriendID int64 `json:"friend_id,omitempty"`
	// Email is set only for outgoing pending invites and carries the
	// inviter-typed address. Omitted otherwise.
	Email       string     `json:"email,omitempty"`
	Status      string     `json:"status"` // "pending" | "accepted"
	Direction   string     `json:"direction,omitempty"`
	RequestedAt time.Time  `json:"requested_at"`
	AcceptedAt  *time.Time `json:"accepted_at,omitempty"`
}

// ToFriendshipDTO orients a *store.Friendship around viewerID and renders
// it for the wire. Callers must ensure viewerID is one of the pair.
func ToFriendshipDTO(f *store.Friendship, viewerID int64) FriendshipDTO {
	dto := FriendshipDTO{
		Status:      f.Status,
		RequestedAt: f.RequestedAt,
		AcceptedAt:  f.AcceptedAt,
	}
	if f.Status == "pending" {
		if f.RequestedBy == viewerID {
			dto.Direction = "outgoing"
			// No FriendID on the wire: it would let the inviter look up
			// the target in /api/users.
			dto.Email = f.InvitedEmail
			return dto
		}
		dto.Direction = "incoming"
	}
	dto.FriendID = f.FriendID(viewerID)
	return dto
}

// OutgoingInviteToFriendshipDTO renders a pending_friend_invites row as
// an outgoing-pending FriendshipDTO. Used by the list handler to union
// email-only invites (target not yet registered) with friendship rows.
func OutgoingInviteToFriendshipDTO(p *store.PendingFriendInvite) FriendshipDTO {
	return FriendshipDTO{
		Email:       p.EmailLower,
		Status:      "pending",
		Direction:   "outgoing",
		RequestedAt: p.CreatedAt,
	}
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
	OriginTZ     string     `json:"origin_tz,omitempty"`
	DestIATA     string     `json:"dest_iata"`
	DestLat      *float64   `json:"dest_lat,omitempty"`
	DestLon      *float64   `json:"dest_lon,omitempty"`
	DestTZ       string     `json:"dest_tz,omitempty"`
	Status       string     `json:"status"`
	Notes        string     `json:"notes"`
	LastPolledAt *time.Time `json:"last_polled_at,omitempty"`
	CreatedBy    *int64     `json:"created_by,omitempty"`
	PassengerIDs []int64    `json:"passenger_ids"`
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

// NotificationsDTO is the body of GET /api/notifications and the
// payload of notifications.updated SSE events. It is intentionally an
// open-shape struct: new notification kinds get added as new fields
// with omitempty, so older clients ignoring them keep working.
type NotificationsDTO struct {
	FriendRequestsPending int `json:"friend_requests_pending"`
}

// =====================================================================
// Trip-planning DTOs (LOCKED CONTRACT — shared verbatim with the frontend
// agent). Field names/types must not drift; see the Wave 0a contract in
// docs/plan/2026-05-29-trip-planning-implementation-plan.md §4.
// =====================================================================

// TripDTO is one trip with the viewer's role, members, and tags. Dates are
// "YYYY-MM-DD" strings (nullable); the effective span is derived client-side
// from the plans' parts when these are null.
type TripDTO struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Destination string          `json:"destination"`
	StartsOn    *string         `json:"starts_on,omitempty"` // YYYY-MM-DD
	EndsOn      *string         `json:"ends_on,omitempty"`
	CreatedBy   *int64          `json:"created_by,omitempty"`
	MyRole      string          `json:"my_role"` // owner|editor|viewer
	Members     []TripMemberDTO `json:"members"`
	Tags        []string        `json:"tags"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// TripMemberDTO is one membership edge.
type TripMemberDTO struct {
	UserID int64  `json:"user_id"`
	Role   string `json:"role"` // owner|editor|viewer
}

// PlanDTO is a booking with its parts and per-plan visibility. passenger_ids
// is always non-nil.
type PlanDTO struct {
	ID              int64             `json:"id"`
	TripID          int64             `json:"trip_id"`
	Type            string            `json:"type"`
	Title           string            `json:"title"`
	ConfirmationRef string            `json:"confirmation_ref"`
	Notes           string            `json:"notes"`
	Source          string            `json:"source"`
	CreatedBy       *int64            `json:"created_by,omitempty"`
	PassengerIDs    []int64           `json:"passenger_ids"`
	Visibility      PlanVisibilityDTO `json:"visibility"`
	Parts           []PlanPartDTO     `json:"parts"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// ProposedPlanDTO is a plan the ingest pipeline proposes, awaiting
// confirmation (matches the FE ProposedPlan). Confidence is 0..1;
// SupersedesPartID is set when the proposal would supersede an existing part
// (a rebooking).
type ProposedPlanDTO struct {
	Type             string        `json:"type"`
	Title            string        `json:"title"`
	ConfirmationRef  string        `json:"confirmation_ref"`
	Notes            string        `json:"notes"`
	Confidence       float64       `json:"confidence"`
	Parts            []PlanPartDTO `json:"parts"`
	SupersedesPartID *int64        `json:"supersedes_part_id,omitempty"`
}

// IngestResultDTO is the response of the propose endpoint (matches the FE
// IngestResult). Proposals is always non-nil.
type IngestResultDTO struct {
	Proposals []ProposedPlanDTO `json:"proposals"`
}

// PlanVisibilityDTO carries the per-plan privacy mode and named users.
// mode is "everyone" when no override row exists; user_ids is always non-nil.
type PlanVisibilityDTO struct {
	Mode    string  `json:"mode"` // everyone|hidden_from|only_visible_to
	UserIDs []int64 `json:"user_ids"`
}

// PlanPartDTO is one timeline entry. Exactly one of the typed detail pointers
// is populated, selected by Type. effective_at = COALESCE(actual, estimated,
// scheduled) so the front end sorts every type uniformly.
type PlanPartDTO struct {
	ID           int64               `json:"id"`
	PlanID       int64               `json:"plan_id"`
	Type         string              `json:"type"`
	Seq          int                 `json:"seq"`
	StartsAt     time.Time           `json:"starts_at"`
	EndsAt       *time.Time          `json:"ends_at,omitempty"`
	StartTZ      string              `json:"start_tz"`
	EndTZ        string              `json:"end_tz"`
	StartLabel   string              `json:"start_label"`
	StartLat     *float64            `json:"start_lat,omitempty"`
	StartLon     *float64            `json:"start_lon,omitempty"`
	EndLabel     string              `json:"end_label"`
	EndLat       *float64            `json:"end_lat,omitempty"`
	EndLon       *float64            `json:"end_lon,omitempty"`
	Status       string              `json:"status"`
	EffectiveAt  time.Time           `json:"effective_at"`
	SupersedesID *int64              `json:"supersedes_id,omitempty"`
	DismissedAt  *time.Time          `json:"dismissed_at,omitempty"`
	Flight       *FlightDetailDTO    `json:"flight,omitempty"`
	Hotel        *HotelDetailDTO     `json:"hotel,omitempty"`
	Train        *TrainDetailDTO     `json:"train,omitempty"`
	Ground       *GroundDetailDTO    `json:"ground,omitempty"`
	Dining       *DiningDetailDTO    `json:"dining,omitempty"`
	Excursion    *ExcursionDetailDTO `json:"excursion,omitempty"`
}

// FlightDetailDTO is the flight-type satellite payload, including tracker
// positions.
type FlightDetailDTO struct {
	Ident          string        `json:"ident"`
	ICAO24         *string       `json:"icao24,omitempty"`
	Callsign       string        `json:"callsign"`
	ScheduledOut   time.Time     `json:"scheduled_out"`
	ScheduledIn    time.Time     `json:"scheduled_in"`
	EstimatedOut   *time.Time    `json:"estimated_out,omitempty"`
	EstimatedIn    *time.Time    `json:"estimated_in,omitempty"`
	ActualOut      *time.Time    `json:"actual_out,omitempty"`
	ActualIn       *time.Time    `json:"actual_in,omitempty"`
	OriginIATA     string        `json:"origin_iata"`
	DestIATA       string        `json:"dest_iata"`
	FlightStatus   string        `json:"flight_status"`
	LastPolledAt   *time.Time    `json:"last_polled_at,omitempty"`
	LatestPosition *PositionDTO  `json:"latest_position,omitempty"`
	Track          []PositionDTO `json:"track,omitempty"`
}

// HotelDetailDTO is the hotel-type satellite payload. standard_checkin/out are
// "HH:MM" local; checkin_suggested/checkout_suggested are the derived smart
// times (§10), nil when not computed.
type HotelDetailDTO struct {
	PropertyName      string     `json:"property_name"`
	Address           string     `json:"address"`
	Phone             string     `json:"phone"`
	RoomType          string     `json:"room_type"`
	Guests            *int       `json:"guests,omitempty"`
	StandardCheckin   *string    `json:"standard_checkin,omitempty"` // HH:MM
	StandardCheckout  *string    `json:"standard_checkout,omitempty"`
	CheckinSuggested  *time.Time `json:"checkin_suggested,omitempty"`
	CheckoutSuggested *time.Time `json:"checkout_suggested,omitempty"`
}

// TrainDetailDTO is the train-type satellite payload.
type TrainDetailDTO struct {
	Operator  string `json:"operator"`
	ServiceNo string `json:"service_no"`
	Coach     string `json:"coach"`
	Seat      string `json:"seat"`
	Class     string `json:"class"`
	Platform  string `json:"platform"`
}

// GroundDetailDTO is the ground-transport satellite payload.
type GroundDetailDTO struct {
	Provider string `json:"provider"`
	Phone    string `json:"phone"`
	Vehicle  string `json:"vehicle"`
	Driver   string `json:"driver"`
	Pax      *int   `json:"pax,omitempty"`
}

// DiningDetailDTO is the dining-reservation satellite payload.
type DiningDetailDTO struct {
	PartySize       *int   `json:"party_size,omitempty"`
	ReservationName string `json:"reservation_name"`
	Phone           string `json:"phone"`
}

// ExcursionDetailDTO is the excursion/activity satellite payload.
type ExcursionDetailDTO struct {
	Provider    string `json:"provider"`
	TicketCount *int   `json:"ticket_count,omitempty"`
}

// TrackerPartDTO is the convergence-view payload: a labelled trackable part
// with its latest position, flattened across plans/trips.
type TrackerPartDTO struct {
	PlanPartID     int64        `json:"plan_part_id"`
	PlanID         int64        `json:"plan_id"`
	TripID         int64        `json:"trip_id"`
	OwnerID        *int64       `json:"owner_id,omitempty"`
	Title          string       `json:"title"`
	Status         string       `json:"status"`
	EffectiveAt    time.Time    `json:"effective_at"`
	Ident          string       `json:"ident"`
	DestIATA       string       `json:"dest_iata"`
	LatestPosition *PositionDTO `json:"latest_position,omitempty"`
}

// TagSuggestionDTO is one autocomplete entry for GET /api/tags/suggest.
type TagSuggestionDTO struct {
	Label string `json:"label"`
}

// ToTripDTO renders a trip with the viewer's role, members, and tags. The
// caller supplies myRole, members, and tags (already gathered) so the DTO
// stays a pure projection. Wave 1A wires the gathering in handlers_trips.go.
func ToTripDTO(t *store.Trip, myRole string, members []TripMemberDTO, tags []string) TripDTO {
	if members == nil {
		members = []TripMemberDTO{}
	}
	if tags == nil {
		tags = []string{}
	}
	dto := TripDTO{
		ID:          t.ID,
		Name:        t.Name,
		Destination: t.Destination,
		CreatedBy:   t.CreatedBy,
		MyRole:      myRole,
		Members:     members,
		Tags:        tags,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
	if t.StartsOn != nil {
		s := t.StartsOn.Format("2006-01-02")
		dto.StartsOn = &s
	}
	if t.EndsOn != nil {
		s := t.EndsOn.Format("2006-01-02")
		dto.EndsOn = &s
	}
	return dto
}

// ToTripMemberDTO projects a membership row.
func ToTripMemberDTO(m *store.TripMember) TripMemberDTO {
	return TripMemberDTO{UserID: m.UserID, Role: m.Role}
}

// ToPlanPartDTO renders a part with its type-specific detail. The detail is
// passed in (already loaded by the store) as exactly one non-nil pointer; the
// TZ-lookup convenience that lived in ToFlightDTO now happens here, filling
// start_tz/end_tz from the airports table when the part left them blank.
//
// effective_at = COALESCE(actual, estimated, scheduled): for flight parts it
// uses the detail's effective departure; for every other type it is StartsAt.
func ToPlanPartDTO(
	p *store.PlanPart,
	flight *store.FlightDetail,
	hotel *store.HotelDetail,
	train *store.TrainDetail,
	ground *store.GroundDetail,
	dining *store.DiningDetail,
	excursion *store.ExcursionDetail,
	latest *store.Position,
	track []*store.Position,
) PlanPartDTO {
	startTZ, endTZ := p.StartTZ, p.EndTZ
	dto := PlanPartDTO{
		ID:           p.ID,
		PlanID:       p.PlanID,
		Type:         p.Type,
		Seq:          p.Seq,
		StartsAt:     p.StartsAt,
		EndsAt:       p.EndsAt,
		StartLabel:   p.StartLabel,
		StartLat:     p.StartLat,
		StartLon:     p.StartLon,
		EndLabel:     p.EndLabel,
		EndLat:       p.EndLat,
		EndLon:       p.EndLon,
		Status:       p.Status,
		EffectiveAt:  p.EffectiveAt(),
		SupersedesID: p.SupersedesID,
		DismissedAt:  p.DismissedAt,
	}
	switch {
	case flight != nil:
		// Fall back to airport TZ when the part didn't store one.
		if startTZ == "" {
			if tz, ok := airports.LookupTZ(flight.OriginIATA); ok {
				startTZ = tz
			}
		}
		if endTZ == "" {
			if tz, ok := airports.LookupTZ(flight.DestIATA); ok {
				endTZ = tz
			}
		}
		dto.Flight = ToFlightDetailDTO(flight, latest, track)
		dto.EffectiveAt = flight.EffectiveOut()
	case hotel != nil:
		dto.Hotel = ToHotelDetailDTO(hotel)
	case train != nil:
		dto.Train = ToTrainDetailDTO(train)
	case ground != nil:
		dto.Ground = ToGroundDetailDTO(ground)
	case dining != nil:
		dto.Dining = ToDiningDetailDTO(dining)
	case excursion != nil:
		dto.Excursion = ToExcursionDetailDTO(excursion)
	}
	dto.StartTZ = startTZ
	dto.EndTZ = endTZ
	return dto
}

// ToFlightDetailDTO mirrors the old ToFlightDTO position handling.
func ToFlightDetailDTO(d *store.FlightDetail, latest *store.Position, track []*store.Position) *FlightDetailDTO {
	callsign := ""
	if d.Callsign != nil {
		callsign = *d.Callsign
	}
	out := &FlightDetailDTO{
		Ident:        d.Ident,
		ICAO24:       d.ICAO24,
		Callsign:     callsign,
		ScheduledOut: d.ScheduledOut,
		ScheduledIn:  d.ScheduledIn,
		EstimatedOut: d.EstimatedOut,
		EstimatedIn:  d.EstimatedIn,
		ActualOut:    d.ActualOut,
		ActualIn:     d.ActualIn,
		OriginIATA:   d.OriginIATA,
		DestIATA:     d.DestIATA,
		FlightStatus: d.FlightStatus,
		LastPolledAt: d.LastPolledAt,
	}
	if latest != nil {
		pp := ToPositionDTO(latest)
		out.LatestPosition = &pp
	}
	if len(track) > 0 {
		out.Track = make([]PositionDTO, len(track))
		for i, p := range track {
			out.Track[i] = ToPositionDTO(p)
		}
	}
	return out
}

// ToHotelDetailDTO projects a hotel satellite. Suggested smart times are left
// nil here; Wave 1E computes and sets them at assembly time.
func ToHotelDetailDTO(d *store.HotelDetail) *HotelDetailDTO {
	return &HotelDetailDTO{
		PropertyName:     d.PropertyName,
		Address:          d.Address,
		Phone:            d.Phone,
		RoomType:         d.RoomType,
		Guests:           d.Guests,
		StandardCheckin:  d.StandardCheckin,
		StandardCheckout: d.StandardCheckout,
	}
}

// ToTrainDetailDTO projects a train satellite.
func ToTrainDetailDTO(d *store.TrainDetail) *TrainDetailDTO {
	return &TrainDetailDTO{
		Operator:  d.Operator,
		ServiceNo: d.ServiceNo,
		Coach:     d.Coach,
		Seat:      d.Seat,
		Class:     d.Class,
		Platform:  d.Platform,
	}
}

// ToGroundDetailDTO projects a ground satellite.
func ToGroundDetailDTO(d *store.GroundDetail) *GroundDetailDTO {
	return &GroundDetailDTO{
		Provider: d.Provider,
		Phone:    d.Phone,
		Vehicle:  d.Vehicle,
		Driver:   d.Driver,
		Pax:      d.Pax,
	}
}

// ToDiningDetailDTO projects a dining satellite.
func ToDiningDetailDTO(d *store.DiningDetail) *DiningDetailDTO {
	return &DiningDetailDTO{
		PartySize:       d.PartySize,
		ReservationName: d.ReservationName,
		Phone:           d.Phone,
	}
}

// ToExcursionDetailDTO projects an excursion satellite.
func ToExcursionDetailDTO(d *store.ExcursionDetail) *ExcursionDetailDTO {
	return &ExcursionDetailDTO{
		Provider:    d.Provider,
		TicketCount: d.TicketCount,
	}
}
