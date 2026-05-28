package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/mail"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/mailer"
	"github.com/dpage/aerly/internal/store"
)

// validateHeaderAddress rejects values that would break RFC822 framing
// (embedded CR/LF can inject extra headers) or that aren't parseable as
// a mail address at all. Both inputs we'd write into From: / To: come
// from arguably trusted sources — MailFromAddress is operator config,
// matchEmail comes from a verified user_emails row — but the cost of
// validating is trivial and the cost of a header-injection bug isn't.
func validateHeaderAddress(v string) error {
	if strings.ContainsAny(v, "\r\n") {
		return errors.New("contains CR/LF")
	}
	if _, err := mail.ParseAddress(v); err != nil {
		return err
	}
	return nil
}

// notifyIdentityLinked sends a heads-up email to the account holder when a
// new OAuth identity has just been attached to their existing account via a
// verified-email match (LinkLogin Step 2). The message lets them spot —
// and react to — an unwanted link, since the trust placed in
// "provider asserts this email is verified" is not absolute (a Workspace
// admin can issue verified addresses for any domain they control).
//
// Best-effort: every failure is logged and swallowed so the user's sign-in
// flow itself never breaks. matchEmail is the address that triggered the
// cross-provider match — it's always one of the user's verified addresses
// and is the most reliable target.
func (h *Handler) notifyIdentityLinked(ctx context.Context, user *store.User, prov *Provider, matchEmail string) {
	if h.MailFromAddress == "" {
		slog.Warn("identity-link notify: MAIL_FROM_ADDRESS unset, skipping",
			"user_id", user.ID, "provider", prov.Name)
		return
	}
	to := strings.TrimSpace(matchEmail)
	if to == "" {
		slog.Warn("identity-link notify: no match email available",
			"user_id", user.ID, "provider", prov.Name)
		return
	}
	if err := validateHeaderAddress(h.MailFromAddress); err != nil {
		slog.Warn("identity-link notify: invalid MAIL_FROM_ADDRESS, skipping",
			"err", err, "user_id", user.ID, "provider", prov.Name)
		return
	}
	if err := validateHeaderAddress(to); err != nil {
		slog.Warn("identity-link notify: invalid recipient address, skipping",
			"err", err, "user_id", user.ID, "provider", prov.Name)
		return
	}
	send := h.SendNotification
	if send == nil {
		send = mailer.Send
	}
	msg := buildIdentityLinkedEmail(identityLinkedInput{
		FromAddr:      h.MailFromAddress,
		ToAddr:        to,
		PublicURL:     h.PublicURL,
		UserName:      user.Name,
		UserUsername:  user.Username,
		ProviderLabel: prov.Label,
		MatchEmail:    to,
	})
	if err := send(ctx, h.SendmailPath, h.MailFromAddress, msg); err != nil {
		slog.Error("identity-link notify: send failed",
			"err", err, "user_id", user.ID, "provider", prov.Name)
	}
}

// identityLinkedInput is the data needed to render the cross-provider
// "new sign-in method linked" notification.
type identityLinkedInput struct {
	FromAddr      string
	ToAddr        string
	PublicURL     string
	UserName      string
	UserUsername  string
	ProviderLabel string // e.g. "Google"
	MatchEmail    string // verified email that triggered the link
}

// greetName returns the friendliest available label for the account holder
// (display name, else username).
func greetName(name, username string) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	return username
}

func buildIdentityLinkedEmail(in identityLinkedInput) string {
	site := strings.TrimRight(in.PublicURL, "/")
	greet := greetName(in.UserName, in.UserUsername)

	plain := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"A new %s sign-in method has just been linked to your Aerly account "+
			"because it presented your verified email address (%s).\r\n\r\n"+
			"If that was you — no action needed; you can now sign in via %s as well.\r\n\r\n"+
			"If it wasn't you, sign in at %s/ and review your account, or reply to this "+
			"message and let an administrator know.\r\n\r\n"+
			"— Aerly\r\n",
		greet, in.ProviderLabel, in.MatchEmail, in.ProviderLabel, site)

	htmlBody := fmt.Sprintf(
		`<p style="margin:0 0 12px;font-size:15px;">Hi %s,</p>`+
			`<p style="margin:0 0 16px;font-size:15px;">A new <strong>%s</strong> sign-in method has just been linked to your Aerly account because it presented your verified email address (<strong>%s</strong>).</p>`+
			`<p style="margin:0 0 16px;font-size:15px;">If that was you, no action is needed — you can now sign in via %s as well.</p>`+
			`<p style="margin:0 0 20px;font-size:15px;">If it wasn't you, open Aerly and review your account, or reply to this message so an administrator can investigate.</p>`+
			`<p style="margin:0;"><a href="%s/" style="display:inline-block;padding:10px 18px;border-radius:6px;background:%s;color:#ffffff;font-weight:600;text-decoration:none;">Open Aerly</a></p>`,
		mailer.HTMLEscape(greet),
		mailer.HTMLEscape(in.ProviderLabel),
		mailer.HTMLEscape(in.MatchEmail),
		mailer.HTMLEscape(in.ProviderLabel),
		mailer.HTMLEscape(site),
		mailer.BrandColor)

	subject := "A new sign-in method was linked to your Aerly account"
	return assembleNotificationRFC822(in.FromAddr, in.ToAddr, subject,
		plain, mailer.HTMLShell(subject, htmlBody, in.PublicURL))
}

// assembleNotificationRFC822 wraps a plain + html pair into a complete
// RFC822 message with the standard Aerly headers. Mirrors the helper in
// internal/handlers/friend_emails.go but lives here so the auth package
// doesn't depend on handlers.
func assembleNotificationRFC822(fromAddr, toAddr, subject, plain, htmlBody string) string {
	contentType, body := mailer.MultipartBody(plain, htmlBody)
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
