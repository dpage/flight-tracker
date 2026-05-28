package handlers

import (
	"fmt"
	"mime"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/mailer"
)

// friendInviteInput is the data needed to render an email to an address
// that doesn't have an Aerly account yet — they're being asked to sign up
// and the queued request will auto-accept on first sign-in.
type friendInviteInput struct {
	FromAddr     string
	ToAddr       string
	PublicURL    string
	InviterName  string // falls back to InviterUsername when empty
	InviterLogin string
	Message      string
}

// friendRequestInput is the data needed for the "X added you as a friend"
// notification sent to an existing user.
type friendRequestInput struct {
	FromAddr     string
	ToAddr       string
	PublicURL    string
	InviterName  string
	InviterLogin string
	Message      string
}

func inviterLabel(name, login string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	return login
}

func buildFriendInviteEmail(in friendInviteInput) string {
	link := strings.TrimRight(in.PublicURL, "/")
	inviter := inviterLabel(in.InviterName, in.InviterLogin)

	plain := fmt.Sprintf(
		"%s has invited you to join them on Aerly so you can keep track of each other's flights.\r\n\r\n"+
			"Sign in at %s/ and your friendship will be set up automatically.\r\n",
		inviter, link)
	if msg := strings.TrimSpace(in.Message); msg != "" {
		plain += "\r\nMessage from " + inviter + ":\r\n  " + msg + "\r\n"
	}
	plain += "\r\n— Aerly\r\n"

	htmlBody := fmt.Sprintf(
		`<p style="margin:0 0 12px;font-size:15px;"><strong>%s</strong> has invited you to join them on Aerly so you can keep track of each other's flights.</p>`+
			`<p style="margin:0 0 16px;font-size:15px;">Sign in and your friendship will be set up automatically — no extra step on your side.</p>`+
			`<p style="margin:0;"><a href="%s/" style="display:inline-block;padding:10px 18px;border-radius:6px;background:%s;color:#ffffff;font-weight:600;text-decoration:none;">Open Aerly</a></p>`,
		mailer.HTMLEscape(inviter), mailer.HTMLEscape(link), mailer.BrandColor)
	if msg := strings.TrimSpace(in.Message); msg != "" {
		htmlBody += fmt.Sprintf(
			`<p style="margin:18px 0 6px;font-size:13px;color:#666;">Message from %s:</p>`+
				`<blockquote style="margin:0;padding:10px 14px;border-left:3px solid #eaeaea;color:#333;font-size:14px;">%s</blockquote>`,
			mailer.HTMLEscape(inviter), mailer.HTMLEscape(msg))
	}

	subject := inviter + " invited you to Aerly"
	return assembleRFC822(in.FromAddr, in.ToAddr, subject,
		plain, mailer.HTMLShell(subject, htmlBody, in.PublicURL))
}

func buildFriendRequestEmail(in friendRequestInput) string {
	link := strings.TrimRight(in.PublicURL, "/")
	inviter := inviterLabel(in.InviterName, in.InviterLogin)

	plain := fmt.Sprintf(
		"%s wants to add you as a friend on Aerly. Once you accept they'll be able to see your flights and you'll see theirs.\r\n\r\n"+
			"Review the request at %s/friends .\r\n",
		inviter, link)
	if msg := strings.TrimSpace(in.Message); msg != "" {
		plain += "\r\nMessage from " + inviter + ":\r\n  " + msg + "\r\n"
	}
	plain += "\r\n— Aerly\r\n"

	htmlBody := fmt.Sprintf(
		`<p style="margin:0 0 12px;font-size:15px;"><strong>%s</strong> wants to add you as a friend on Aerly.</p>`+
			`<p style="margin:0 0 16px;font-size:15px;">Once you accept they'll be able to see your flights and you'll see theirs.</p>`+
			`<p style="margin:0;"><a href="%s/friends" style="display:inline-block;padding:10px 18px;border-radius:6px;background:%s;color:#ffffff;font-weight:600;text-decoration:none;">Review the request</a></p>`,
		mailer.HTMLEscape(inviter), mailer.HTMLEscape(link), mailer.BrandColor)
	if msg := strings.TrimSpace(in.Message); msg != "" {
		htmlBody += fmt.Sprintf(
			`<p style="margin:18px 0 6px;font-size:13px;color:#666;">Message from %s:</p>`+
				`<blockquote style="margin:0;padding:10px 14px;border-left:3px solid #eaeaea;color:#333;font-size:14px;">%s</blockquote>`,
			mailer.HTMLEscape(inviter), mailer.HTMLEscape(msg))
	}

	subject := inviter + " sent you a friend request on Aerly"
	return assembleRFC822(in.FromAddr, in.ToAddr, subject,
		plain, mailer.HTMLShell(subject, htmlBody, in.PublicURL))
}

func assembleRFC822(fromAddr, toAddr, subject, plain, htmlBody string) string {
	contentType, body := mailer.MultipartBody(plain, htmlBody)
	// RFC 2047 Q-encode the subject so non-ASCII inviter names survive
	// strict MTAs; pure-ASCII subjects round-trip unchanged.
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
