package emailingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dpage/flight-tracker/internal/flightops"
	"github.com/dpage/flight-tracker/internal/store"
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

	body := buildBodyText(parsed, s.Cfg.MaxBodyBytes)
	legs, err := s.Extractor.Extract(ctx, body)
	if err != nil {
		// Treat any extractor failure as transient: drain loop will retry.
		slog.Warn("emailingest: extractor", "err", err)
		return outcome{kind: outcomeTransient}
	}

	added := []ReplyLeg{}
	failed := []ReplyFailure{}
	for _, leg := range legs {
		if _, err := flightops.Create(ctx, s.FlightDeps, u.ID, leg.Ident, leg.Date); err != nil {
			failed = append(failed, ReplyFailure{Ident: leg.Ident, Date: leg.Date, Reason: shortErr(err)})
			continue
		}
		added = append(added, ReplyLeg{Ident: leg.Ident, Date: leg.Date})
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
	if err := Send(ctx, s.Cfg.SendmailPath, msg); err != nil {
		slog.Warn("emailingest: send reply", "err", err)
		// We still consider the message processed — flights were added (or
		// the audit row was written). Don't loop on send failures.
	}
	return outcome{kind: outcomeOK}
}

// buildBodyText concatenates the text body, HTML body, and any
// PDF-extracted text into a single string, truncated to max bytes.
func buildBodyText(p *Parsed, max int) string {
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
	for i, pdfBytes := range p.PDFs {
		text, err := ExtractPDFText(pdfBytes)
		if err != nil {
			continue
		}
		fmt.Fprintf(&sb, "--- pdf attachment %d ---\n", i+1)
		sb.WriteString(text)
		sb.WriteString("\n")
	}
	out := sb.String()
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
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
