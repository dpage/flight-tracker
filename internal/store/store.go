// Package store contains the typed pgx queries used by the HTTP and poller layers.
package store

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Pool returns the underlying connection pool. Intended for cross-package
// test setup that needs direct SQL access — production code should use
// the typed query methods on *Store instead.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

type User struct {
	ID          int64
	Username    string
	Name        string
	AvatarURL   string
	IsSuperuser bool
	IsActive    bool
	LastLoginAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// UserIdentity links a user to one external OAuth identity. A user may have
// multiple rows (e.g. linked GitHub + Google), keyed by (Provider, ProviderUserID).
type UserIdentity struct {
	ID             int64
	UserID         int64
	Provider       string
	ProviderUserID string
	CreatedAt      time.Time
	LastUsedAt     *time.Time
}

type Flight struct {
	ID           int64
	Ident        string
	ScheduledOut time.Time
	ScheduledIn  time.Time
	EstimatedOut *time.Time
	EstimatedIn  *time.Time
	ActualOut    *time.Time
	ActualIn     *time.Time
	OriginIATA   string
	OriginLat    *float64
	OriginLon    *float64
	DestIATA     string
	DestLat      *float64
	DestLon      *float64
	Status       string
	ICAO24       *string
	// Callsign is the ICAO radio identifier (e.g. "DLH493") used by the
	// aircraft's transponder. Populated by the resolver alongside ICAO24
	// once an airframe has been assigned to the leg; NULL until then.
	Callsign     *string
	LastPolledAt *time.Time
	// LastResolvedAt records the most recent successful or attempted
	// resolver call for this flight. The poller uses it to throttle
	// late-refresh attempts so we don't hammer AeroDataBox every tick.
	LastResolvedAt *time.Time
	CreatedBy      *int64
	Notes          string
	// IsPublic makes the flight visible to every authenticated user,
	// independent of the creator / passenger / share-list membership.
	IsPublic  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Position struct {
	FlightID      int64
	Ts            time.Time
	Lat           float64
	Lon           float64
	AltitudeFt    *int32
	GroundspeedKt *int32
	HeadingDeg    *int16
	IsEstimated   bool
}

// UserEmail is a verified or pending email address belonging to a user.
type UserEmail struct {
	ID           int64
	UserID       int64
	Address      string
	Verified     bool
	VerifyToken  *string
	VerifySentAt *time.Time
	VerifiedAt   *time.Time
	CreatedAt    time.Time
}

// EmailIngest records the outcome of processing one forwarded email.
type EmailIngest struct {
	ID            int64
	ReceivedAt    time.Time
	MessageID     *string
	FromAddress   string
	Subject       string
	DKIMPass      bool
	UserID        *int64
	Status        string
	FlightsAdded  int
	FlightsFailed int
	Error         string
}
