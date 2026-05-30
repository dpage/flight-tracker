package mailer

import (
	"fmt"
	"mime"
	"strings"
	"time"
)

// FlightAlertInput is the data needed to render a flight-change alert email
// (spec §9). Kind selects the headline ("delayed" / "cancelled" / "diverted");
// When is the new effective time for a delay (zero for cancellations, which
// have no meaningful new time). Detail is a short human phrase describing the
// change, e.g. "now departing 14:35" or "to a different airport".
type FlightAlertInput struct {
	FromAddr  string
	ToAddr    string
	PublicURL string
	Ident     string    // flight ident, e.g. "BA123"
	Kind      string    // delayed|cancelled|diverted
	Detail    string    // ready-to-render change phrase
	When      time.Time // new effective time for delays; zero otherwise
}

// FlightAlertSubject returns the Subject line for a flight-change alert, e.g.
// "Your flight BA123 is now delayed". Exposed so callers can log / reuse it.
func FlightAlertSubject(ident, kind string) string {
	switch kind {
	case "cancelled":
		return fmt.Sprintf("Your flight %s has been cancelled", ident)
	case "diverted":
		return fmt.Sprintf("Your flight %s has been diverted", ident)
	default:
		return fmt.Sprintf("Your flight %s is now delayed", ident)
	}
}

// BuildFlightAlertEmail renders the complete RFC822 message (plain + branded
// HTML alternative) for a flight-change alert. The body leads with the
// headline ("Your flight BA123 is now delayed to …") so it reads well in a
// notification preview, per spec §9.
func BuildFlightAlertEmail(in FlightAlertInput) string {
	site := strings.TrimRight(in.PublicURL, "/")
	subject := FlightAlertSubject(in.Ident, in.Kind)

	// Lead line mirrors the spec's example phrasing.
	lead := subject
	if in.Detail != "" {
		lead = subject + " — " + in.Detail
	}

	plain := fmt.Sprintf(
		"%s.\r\n\r\n"+
			"Open Aerly to see the latest on this flight: %s/\r\n\r\n"+
			"— Aerly\r\n",
		lead, site)

	htmlBody := fmt.Sprintf(
		`<p style="margin:0 0 16px;font-size:15px;">%s.</p>`+
			`<p style="margin:0;"><a href="%s/" style="display:inline-block;padding:10px 18px;border-radius:6px;background:%s;color:#ffffff;font-weight:600;text-decoration:none;">Open Aerly</a></p>`,
		HTMLEscape(lead), HTMLEscape(site), BrandColor)

	return assembleAlertRFC822(in.FromAddr, in.ToAddr, subject,
		plain, HTMLShell(subject, htmlBody, in.PublicURL))
}

// assembleAlertRFC822 wraps a plain + html pair into a complete RFC822 message
// with the standard Aerly headers. Mirrors the helper in auth/notify.go; kept
// here so the poller's alert step depends only on mailer.
func assembleAlertRFC822(fromAddr, toAddr, subject, plain, htmlBody string) string {
	contentType, body := MultipartBody(plain, htmlBody)
	encodedSubject := mime.QEncoding.Encode("utf-8", subject)
	var sb strings.Builder
	fmt.Fprintf(&sb, "From: %s\r\n", fromAddr)
	fmt.Fprintf(&sb, "To: %s\r\n", toAddr)
	fmt.Fprintf(&sb, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&sb, "Subject: %s\r\n", encodedSubject)
	sb.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&sb, "Content-Type: %s\r\n\r\n", contentType)
	sb.WriteString(body)
	return sb.String()
}
