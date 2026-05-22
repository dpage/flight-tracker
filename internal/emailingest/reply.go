package emailingest

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// ReplyLeg is a flight that was added.
type ReplyLeg struct {
	Ident string
	Date  string
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

// BuildReply renders a complete RFC822 reply message as a single string.
func BuildReply(in ReplyInput) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "From: %s\r\n", in.FromAddr)
	fmt.Fprintf(&sb, "To: %s\r\n", in.ToAddr)
	if in.InReplyTo != "" {
		fmt.Fprintf(&sb, "In-Reply-To: %s\r\nReferences: %s\r\n", in.InReplyTo, in.InReplyTo)
	}
	subj := strings.TrimSpace(in.Subject)
	if subj == "" {
		subj = "Your forwarded flight email"
	}
	if !strings.HasPrefix(strings.ToLower(subj), "re:") {
		subj = "Re: " + subj
	}
	fmt.Fprintf(&sb, "Subject: %s\r\n", subj)
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")

	link := strings.TrimRight(in.PublicURL, "/")
	switch {
	case len(in.Added) > 0 && len(in.Failed) == 0:
		sb.WriteString("I processed your forwarded email and added the following flight(s):\r\n\r\n")
		for _, l := range in.Added {
			fmt.Fprintf(&sb, "  + %s on %s\r\n", l.Ident, l.Date)
		}
	case len(in.Added) > 0 && len(in.Failed) > 0:
		sb.WriteString("I processed your forwarded email and added the following flight(s):\r\n\r\n")
		for _, l := range in.Added {
			fmt.Fprintf(&sb, "  + %s on %s\r\n", l.Ident, l.Date)
		}
		fmt.Fprintf(&sb, "\r\nI couldn't add %d flight(s):\r\n\r\n", len(in.Failed))
		for _, l := range in.Failed {
			fmt.Fprintf(&sb, "  - %s on %s — %s\r\n", l.Ident, l.Date, l.Reason)
		}
		fmt.Fprintf(&sb, "\r\nPlease add those manually at %s/ .\r\n", link)
	case len(in.Failed) > 0:
		sb.WriteString("I couldn't add any of the flights from this email:\r\n\r\n")
		for _, l := range in.Failed {
			fmt.Fprintf(&sb, "  - %s on %s — %s\r\n", l.Ident, l.Date, l.Reason)
		}
		fmt.Fprintf(&sb, "\r\nPlease add them manually at %s/ .\r\n", link)
	default:
		fmt.Fprintf(&sb, "I couldn't find any flight information in this email — please add it manually at %s/ .\r\n", link)
	}
	sb.WriteString("\r\n— flight-tracker\r\n")
	return sb.String()
}

// Send pipes an RFC822 message through `sendmailPath -t`.
func Send(ctx context.Context, sendmailPath, message string) error {
	cmd := exec.CommandContext(ctx, sendmailPath, "-t")
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
