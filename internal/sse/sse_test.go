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

// public is a convenience for tests: events with no VisibleTo go to all
// subscribers regardless of identity.
var anySub = Subscription{ViewerID: 1}

func TestSubscribePublishReceive(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe(anySub)
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
	ch, unsub := h.Subscribe(anySub)
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
	ch, unsub := h.Subscribe(anySub)
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

func TestPublishFiltersByVisibleTo(t *testing.T) {
	h := NewHub()
	chA, unsubA := h.Subscribe(Subscription{ViewerID: 1})
	defer unsubA()
	chB, unsubB := h.Subscribe(Subscription{ViewerID: 2})
	defer unsubB()
	chSup, unsubSup := h.Subscribe(Subscription{ViewerID: 99, IsSuperuser: true, ShowAll: true})
	defer unsubSup()

	// Private event: only viewer 1 + show-all superuser see it.
	h.Publish(Event{Type: "flight.updated", Data: []byte("p"), VisibleTo: []int64{1}})
	select {
	case <-chA:
	case <-time.After(time.Second):
		t.Fatal("viewer 1 missed private event addressed to them")
	}
	select {
	case <-chB:
		t.Fatal("viewer 2 received private event not addressed to them")
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case <-chSup:
	case <-time.After(time.Second):
		t.Fatal("show-all superuser missed private event")
	}

	// Public event (empty VisibleTo): everyone gets it.
	h.Publish(Event{Type: "flight.updated", Data: []byte("g")})
	for _, ch := range []<-chan Event{chA, chB, chSup} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("public event not delivered to a subscriber")
		}
	}
}

func TestUserPrivateBypassesShowAll(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe(Subscription{ViewerID: 1, IsSuperuser: true, ShowAll: true})
	defer unsub()

	// Without UserPrivate, show_all delivers regardless of VisibleTo.
	h.Publish(Event{Type: "flight.updated", Data: []byte("{}"), VisibleTo: []int64{99}})
	h.Publish(Event{Type: "notifications.updated", Data: []byte("{}"), VisibleTo: []int64{99}, UserPrivate: true})

	var got []string
	deadline := time.After(50 * time.Millisecond)
loop:
	for {
		select {
		case ev := <-ch:
			got = append(got, ev.Type)
		case <-deadline:
			break loop
		}
	}
	if len(got) != 1 || got[0] != "flight.updated" {
		t.Errorf("got events = %v, want exactly [flight.updated]", got)
	}
}

func TestSuperuserWithoutShowAllRespectsVisibility(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe(Subscription{ViewerID: 99, IsSuperuser: true, ShowAll: false})
	defer unsub()
	h.Publish(Event{Type: "x", VisibleTo: []int64{1}})
	select {
	case <-ch:
		t.Fatal("superuser without show_all received private event for another user")
	case <-time.After(50 * time.Millisecond):
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
	h.Stream(w, httptest.NewRequest("GET", "/api/events", nil), anySub)
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
	go func() { h.Stream(w, r, anySub); close(done) }()

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
	go func() { h.Stream(w, r, anySub); close(done) }()
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
	go func() { h.Stream(w, r, anySub); close(done) }()
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
