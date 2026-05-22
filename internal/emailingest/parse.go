// Package emailingest implements the forwarded-email → flight ingest pipeline.
package emailingest

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
)

// Parsed is the result of breaking an RFC822 message into the parts we care about.
type Parsed struct {
	From                  string
	MessageID             string
	Subject               string
	AuthenticationResults string
	TextBody              string
	HTMLBody              string
	PDFs                  [][]byte
}

// Parse reads an RFC822 message. Returns an error only if the headers can't
// be parsed; missing parts are zero-valued.
func Parse(raw []byte) (*Parsed, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("read message: %w", err)
	}
	out := &Parsed{}

	if addr, err := mail.ParseAddress(msg.Header.Get("From")); err == nil {
		out.From = addr.Address
	}
	out.MessageID = strings.TrimSpace(msg.Header.Get("Message-ID"))
	out.Subject = decodeRFC2047(msg.Header.Get("Subject"))
	// Concatenate every Authentication-Results header value, one per line.
	out.AuthenticationResults = strings.Join(msg.Header["Authentication-Results"], "\n")

	ct := msg.Header.Get("Content-Type")
	if ct == "" {
		ct = "text/plain"
	}
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || !strings.Contains(mediaType, "/") {
		// Treat anything we can't parse as plain text.
		mediaType, params = "text/plain", nil
	}
	if err := walkBody(msg.Body, mediaType, params, msg.Header.Get("Content-Transfer-Encoding"), out); err != nil {
		return nil, err
	}
	return out, nil
}

func walkBody(r io.Reader, mediaType string, params map[string]string, encoding string, out *Parsed) error {
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		boundary := params["boundary"]
		if boundary == "" {
			return fmt.Errorf("multipart without boundary")
		}
		mr := multipart.NewReader(r, boundary)
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			partCT := p.Header.Get("Content-Type")
			if partCT == "" {
				partCT = "text/plain"
			}
			partMT, partParams, err := mime.ParseMediaType(partCT)
			if err != nil {
				continue
			}
			partEnc := p.Header.Get("Content-Transfer-Encoding")
			if err := walkBody(p, partMT, partParams, partEnc, out); err != nil {
				return err
			}
		}
	case mediaType == "text/plain":
		b, err := readEncoded(r, encoding)
		if err != nil {
			return err
		}
		out.TextBody += string(b)
	case mediaType == "text/html":
		b, err := readEncoded(r, encoding)
		if err != nil {
			return err
		}
		out.HTMLBody += string(b)
	case mediaType == "application/pdf":
		b, err := readEncoded(r, encoding)
		if err != nil {
			return err
		}
		out.PDFs = append(out.PDFs, b)
	}
	return nil
}

// readEncoded reads r and decodes per Content-Transfer-Encoding.
// Recognised: base64 (with whitespace folding), quoted-printable.
// Anything else is treated as raw.
func readEncoded(r io.Reader, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		raw, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		// Strip whitespace that splits base64 across MIME lines.
		clean := bytes.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, raw)
		decoded, err := base64.StdEncoding.DecodeString(string(clean))
		if err != nil {
			return nil, fmt.Errorf("base64 decode: %w", err)
		}
		return decoded, nil
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(r))
	default:
		return io.ReadAll(r)
	}
}

func decodeRFC2047(s string) string {
	dec := new(mime.WordDecoder)
	out, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return out
}
