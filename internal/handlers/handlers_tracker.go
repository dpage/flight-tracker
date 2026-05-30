package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/store"
)

// Tracker re-scope (spec §7). Two read views over flight plan_parts + positions,
// gated by the §4 plan-visibility predicate. There is NO leaderboard / ranking:
// the payload is just labelled parts + their latest positions.
//
//   - getTracker      — GET /api/tracker (convergence): every visible flight
//     part whose effective arrival falls within [now-before, now+after], with
//     latest positions. ?window_before / ?window_after are duration strings
//     ("7d", "12h"); ?tag scopes to a tag and, when no explicit window is given,
//     derives the default window from the tagged trips' span (spec §7).
//   - getTrackerPart  — the focused single-flight view (one part + its track).
//     Wired by a later wave when its route lands (handlers.go is owned by Wave
//     0a); the store capability and DTO it returns are exercised by 1C tests.

// defaultTrackerWindow is the fallback half-window when neither an explicit
// param nor a tag-derived span is available — matches the front end's 7d/7d
// default in trackerSlice.ts.
const defaultTrackerWindow = 7 * 24 * time.Hour

func (a *API) getTracker(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))

	before, beforeOK := parseWindow(r.URL.Query().Get("window_before"))
	after, afterOK := parseWindow(r.URL.Query().Get("window_after"))

	now := time.Now()
	var from, to time.Time

	// When the caller gave no explicit window AND a tag is set, derive the
	// default span server-side from the tagged trips' min/max (spec §7). An
	// explicit window always wins — it stays overridable.
	if !beforeOK && !afterOK && tag != "" {
		spanFrom, spanTo, ok, err := a.Store.TaggedTripSpan(r.Context(), me.ID, tag)
		if err != nil {
			handleStoreErr(w, err)
			return
		}
		if ok {
			from, to = spanFrom, spanTo
		}
	}
	if from.IsZero() && to.IsZero() {
		if !beforeOK {
			before = defaultTrackerWindow
		}
		if !afterOK {
			after = defaultTrackerWindow
		}
		from, to = now.Add(-before), now.Add(after)
	}

	parts, err := a.Store.ConvergenceParts(r.Context(), me.ID, from, to, tag)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		ids = append(ids, p.PlanPartID)
	}
	latest, err := a.Store.LatestPartPositions(r.Context(), ids)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.TrackerPartDTO, 0, len(parts))
	for _, p := range parts {
		out = append(out, toTrackerPartDTO(p, latest[p.PlanPartID]))
	}
	writeJSON(w, http.StatusOK, out)
}

// getTrackerPart is the focused single-flight view: one trackable part with its
// latest position. 404 (not 403) when the viewer can't see it, so part
// existence isn't leaked. Backs the FlightDetailPanel / FlightMap.
func (a *API) getTrackerPart(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	tp, err := a.Store.TrackerPartByID(r.Context(), me.ID, id)
	if err != nil {
		handleStoreErr(w, err) // ErrNotFound → 404, covers the hidden case
		return
	}
	latest, _ := a.Store.LatestPartPositions(r.Context(), []int64{id})
	writeJSON(w, http.StatusOK, toTrackerPartDTO(tp, latest[id]))
}

// toTrackerPartDTO projects a store.TrackerPart (+ optional latest position)
// into the locked TrackerPartDTO. Built here rather than in the api package so
// dto.go's json tags stay untouched.
func toTrackerPartDTO(p *store.TrackerPart, latest *store.Position) api.TrackerPartDTO {
	dto := api.TrackerPartDTO{
		PlanPartID:  p.PlanPartID,
		PlanID:      p.PlanID,
		TripID:      p.TripID,
		OwnerID:     p.OwnerID,
		Title:       p.Title,
		Status:      p.Status,
		EffectiveAt: p.EffectiveAt,
		Ident:       p.Ident,
		DestIATA:    p.DestIATA,
	}
	if latest != nil {
		pd := api.ToPositionDTO(latest)
		dto.LatestPosition = &pd
	}
	return dto
}

// parseWindow parses a window duration string. It accepts a trailing "d" (days)
// in addition to Go's stdlib units (h/m/s) since the front end sends "7d". An
// empty / unparseable / non-positive value reports ok=false so the caller falls
// back to its default.
func parseWindow(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n <= 0 {
			return 0, false
		}
		return time.Duration(n) * 24 * time.Hour, true
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}
