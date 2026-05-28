// Package mailer carries the shared outbound-email primitives: the branded
// HTML shell, multipart/alternative assembly, and the local sendmail pipe.
// Feature-specific senders (auth notifications, email verification, ...)
// build on it.
package mailer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"mime/quotedprintable"
	"net/url"
	"os/exec"
	"strings"
)

// BrandColor is the primary brand colour shared with the SPA's MUI theme
// (`palette.primary.main` in web/src/theme.ts). Keep these in sync.
const BrandColor = "#1f5fa8"

// BrandLinkStyle is the inline style for in-body links.
const BrandLinkStyle = "color:" + BrandColor + ";text-decoration:none;"

// HTMLShell wraps body content in the branded HTML shell — a header bar
// with the brand mark and wordmark, the body in a white card, and a
// muted footer linking back to publicURL. All styling is inline so
// rendering survives Gmail / Outlook / Apple Mail's CSS stripping.
//
// The body argument must be HTML — callers escape user-supplied content
// themselves using HTMLEscape() before composing.
func HTMLShell(title, body, publicURL string) string {
	site := strings.TrimRight(publicURL, "/")
	host := site
	if u, err := url.Parse(site); err == nil && u.Host != "" {
		host = u.Host
	}
	safeTitle := html.EscapeString(title)
	safeSite := html.EscapeString(site)
	safeHost := html.EscapeString(host)
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + safeTitle + `</title>
</head>
<body style="margin:0;padding:0;background:#f5f6fa;font-family:system-ui,-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#1a1a1a;">
<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%" style="background:#f5f6fa;padding:24px 12px;">
<tr><td align="center">
<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%" style="max-width:560px;background:#ffffff;border-radius:8px;overflow:hidden;border:1px solid #e5e7eb;">
<tr><td style="background:` + BrandColor + `;padding:18px 24px;">
<table role="presentation" cellpadding="0" cellspacing="0" border="0">
<tr>
<td valign="middle" style="padding-right:12px;">
<div style="width:36px;height:36px;border-radius:8px;background:rgba(255,255,255,0.18);color:#ffffff;font-size:20px;line-height:36px;text-align:center;font-family:Arial,sans-serif;">&#9992;&#xFE0E;</div>
</td>
<td valign="middle" style="color:#ffffff;font-size:18px;font-weight:600;letter-spacing:0.2px;">Aerly</td>
</tr>
</table>
</td></tr>
<tr><td style="padding:24px;font-size:15px;line-height:1.55;color:#1a1a1a;">
` + body + `
</td></tr>
<tr><td style="padding:14px 24px;background:#fafafa;border-top:1px solid #eaeaea;color:#666;font-size:12px;line-height:1.4;">
Sent by Aerly · <a href="` + safeSite + `" style="color:` + BrandColor + `;text-decoration:none;">` + safeHost + `</a>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>`
}

// HTMLEscape escapes user-supplied content for safe inclusion in HTML.
func HTMLEscape(s string) string { return html.EscapeString(s) }

// MultipartBody renders the multipart/alternative body (boundary + two
// parts) and returns the Content-Type header value and the body. The
// text/plain part comes first; text/html last, so MIME-aware clients
// prefer the HTML alternative (RFC 2046 §5.1.4).
//
// The text/html part is sent as quoted-printable so every line stays
// under the 76-column convention — well below the 998-octet SMTP line
// limit (RFC 5321 §4.5.3.1.6). Without this, our inline-CSS HTML body
// would emit one logical line per major block and an RFC-compliant MTA
// would soft-wrap it with CRLF<SP> after opendkim signed, breaking the
// DKIM body hash at the receiver. The text/plain part stays as 8bit
// because every line we emit there is already short.
func MultipartBody(plain, htmlBody string) (contentType, body string) {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	boundary := "ae-" + hex.EncodeToString(b)

	var sb strings.Builder
	sb.WriteString("This is a multipart message in MIME format.\r\n\r\n")
	fmt.Fprintf(&sb, "--%s\r\n", boundary)
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	sb.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	sb.WriteString(plain)
	if !strings.HasSuffix(plain, "\r\n") {
		sb.WriteString("\r\n")
	}
	fmt.Fprintf(&sb, "\r\n--%s\r\n", boundary)
	sb.WriteString("Content-Type: text/html; charset=utf-8\r\n")
	sb.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	sb.WriteString(QuotedPrintable(htmlBody))
	if !strings.HasSuffix(sb.String(), "\r\n") {
		sb.WriteString("\r\n")
	}
	fmt.Fprintf(&sb, "\r\n--%s--\r\n", boundary)

	return fmt.Sprintf("multipart/alternative; boundary=\"%s\"", boundary), sb.String()
}

// QuotedPrintable encodes s with RFC 2045 quoted-printable transfer
// encoding. Go's mime/quotedprintable.Writer wraps lines at 76 chars
// with soft line breaks (=\r\n).
func QuotedPrintable(s string) string {
	var buf strings.Builder
	w := quotedprintable.NewWriter(&buf)
	_, _ = io.WriteString(w, s)
	_ = w.Close()
	return buf.String()
}

// Send pipes an RFC822 message through `sendmailPath -t -f <envelopeSender>`.
//
// The envelope sender (the address Postfix will use as the SMTP MAIL FROM)
// must align with the visible From: header's domain so DMARC with strict
// aSPF can pass via SPF as well as DKIM. An empty envelopeSender omits the
// -f flag and falls back to sendmail's default (typically the local Unix
// user), which won't align — pass a configured outbound address.
func Send(ctx context.Context, sendmailPath, envelopeSender, message string) error {
	if sendmailPath == "" {
		return errors.New("sendmail path not configured")
	}
	args := []string{"-t"}
	if envelopeSender != "" {
		args = append(args, "-f", envelopeSender)
	}
	cmd := exec.CommandContext(ctx, sendmailPath, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if _, err := io.WriteString(stdin, message); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return err
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Wait()
		return err
	}
	return cmd.Wait()
}
