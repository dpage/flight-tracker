package emailingest_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/emailingest"
	"github.com/dpage/aerly/internal/flightops"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
)

// fakeLLM returns a fixed JSON response and records the docs it received.
type fakeLLM struct {
	resp    string
	err     error
	gotDocs int
}

func (f *fakeLLM) Complete(ctx context.Context, prompt string, docs []emailingest.Document) (string, error) {
	f.gotDocs = len(docs)
	return f.resp, f.err
}

type fakeResolver struct {
	err error
}

func (f fakeResolver) Resolve(ctx context.Context, ident string, date time.Time) (*providers.ResolvedFlight, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &providers.ResolvedFlight{
		Ident:        ident,
		ScheduledOut: date.Add(9 * time.Hour),
		ScheduledIn:  date.Add(13 * time.Hour),
		OriginIATA:   "IST",
		DestIATA:     "LHR",
	}, nil
}

// buildTestSendmail compiles a stub binary that writes stdin to $SENDMAIL_OUT.
// Cached in a sub-test temp dir, returned as the absolute path.
func buildTestSendmail(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "sendmail.go")
	out := filepath.Join(dir, "sendmail")
	code := `package main
import ("io"; "os")
func main() {
	out := os.Getenv("SENDMAIL_OUT")
	if out == "" { io.Copy(io.Discard, os.Stdin); return }
	f, err := os.OpenFile(out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil { os.Exit(2) }
	defer f.Close()
	io.Copy(f, os.Stdin)
}
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", out, src)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build stub sendmail: %v %s", err, b)
	}
	return out
}

type harness struct {
	svc         *emailingest.Service
	sendmailOut string
	maildir     string
	store       *store.Store
	hub         *sse.Hub
}

// newHarness builds a Service wired to a real DB, a fake LLM, a fake resolver,
// and a stub sendmail. Caller drops messages into <maildir>/new/.
func newHarness(t *testing.T, llmResp string, resolverErr error, requireDKIM bool) *harness {
	t.Helper()
	pool := testsupport.NewPool(t)
	s := store.New(pool)

	maildir := t.TempDir()
	sendmailOut := filepath.Join(t.TempDir(), "sent.txt")
	t.Setenv("SENDMAIL_OUT", sendmailOut)

	hub := sse.NewHub()
	svc := &emailingest.Service{
		Cfg: emailingest.Config{
			MaildirPath:   maildir,
			PollInterval:  50 * time.Millisecond,
			RequireDKIM:   requireDKIM,
			MaxBodyBytes:  1 << 20,
			IngestAddress: "flights@flights.example",
			SendmailPath:  buildTestSendmail(t),
			PublicURL:     "https://flights.example",
		},
		Store:      s,
		Extractor:  emailingest.NewExtractor(&fakeLLM{resp: llmResp}, "test"),
		FlightDeps: flightops.Deps{Store: s, Resolver: fakeResolver{err: resolverErr}},
		Hub:        hub,
	}
	if err := svc.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	return &harness{svc: svc, sendmailOut: sendmailOut, maildir: maildir, store: s, hub: hub}
}

// runUntilProcessed runs svc.Run in a goroutine and waits up to timeout for
// the file at maildir/new/name to disappear (success) or land in .failed/.
func (h *harness) runUntilProcessed(t *testing.T, name string, timeout time.Duration) (processedAs string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = h.svc.Run(ctx) }()
	newPath := filepath.Join(h.maildir, "new", name)
	failedPath := filepath.Join(h.maildir, ".failed", name)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			if _, err := os.Stat(failedPath); err == nil {
				return "failed"
			}
			return "removed"
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file %s never processed within %s", name, timeout)
	return ""
}

const goodMessage = "From: alice@example.com\r\n" +
	"To: flights@flights.example\r\n" +
	"Subject: x\r\n" +
	"Message-ID: <1@x>\r\n" +
	"Authentication-Results: ml; dkim=pass header.d=example.com\r\n" +
	"Content-Type: text/plain\r\n\r\n" +
	"body text"

func writeMessage(t *testing.T, maildir, name, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(maildir, "new", name), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIngest_EndToEnd_Success(t *testing.T) {
	h := newHarness(t,
		`{"flights":[{"ident":"TK1980","date":"`+time.Now().AddDate(0, 1, 0).Format("2006-01-02")+`","confidence":"high"}]}`,
		nil, true)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}

	writeMessage(t, h.maildir, "1", goodMessage)
	state := h.runUntilProcessed(t, "1", 5*time.Second)
	if state != "removed" {
		t.Fatalf("expected file removed, got %s", state)
	}
	// The reply sendmail stub should have received an RFC822 message.
	body, _ := os.ReadFile(h.sendmailOut)
	if !strings.Contains(string(body), "TK1980") {
		t.Errorf("sendmail output missing flight: %s", body)
	}
}

func TestIngest_DKIMFailed_Poison(t *testing.T) {
	h := newHarness(t, `{"flights":[]}`, nil, true)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	// Message has no Authentication-Results → DKIM fail.
	msg := "From: alice@example.com\r\nMessage-ID: <2@x>\r\nContent-Type: text/plain\r\n\r\nbody"
	writeMessage(t, h.maildir, "2", msg)
	state := h.runUntilProcessed(t, "2", 5*time.Second)
	if state != "failed" {
		t.Errorf("expected .failed/, got %s", state)
	}
}

func TestIngest_DKIMOff_AcceptsAnyway(t *testing.T) {
	h := newHarness(t,
		`{"flights":[{"ident":"TK1980","date":"`+time.Now().AddDate(0, 1, 0).Format("2006-01-02")+`","confidence":"high"}]}`,
		nil, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	msg := "From: alice@example.com\r\nMessage-ID: <3@x>\r\nContent-Type: text/plain\r\n\r\nbody"
	writeMessage(t, h.maildir, "3", msg)
	state := h.runUntilProcessed(t, "3", 5*time.Second)
	if state != "removed" {
		t.Errorf("expected removed (DKIM not required), got %s", state)
	}
}

func TestIngest_UnknownSender_Poison(t *testing.T) {
	h := newHarness(t, `{"flights":[]}`, nil, true)
	writeMessage(t, h.maildir, "4", goodMessage) // From: alice@example.com but no user registered
	state := h.runUntilProcessed(t, "4", 5*time.Second)
	if state != "failed" {
		t.Errorf("expected .failed/, got %s", state)
	}
}

func TestIngest_SelfAddressed_Poison(t *testing.T) {
	h := newHarness(t, `{"flights":[]}`, nil, false)
	msg := "From: flights@flights.example\r\nMessage-ID: <5@x>\r\nContent-Type: text/plain\r\n\r\nbody"
	writeMessage(t, h.maildir, "5", msg)
	state := h.runUntilProcessed(t, "5", 5*time.Second)
	if state != "failed" {
		t.Errorf("expected .failed/, got %s", state)
	}
}

func TestIngest_ResolverError_PartialAllFailed(t *testing.T) {
	h := newHarness(t,
		`{"flights":[{"ident":"TK1980","date":"`+time.Now().AddDate(0, 1, 0).Format("2006-01-02")+`","confidence":"high"}]}`,
		errors.New("upstream down"), false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	writeMessage(t, h.maildir, "6", goodMessage)
	state := h.runUntilProcessed(t, "6", 5*time.Second)
	if state != "removed" {
		t.Errorf("expected file removed (we sent the failure reply), got %s", state)
	}
	body, _ := os.ReadFile(h.sendmailOut)
	if !strings.Contains(string(body), "couldn't add any") {
		t.Errorf("expected all-failed reply, got: %s", body)
	}
}

func TestIngest_ResolverUnscheduled_ManualFallback(t *testing.T) {
	// LLM extracts a leg with full manual details. Resolver reports the
	// flight is unscheduled. We should insert it manually and tell the
	// user to verify times.
	depDate := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	arrDate := time.Now().AddDate(0, 1, 0).AddDate(0, 0, 1).Format("2006-01-02")
	llmResp := `{"flights":[{
		"ident":"TK1980","date":"` + depDate + `","confidence":"high",
		"origin_iata":"IST","dest_iata":"LHR",
		"depart_time":"22:30","arrive_date":"` + arrDate + `","arrive_time":"01:15"
	}]}`
	h := newHarness(t, llmResp, providers.ErrFlightUnscheduled, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	writeMessage(t, h.maildir, "10", goodMessage)
	state := h.runUntilProcessed(t, "10", 5*time.Second)
	if state != "removed" {
		t.Fatalf("expected removed, got %s", state)
	}
	body, _ := os.ReadFile(h.sendmailOut)
	bs := string(body)
	if !strings.Contains(bs, "TK1980 on "+depDate+" (from the email") {
		t.Errorf("expected manual-fallback note in reply, got:\n%s", bs)
	}
	if !strings.Contains(bs, "please check the departure and arrival times") {
		t.Errorf("expected manual trailer in reply, got:\n%s", bs)
	}
	// The flight should be in the DB attached to alice.
	flights, err := h.store.ListVisibleFlights(ctx, u.ID, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(flights) != 1 {
		t.Fatalf("expected 1 flight in DB, got %d", len(flights))
	}
	if flights[0].Ident != "TK1980" || flights[0].OriginIATA != "IST" || flights[0].DestIATA != "LHR" {
		t.Errorf("flight wrong: %+v", flights[0])
	}
}

func TestIngest_ResolverUnscheduled_NoManualDetails_Failure(t *testing.T) {
	// Resolver fails AND the LLM didn't extract manual details — we
	// fall through to the original failure path rather than guessing.
	depDate := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	llmResp := `{"flights":[{"ident":"TK1980","date":"` + depDate + `","confidence":"high"}]}`
	h := newHarness(t, llmResp, providers.ErrFlightUnscheduled, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	writeMessage(t, h.maildir, "11", goodMessage)
	state := h.runUntilProcessed(t, "11", 5*time.Second)
	if state != "removed" {
		t.Fatalf("expected removed, got %s", state)
	}
	body, _ := os.ReadFile(h.sendmailOut)
	if !strings.Contains(string(body), "couldn't add any") {
		t.Errorf("expected all-failed reply when no manual details, got:\n%s", body)
	}
	flights, _ := h.store.ListVisibleFlights(ctx, u.ID, false, false)
	if len(flights) != 0 {
		t.Errorf("expected 0 flights when no manual fallback possible, got %d", len(flights))
	}
}

func TestIngest_MalformedMessage_Poison(t *testing.T) {
	h := newHarness(t, `{"flights":[]}`, nil, false)
	writeMessage(t, h.maildir, "7", "not an email at all")
	state := h.runUntilProcessed(t, "7", 5*time.Second)
	if state != "failed" {
		t.Errorf("expected .failed/, got %s", state)
	}
}

// TestIngest_PublishesSSEOnInsert exercises the resolver-backed create path
// (flightops.Create) and asserts a flight.updated SSE event is broadcast to
// the user who owns the newly-inserted flight. Without this, connected SPA
// clients wouldn't learn about the new flight until they manually refresh.
func TestIngest_PublishesSSEOnInsert(t *testing.T) {
	depDate := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	h := newHarness(t,
		`{"flights":[{"ident":"TK1980","date":"`+depDate+`","confidence":"high"}]}`,
		nil, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}

	events, unsub := h.hub.Subscribe(sse.Subscription{ViewerID: u.ID})
	defer unsub()

	writeMessage(t, h.maildir, "20", goodMessage)
	if state := h.runUntilProcessed(t, "20", 5*time.Second); state != "removed" {
		t.Fatalf("expected removed, got %s", state)
	}

	select {
	case ev := <-events:
		if ev.Type != "flight.updated" {
			t.Errorf("event type = %q, want flight.updated", ev.Type)
		}
		var got struct {
			Ident        string  `json:"ident"`
			PassengerIDs []int64 `json:"passenger_ids"`
		}
		if err := json.Unmarshal(ev.Data, &got); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		if got.Ident != "TK1980" {
			t.Errorf("event ident = %q, want TK1980", got.Ident)
		}
		if len(got.PassengerIDs) != 1 || got.PassengerIDs[0] != u.ID {
			t.Errorf("event passenger_ids = %v, want [%d]", got.PassengerIDs, u.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("expected flight.updated SSE event after email-ingest insert")
	}
}

// TestIngest_ManualFallback_PublishesSSE covers the same publish behavior on
// the manual-fallback path (flightops.CreateManual) used when the resolver
// has no record but the email itself spells out the schedule.
func TestIngest_ManualFallback_PublishesSSE(t *testing.T) {
	depDate := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	arrDate := time.Now().AddDate(0, 1, 0).AddDate(0, 0, 1).Format("2006-01-02")
	llmResp := `{"flights":[{
		"ident":"TK1980","date":"` + depDate + `","confidence":"high",
		"origin_iata":"IST","dest_iata":"LHR",
		"depart_time":"22:30","arrive_date":"` + arrDate + `","arrive_time":"01:15"
	}]}`
	h := newHarness(t, llmResp, providers.ErrFlightUnscheduled, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}

	events, unsub := h.hub.Subscribe(sse.Subscription{ViewerID: u.ID})
	defer unsub()

	writeMessage(t, h.maildir, "21", goodMessage)
	if state := h.runUntilProcessed(t, "21", 5*time.Second); state != "removed" {
		t.Fatalf("expected removed, got %s", state)
	}

	select {
	case ev := <-events:
		if ev.Type != "flight.updated" {
			t.Errorf("event type = %q, want flight.updated", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("expected flight.updated SSE event after manual-fallback insert")
	}
}
