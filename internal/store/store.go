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

type User struct {
	ID          int64
	GitHubID    *int64
	GitHubLogin string
	Name        string
	AvatarURL   string
	IsSuperuser bool
	IsActive    bool
	LastLoginAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Flight struct {
	ID            int64
	Ident         string
	ScheduledOut  time.Time
	ScheduledIn   time.Time
	EstimatedOut  *time.Time
	EstimatedIn   *time.Time
	ActualOut     *time.Time
	ActualIn      *time.Time
	OriginIATA    string
	OriginLat     *float64
	OriginLon     *float64
	DestIATA      string
	DestLat       *float64
	DestLon       *float64
	Status        string
	AeroAPIID     *string
	ICAO24        *string
	LastPolledAt  *time.Time
	CreatedBy     *int64
	Notes         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
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
