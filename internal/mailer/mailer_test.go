package mailer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHTMLShellEscapesTitleAndSite(t *testing.T) {
	out := HTMLShell("Hello <world>", "<p>body</p>", "https://flights.example.com")
	if !strings.Contains(out, "Hello &lt;world&gt;") {
		t.Errorf("title not escaped: %s", out)
	}
	if !strings.Contains(out, "flights.example.com") {
		t.Errorf("host not rendered: %s", out)
	}
	if !strings.Contains(out, "<p>body</p>") {
		t.Errorf("body not embedded: %s", out)
	}
}

func TestHTMLShell_NonURLPublicURLFallsBackToString(t *testing.T) {
	// A "site" without a parseable host should still render — we fall back
	// to the full string for the footer label.
	out := HTMLShell("t", "b", "not a url")
	if !strings.Contains(out, "not a url") {
		t.Errorf("expected footer to contain the literal site string: %s", out)
	}
}

func TestHTMLEscape(t *testing.T) {
	if got := HTMLEscape(`<a "x">&'`); got != "&lt;a &#34;x&#34;&gt;&amp;&#39;" {
		t.Errorf("HTMLEscape = %q", got)
	}
}

func TestMultipartBody_HasBothPartsAndBoundary(t *testing.T) {
	ct, body := MultipartBody("hi\r\n", "<p>hi</p>")
	if !strings.HasPrefix(ct, "multipart/alternative; boundary=\"ae-") {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.Contains(body, "Content-Type: text/plain") {
		t.Errorf("missing text/plain part: %s", body)
	}
	if !strings.Contains(body, "Content-Type: text/html") {
		t.Errorf("missing text/html part: %s", body)
	}
	if !strings.Contains(body, "Content-Transfer-Encoding: quoted-printable") {
		t.Errorf("html part should be quoted-printable: %s", body)
	}
}

func TestMultipartBody_AddsTrailingCRLFToPlain(t *testing.T) {
	// Caller passes plain text without trailing CRLF — the assembler must
	// add one so the next boundary isn't smashed against the body.
	_, body := MultipartBody("no trailing newline", "<p>x</p>")
	if !strings.Contains(body, "no trailing newline\r\n") {
		t.Errorf("expected synthetic CRLF after plain body: %q", body)
	}
}

func TestQuotedPrintable(t *testing.T) {
	out := QuotedPrintable("hello = world")
	if !strings.Contains(out, "=3D") {
		t.Errorf("'=' should be QP-encoded as =3D, got %q", out)
	}
}

func TestSend_NoPath(t *testing.T) {
	if err := Send(context.Background(), "", "", "msg"); err == nil {
		t.Error("expected error when sendmail path unset")
	}
}

func TestSend_BinaryMissing(t *testing.T) {
	if err := Send(context.Background(), "/no/such/sendmail", "x@y", "msg"); err == nil {
		t.Error("expected error when sendmail binary doesn't exist")
	}
}

func TestSend_PassesMessageToBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sendmail pipe is POSIX-shaped; skip on Windows")
	}
	// Drop a stub script onto disk that drains stdin and exits 0. This
	// exercises the build / start / write-stdin / close / wait path
	// end-to-end without relying on coreutils behaviour for sendmail's
	// -t / -f flags (which `cat` mis-interprets as filenames).
	dir := t.TempDir()
	stub := filepath.Join(dir, "sendmail.sh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ncat >/dev/null\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	if err := Send(context.Background(), stub, "x@y", "msg"); err != nil {
		t.Errorf("Send(stub) = %v, want nil", err)
	}
}
