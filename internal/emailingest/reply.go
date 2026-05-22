package emailingest

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
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
	anyManual := false
	for _, l := range in.Added {
		if l.ManualNote {
			anyManual = true
			break
		}
	}
	manualSuffix := func(l ReplyLeg) string {
		if l.ManualNote {
			return " (from the email — please verify the times)"
		}
		return ""
	}
	manualTrailer := ""
	if anyManual {
		manualTrailer = "\r\nFor flight(s) marked \"from the email\", the airline hadn't published a schedule yet, so I used the details from the email itself — please check the departure and arrival times in the app.\r\n"
	}
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
