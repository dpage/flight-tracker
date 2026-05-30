package planops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/store"
)

// Deps bundles the collaborators Propose and Commit need. Resolver may be nil
// to disable flight-schedule enrichment (flight parts then fall back to the
// schedule the extractor pulled from the email).
type Deps struct {
	Store     *store.Store
	Extractor Extractor
	Resolver  providers.Resolver
}

// ProposedPart is one extracted timeline entry awaiting confirmation. It mirrors
// the shape a store.CreatePlanPartPayload will take on commit, plus the place
// labels and a resolved/typed satellite detail when one was enriched.
type ProposedPart struct {
	Type       string
	StartsAt   time.Time
	EndsAt     *time.Time
	StartTZ    string
	EndTZ      string
	StartLabel string
	EndLabel   string
	Status     string

	Flight    *store.FlightDetail
	Hotel     *store.HotelDetail
	Train     *store.TrainDetail
	Ground    *store.GroundDetail
	Dining    *store.DiningDetail
	Excursion *store.ExcursionDetail
}

// ProposedPlan is a plan the ingest pipeline proposes, awaiting user
// confirmation (never auto-committed — spec §6.1). Confidence is 0..1.
// SupersedesPartID is set when a flight part matches an existing visible flight
// part in the trip (a proposed rebooking).
type ProposedPlan struct {
	Type             string
	Title            string
	ConfirmationRef  string
	Notes            string
	Confidence       float64
	Parts            []ProposedPart
	SupersedesPartID *int64
}

// confidenceScore maps the extractor's "high"|"medium"|"low" to a 0..1 score
// for the FE confirm step. Low parts are dropped upstream, so we only see
// high/medium here in practice.
func confidenceScore(s string) float64 {
	switch strings.ToLower(s) {
	case "high":
		return 0.95
	case "medium":
		return 0.6
	case "low":
		return 0.3
	default:
		return 0.6
	}
}

// Propose runs the extractor over the supplied text + documents, enriches
// flight parts via the resolver, runs the rebooking match against existing
// visible flight parts in the trip, and returns proposed plans for
// confirmation. Nothing is written here.
func Propose(ctx context.Context, deps Deps, userID, tripID int64, text string, docs []Document) ([]ProposedPlan, error) {
	if deps.Store == nil {
		return nil, errors.New("planops.Propose: nil Store")
	}
	if deps.Extractor == nil {
		return nil, errors.New("planops.Propose: nil Extractor")
	}
	extracted, err := deps.Extractor.ExtractPlans(ctx, text, docs)
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}

	// Gather the trip's existing visible flight parts once, for the rebooking
	// match. tripID==0 (email pre-trip-selection) yields an empty candidate set.
	var candidates []rebookCandidate
	if tripID != 0 {
		candidates, err = visibleFlightCandidates(ctx, deps, userID, tripID)
		if err != nil {
			return nil, err
		}
	}

	out := make([]ProposedPlan, 0, len(extracted))
	for _, ep := range extracted {
		pp := ProposedPlan{
			Type:            ep.Type,
			Title:           ep.Title,
			ConfirmationRef: ep.ConfirmationRef,
		}
		minConf := 1.0
		for _, part := range ep.Parts {
			converted, conf := proposePart(ctx, deps, part)
			pp.Parts = append(pp.Parts, converted)
			if conf < minConf {
				minConf = conf
			}
		}
		if len(pp.Parts) == 0 {
			continue
		}
		pp.Confidence = minConf
		// Rebooking match: only for single-flight plans (a rebooking replaces
		// one flight leg). Match against the trip's existing flight parts.
		if ep.Type == "flight" && len(pp.Parts) == 1 && pp.Parts[0].Flight != nil {
			if m := matchRebooking(ep.ConfirmationRef, pp.Parts[0].Flight, candidates); m != nil {
				id := m.partID
				pp.SupersedesPartID = &id
				if m.confidence > pp.Confidence {
					// A PNR match is high-confidence regardless of extraction.
					pp.Confidence = m.confidence
				}
			}
		}
		out = append(out, pp)
	}
	return out, nil
}

// proposePart converts one ExtractedPart into a ProposedPart, enriching flight
// parts via the resolver when possible. Returns the part and its 0..1
// confidence.
func proposePart(ctx context.Context, deps Deps, part ExtractedPart) (ProposedPart, float64) {
	conf := confidenceScore(part.Confidence)
	out := ProposedPart{Type: part.Type, Status: "planned", StartLabel: part.StartLabel, EndLabel: part.EndLabel}
	switch part.Type {
	case "flight":
		fd := enrichFlight(ctx, deps, part.Flight)
		out.Flight = fd
		out.StartsAt = fd.ScheduledOut
		end := fd.ScheduledIn
		out.EndsAt = &end
		if tz, ok := airports.LookupTZ(fd.OriginIATA); ok {
			out.StartTZ = tz
		}
		if tz, ok := airports.LookupTZ(fd.DestIATA); ok {
			out.EndTZ = tz
		}
		if out.StartLabel == "" {
			out.StartLabel = fd.OriginIATA
		}
		if out.EndLabel == "" {
			out.EndLabel = fd.DestIATA
		}
	case "hotel":
		out.StartsAt = combineLocal(part.StartDate, part.StartTime, 15)
		if part.EndDate != "" {
			e := combineLocal(part.EndDate, part.EndTime, 11)
			out.EndsAt = &e
		}
		out.Hotel = &store.HotelDetail{
			PropertyName: part.HotelName,
			Address:      part.Address,
			Phone:        part.Phone,
			RoomType:     part.RoomType,
		}
		if out.StartLabel == "" {
			out.StartLabel = part.HotelName
		}
	case "train":
		out.StartsAt = combineLocal(part.StartDate, part.StartTime, 9)
		if part.EndDate != "" || part.EndTime != "" {
			d := part.EndDate
			if d == "" {
				d = part.StartDate
			}
			e := combineLocal(d, part.EndTime, 9)
			out.EndsAt = &e
		}
		out.Train = &store.TrainDetail{
			Operator:  part.Operator,
			ServiceNo: part.ServiceNo,
			Class:     part.Class,
		}
	case "ground":
		out.StartsAt = combineLocal(part.StartDate, part.StartTime, 9)
		out.Ground = &store.GroundDetail{Provider: part.Provider, Vehicle: part.Vehicle}
	case "dining":
		out.StartsAt = combineLocal(part.StartDate, part.StartTime, 19)
		out.Dining = &store.DiningDetail{ReservationName: part.ReservationName}
	case "excursion":
		out.StartsAt = combineLocal(part.StartDate, part.StartTime, 9)
		out.Excursion = &store.ExcursionDetail{}
		if out.StartLabel == "" {
			out.StartLabel = part.ExcursionTitle
		}
	}
	return out, conf
}

// enrichFlight builds a FlightDetail for a flight part: it asks the resolver to
// fill in the schedule, falling back to the email's own schedule details (or
// bare scheduled-out=now placeholders) when the resolver has no record. The
// resolver gap is not fatal here — Propose surfaces what it has for the user to
// confirm/correct.
func enrichFlight(ctx context.Context, deps Deps, leg FlightFields) *store.FlightDetail {
	storedIdent := strings.ToUpper(strings.Join(strings.Fields(leg.Ident), ""))
	if deps.Resolver != nil {
		if d, err := time.Parse("2006-01-02", leg.Date); err == nil {
			if rf, rerr := deps.Resolver.Resolve(ctx, leg.Ident, d); rerr == nil {
				fd := &store.FlightDetail{
					Ident:        storedIdent,
					ScheduledOut: rf.ScheduledOut,
					ScheduledIn:  rf.ScheduledIn,
					OriginIATA:   rf.OriginIATA,
					DestIATA:     rf.DestIATA,
				}
				if rf.ICAO24 != "" {
					icao := rf.ICAO24
					fd.ICAO24 = &icao
				}
				return fd
			}
		}
	}
	// Fall back to the email's own schedule when we have it.
	out := flightFromLeg(leg, storedIdent)
	return out
}

// flightFromLeg builds a best-effort FlightDetail purely from the extracted
// leg, parsing local times in each airport's tz when present.
func flightFromLeg(leg FlightFields, storedIdent string) *store.FlightDetail {
	fd := &store.FlightDetail{Ident: storedIdent, OriginIATA: leg.OriginIATA, DestIATA: leg.DestIATA}
	depDate := leg.Date
	if leg.DepartTimeLocal != "" {
		fd.ScheduledOut = parseLocalInTZ(depDate, leg.DepartTimeLocal, leg.OriginIATA)
	} else {
		fd.ScheduledOut = combineLocal(depDate, "", 0)
	}
	arrDate := leg.ArriveDate
	if arrDate == "" {
		arrDate = leg.Date
	}
	if leg.ArriveTimeLocal != "" {
		fd.ScheduledIn = parseLocalInTZ(arrDate, leg.ArriveTimeLocal, leg.DestIATA)
	} else {
		fd.ScheduledIn = fd.ScheduledOut
	}
	return fd
}

// combineLocal builds a UTC instant from a YYYY-MM-DD date and an optional
// HH:MM time, defaulting to defaultHour when the time is absent. A blank date
// yields the zero time.
func combineLocal(date, hhmm string, defaultHour int) time.Time {
	if date == "" {
		return time.Time{}
	}
	if hhmm == "" {
		hhmm = fmt.Sprintf("%02d:00", defaultHour)
	}
	t, err := time.Parse("2006-01-02T15:04", date+"T"+hhmm)
	if err != nil {
		if d, derr := time.Parse("2006-01-02", date); derr == nil {
			return d
		}
		return time.Time{}
	}
	return t.UTC()
}

// parseLocalInTZ interprets date+time in the airport's tz, falling back to UTC.
func parseLocalInTZ(date, hhmm, iata string) time.Time {
	loc := time.UTC
	if tzName, ok := airports.LookupTZ(iata); ok {
		if l, err := time.LoadLocation(tzName); err == nil {
			loc = l
		}
	}
	t, err := time.ParseInLocation("2006-01-02T15:04", date+"T"+hhmm, loc)
	if err != nil {
		return combineLocal(date, hhmm, 0)
	}
	return t.UTC()
}
