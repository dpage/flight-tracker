package poller

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

// captureMailer records the messages a poller's email channel would have sent.
type captureMailer struct {
	mu   sync.Mutex
	sent []string
}

func (c *captureMailer) send(_ context.Context, _, _, msg string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, msg)
	return nil
}

func (c *captureMailer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

// setEstimatedOut writes an estimated departure on the part's flight_details,
// simulating what a resolver/tracker would persist when a delay is published.
func setEstimatedOut(t *testing.T, s *store.Store, partID int64, est time.Time) {
	t.Helper()
	if _, err := s.Pool().Exec(context.Background(),
		`UPDATE flight_details SET estimated_out = $2 WHERE plan_part_id = $1`,
		partID, est); err != nil {
		t.Fatalf("set estimated_out: %v", err)
	}
}

func setStatus(t *testing.T, s *store.Store, partID int64, status string) {
	t.Helper()
	if _, err := s.Pool().Exec(context.Background(),
		`UPDATE flight_details SET flight_status = $2 WHERE plan_part_id = $1`,
		partID, status); err != nil {
		t.Fatalf("set status: %v", err)
	}
}

// alertPoller builds a poller wired with a capture mailer + email enabled.
func alertPoller(t *testing.T) (*Poller, *store.Store, *sse.Hub, *captureMailer) {
	t.Helper()
	p, s, hub := newPoller(t, &mockTracker{}, time.Minute)
	cap := &captureMailer{}
	p.MailFromAddress = "alerts@aerly.test"
	p.SendmailPath = "/bin/true"
	p.PublicURL = "http://localhost:8080"
	p.SendAlertEmail = cap.send
	return p, s, hub, cap
}

// drainAlerts collects alert.created payloads delivered to a subscription until
// a short quiet period. Returns the decoded alerts.
func drainAlerts(t *testing.T, ch <-chan sse.Event) []api.FlightAlertDTO {
	t.Helper()
	var out []api.FlightAlertDTO
	for {
		select {
		case ev := <-ch:
			if ev.Type != "alert.created" {
				continue
			}
			var dto api.NotificationsDTO
			if err := json.Unmarshal(ev.Data, &dto); err != nil {
				t.Fatalf("unmarshal alert: %v", err)
			}
			if dto.Alert != nil {
				out = append(out, *dto.Alert)
			}
		case <-time.After(150 * time.Millisecond):
			return out
		}
	}
}

func TestAlert_DelayBelowThresholdDoesNotAlert(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "BA100", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}

	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	prev := f // no delay yet
	// 10-minute delay: below the 15-minute default threshold.
	setEstimatedOut(t, s, f.ID, f.ScheduledOut.Add(10*time.Minute))
	p.maybeAlert(ctx, prev, f.ID)

	if got := drainAlerts(t, ch); len(got) != 0 {
		t.Fatalf("expected no in-app alert below threshold, got %d", len(got))
	}
	if cap.count() != 0 {
		t.Fatalf("expected no email below threshold, got %d", cap.count())
	}
}

func TestAlert_DelayAboveThresholdAlerts(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "BA200", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	prev := f
	setEstimatedOut(t, s, f.ID, f.ScheduledOut.Add(45*time.Minute))
	p.maybeAlert(ctx, prev, f.ID)

	got := drainAlerts(t, ch)
	if len(got) != 1 {
		t.Fatalf("expected 1 in-app alert, got %d", len(got))
	}
	if got[0].Kind != "delayed" || got[0].Ident != "BA200" {
		t.Fatalf("unexpected alert: %+v", got[0])
	}
	if cap.count() != 1 {
		t.Fatalf("expected 1 email, got %d", cap.count())
	}
}

func TestAlert_CancellationAlwaysAlertsRegardlessOfThreshold(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	// Crank the owner's threshold way up: cancellation must still alert.
	if err := s.SetAlertPrefs(ctx, store.AlertPrefs{UserID: owner, InApp: true, Email: true, MinDelayMin: 600}); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "BA300", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	prev := f
	setStatus(t, s, f.ID, "Cancelled")
	p.maybeAlert(ctx, prev, f.ID)

	got := drainAlerts(t, ch)
	if len(got) != 1 || got[0].Kind != "cancelled" {
		t.Fatalf("expected 1 cancelled alert, got %+v", got)
	}
	if cap.count() != 1 {
		t.Fatalf("expected 1 cancellation email, got %d", cap.count())
	}
}

func TestAlert_ViewerWithoutOptInGetsNothing(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	viewer := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, viewer, "viewer@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "BA400", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	// viewer is a trip member but NOT a passenger and did NOT opt in.
	tp, err := s.TrackerPartRow(ctx, f.ID)
	if err != nil {
		t.Fatalf("tracker row: %v", err)
	}
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'viewer')`,
		tp.TripID, viewer); err != nil {
		t.Fatalf("add viewer: %v", err)
	}

	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: viewer})
	defer unsub()

	prev := f
	setStatus(t, s, f.ID, "Cancelled")
	p.maybeAlert(ctx, prev, f.ID)

	if got := drainAlerts(t, ch); len(got) != 0 {
		t.Fatalf("non-opted-in viewer got %d in-app alerts", len(got))
	}
	// The only verified email is the viewer's; owner has none → no email at all.
	if cap.count() != 0 {
		t.Fatalf("non-opted-in viewer got email, count=%d", cap.count())
	}

	// After opt-in, the viewer DOES get alerted on the next (distinct) change.
	if err := s.AddPlanAlertOptin(ctx, tp.PlanID, viewer); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	prev2, err := s.FlightPartByID(ctx, f.ID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	setStatus(t, s, f.ID, "Diverted")
	p.maybeAlert(ctx, prev2, f.ID)

	got := drainAlerts(t, ch)
	if len(got) != 1 || got[0].Kind != "diverted" {
		t.Fatalf("opted-in viewer expected 1 diverted alert, got %+v", got)
	}
	if cap.count() != 1 {
		t.Fatalf("opted-in viewer expected 1 email, got %d", cap.count())
	}
}

func TestAlert_DedupeSuppressesRepeatOfSameChange(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, store.CreateFlightPayload{
		Ident: "BA500", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	// First tick: a 45-minute delay alerts.
	prev := f
	setEstimatedOut(t, s, f.ID, f.ScheduledOut.Add(45*time.Minute))
	p.maybeAlert(ctx, prev, f.ID)
	if got := drainAlerts(t, ch); len(got) != 1 {
		t.Fatalf("first delay: expected 1 alert, got %d", len(got))
	}
	if cap.count() != 1 {
		t.Fatalf("first delay: expected 1 email, got %d", cap.count())
	}

	// Second tick: same 45-minute delay still present. The pre-state now also
	// carries the delay, and the dedupe signature is unchanged → no re-alert.
	prev2, err := s.FlightPartByID(ctx, f.ID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	p.maybeAlert(ctx, prev2, f.ID)
	if got := drainAlerts(t, ch); len(got) != 0 {
		t.Fatalf("repeat delay: expected no alert, got %d", len(got))
	}
	if cap.count() != 1 {
		t.Fatalf("repeat delay: expected still 1 email, got %d", cap.count())
	}
}
