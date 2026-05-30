package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/mailer"
	"github.com/dpage/aerly/internal/store"
)

// Flight-alert step (spec §9). The poller refresh path calls maybeAlert after
// it has refreshed a part's flight_details. We compare the part's NEW effective
// state against (a) the part's PRE-refresh state, to decide whether a
// meaningful change happened, and (b) a stored per-part dedupe signature, to
// suppress re-sending the same change on the next tick.
//
// "Meaningful" per spec §9 / PRD §6.8:
//   - any transition INTO Cancelled or Diverted always alerts;
//   - a departure delay (effective_out - scheduled_out) that grows by at least
//     the recipient's alert_prefs.min_delay_min threshold alerts.
//
// There is no gate column in flight_details (the schema never modelled gate),
// so gate-change detection from the spec is not implementable here without a
// schema addition; status + delay cover the cancellation/diversion/delay cases
// the PRD calls out as always-alert / threshold-gated.

// delaySigBucketMin is the granularity at which a delay is folded into the
// dedupe signature. Two polls reporting the same delay (to the minute) produce
// the same signature and so don't re-alert. We bucket to whole minutes; the
// effective-out time is already minute-resolution in practice.
const delaySigBucketMin = 1

// alertState is the minimal snapshot of a flight part used both to detect a
// meaningful change and to build the dedupe signature.
type alertState struct {
	status     string
	delayMin   int  // departure delay = effective_out - scheduled_out, clamped >= 0
	hasDelay   bool // false when there's no estimated/actual departure yet
	terminalDV bool // status is Cancelled or Diverted
}

func snapshot(f *store.Flight) alertState {
	st := alertState{status: f.Status}
	st.terminalDV = f.Status == "Cancelled" || f.Status == "Diverted"
	eff := effectiveOut(f)
	if eff != nil {
		d := int(eff.Sub(f.ScheduledOut).Minutes())
		if d < 0 {
			d = 0
		}
		st.delayMin, st.hasDelay = d, true
	}
	return st
}

// effectiveOut mirrors FlightDetail.EffectiveOut on the carrier struct: prefer
// actual, then estimated; nil when neither is set (so a flight with only a
// scheduled time reports no delay).
func effectiveOut(f *store.Flight) *time.Time {
	if f.ActualOut != nil {
		return f.ActualOut
	}
	if f.EstimatedOut != nil {
		return f.EstimatedOut
	}
	return nil
}

// alertSignature is the per-part dedupe key for a state. Cancellation/diversion
// key on status alone; a delay keys on the bucketed delay so the same delay
// isn't re-sent, but a deeper delay produces a new signature and re-alerts.
func alertSignature(st alertState) string {
	if st.terminalDV {
		return "status:" + st.status
	}
	if st.hasDelay {
		return fmt.Sprintf("delay:%d", (st.delayMin/delaySigBucketMin)*delaySigBucketMin)
	}
	return ""
}

// maybeAlert is invoked by refresh after the part's flight_details have been
// refreshed. prev is the part's pre-refresh carrier; partID identifies the
// part. It re-fetches the post-refresh state, decides whether a meaningful
// change occurred, dedupes against the stored signature, and fans out in-app +
// email alerts to the recipient set filtered by each user's alert_prefs.
func (p *Poller) maybeAlert(ctx context.Context, prev *store.Flight, partID int64) {
	cur, err := p.Store.FlightPartByID(ctx, partID)
	if err != nil {
		// Benign: the part may have been deleted concurrently. Anything else
		// is logged by the caller's own paths; here we just skip.
		return
	}
	prevSt := snapshot(prev)
	curSt := snapshot(cur)

	kind := changeKind(prevSt, curSt)
	if kind == "" {
		return
	}

	// Dedupe: don't re-send a change whose signature matches the last one we
	// alerted on for this part.
	sig := alertSignature(curSt)
	if last, ok, serr := p.Store.FlightPartAlertSig(ctx, partID); serr == nil && ok && last == sig {
		return
	} else if serr != nil {
		slog.Error("alert: read dedupe sig", "id", partID, "err", serr)
		return
	}

	// Resolve the trip/plan context (for the SSE payload + visibility) and the
	// recipient set with prefs.
	tp, err := p.Store.TrackerPartRow(ctx, partID)
	if err != nil {
		slog.Error("alert: tracker row", "id", partID, "err", err)
		return
	}
	recips, err := p.Store.AlertRecipientsWithPrefs(ctx, tp.PlanID)
	if err != nil {
		slog.Error("alert: recipients", "plan_id", tp.PlanID, "err", err)
		return
	}

	detail := changeDetail(kind, cur)
	p.dispatchAlert(ctx, tp, cur, kind, detail, curSt, recips)

	// Stamp the dedupe signature only after we've attempted delivery so a mid-
	// flight crash re-alerts rather than silently swallowing the change.
	if err := p.Store.SetFlightPartAlertSig(ctx, partID, sig); err != nil {
		slog.Error("alert: stamp dedupe sig", "id", partID, "err", err)
	}
}

// changeKind classifies the transition prev → cur into an alert kind, or ""
// when nothing alert-worthy happened. Cancellation/diversion are always
// alert-worthy on entry. A delay is alert-worthy when it grew vs the previous
// snapshot (threshold filtering is per-recipient and happens in dispatchAlert).
func changeKind(prev, cur alertState) string {
	// Cancellation / diversion: alert on entry into the terminal state.
	if cur.status == "Cancelled" && prev.status != "Cancelled" {
		return "cancelled"
	}
	if cur.status == "Diverted" && prev.status != "Diverted" {
		return "diverted"
	}
	// Delay: a (deeper) departure delay. We treat any increase in the measured
	// delay as a candidate; the min_delay_min threshold is applied per
	// recipient so a 5-minute slip below everyone's threshold is suppressed
	// there, and dedupe stops the same delay re-firing.
	if cur.hasDelay && cur.delayMin > prev.delayMin && cur.delayMin > 0 {
		return "delayed"
	}
	return ""
}

// changeDetail builds the human phrase appended to the headline. For a delay it
// names the new effective departure; cancellation/diversion stand alone.
func changeDetail(kind string, cur *store.Flight) string {
	switch kind {
	case "delayed":
		if eff := effectiveOut(cur); eff != nil {
			return "now departing " + eff.UTC().Format("15:04 MST")
		}
		return "now delayed"
	case "diverted":
		return "diverted to a different airport"
	default:
		return ""
	}
}

// alertMessage is the one-liner carried in the in-app SSE payload.
func alertMessage(ident, kind, detail string) string {
	subj := mailer.FlightAlertSubject(ident, kind)
	if detail != "" {
		return subj + " — " + detail
	}
	return subj
}

// dispatchAlert fans the change out to every recipient, filtered by each user's
// alert_prefs: in-app via a per-user notifications.updated-style SSE event, and
// email via the mailer when an address is on file and email alerts are on. For
// a delay, the recipient's min_delay_min threshold gates BOTH channels.
func (p *Poller) dispatchAlert(
	ctx context.Context,
	tp *store.TrackerPart,
	cur *store.Flight,
	kind, detail string,
	st alertState,
	recips []store.AlertRecipient,
) {
	msg := alertMessage(cur.Ident, kind, detail)
	always := kind == "cancelled" || kind == "diverted"

	for _, r := range recips {
		// Threshold filter for delays only; cancellations/diversions always
		// alert (PRD §6.8).
		if !always && st.delayMin < r.MinDelayMin {
			continue
		}
		if r.InApp {
			p.publishAlert(r.UserID, tp, cur.Ident, kind, msg)
		}
		if r.Email && r.EmailAddr != "" {
			p.sendAlertEmail(ctx, r.EmailAddr, cur.Ident, kind, detail)
		}
	}
}

// publishAlert pushes a single-user, user-private alert.created SSE event. The
// payload reuses the open-shape NotificationsDTO with the Alert field set, so
// existing clients that only read friend_requests_pending ignore it safely.
func (p *Poller) publishAlert(userID int64, tp *store.TrackerPart, ident, kind, msg string) {
	dto := api.NotificationsDTO{
		Alert: &api.FlightAlertDTO{
			PlanPartID: tp.PlanPartID,
			PlanID:     tp.PlanID,
			TripID:     tp.TripID,
			Ident:      ident,
			Kind:       kind,
			Status:     tp.Status,
			Message:    msg,
		},
	}
	payload, err := json.Marshal(dto)
	if err != nil {
		slog.Error("alert: marshal", "err", err)
		return
	}
	p.Hub.Publish(sseAlertEvent(userID, payload))
}

// sendAlertEmail dispatches the templated flight-change email. Best-effort:
// failures are logged and never block the poll loop. Skipped when no sender or
// MailFrom is configured (e.g. dev without a sendmail pipe).
func (p *Poller) sendAlertEmail(ctx context.Context, to, ident, kind, detail string) {
	if p.MailFromAddress == "" {
		return
	}
	send := p.SendAlertEmail
	if send == nil {
		send = mailer.Send
	}
	msg := mailer.BuildFlightAlertEmail(mailer.FlightAlertInput{
		FromAddr:  p.MailFromAddress,
		ToAddr:    to,
		PublicURL: p.PublicURL,
		Ident:     ident,
		Kind:      kind,
		Detail:    detail,
	})
	if err := send(ctx, p.SendmailPath, p.MailFromAddress, msg); err != nil {
		slog.Error("alert: send email", "to", to, "ident", ident, "err", err)
	}
}
