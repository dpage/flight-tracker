package emailingest

import (
	"context"
	"fmt"
	"strings"

	"github.com/dpage/aerly/internal/mailer"
)

// ReplyLeg is a flight that was added.
//
// ManualNote is set when the flight was inserted from the email's own
// schedule details rather than the airline's provider data — the reply
// tells the user to double-check the times in that case.
type ReplyLeg struct {
	Ident      string
	Date       string
	ManualNote bool
}

// ReplyFailure is a flight that the LLM extracted but we couldn't add.
type ReplyFailure struct {
	Ident  string
	Date   string
	Reason string
}

// ReplyInput is everything BuildReply needs to render an RFC822 message.
type ReplyInput struct {
	FromAddr  string
	ToAddr    string
	InReplyTo string // original Message-ID, including angle brackets
	Subject   string // original Subject (we'll prefix Re: if missing)
	Added     []ReplyLeg
	Failed    []ReplyFailure
	PublicURL string // for the "add manually" link
}

const manualSuffixText = " (from the email — please verify the times)"
const manualTrailerText = "For flight(s) marked \"from the email\", the airline hadn't published a schedule yet, so I used the details from the email itself — please check the departure and arrival times in the app."

// BuildReply renders the reply as a multipart/alternative RFC822 message:
// plain-text first for legacy clients, branded HTML last so MIME-aware
// clients prefer it. Both bodies carry the same flight list and link.
func BuildReply(in ReplyInput) string {
	link := strings.TrimRight(in.PublicURL, "/")
	plain := replyPlainBody(in, link)
	htmlPart := htmlShell("Your forwarded itinerary", replyHTMLBody(in, link), in.PublicURL)
	contentType, body := multipartBody(plain, htmlPart)

	subj := strings.TrimSpace(in.Subject)
	if subj == "" {
		subj = "Your forwarded flight email"
	}
	if !strings.HasPrefix(strings.ToLower(subj), "re:") {
		subj = "Re: " + subj
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "From: %s\r\n", in.FromAddr)
	fmt.Fprintf(&sb, "To: %s\r\n", in.ToAddr)
	if in.InReplyTo != "" {
		fmt.Fprintf(&sb, "In-Reply-To: %s\r\nReferences: %s\r\n", in.InReplyTo, in.InReplyTo)
	}
	fmt.Fprintf(&sb, "Subject: %s\r\n", subj)
	sb.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&sb, "Content-Type: %s\r\n\r\n", contentType)
	sb.WriteString(body)
	return sb.String()
}

// anyManual reports whether any added leg was inserted from email-supplied
// schedule details (vs the airline's published schedule).
func anyManual(added []ReplyLeg) bool {
	for _, l := range added {
		if l.ManualNote {
			return true
		}
	}
	return false
}

func replyPlainBody(in ReplyInput, link string) string {
	manualSuffix := func(l ReplyLeg) string {
		if l.ManualNote {
			return manualSuffixText
		}
		return ""
	}
	manualTrailer := ""
	if anyManual(in.Added) {
		manualTrailer = "\r\n" + manualTrailerText + "\r\n"
	}

	var sb strings.Builder
	switch {
	case len(in.Added) > 0 && len(in.Failed) == 0:
		sb.WriteString("I processed your forwarded email and added the following flight(s):\r\n\r\n")
		for _, l := range in.Added {
			fmt.Fprintf(&sb, "  + %s on %s%s\r\n", l.Ident, l.Date, manualSuffix(l))
		}
		sb.WriteString(manualTrailer)
	case len(in.Added) > 0 && len(in.Failed) > 0:
		sb.WriteString("I processed your forwarded email and added the following flight(s):\r\n\r\n")
		for _, l := range in.Added {
			fmt.Fprintf(&sb, "  + %s on %s%s\r\n", l.Ident, l.Date, manualSuffix(l))
		}
		fmt.Fprintf(&sb, "\r\nI couldn't add %d flight(s):\r\n\r\n", len(in.Failed))
		for _, l := range in.Failed {
			fmt.Fprintf(&sb, "  - %s on %s — %s\r\n", l.Ident, l.Date, l.Reason)
		}
		sb.WriteString(manualTrailer)
		fmt.Fprintf(&sb, "\r\nPlease add the failed flight(s) manually at %s/ .\r\n", link)
	case len(in.Failed) > 0:
		sb.WriteString("I couldn't add any of the flights from this email:\r\n\r\n")
		for _, l := range in.Failed {
			fmt.Fprintf(&sb, "  - %s on %s — %s\r\n", l.Ident, l.Date, l.Reason)
		}
		fmt.Fprintf(&sb, "\r\nPlease add them manually at %s/ .\r\n", link)
	default:
		fmt.Fprintf(&sb, "I couldn't find any flight information in this email — please add it manually at %s/ .\r\n", link)
	}
	sb.WriteString("\r\n— Aerly\r\n")
	return sb.String()
}

func replyHTMLBody(in ReplyInput, link string) string {
	addedHTML := legBlockHTML(
		"I added the following flight(s):",
		"added", "#dcfce7", "#166534",
		legsFromAdded(in.Added))
	failedHTML := legBlockHTML(
		fmt.Sprintf("I couldn't add %d flight(s):", len(in.Failed)),
		"skipped", "#fef3c7", "#92400e",
		legsFromFailed(in.Failed))

	manualTrailerHTML := ""
	if anyManual(in.Added) {
		manualTrailerHTML = fmt.Sprintf(
			`<p style="margin:0 0 16px;padding:10px 14px;background:#fef3c7;border-left:3px solid #92400e;color:#5b3a07;font-size:13px;line-height:1.5;">%s</p>`,
			htmlEscape(manualTrailerText))
	}

	safeLink := htmlEscape(link + "/")
	manual := fmt.Sprintf(
		`<p style="margin:0;font-size:14px;color:#555;">Please <a href="%s" style="%s">add the failed flight(s) manually</a>.</p>`,
		safeLink, brandLinkStyle)

	switch {
	case len(in.Added) > 0 && len(in.Failed) == 0:
		return addedHTML + manualTrailerHTML
	case len(in.Added) > 0 && len(in.Failed) > 0:
		return addedHTML + failedHTML + manualTrailerHTML + manual
	case len(in.Failed) > 0:
		return `<p style="margin:0 0 12px;font-size:15px;">I couldn't add any of the flights from this email:</p>` + failedHTML + manual
	default:
		return fmt.Sprintf(
			`<p style="margin:0;font-size:15px;">I couldn't find any flight information in this email — please <a href="%s" style="%s">add it manually</a>.</p>`,
			safeLink, brandLinkStyle)
	}
}

// legRow is the row data passed to legBlockHTML. Note carries either a
// failure reason (for skipped rows) or a manual-entry hint (for added
// rows that came from the email's own schedule details).
type legRow struct{ Ident, Date, Note string }

func legsFromAdded(ls []ReplyLeg) []legRow {
	out := make([]legRow, len(ls))
	for i, l := range ls {
		note := ""
		if l.ManualNote {
			note = "From the email — please verify the times in the app."
		}
		out[i] = legRow{Ident: l.Ident, Date: l.Date, Note: note}
	}
	return out
}

func legsFromFailed(ls []ReplyFailure) []legRow {
	out := make([]legRow, len(ls))
	for i, l := range ls {
		out[i] = legRow{Ident: l.Ident, Date: l.Date, Note: l.Reason}
	}
	return out
}

// legBlockHTML renders an intro paragraph plus a chip-prefixed list of
// flight rows. chipBG and chipFG are the chip's background and foreground
// colours; chipLabel is its text.
func legBlockHTML(intro, chipLabel, chipBG, chipFG string, rows []legRow) string {
	if len(rows) == 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, `<p style="margin:0 0 12px;font-size:15px;">%s</p>`, htmlEscape(intro))
	sb.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%" style="margin:0 0 20px;border-collapse:collapse;">`)
	for _, r := range rows {
		sb.WriteString(`<tr><td style="padding:8px 0;border-top:1px solid #eaeaea;">`)
		fmt.Fprintf(&sb,
			`<span style="display:inline-block;padding:2px 10px;border-radius:12px;background:%s;color:%s;font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:0.3px;">%s</span>`,
			chipBG, chipFG, htmlEscape(chipLabel))
		fmt.Fprintf(&sb,
			`<span style="font-weight:600;margin-left:10px;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;">%s</span>`,
			htmlEscape(r.Ident))
		fmt.Fprintf(&sb,
			`<span style="color:#666;margin-left:10px;font-size:14px;">%s</span>`,
			htmlEscape(r.Date))
		if r.Note != "" {
			fmt.Fprintf(&sb,
				`<div style="margin-top:4px;font-size:13px;color:#666;">%s</div>`,
				htmlEscape(r.Note))
		}
		sb.WriteString(`</td></tr>`)
	}
	sb.WriteString(`</table>`)
	return sb.String()
}

// Send is retained as a thin alias over mailer.Send so existing callers and
// tests in this package keep their import surface stable. New senders
// should call mailer.Send directly.
func Send(ctx context.Context, sendmailPath, envelopeSender, message string) error {
	return mailer.Send(ctx, sendmailPath, envelopeSender, message)
}
