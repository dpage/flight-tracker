// Package sse provides a tiny Server-Sent Events broadcast hub plus an
// http.Handler that streams events to a single subscriber.
package sse

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type Event struct {
	Type string
	Data []byte // pre-serialized JSON
	// VisibleTo restricts delivery to a specific set of viewer user IDs.
	// nil / empty = public (delivered to every subscriber). Non-empty =
	// delivered only to subscribers whose ViewerID is in the set, OR to
	// superusers who opted into the show-all view, UNLESS UserPrivate
	// is true.
	VisibleTo []int64
	// UserPrivate, when true, means VisibleTo is authoritative even for
	// superuser show-all subscriptions. Use for events whose payload is
	// intrinsically scoped to a single user (e.g. notification counts) —
	// a superuser dashboarding other users' flights should NOT receive
	// another user's notification counts.
	UserPrivate bool
}

// Subscription holds the per-connection state the hub needs to decide
// whether to forward each event.
type Subscription struct {
	ViewerID    int64
	IsSuperuser bool
	// ShowAll, when true on a superuser subscription, delivers every event
	// regardless of VisibleTo. Ignored on non-superuser subscriptions.
	ShowAll bool
}

// Hub fans events out to currently-connected SSE clients, filtering each
// event by the subscriber's identity and the event's VisibleTo list.
// Publish never blocks; slow subscribers drop events silently.
type Hub struct {
	mu    sync.RWMutex
	subs  map[chan Event]Subscription
	bufSz int
}

func NewHub() *Hub {
	return &Hub{
		subs:  make(map[chan Event]Subscription),
		bufSz: 16,
	}
}

// Subscribe returns a channel of events and an unsubscribe func. Callers
// must invoke the unsubscribe func when done. The Subscription describes
// which events the hub should deliver to this connection.
func (h *Hub) Subscribe(sub Subscription) (<-chan Event, func()) {
	ch := make(chan Event, h.bufSz)
	h.mu.Lock()
	h.subs[ch] = sub
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Publish fans an event to every subscriber whose Subscription is allowed
// to see it. Non-blocking: if a subscriber's buffer is full the event is
// dropped for that subscriber only.
func (h *Hub) Publish(e Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	visible := visibleSet(e.VisibleTo)
	for ch, sub := range h.subs {
		if !shouldDeliver(visible, sub, e.UserPrivate) {
			continue
		}
		select {
		case ch <- e:
		default:
			slog.Warn("sse subscriber slow — dropping event", "type", e.Type)
		}
	}
}

// SubscriberCount is exposed for tests and metrics. Cheap (O(1)) read of
// the map length under the hub lock.
func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

func visibleSet(ids []int64) map[int64]struct{} {
	if len(ids) == 0 {
		return nil
	}
	m := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

// shouldDeliver returns true if the event's visibility set (or its absence
// — a nil set meaning "public") matches the subscription.
func shouldDeliver(visible map[int64]struct{}, sub Subscription, userPrivate bool) bool {
	if visible == nil {
		return true
	}
	if sub.IsSuperuser && sub.ShowAll && !userPrivate {
		return true
	}
	_, ok := visible[sub.ViewerID]
	return ok
}

// Stream serves /api/events to a single client. It assumes auth was
// already enforced by middleware. The Subscription is built by the
// surrounding API layer (so the hub stays decoupled from auth).
func (h *Hub) Stream(w http.ResponseWriter, r *http.Request, sub Subscription) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Defeat nginx response buffering so events flush promptly through proxies.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, unsub := h.Subscribe(sub)
	defer unsub()

	if _, err := fmt.Fprint(w, ": ok\n\n"); err != nil {
		return
	}
	flusher.Flush()

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, ev.Data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
