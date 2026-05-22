package emailingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractPDFText_HasText(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "sample.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExtractPDFText(b)
	if err != nil {
		t.Fatalf("ExtractPDFText: %v", err)
	}
	// Sample PDF (from ledongthuc/pdf/examples) contains an exact phrase.
	if !strings.Contains(got, "This is a heading") {
		t.Errorf("text does not contain expected token, got %q", got)
	}
}

func TestExtractPDFText_Invalid(t *testing.T) {
	_, err := ExtractPDFText([]byte("not a pdf"))
	if err == nil {
		t.Error("expected error for non-PDF input")
	}
}

func TestExtractPDFText_Empty(t *testing.T) {
	_, err := ExtractPDFText([]byte{})
	if err == nil {
		t.Error("expected error for empty input")
	}
}
