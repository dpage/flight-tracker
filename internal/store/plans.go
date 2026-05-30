package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Plan is a booking: the unit of sharing, privacy, passengers, and
// confirmation identity. Its timeline entries are PlanParts; the per-type
// detail lives in a 1:1 satellite selected by Type.
type Plan struct {
	ID              int64
	TripID          int64
	Type            string // flight|train|hotel|ground|dining|excursion
	Title           string
	ConfirmationRef string
	Notes           string
	Source          string // manual|paste|upload|email
	CreatedBy       *int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PlanPart is the spine: one timeline entry — a time range with a start and
// end place, a status, and an optional supersession link. Type-specific data
// hangs off the matching *Detail satellite keyed on the part id.
type PlanPart struct {
	ID           int64
	PlanID       int64
	Type         string // mirror of the owning plan's type, for convenience
	Seq          int
	StartsAt     time.Time
	EndsAt       *time.Time
	StartTZ      string
	EndTZ        string
	StartLabel   string
	StartLat     *float64
	StartLon     *float64
	EndLabel     string
	EndLat       *float64
	EndLon       *float64
	Status       string // planned|confirmed|cancelled
	SupersedesID *int64
	DismissedAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// EffectiveAt returns the time the front end sorts/renders a part by:
// COALESCE(actual, estimated, scheduled). For non-flight parts there are no
// estimated/actual times, so it is simply StartsAt; flight parts override via
// their detail (see EffectiveAt on FlightDetail). Kept as a helper so the rule
// lives in one place (mirrors flights.go's COALESCE).
func (p *PlanPart) EffectiveAt() time.Time { return p.StartsAt }

// FlightDetail is the flight-type satellite: the tracker-specific machinery
// (the three time pairs, the rich status enum, airframe ids, poll timestamps).
type FlightDetail struct {
	PlanPartID     int64
	Ident          string
	ICAO24         *string
	Callsign       *string
	ScheduledOut   time.Time
	ScheduledIn    time.Time
	EstimatedOut   *time.Time
	EstimatedIn    *time.Time
	ActualOut      *time.Time
	ActualIn       *time.Time
	OriginIATA     string
	DestIATA       string
	FlightStatus   string
	LastPolledAt   *time.Time
	LastResolvedAt *time.Time
}

// EffectiveOut / EffectiveIn collapse the three time pairs the way the tracker
// does: prefer actual, then estimated, then scheduled.
func (d *FlightDetail) EffectiveOut() time.Time {
	return coalesceTime(d.ActualOut, d.EstimatedOut, &d.ScheduledOut)
}

func (d *FlightDetail) EffectiveIn() time.Time {
	return coalesceTime(d.ActualIn, d.EstimatedIn, &d.ScheduledIn)
}

// coalesceTime returns the first non-nil time in priority order. The final
// argument is expected to be the always-present scheduled fallback.
func coalesceTime(ts ...*time.Time) time.Time {
	for _, t := range ts {
		if t != nil {
			return *t
		}
	}
	return time.Time{}
}

// HotelDetail is the hotel-type satellite. The actual check-in/out instants
// are the part's StartsAt / EndsAt; StandardCheckin/Checkout are local
// time-of-day hints for the smart-times calc.
type HotelDetail struct {
	PlanPartID       int64
	PropertyName     string
	Address          string
	Phone            string
	RoomType         string
	Guests           *int
	StandardCheckin  *string // "HH:MM" local, nil → default
	StandardCheckout *string
}

// TrainDetail is the train-type satellite.
type TrainDetail struct {
	PlanPartID int64
	Operator   string
	ServiceNo  string
	Coach      string
	Seat       string
	Class      string
	Platform   string
}

// GroundDetail is the ground-transport satellite (pickup/dropoff).
type GroundDetail struct {
	PlanPartID int64
	Provider   string
	Phone      string
	Vehicle    string
	Driver     string
	Pax        *int
}

// DiningDetail is the dining-reservation satellite.
type DiningDetail struct {
	PlanPartID      int64
	PartySize       *int
	ReservationName string
	Phone           string
}

// ExcursionDetail is the excursion/activity satellite.
type ExcursionDetail struct {
	PlanPartID  int64
	Provider    string
	TicketCount *int
}

// CreatePlanPayload bundles a plan plus its parts and per-type details for an
// atomic insert. The detail slices are written according to Type.
type CreatePlanPayload struct {
	TripID          int64
	Type            string
	Title           string
	ConfirmationRef string
	Notes           string
	Source          string
	Parts           []CreatePlanPartPayload
}

// CreatePlanPartPayload is one part to insert under a plan, with at most one
// populated detail matching the plan's type.
type CreatePlanPartPayload struct {
	Seq          int
	StartsAt     time.Time
	EndsAt       *time.Time
	StartTZ      string
	EndTZ        string
	StartLabel   string
	StartLat     *float64
	StartLon     *float64
	EndLabel     string
	EndLat       *float64
	EndLon       *float64
	Status       string
	SupersedesID *int64

	Flight    *FlightDetail
	Hotel     *HotelDetail
	Train     *TrainDetail
	Ground    *GroundDetail
	Dining    *DiningDetail
	Excursion *ExcursionDetail
}

// UpdatePlanPayload carries the optionally-set fields of a plan edit.
type UpdatePlanPayload struct {
	Title           *string
	ConfirmationRef *string
	Notes           *string
}

// UpdatePlanPartPayload carries the optionally-set fields of a part edit
// (time/place/status).
type UpdatePlanPartPayload struct {
	StartsAt   *time.Time
	EndsAt     *time.Time
	StartTZ    *string
	EndTZ      *string
	StartLabel *string
	StartLat   *float64
	StartLon   *float64
	EndLabel   *string
	EndLat     *float64
	EndLon     *float64
	Status     *string
}

// ----- Stubbed CRUD (filled in by Wave 1B) -----

// CreatePlan inserts a plan, its parts, and the matching detail rows.
func (s *Store) CreatePlan(ctx context.Context, in CreatePlanPayload, createdBy int64) (*Plan, error) {
	return nil, ErrNotImplemented
}

// PlanByID returns a single plan by id.
func (s *Store) PlanByID(ctx context.Context, id int64) (*Plan, error) {
	return nil, ErrNotImplemented
}

// PlansByTrip returns the plans in a trip.
func (s *Store) PlansByTrip(ctx context.Context, tripID int64) ([]*Plan, error) {
	return nil, ErrNotImplemented
}

// UpdatePlan applies the supplied fields to a plan.
func (s *Store) UpdatePlan(ctx context.Context, id int64, in UpdatePlanPayload) (*Plan, error) {
	return nil, ErrNotImplemented
}

// DeletePlan removes a plan and its parts/details (cascade).
func (s *Store) DeletePlan(ctx context.Context, id int64) error {
	return ErrNotImplemented
}

// MovePlan reassigns a plan (and its parts/passengers/visibility) to another
// trip. Visibility is thereafter evaluated against the destination trip.
func (s *Store) MovePlan(ctx context.Context, planID, destTripID int64) error {
	return ErrNotImplemented
}

// PartsByPlan returns the parts of a plan, ordered by seq.
func (s *Store) PartsByPlan(ctx context.Context, planID int64) ([]*PlanPart, error) {
	return nil, ErrNotImplemented
}

// PlanPartByID returns a single part by id.
func (s *Store) PlanPartByID(ctx context.Context, id int64) (*PlanPart, error) {
	return nil, ErrNotImplemented
}

// UpdatePlanPart applies the supplied fields to a part.
func (s *Store) UpdatePlanPart(ctx context.Context, id int64, in UpdatePlanPartPayload) (*PlanPart, error) {
	return nil, ErrNotImplemented
}

// DismissPlanPart stamps dismissed_at so a superseded part drops off the
// timeline.
func (s *Store) DismissPlanPart(ctx context.Context, id int64) error {
	return ErrNotImplemented
}

// ----- Passengers (the trigger keeps trip_members in sync) -----

// AddPlanPassenger adds a passenger to a plan. The DB trigger ensures the
// matching trip_members viewer row.
func (s *Store) AddPlanPassenger(ctx context.Context, planID, userID int64) error {
	return ErrNotImplemented
}

// RemovePlanPassenger drops a plan passenger (the trip membership is left
// intact — once on the trip, they stay a viewer).
func (s *Store) RemovePlanPassenger(ctx context.Context, planID, userID int64) error {
	return ErrNotImplemented
}

// PassengersByPlan returns a plan_id → []user_id map for the given plans.
func (s *Store) PassengersByPlan(ctx context.Context, planIDs []int64) (map[int64][]int64, error) {
	return nil, ErrNotImplemented
}

// ----- Per-plan visibility -----

// PlanVisibility is the per-plan privacy override. A nil result (ErrNotFound)
// means the default "everyone on the trip".
type PlanVisibility struct {
	PlanID  int64
	Mode    string // hidden_from|only_visible_to
	UserIDs []int64
}

// PlanVisibilityFor returns the per-plan visibility row, or ErrNotFound when
// the plan uses the default everyone-on-trip rule.
func (s *Store) PlanVisibilityFor(ctx context.Context, planID int64) (*PlanVisibility, error) {
	return nil, ErrNotImplemented
}

// SetPlanVisibility writes the parent mode row and member list atomically. An
// empty mode clears the override (back to everyone-on-trip).
func (s *Store) SetPlanVisibility(ctx context.Context, planID int64, mode string, userIDs []int64) error {
	return ErrNotImplemented
}

// ----- Visibility predicate (implemented now — spec §4) -----

// canViewPlanPredicate is the SQL fragment of the spec §4 plan-visibility
// rule, parameterised on $1 = planID, $2 = viewerID. It is shared by
// CanViewPlan and ListVisiblePlanParts so the rule lives in exactly one place
// (replacing the three duplicated flight predicates).
//
// A viewer V can see plan P in trip T when V is on T (or owns it) AND P is not
// hidden from V — the creator, passengers, and the trip owner are granted
// before plan_visibility is consulted, so a stray hidden_from row naming one of
// them is inert.
const canViewPlanPredicate = `
	EXISTS (
		SELECT 1 FROM plans p
		JOIN trips t ON t.id = p.trip_id
		WHERE p.id = $1
		  AND (
		       t.created_by = $2
		    OR (
		         EXISTS (SELECT 1 FROM trip_members tm
		                 WHERE tm.trip_id = p.trip_id AND tm.user_id = $2)
		         AND (
		              p.created_by = $2
		           OR EXISTS (SELECT 1 FROM plan_passengers pp
		                      WHERE pp.plan_id = p.id AND pp.user_id = $2)
		           OR NOT EXISTS (SELECT 1 FROM plan_visibility pv
		                          WHERE pv.plan_id = p.id)
		           OR EXISTS (SELECT 1 FROM plan_visibility pv
		                      WHERE pv.plan_id = p.id
		                        AND pv.mode = 'hidden_from'
		                        AND NOT EXISTS (SELECT 1 FROM plan_visibility_members m
		                                        WHERE m.plan_id = p.id AND m.user_id = $2))
		           OR EXISTS (SELECT 1 FROM plan_visibility pv
		                      JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
		                      WHERE pv.plan_id = p.id
		                        AND pv.mode = 'only_visible_to'
		                        AND m.user_id = $2)
		         )
		       )
		  )
	)`

// CanViewPlan reports whether viewerID may see planID under the spec §4
// predicate. showAllForSuperuser keeps the existing superuser bypass: when
// true (caller must verify the session is a superuser opting in), it is a mere
// existence check so a missing plan still returns false.
func (s *Store) CanViewPlan(ctx context.Context, planID, viewerID int64, showAllForSuperuser bool) (bool, error) {
	if showAllForSuperuser {
		var n int
		err := s.pool.QueryRow(ctx,
			`SELECT 1 FROM plans WHERE id = $1`, planID).Scan(&n)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT `+canViewPlanPredicate, planID, viewerID).Scan(&ok)
	return ok, err
}

// ListVisiblePlanPartsOpts narrows ListVisiblePlanParts. A nil time bound is
// open-ended; TripID==0 means "any trip the viewer can see".
type ListVisiblePlanPartsOpts struct {
	TripID              int64
	ShowAllForSuperuser bool
	IncludeDismissed    bool
	// Type, when non-empty, restricts to plans of that type (e.g. "flight"
	// for the tracker).
	Type string
}

// ListVisiblePlanParts returns the parts the viewer is allowed to see (their
// plan passes the §4 predicate), newest-startable last. Bodies for the join to
// satellite details are filled in by the feature waves; the visibility gate is
// authoritative here.
func (s *Store) ListVisiblePlanParts(ctx context.Context, viewerID int64, opts ListVisiblePlanPartsOpts) ([]*PlanPart, error) {
	conds := []string{}
	args := []any{viewerID}
	// The predicate keys on $1=planID, $2=viewerID; here viewerID is $1 and we
	// correlate planID to the outer row, so we inline an adapted form rather
	// than reuse canViewPlanPredicate verbatim.
	visible := `(
		t.created_by = $1
	 OR (
		  EXISTS (SELECT 1 FROM trip_members tm
		          WHERE tm.trip_id = pl.trip_id AND tm.user_id = $1)
		  AND (
		       pl.created_by = $1
		    OR EXISTS (SELECT 1 FROM plan_passengers pp
		               WHERE pp.plan_id = pl.id AND pp.user_id = $1)
		    OR NOT EXISTS (SELECT 1 FROM plan_visibility pv WHERE pv.plan_id = pl.id)
		    OR EXISTS (SELECT 1 FROM plan_visibility pv
		               WHERE pv.plan_id = pl.id AND pv.mode = 'hidden_from'
		                 AND NOT EXISTS (SELECT 1 FROM plan_visibility_members m
		                                 WHERE m.plan_id = pl.id AND m.user_id = $1))
		    OR EXISTS (SELECT 1 FROM plan_visibility pv
		               JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
		               WHERE pv.plan_id = pl.id AND pv.mode = 'only_visible_to'
		                 AND m.user_id = $1)
		  )
		)
	)`
	if !opts.ShowAllForSuperuser {
		conds = append(conds, visible)
	}
	if opts.TripID != 0 {
		args = append(args, opts.TripID)
		conds = append(conds, `pl.trip_id = $`+strconv.Itoa(len(args)))
	}
	if opts.Type != "" {
		args = append(args, opts.Type)
		conds = append(conds, `pl.type = $`+strconv.Itoa(len(args)))
	}
	if !opts.IncludeDismissed {
		conds = append(conds, `part.dismissed_at IS NULL`)
	}
	q := `SELECT part.id, part.plan_id, pl.type, part.seq, part.starts_at,
		part.ends_at, part.start_tz, part.end_tz,
		part.start_label, part.start_lat, part.start_lon,
		part.end_label, part.end_lat, part.end_lon,
		part.status, part.supersedes_id, part.dismissed_at,
		part.created_at, part.updated_at
		FROM plan_parts part
		JOIN plans pl ON pl.id = part.plan_id
		JOIN trips t ON t.id = pl.trip_id`
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY part.starts_at ASC"
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PlanPart
	for rows.Next() {
		var p PlanPart
		if err := rows.Scan(&p.ID, &p.PlanID, &p.Type, &p.Seq, &p.StartsAt,
			&p.EndsAt, &p.StartTZ, &p.EndTZ,
			&p.StartLabel, &p.StartLat, &p.StartLon,
			&p.EndLabel, &p.EndLat, &p.EndLon,
			&p.Status, &p.SupersedesID, &p.DismissedAt,
			&p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// VisiblePlanUserIDs returns the user IDs that can see the plan through any
// non-superuser path — used by publishers to populate the VisibleTo set on
// SSE events. It is the set form of the §4 predicate: trip owner + every trip
// member who passes the per-plan rule, unioned with passengers and the plan
// creator (who are always granted).
//
// Named VisiblePlanUserIDs (not VisibleUserIDs) because the legacy
// flights.go still defines a flight-keyed VisibleUserIDs; Wave 3 retires that
// one. Feature agents fanning out plan-part SSE events should call this.
func (s *Store) VisiblePlanUserIDs(ctx context.Context, planID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id FROM users u
		JOIN plans p ON p.id = $1
		WHERE
		     u.id = p.created_by
		  OR EXISTS (SELECT 1 FROM trips t WHERE t.id = p.trip_id AND t.created_by = u.id)
		  OR EXISTS (SELECT 1 FROM plan_passengers pp WHERE pp.plan_id = p.id AND pp.user_id = u.id)
		  OR (
		       EXISTS (SELECT 1 FROM trip_members tm WHERE tm.trip_id = p.trip_id AND tm.user_id = u.id)
		       AND (
		            NOT EXISTS (SELECT 1 FROM plan_visibility pv WHERE pv.plan_id = p.id)
		         OR EXISTS (SELECT 1 FROM plan_visibility pv
		                    WHERE pv.plan_id = p.id AND pv.mode = 'hidden_from'
		                      AND NOT EXISTS (SELECT 1 FROM plan_visibility_members m
		                                      WHERE m.plan_id = p.id AND m.user_id = u.id))
		         OR EXISTS (SELECT 1 FROM plan_visibility pv
		                    JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
		                    WHERE pv.plan_id = p.id AND pv.mode = 'only_visible_to'
		                      AND m.user_id = u.id)
		       )
		     )`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}
