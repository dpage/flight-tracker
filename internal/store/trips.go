package store

import (
	"context"
	"time"
)

// Trip is the top-level container: a set of plans, a membership/visibility
// scope, and a tag bucket. starts_on / ends_on are nullable hints; the
// effective span is usually derived from the trip's plan_parts.
type Trip struct {
	ID          int64
	Name        string
	Destination string
	StartsOn    *time.Time
	EndsOn      *time.Time
	CreatedBy   *int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TripMember is one (trip, user, role) edge — the sharing boundary. Role is
// one of owner / editor / viewer (DB CHECK).
type TripMember struct {
	TripID  int64
	UserID  int64
	Role    string
	AddedAt time.Time
}

// CreateTripPayload carries the fields a caller may set when creating a trip.
type CreateTripPayload struct {
	Name        string
	Destination string
	StartsOn    *time.Time
	EndsOn      *time.Time
}

// UpdateTripPayload carries the optionally-set fields of a trip edit. A nil
// pointer means "leave this column untouched".
type UpdateTripPayload struct {
	Name        *string
	Destination *string
	StartsOn    *time.Time
	EndsOn      *time.Time
}

// ListTrips returns the trips the viewer can see (member of, or owner).
func (s *Store) ListTrips(ctx context.Context, viewerID int64) ([]*Trip, error) {
	return nil, ErrNotImplemented
}

// TripByID returns a single trip by id.
func (s *Store) TripByID(ctx context.Context, id int64) (*Trip, error) {
	return nil, ErrNotImplemented
}

// CreateTrip inserts a trip and an owner trip_members row for createdBy.
func (s *Store) CreateTrip(ctx context.Context, in CreateTripPayload, createdBy int64) (*Trip, error) {
	return nil, ErrNotImplemented
}

// UpdateTrip applies the supplied fields to a trip.
func (s *Store) UpdateTrip(ctx context.Context, id int64, in UpdateTripPayload) (*Trip, error) {
	return nil, ErrNotImplemented
}

// DeleteTrip removes a trip and (via cascade) its plans, parts, and members.
func (s *Store) DeleteTrip(ctx context.Context, id int64) error {
	return ErrNotImplemented
}

// TripMembers returns the membership rows for a trip.
func (s *Store) TripMembers(ctx context.Context, tripID int64) ([]*TripMember, error) {
	return nil, ErrNotImplemented
}

// AddTripMember inserts or updates a (trip, user) membership at the given role.
func (s *Store) AddTripMember(ctx context.Context, tripID, userID int64, role string) error {
	return ErrNotImplemented
}

// RemoveTripMember drops a (trip, user) membership.
func (s *Store) RemoveTripMember(ctx context.Context, tripID, userID int64) error {
	return ErrNotImplemented
}

// TripRole returns the viewer's role on the trip ("owner"|"editor"|"viewer"),
// or ErrNotFound if they are not a member.
func (s *Store) TripRole(ctx context.Context, tripID, viewerID int64) (string, error) {
	return "", ErrNotImplemented
}

// CanEditTrip reports whether the viewer may mutate the trip's plans/parts
// (owner or editor).
func (s *Store) CanEditTrip(ctx context.Context, tripID, viewerID int64) (bool, error) {
	return false, ErrNotImplemented
}
