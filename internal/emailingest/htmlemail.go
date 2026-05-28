package emailingest

import (
	"github.com/dpage/aerly/internal/mailer"
)

// These are thin package-local aliases over the canonical helpers in
// internal/mailer. Keeping the names short is convenient for the email-
// building functions in this package, which call them on nearly every
// line; new senders should import the mailer package directly.
const (
	brandColor     = mailer.BrandColor
	brandLinkStyle = mailer.BrandLinkStyle
)

func htmlShell(title, body, publicURL string) string {
	return mailer.HTMLShell(title, body, publicURL)
}

func htmlEscape(s string) string { return mailer.HTMLEscape(s) }

func multipartBody(plain, htmlBody string) (contentType, body string) {
	return mailer.MultipartBody(plain, htmlBody)
}

func quotedPrintable(s string) string { return mailer.QuotedPrintable(s) }
