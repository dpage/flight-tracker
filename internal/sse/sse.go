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
}

// Hub fans events out to all currently-connected SSE clients.
// Publish never blocks; slow subscribers drop events silently.
type Hub struct {
	mu    sync.RWMutex
	subs  map[chan Event]struct{}
	bufSz int
}

func NewHub() *Hub {
	return &Hub{
		subs:  make(map[chan Event]struct{}),
		bufSz: 16,
	}
}

// Subscribe returns a channel of events and an unsubscribe func. Callers must
// invoke the unsubscribe func when done.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, h.bufSz)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
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

// Publish fans an event to every subscriber. Non-blocking: if a subscriber's
// buffer is full the event is dropped for that subscriber only.
func (h *Hub) Publish(e Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
			slog.Warn("sse subscriber slow — dropping event", "type", e.Type)
		}
	}
}

// Handle serves /api/events to a single client. It assumes auth was already
// enforced by middleware.
func (h *Hub) Handle(w http.ResponseWriter, r *http.Request) {
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

	ch, unsub := h.Subscribe()
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
