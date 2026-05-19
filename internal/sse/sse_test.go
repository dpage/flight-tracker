package sse

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSubscribePublishReceive(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe()
	defer unsub()
	h.Publish(Event{Type: "x", Data: []byte("hi")})
	select {
	case ev := <-ch:
		if ev.Type != "x" || string(ev.Data) != "hi" {
			t.Errorf("unexpected event %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive published event")
	}
}

func TestUnsubscribeIdempotentAndStopsDelivery(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe()
	unsub()
	unsub() // second call must not panic / double-close
	// Channel is closed; publishing must not panic and must not deliver.
	h.Publish(Event{Type: "y"})
	if _, ok := <-ch; ok {
		t.Error("expected closed channel after unsubscribe")
	}
}

func TestPublishDropsWhenSubscriberSlow(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe()
	defer unsub()
	// Fill the buffer (bufSz=16) then publish more — extra events are
	// dropped for this subscriber without blocking Publish.
	for i := 0; i < h.bufSz+10; i++ {
		h.Publish(Event{Type: "flood"})
	}
	got := 0
	for {
		select {
		case <-ch:
			got++
			continue
		default:
		}
		break
	}
	if got != h.bufSz {
		t.Errorf("buffered %d events, want %d (rest dropped)", got, h.bufSz)
	}
}

// nonFlusher is a ResponseWriter that deliberately does NOT implement
// http.Flusher.
type nonFlusher struct{ h http.Header }

func (n *nonFlusher) Header() http.Header         { return n.h }
func (n *nonFlusher) Write(b []byte) (int, error) { return len(b), nil }
func (n *nonFlusher) WriteHeader(int)             {}

func TestHandleRejectsNonFlusher(t *testing.T) {
	h := NewHub()
	w := &nonFlusher{h: http.Header{}}
	h.Handle(w, httptest.NewRequest("GET", "/api/events", nil))
	// No panic, returns immediately; nothing to assert beyond not hanging.
}

// syncWriter is a thread-safe flushable ResponseWriter for streaming tests.
type syncWriter struct {
	mu       sync.Mutex
	buf      strings.Builder
	hdr      http.Header
	failAt   int // Write call index (1-based) at which to return an error; 0 = never
	writeNum int
}

func (s *syncWriter) Header() http.Header { return s.hdr }
func (s *syncWriter) WriteHeader(int)     {}
func (s *syncWriter) Flush()              {}
func (s *syncWriter) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeNum++
	if s.failAt != 0 && s.writeNum >= s.failAt {
		return 0, errors.New("write failed")
	}
	return s.buf.Write(b)
}
func (s *syncWriter) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestHandleStreamsEventThenContextDone(t *testing.T) {
	h := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", "/api/events", nil).WithContext(ctx)
	w := &syncWriter{hdr: http.Header{}}

	done := make(chan struct{})
	go func() { h.Handle(w, r); close(done) }()

	waitFor(t, func() bool { return strings.Contains(w.String(), ": ok") })
	if w.hdr.Get("Content-Type") != "text/event-stream" {
		t.Errorf("missing SSE content-type, got %q", w.hdr.Get("Content-Type"))
	}

	// Give the subscription a moment, then publish.
	waitFor(t, func() bool {
		h.Publish(Event{Type: "flight.updated", Data: []byte(`{"id":1}`)})
		return strings.Contains(w.String(), "event: flight.updated")
	})
	if !strings.Contains(w.String(), `data: {"id":1}`) {
		t.Errorf("event payload missing: %q", w.String())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handle did not return after context cancel")
	}
}

func TestHandleReturnsOnInitialWriteError(t *testing.T) {
	h := NewHub()
	r := httptest.NewRequest("GET", "/api/events", nil)
	w := &syncWriter{hdr: http.Header{}, failAt: 1} // fail the ": ok" write
	done := make(chan struct{})
	go func() { h.Handle(w, r); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handle should return when initial write fails")
	}
}

func TestHandleReturnsOnEventWriteError(t *testing.T) {
	h := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("GET", "/api/events", nil).WithContext(ctx)
	w := &syncWriter{hdr: http.Header{}, failAt: 2} // ": ok" ok, event write fails
	done := make(chan struct{})
	go func() { h.Handle(w, r); close(done) }()
	waitFor(t, func() bool { return strings.Contains(w.String(), ": ok") })
	waitFor(t, func() bool {
		h.Publish(Event{Type: "e", Data: []byte("d")})
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
}
