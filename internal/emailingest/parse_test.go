package emailingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParse_PlainText(t *testing.T) {
	p, err := Parse(loadFixture(t, "plain_text.eml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.From != "devrim@example.com" {
		t.Errorf("From = %q, want devrim@example.com", p.From)
	}
	if p.MessageID != "<plain1@example.com>" {
		t.Errorf("MessageID = %q", p.MessageID)
	}
	if p.Subject != "Fwd: TK1980 confirmation" {
		t.Errorf("Subject = %q", p.Subject)
	}
	if !strings.Contains(p.TextBody, "TK1980 IST") {
		t.Errorf("TextBody missing flight info: %q", p.TextBody)
	}
	if p.HTMLBody != "" {
		t.Errorf("HTMLBody should be empty, got %q", p.HTMLBody)
	}
	if !strings.Contains(p.AuthenticationResults, "dkim=pass") {
		t.Errorf("AuthenticationResults missing dkim=pass: %q", p.AuthenticationResults)
	}
}

func TestParse_HTMLOnly(t *testing.T) {
	p, err := Parse(loadFixture(t, "html_only.eml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !strings.Contains(p.HTMLBody, "BA432") {
		t.Errorf("HTMLBody missing flight info: %q", p.HTMLBody)
	}
	if p.TextBody != "" {
		t.Errorf("TextBody should be empty, got %q", p.TextBody)
	}
}

func TestParse_MultipartWithPDF(t *testing.T) {
	p, err := Parse(loadFixture(t, "multipart.eml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !strings.Contains(p.TextBody, "TK1980") {
		t.Errorf("TextBody missing flight info: %q", p.TextBody)
	}
	if !strings.Contains(p.HTMLBody, "TK1980") {
		t.Errorf("HTMLBody missing flight info: %q", p.HTMLBody)
	}
	if len(p.PDFs) != 1 {
		t.Errorf("len(PDFs) = %d, want 1", len(p.PDFs))
	}
	if len(p.PDFs) == 1 && !strings.HasPrefix(string(p.PDFs[0]), "%PDF-1.4") {
		t.Errorf("PDF bytes not decoded from base64: %q", p.PDFs[0])
	}
	// RFC2047 (=?UTF-8?B?...?=) subject should decode.
	if !strings.Contains(p.Subject, "TK1980") {
		t.Errorf("RFC2047 subject not decoded: %q", p.Subject)
	}
	// QP decode dropped the soft line break ("=" + newline).
	if !strings.Contains(p.TextBody, "TK1980 on 12 June 2026. Extra QP-encoded text.") {
		t.Errorf("QP not decoded properly: %q", p.TextBody)
	}
}

func TestParse_AddressInAngles(t *testing.T) {
	raw := []byte("From: \"Alice\" <alice@example.com>\r\nSubject: t\r\n\r\nbody\r\n")
	p, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.From != "alice@example.com" {
		t.Errorf("From = %q, want alice@example.com", p.From)
	}
}

func TestParse_NoContentType(t *testing.T) {
	raw := []byte("From: a@x\r\nSubject: t\r\n\r\nhello world\r\n")
	p, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.TextBody, "hello world") {
		t.Errorf("missing body: %q", p.TextBody)
	}
}

func TestParse_BadContentTypeFallsBackToPlain(t *testing.T) {
	raw := []byte("From: a@x\r\nContent-Type: !!!garbage!!!\r\n\r\nhi\r\n")
	p, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.TextBody, "hi") {
		t.Errorf("expected fall-through to plain text, got %q", p.TextBody)
	}
}

func TestParse_MultipartWithoutBoundary(t *testing.T) {
	raw := []byte("From: a@x\r\nContent-Type: multipart/mixed\r\n\r\nbody\r\n")
	_, err := Parse(raw)
	if err == nil {
		t.Error("expected error for multipart without boundary")
	}
}

func TestParse_Malformed(t *testing.T) {
	_, err := Parse([]byte("not an email"))
	if err == nil {
		t.Error("expected error for malformed input")
	}
}

func TestParse_MultipartSkipsBadPartContentType(t *testing.T) {
	raw := []byte("From: a@x\r\nContent-Type: multipart/mixed; boundary=B\r\n\r\n--B\r\n" +
		"Content-Type: !!!garbage!!!\r\n\r\nignored\r\n--B\r\n" +
		"Content-Type: text/plain\r\n\r\nkept\r\n--B--\r\n")
	p, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.TextBody, "kept") {
		t.Errorf("good part lost: %q", p.TextBody)
	}
	if strings.Contains(p.TextBody, "ignored") {
		t.Errorf("bad part should be skipped: %q", p.TextBody)
	}
}

func TestReadEncoded_RawIfUnknown(t *testing.T) {
	r := strings.NewReader("hello")
	out, err := readEncoded(r, "7bit")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "hello" {
		t.Errorf("got %q, want hello", out)
	}
}

func TestReadEncoded_BadBase64(t *testing.T) {
	if _, err := readEncoded(strings.NewReader("not!!base64"), "base64"); err == nil {
		t.Error("expected base64 decode error")
	}
}
