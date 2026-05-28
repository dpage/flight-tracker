package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

type createFlightReq struct {
	Ident         string    `json:"ident"`
	ScheduledOut  time.Time `json:"scheduled_out"`
	ScheduledIn   time.Time `json:"scheduled_in"`
	OriginIATA    string    `json:"origin_iata"`
	DestIATA      string    `json:"dest_iata"`
	ICAO24        string    `json:"icao24"`
	Notes         string    `json:"notes"`
	PassengerIDs  []int64   `json:"passenger_ids"`
	SharedUserIDs []int64   `json:"shared_user_ids"`
	IsPublic      bool      `json:"is_public"`
}

type updateFlightReq struct {
	ScheduledOut *time.Time `json:"scheduled_out,omitempty"`
	ScheduledIn  *time.Time `json:"scheduled_in,omitempty"`
	OriginIATA   *string    `json:"origin_iata,omitempty"`
	DestIATA     *string    `json:"dest_iata,omitempty"`
	ICAO24       *string    `json:"icao24,omitempty"`
	Notes        *string    `json:"notes,omitempty"`
	Status       *string    `json:"status,omitempty"`
	IsPublic     *bool      `json:"is_public,omitempty"`
}

type userIDReq struct {
	UserID int64 `json:"user_id"`
}

// listFlights returns flights the caller can see. Superusers may opt into
// an "all flights" view with ?show_all=1 (the param is silently ignored
// for non-superusers). Any authenticated caller may opt into the archive
// of flights whose effective arrival is more than 24 hours ago with
// ?show_old=1.
func (a *API) listFlights(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	showAll := wantsShowAll(r, me)
	showOld := wantsShowOld(r)
	flights, err := a.Store.ListVisibleFlights(r.Context(), me.ID, showAll, showOld)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	ids := make([]int64, 0, len(flights))
	for _, f := range flights {
		ids = append(ids, f.ID)
	}
	passengers, err := a.Store.PassengersByFlight(r.Context(), ids)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	shares, err := a.Store.SharedUserIDsByFlight(r.Context(), ids)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	latest, err := a.Store.LatestPositions(r.Context(), ids)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	tracks, err := a.Store.RecentTracks(r.Context(), ids, 200)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.FlightDTO, 0, len(flights))
	for _, f := range flights {
		out = append(out, api.ToFlightDTO(f, passengers[f.ID], shares[f.ID], latest[f.ID], tracks[f.ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) getFlight(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if ok, err := a.canView(r.Context(), id, me); err != nil {
		handleStoreErr(w, err)
		return
	} else if !ok {
		// 404 rather than 403 to avoid leaking flight existence to users
		// who aren't allowed to see it.
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	f, err := a.Store.FlightByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	passengers, _ := a.Store.PassengersByFlight(r.Context(), []int64{id})
	shares, _ := a.Store.SharedUserIDsByFlight(r.Context(), []int64{id})
	latest, _ := a.Store.LatestPositions(r.Context(), []int64{id})
	tracks, _ := a.Store.RecentTracks(r.Context(), []int64{id}, 200)
	writeJSON(w, http.StatusOK, api.ToFlightDTO(f, passengers[id], shares[id], latest[id], tracks[id]))
}

func (a *API) createFlight(w http.ResponseWriter, r *http.Request) {
	var in createFlightReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	me := auth.UserFrom(r.Context())
	for _, uid := range in.PassengerIDs {
		if uid == me.ID {
			continue
		}
		ok, err := a.Store.AreAcceptedFriends(r.Context(), me.ID, uid)
		if err != nil {
			handleStoreErr(w, err)
			return
		}
		if !ok {
			writeError(w, http.StatusBadRequest, "passenger is not a friend")
			return
		}
	}
	for _, uid := range in.SharedUserIDs {
		if uid == me.ID {
			continue
		}
		ok, err := a.Store.AreAcceptedFriends(r.Context(), me.ID, uid)
		if err != nil {
			handleStoreErr(w, err)
			return
		}
		if !ok {
			writeError(w, http.StatusBadRequest, "share target is not a friend")
			return
		}
	}
	f, err := a.Store.CreateFlight(r.Context(), store.CreateFlightPayload{
		Ident:        in.Ident,
		ScheduledOut: in.ScheduledOut,
		ScheduledIn:  in.ScheduledIn,
		OriginIATA:   in.OriginIATA,
		DestIATA:     in.DestIATA,
		ICAO24:       in.ICAO24,
		Notes:        in.Notes,
		IsPublic:     in.IsPublic,
	}, me.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f = a.backfillCoordsIfNeeded(r.Context(), f)
	for _, uid := range in.PassengerIDs {
		if err := a.Store.AddPassenger(r.Context(), f.ID, uid); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	for _, uid := range in.SharedUserIDs {
		if err := a.Store.AddShare(r.Context(), f.ID, uid); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	passengers, _ := a.Store.PassengersByFlight(r.Context(), []int64{f.ID})
	shares, _ := a.Store.SharedUserIDsByFlight(r.Context(), []int64{f.ID})
	dto := api.ToFlightDTO(f, passengers[f.ID], shares[f.ID], nil, nil)
	a.publishFlightDTO(r.Context(), dto)
	writeJSON(w, http.StatusCreated, dto)
}

func (a *API) updateFlight(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in updateFlightReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f, err := a.Store.UpdateFlight(r.Context(), id, store.UpdateFlightPayload{
		ScheduledOut: in.ScheduledOut,
		ScheduledIn:  in.ScheduledIn,
		OriginIATA:   in.OriginIATA,
		DestIATA:     in.DestIATA,
		ICAO24:       in.ICAO24,
		Notes:        in.Notes,
		Status:       in.Status,
		IsPublic:     in.IsPublic,
	})
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	f = a.backfillCoordsIfNeeded(r.Context(), f)
	passengers, _ := a.Store.PassengersByFlight(r.Context(), []int64{id})
	shares, _ := a.Store.SharedUserIDsByFlight(r.Context(), []int64{id})
	latest, _ := a.Store.LatestPositions(r.Context(), []int64{id})
	tracks, _ := a.Store.RecentTracks(r.Context(), []int64{id}, 200)
	dto := api.ToFlightDTO(f, passengers[id], shares[id], latest[id], tracks[id])
	a.publishFlightDTO(r.Context(), dto)
	writeJSON(w, http.StatusOK, dto)
}

func (a *API) deleteFlight(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireEdit(r.Context(), id, me, w); err != nil {
		return
	}
	// Capture the visibility set BEFORE deleting the row so the delete
	// SSE event reaches exactly the subscribers who had the flight in
	// their state — once the row is gone we can no longer derive it.
	viewers := a.flightViewers(r.Context(), id)
	if err := a.Store.DeleteFlight(r.Context(), id); err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishFlightDelete(id, viewers)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) addPassenger(w http.ResponseWriter, r *http.Request) {
	fid, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireEdit(r.Context(), fid, me, w); err != nil {
		return
	}
	var in userIDReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.UserID == 0 {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	if err := a.requireFriendOfCreator(r.Context(), fid, in.UserID, w); err != nil {
		return
	}
	if err := a.Store.AddPassenger(r.Context(), fid, in.UserID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.publishFlightByID(r.Context(), fid)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) removePassenger(w http.ResponseWriter, r *http.Request) {
	fid, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireEdit(r.Context(), fid, me, w); err != nil {
		return
	}
	uid, err := pathID(r, "userId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad userId")
		return
	}
	if err := a.Store.RemovePassenger(r.Context(), fid, uid); err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishFlightByID(r.Context(), fid)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) addShare(w http.ResponseWriter, r *http.Request) {
	fid, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireEdit(r.Context(), fid, me, w); err != nil {
		return
	}
	var in userIDReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.UserID == 0 {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	if err := a.requireFriendOfCreator(r.Context(), fid, in.UserID, w); err != nil {
		return
	}
	if err := a.Store.AddShare(r.Context(), fid, in.UserID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.publishFlightByID(r.Context(), fid)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) removeShare(w http.ResponseWriter, r *http.Request) {
	fid, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireEdit(r.Context(), fid, me, w); err != nil {
		return
	}
	uid, err := pathID(r, "userId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad userId")
		return
	}
	if err := a.Store.RemoveShare(r.Context(), fid, uid); err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishFlightByID(r.Context(), fid)
	w.WriteHeader(http.StatusNoContent)
}

// events streams SSE to the caller. Builds a Subscription from the auth
// context + ?show_all=1 query param (only honored for superusers).
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	a.Hub.Stream(w, r, sse.Subscription{
		ViewerID:    me.ID,
		IsSuperuser: me.IsSuperuser,
		ShowAll:     wantsShowAll(r, me),
	})
}

// wantsShowAll returns true when the caller asked for ?show_all=1 AND is
// a superuser. Non-superusers cannot opt into the all-flights view.
func wantsShowAll(r *http.Request, u *store.User) bool {
	if u == nil || !u.IsSuperuser {
		return false
	}
	v := r.URL.Query().Get("show_all")
	return v == "1" || v == "true"
}

// wantsShowOld returns true when the caller asked to include flights whose
// effective arrival is more than 24 hours ago. Available to every
// authenticated user — there is no superuser gate, unlike wantsShowAll.
func wantsShowOld(r *http.Request) bool {
	v := r.URL.Query().Get("show_old")
	return v == "1" || v == "true"
}

// canView returns true if u can see the flight: the visibility predicate,
// OR the caller is a superuser (the API treats superusers as universally
// allowed for individual-resource lookups even without show_all, so
// admin-style probing still works).
func (a *API) canView(ctx context.Context, id int64, u *store.User) (bool, error) {
	if u == nil {
		return false, nil
	}
	return a.Store.CanView(ctx, id, u.ID, u.IsSuperuser)
}

// requireEdit writes the appropriate error response and returns a non-nil
// error if the caller may not edit the flight. Returns nil if edits are
// allowed; in that case the caller continues normally.
func (a *API) requireEdit(ctx context.Context, id int64, u *store.User, w http.ResponseWriter) error {
	if u == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return errors.New("unauthorized")
	}
	if u.IsSuperuser {
		return nil
	}
	ok, err := a.Store.CanEdit(ctx, id, u.ID)
	if err != nil {
		handleStoreErr(w, err)
		return err
	}
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden")
		return errors.New("forbidden")
	}
	return nil
}

// requireFriendOfCreator writes a 400 response and returns a non-nil error
// when target is neither the flight's creator nor an accepted friend of the
// creator. Used to gate addPassenger and addShare on an existing flight so
// we never link a flight to a non-friend.
//
// The check is against the creator's friend graph, not the actor's — a
// superuser editing on someone else's behalf still has to respect the
// creator's friendships. createFlight runs an analogous check inline
// against me.ID since the flight doesn't exist yet when its
// passenger_ids / shared_user_ids are validated.
func (a *API) requireFriendOfCreator(ctx context.Context, flightID, target int64, w http.ResponseWriter) error {
	f, err := a.Store.FlightByID(ctx, flightID)
	if err != nil {
		handleStoreErr(w, err)
		return err
	}
	if f.CreatedBy == nil {
		// requireEdit precondition guarantees a non-nil creator on any flight
		// reachable from this code path; a nil here means the flight was
		// orphaned by some other path and there's no creator graph to consult.
		writeError(w, http.StatusBadRequest, "flight has no creator")
		return errors.New("flight has no creator")
	}
	if target == *f.CreatedBy {
		return nil
	}
	ok, err := a.Store.AreAcceptedFriends(ctx, *f.CreatedBy, target)
	if err != nil {
		handleStoreErr(w, err)
		return err
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "target is not a friend of the flight creator")
		return errNotFriend
	}
	return nil
}

var errNotFriend = errors.New("not a friend of creator")

// flightViewers returns the visibility set for an existing flight, used by
// publishers to scope SSE events. On any error (including the flight no
// longer existing) we return nil — the publisher will treat that as
// "deliver to nobody" rather than "deliver publicly", which is the safer
// default.
func (a *API) flightViewers(ctx context.Context, id int64) []int64 {
	viewers, err := a.Store.VisibleUserIDs(ctx, id)
	if err != nil {
		slog.Warn("flightViewers: lookup failed", "err", err, "id", id)
		return nil
	}
	return viewers
}

// publishFlightDTO fans a flight.updated SSE event, scoped to the flight's
// current visibility set. The viewer set already accounts for is_public
// flights (creator's accepted friends are unioned in by VisibleUserIDs only
// when the flight is public), so we never publish with an empty VisibleTo
// for an actual flight — the hub's broadcast-to-all path is reserved for
// system events with no per-flight scope.
func (a *API) publishFlightDTO(ctx context.Context, dto api.FlightDTO) {
	if a.Hub == nil {
		return
	}
	payload, err := json.Marshal(dto)
	if err != nil {
		slog.Error("publishFlightDTO: marshal", "err", err, "id", dto.ID)
		return
	}
	visible := a.flightViewers(ctx, dto.ID)
	a.Hub.Publish(sse.Event{Type: "flight.updated", Data: payload, VisibleTo: visible})
}

// publishFlightByID refetches the flight + associated data and broadcasts
// the DTO. Used by endpoints that mutate a flight indirectly (passenger /
// share add/remove) and so don't already have a complete DTO in hand.
func (a *API) publishFlightByID(ctx context.Context, id int64) {
	if a.Hub == nil {
		return
	}
	f, err := a.Store.FlightByID(ctx, id)
	if err != nil {
		slog.Warn("publishFlightByID: refetch", "err", err, "id", id)
		return
	}
	passengers, _ := a.Store.PassengersByFlight(ctx, []int64{id})
	shares, _ := a.Store.SharedUserIDsByFlight(ctx, []int64{id})
	latest, _ := a.Store.LatestPositions(ctx, []int64{id})
	tracks, _ := a.Store.RecentTracks(ctx, []int64{id}, 200)
	a.publishFlightDTO(ctx, api.ToFlightDTO(f, passengers[id], shares[id], latest[id], tracks[id]))
}

// needsCoordBackfill is true when any of the four coord columns is NULL
// on the flight row — typically because the user supplied an IATA that
// isn't in the embedded airports table.
func needsCoordBackfill(f *store.Flight) bool {
	return f.OriginLat == nil || f.OriginLon == nil ||
		f.DestLat == nil || f.DestLon == nil
}

// backfillCoordsIfNeeded is a no-op when no Resolver is configured or
// when the flight already has every coord column populated (the table
// fast path). Otherwise it synchronously resolves the flight via
// a.Resolver and writes any coords / airframe / notes the resolver
// returned through Store.BackfillFlight, which only fills empty columns
// so user-typed values survive. On any error the row stays as-is and we
// return f unchanged — the create or update request still succeeds, just with a
// "no map" pill until the poller catches up later. Mirrors the path the
// poller uses at poller.resolveAndUpdate.
func (a *API) backfillCoordsIfNeeded(ctx context.Context, f *store.Flight) *store.Flight {
	if a.Resolver == nil || !needsCoordBackfill(f) {
		return f
	}
	rf, err := a.Resolver.Resolve(ctx, f.Ident, f.ScheduledOut)
	if err != nil {
		slog.Warn("handlers: resolve for coord backfill failed",
			"ident", f.Ident, "id", f.ID, "err", err)
		return f
	}
	if err := a.Store.BackfillFlight(ctx, f.ID, store.BackfillPayload{
		OriginIATA: rf.OriginIATA, OriginLat: rf.OriginLat, OriginLon: rf.OriginLon,
		DestIATA:   rf.DestIATA, DestLat: rf.DestLat, DestLon: rf.DestLon,
		ICAO24:     rf.ICAO24, Callsign: rf.Callsign,
		Notes: rf.Notes,
	}); err != nil {
		slog.Error("handlers: coord backfill write failed", "id", f.ID, "err", err)
		return f
	}
	fresh, err := a.Store.FlightByID(ctx, f.ID)
	if err != nil {
		slog.Error("handlers: refetch after coord backfill", "id", f.ID, "err", err)
		return f
	}
	return fresh
}

// publishFlightDelete fans a flight.deleted SSE event so connected clients
// can drop the flight from their local state. The visibility set must be
// captured BEFORE the row is deleted — passed in here. Payload is a
// minimal {"id":N} envelope since the row is gone.
func (a *API) publishFlightDelete(id int64, viewers []int64) {
	if a.Hub == nil {
		return
	}
	payload, err := json.Marshal(struct {
		ID int64 `json:"id"`
	}{ID: id})
	if err != nil {
		slog.Error("publishFlightDelete: marshal", "err", err, "id", id)
		return
	}
	a.Hub.Publish(sse.Event{Type: "flight.deleted", Data: payload, VisibleTo: viewers})
}
