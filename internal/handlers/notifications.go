package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/sse"
)

func (a *API) getNotifications(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	dto, err := a.buildNotificationsDTO(r.Context(), me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// buildNotificationsDTO fans out to every count query the dashboard
// surfaces. Today there is one source (incoming friend requests); new
// kinds add a query + DTO field here.
func (a *API) buildNotificationsDTO(ctx context.Context, userID int64) (api.NotificationsDTO, error) {
	n, err := a.Store.CountIncomingFriendRequests(ctx, userID)
	if err != nil {
		return api.NotificationsDTO{}, err
	}
	return api.NotificationsDTO{FriendRequestsPending: n}, nil
}

// publishNotifications recomputes userID's notification counts and
// pushes them on the SSE hub, restricted to userID. Errors are logged
// but never surface to the HTTP caller — the SPA's bootstrap fetch is
// the safety net for any dropped publish.
func (a *API) publishNotifications(ctx context.Context, userID int64) {
	dto, err := a.buildNotificationsDTO(ctx, userID)
	if err != nil {
		slog.Error("publishNotifications: build dto", "err", err, "user", userID)
		return
	}
	payload, err := json.Marshal(dto)
	if err != nil {
		slog.Error("publishNotifications: marshal", "err", err, "user", userID)
		return
	}
	a.Hub.Publish(sse.Event{
		Type:        "notifications.updated",
		Data:        payload,
		VisibleTo:   []int64{userID},
		UserPrivate: true,
	})
}
