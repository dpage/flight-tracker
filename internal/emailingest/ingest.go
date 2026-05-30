package emailingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/flightops"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

// Config controls the ingest service. All fields are required.
type Config struct {
	MaildirPath   string
	PollInterval  time.Duration
	RequireDKIM   bool
	MaxBodyBytes  int
	IngestAddress string // e.g. "flights@flights.example" — also the reply From
	SendmailPath  string
	PublicURL     string
}

// Service is the long-running ingest goroutine.
type Service struct {
	Cfg        Config
	Store      *store.Store
	Extractor  *Extractor
	FlightDeps flightops.Deps
	// PlanDeps wires the generalized planops capture path (multi-type plans
	// + date-proximity trip selection). When its Store is set, processOne
	// runs the planops path for non-flight bookings alongside the legacy
	// flight handling. Optional — when zero, only flights are ingested
	// (the Wave-1 behaviour).
	PlanDeps planops.Deps
	// Hub is the SSE broadcast hub. Optional — when nil, ingested flights
	// are still inserted but connected clients won't learn of them until
	// they refresh. Wired in production; tests opt in via newHarness.
	Hub *sse.Hub
}

type outcomeKind int

const (
	outcomeOK outcomeKind = iota
	outcomeTransient
	outcomePoison
)

type outcome struct {
	kind outcomeKind
}

// Run loops until ctx is done, draining the Maildir on each tick.
func (s *Service) Run(ctx context.Context) error {
	if err := s.EnsureDirs(); err != nil {
		return err
	}
	interval := s.Cfg.PollInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	// Drain once on startup so we don't wait for the first tick.
	s.drainNew(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			s.drainNew(ctx)
		}
	}
}

// EnsureDirs creates the Maildir sub-directories if they don't already exist.
// Exposed so tests can prep a temp Maildir before dropping fixtures into new/.
func (s *Service) EnsureDirs() error {
	for _, sub := range []string{"new", "cur", "tmp", ".failed"} {
		if err := os.MkdirAll(filepath.Join(s.Cfg.MaildirPath, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) drainNew(ctx context.Context) {
	newDir := filepath.Join(s.Cfg.MaildirPath, "new")
	entries, err := os.ReadDir(newDir)
	if err != nil {
		slog.Warn("emailingest: read maildir", "err", err)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(newDir, e.Name())
		out := s.processOne(ctx, path)
		switch out.kind {
		case outcomeOK:
			if err := os.Remove(path); err != nil {
				slog.Warn("emailingest: remove processed file", "err", err, "path", path)
			}
		case outcomeTransient:
			// leave it; retry next tick
		case outcomePoison:
			dst := filepath.Join(s.Cfg.MaildirPath, ".failed", e.Name())
			if err := os.Rename(path, dst); err != nil {
				slog.Warn("emailingest: move poison", "err", err, "path", path)
			}
		}
	}
}

func (s *Service) processOne(ctx context.Context, path string) outcome {
	raw, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("emailingest: read", "err", err, "path", path)
		return outcome{kind: outcomeTransient}
	}
	parsed, err := Parse(raw)
	if err != nil {
		slog.Info("emailingest: unparseable, poison", "err", err)
		s.logIngest(ctx, "", "", "", false, nil, "parse_error", 0, 0, err.Error())
		return outcome{kind: outcomePoison}
	}

	// Refuse mail addressed from our own ingest address — prevents reply loops.
	if strings.EqualFold(parsed.From, s.Cfg.IngestAddress) {
		slog.Info("emailingest: refusing self-addressed mail", "from", parsed.From)
		return outcome{kind: outcomePoison}
	}

	dkimOK := DKIMPass(parsed.AuthenticationResults, FromDomain(parsed.From))
	if s.Cfg.RequireDKIM && !dkimOK {
		slog.Info("emailingest: DKIM not pass, poison", "from", parsed.From)
		s.logIngest(ctx, parsed.MessageID, parsed.From, parsed.Subject, dkimOK, nil, "dkim_failed", 0, 0, "")
		return outcome{kind: outcomePoison}
	}

	u, err := s.Store.UserByVerifiedEmail(ctx, parsed.From)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			slog.Info("emailingest: no verified user for sender, poison", "from", parsed.From)
			s.logIngest(ctx, parsed.MessageID, parsed.From, parsed.Subject, dkimOK, nil, "no_user", 0, 0, "")
			return outcome{kind: outcomePoison}
		}
		slog.Warn("emailingest: user lookup transient", "err", err)
		return outcome{kind: outcomeTransient}
	}

	body, docs := buildPrompt(parsed, s.Cfg.MaxBodyBytes)
	legs, err := s.Extractor.Extract(ctx, body, docs)
	if err != nil {
		// Treat any extractor failure as transient: drain loop will retry.
		slog.Warn("emailingest: extractor", "err", err)
		return outcome{kind: outcomeTransient}
	}

	added := []ReplyLeg{}
	failed := []ReplyFailure{}
	for _, leg := range legs {
		f, err := flightops.Create(ctx, s.FlightDeps, u.ID, leg.Ident, leg.Date)
		if err != nil {
			// If the provider just doesn't know about this flight yet but
			// the email itself spells out the schedule, fall back to a
			// manual add so we don't make the user re-enter what we
			// already extracted. Reserved for the two "no upstream data"
			// sentinels — transient/auth errors still surface as failures.
			if isResolverGap(err) && leg.HasManualDetails() {
				if mf, mErr := flightops.CreateManual(ctx, s.FlightDeps, u.ID, flightops.ManualCreatePayload{
					Ident:           leg.Ident,
					DepartDate:      leg.Date,
					DepartTimeLocal: leg.DepartTimeLocal,
					ArriveDate:      leg.ArriveDate,
					ArriveTimeLocal: leg.ArriveTimeLocal,
					OriginIATA:      leg.OriginIATA,
					DestIATA:        leg.DestIATA,
					Notes:           "Added from email — schedule not yet published by airline; please verify times.",
				}); mErr == nil {
					added = append(added, ReplyLeg{Ident: leg.Ident, Date: leg.Date, ManualNote: true})
					s.publishFlight(ctx, mf.ID)
					continue
				} else {
					slog.Warn("emailingest: manual fallback insert failed", "err", mErr, "ident", leg.Ident)
				}
			}
			failed = append(failed, ReplyFailure{Ident: leg.Ident, Date: leg.Date, Reason: failureReason(err)})
			continue
		}
		added = append(added, ReplyLeg{Ident: leg.Ident, Date: leg.Date})
		s.publishFlight(ctx, f.ID)
	}

	// Generalized planops capture: non-flight bookings (hotel/train/ground/
	// dining/excursion) are grouped into plans, attached to a trip chosen by
	// date proximity (auto-creating one when nothing matches), and committed
	// against that trip. Flights stay on the legacy path above for tracker /
	// SSE continuity. Gated on PlanDeps being wired so the Wave-1 flight-only
	// behaviour is unchanged when it isn't.
	if s.PlanDeps.Store != nil {
		s.captureNonFlightPlans(ctx, u.ID, body, docs)
	}

	status := "accepted"
	switch {
	case len(legs) == 0:
		status = "no_flights"
	case len(added) == 0:
		status = "all_failed"
	case len(failed) > 0:
		status = "partial"
	}
	s.logIngest(ctx, parsed.MessageID, parsed.From, parsed.Subject, dkimOK, &u.ID, status, len(added), len(failed), "")

	msg := BuildReply(ReplyInput{
		FromAddr:  s.Cfg.IngestAddress,
		ToAddr:    parsed.From,
		InReplyTo: parsed.MessageID,
		Subject:   parsed.Subject,
		Added:     added,
		Failed:    failed,
		PublicURL: s.Cfg.PublicURL,
	})
	if err := Send(ctx, s.Cfg.SendmailPath, s.Cfg.IngestAddress, msg); err != nil {
		slog.Warn("emailingest: send reply", "err", err)
		// We still consider the message processed — flights were added (or
		// the audit row was written). Don't loop on send failures.
	}
	return outcome{kind: outcomeOK}
}

// maxDocBytes caps each document we forward to the LLM. Anthropic accepts
// PDFs up to ~32 MiB; this leaves headroom and prevents an oversized
// attachment from causing the provider to reject the whole request — which
// would otherwise loop in `new/` as a transient extractor failure.
const maxDocBytes = 25 * 1024 * 1024

// buildPrompt returns the text body to put in the LLM prompt and the list
// of document attachments (PDFs) to pass alongside it. Plain text + HTML
// are concatenated into the prompt with section dividers; PDFs are
// passed natively as Document blocks rather than text-extracted. PDFs
// larger than maxDocBytes are dropped with a warning.
//
// max truncates only the text portion; documents within the per-doc cap
// are passed in full.
func buildPrompt(p *Parsed, max int) (string, []Document) {
	var sb strings.Builder
	if p.TextBody != "" {
		sb.WriteString("--- text/plain ---\n")
		sb.WriteString(p.TextBody)
		sb.WriteString("\n")
	}
	if p.HTMLBody != "" {
		sb.WriteString("--- text/html ---\n")
		sb.WriteString(p.HTMLBody)
		sb.WriteString("\n")
	}
	body := sb.String()
	if max > 0 && len(body) > max {
		body = body[:max]
	}
	docs := make([]Document, 0, len(p.PDFs))
	for i, pdfBytes := range p.PDFs {
		if len(pdfBytes) > maxDocBytes {
			slog.Warn("emailingest: dropping oversized PDF attachment",
				"index", i+1, "bytes", len(pdfBytes), "cap", maxDocBytes)
			continue
		}
		docs = append(docs, Document{
			Data:      pdfBytes,
			MediaType: "application/pdf",
			Filename:  fmt.Sprintf("attachment-%d.pdf", i+1),
		})
	}
	return body, docs
}

// isResolverGap reports whether err means the upstream provider had no
// usable record for this ident+date — i.e. the case where falling back
// to the email's own schedule details is appropriate. Transient errors
// (auth, network, rate-limit) are NOT included: those should keep
// surfacing as failures so a retry on the next tick or a user fix can
// pick them up.
func isResolverGap(err error) bool {
	return errors.Is(err, providers.ErrFlightUnscheduled) ||
		errors.Is(err, providers.ErrFlightNotFound)
}

// failureReason renders a per-leg ReplyFailure.Reason string, recognising
// the well-known sentinel errors from the resolver so the user sees a
// terse, actionable message instead of a stack of wrapped errors.
func failureReason(err error) string {
	switch {
	case errors.Is(err, providers.ErrFlightUnscheduled):
		return "the airline hasn't published a schedule for that date yet — try again closer to the departure date"
	case errors.Is(err, providers.ErrFlightNotFound):
		return "no matching flight found for that ident on that date"
	}
	s := err.Error()
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// captureNonFlightPlans runs the planops capture path for the non-flight
// bookings in an email. It proposes plans (tripID 0 → no rebooking match
// pre-attach), picks a target trip by date proximity (auto-creating one when
// nothing overlaps), and commits each plan against that trip. Failures are
// logged, not fatal: the legacy flight reply still goes out. Flight plans are
// skipped here — they are handled by the flightops path so the tracker keeps
// its single source of truth this wave.
func (s *Service) captureNonFlightPlans(ctx context.Context, userID int64, body string, emDocs []Document) {
	docs := make([]planops.Document, 0, len(emDocs))
	for _, d := range emDocs {
		docs = append(docs, planops.Document{Data: d.Data, MediaType: d.MediaType, Filename: d.Filename})
	}
	proposals, err := planops.Propose(ctx, s.PlanDeps, userID, 0, body, docs)
	if err != nil {
		slog.Warn("emailingest: planops propose", "err", err)
		return
	}
	for _, p := range proposals {
		if p.Type == "flight" {
			continue
		}
		start, end := planops.PlanSpan(p.Parts)
		tripID, ok, err := planops.SelectTrip(ctx, s.PlanDeps, userID, start, end)
		if err != nil {
			slog.Warn("emailingest: planops select trip", "err", err)
			continue
		}
		if !ok {
			tripID, err = s.createTripForPlan(ctx, userID, p, start, end)
			if err != nil {
				slog.Warn("emailingest: create trip for ingested plan", "err", err)
				continue
			}
		}
		if _, err := planops.Commit(ctx, s.PlanDeps, tripID, userID, []planops.ConfirmPlanInput{toConfirmInput(p)}); err != nil {
			slog.Warn("emailingest: planops commit", "err", err, "trip", tripID)
		}
	}
}

// createTripForPlan auto-creates a trip named from the plan title / dates when
// no existing trip matches by date proximity (spec §6.3).
func (s *Service) createTripForPlan(ctx context.Context, userID int64, p planops.ProposedPlan, start, end time.Time) (int64, error) {
	name := p.Title
	if name == "" {
		name = "Trip from email"
	}
	in := store.CreateTripPayload{Name: name}
	if !start.IsZero() {
		s := start
		in.StartsOn = &s
	}
	if !end.IsZero() {
		e := end
		in.EndsOn = &e
	}
	t, err := s.Store.CreateTrip(ctx, in, userID)
	if err != nil {
		return 0, err
	}
	return t.ID, nil
}

// toConfirmInput converts a proposed plan into a confirm payload. Email ingest
// has no interactive confirm UI, so it confirms the proposal as-extracted with
// the sender as sole passenger; the user can correct or move it afterwards.
func toConfirmInput(p planops.ProposedPlan) planops.ConfirmPlanInput {
	in := planops.ConfirmPlanInput{
		Type:             p.Type,
		Title:            p.Title,
		ConfirmationRef:  p.ConfirmationRef,
		Notes:            p.Notes,
		Source:           "email",
		SupersedesPartID: p.SupersedesPartID,
	}
	for _, part := range p.Parts {
		in.Parts = append(in.Parts, planops.ConfirmPartInput{
			Type:       part.Type,
			StartsAt:   part.StartsAt,
			EndsAt:     part.EndsAt,
			StartTZ:    part.StartTZ,
			EndTZ:      part.EndTZ,
			StartLabel: part.StartLabel,
			EndLabel:   part.EndLabel,
			Status:     part.Status,
			Flight:     part.Flight,
			Hotel:      part.Hotel,
			Train:      part.Train,
			Ground:     part.Ground,
			Dining:     part.Dining,
			Excursion:  part.Excursion,
		})
	}
	return in
}

// publishFlight broadcasts a flight.updated SSE event for the just-inserted
// flight so connected clients can drop it into their list without waiting
// for the next page refresh. Mirrors the assembly logic in the position
// poller — assembles the full FlightDTO and scopes the broadcast to the
// flight's visibility set. Silent no-op when Hub is nil.
func (s *Service) publishFlight(ctx context.Context, id int64) {
	if s.Hub == nil {
		return
	}
	f, err := s.Store.FlightByID(ctx, id)
	if err != nil {
		slog.Warn("emailingest: publishFlight refetch", "err", err, "id", id)
		return
	}
	passengers, _ := s.Store.PassengersByFlight(ctx, []int64{id})
	shares, _ := s.Store.SharedUserIDsByFlight(ctx, []int64{id})
	dto := api.ToFlightDTO(f, passengers[id], shares[id], nil, nil)
	payload, err := json.Marshal(dto)
	if err != nil {
		slog.Error("emailingest: publishFlight marshal", "err", err, "id", id)
		return
	}
	visible, err := s.Store.VisibleUserIDs(ctx, f.ID)
	if err != nil {
		slog.Warn("emailingest: publishFlight visibility", "err", err, "id", id)
	}
	s.Hub.Publish(sse.Event{Type: "flight.updated", Data: payload, VisibleTo: visible})
}

func (s *Service) logIngest(ctx context.Context, msgID, from, subject string, dkimPass bool, userID *int64, status string, added, failed int, errMsg string) {
	var msgPtr *string
	if msgID != "" {
		msgPtr = &msgID
	}
	if _, err := s.Store.InsertEmailIngest(ctx, store.EmailIngestPayload{
		MessageID:     msgPtr,
		FromAddress:   from,
		Subject:       subject,
		DKIMPass:      dkimPass,
		UserID:        userID,
		Status:        status,
		FlightsAdded:  added,
		FlightsFailed: failed,
		Error:         errMsg,
	}); err != nil {
		slog.Warn("emailingest: insert audit row", "err", err)
	}
}
