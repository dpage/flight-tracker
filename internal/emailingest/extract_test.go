package emailingest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeLLM struct {
	response string
	err      error
	lastPrompt string
}

func (f *fakeLLM) Complete(ctx context.Context, prompt string) (string, error) {
	f.lastPrompt = prompt
	return f.response, f.err
}

// fixedNow returns 2026-05-22 — used to make the 2-year sanity-window
// deterministic across test runs.
func fixedNow() time.Time { return time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC) }

func newExtractor(resp string) (*Extractor, *fakeLLM) {
	l := &fakeLLM{response: resp}
	x := NewExtractor(l, "test")
	x.Now = fixedNow
	return x, l
}

func TestExtract_Valid(t *testing.T) {
	x, _ := newExtractor(`{"flights":[{"ident":"TK1980","date":"2026-06-12","confidence":"high"}],"notes":""}`)
	legs, err := x.Extract(context.Background(), "body text here")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(legs) != 1 {
		t.Fatalf("len(legs) = %d, want 1", len(legs))
	}
	if legs[0].Ident != "TK1980" || legs[0].Date != "2026-06-12" {
		t.Errorf("leg = %+v", legs[0])
	}
}

func TestExtract_DropsLowConfidence(t *testing.T) {
	x, _ := newExtractor(`{"flights":[
		{"ident":"TK1980","date":"2026-06-12","confidence":"high"},
		{"ident":"XX9","date":"2026-06-13","confidence":"low"}
	]}`)
	legs, err := x.Extract(context.Background(), "body")
	if err != nil {
		t.Fatal(err)
	}
	if len(legs) != 1 || legs[0].Ident != "TK1980" {
		t.Errorf("legs = %+v", legs)
	}
}

func TestExtract_DropsRegexFailures(t *testing.T) {
	x, _ := newExtractor(`{"flights":[
		{"ident":"not-an-ident","date":"2026-06-12","confidence":"high"},
		{"ident":"TK1980","date":"06/12/2026","confidence":"high"}
	]}`)
	legs, err := x.Extract(context.Background(), "body")
	if err != nil {
		t.Fatal(err)
	}
	if len(legs) != 0 {
		t.Errorf("legs = %+v, want empty", legs)
	}
}

func TestExtract_DropsOutOfWindowDates(t *testing.T) {
	x, _ := newExtractor(`{"flights":[
		{"ident":"TK1980","date":"2020-01-01","confidence":"high"},
		{"ident":"TK1981","date":"2099-01-01","confidence":"high"}
	]}`)
	legs, err := x.Extract(context.Background(), "body")
	if err != nil {
		t.Fatal(err)
	}
	if len(legs) != 0 {
		t.Errorf("legs = %+v, want empty (both out of 2y window)", legs)
	}
}

func TestExtract_BadJSON(t *testing.T) {
	x, _ := newExtractor("this is not json")
	_, err := x.Extract(context.Background(), "body")
	if err == nil {
		t.Error("expected JSON error")
	}
}

func TestExtract_LLMError(t *testing.T) {
	x, l := newExtractor("")
	l.err = errors.New("boom")
	_, err := x.Extract(context.Background(), "body")
	if err == nil {
		t.Error("expected LLM error to propagate")
	}
}

func TestExtract_StripsCodeFences(t *testing.T) {
	x, _ := newExtractor("```json\n{\"flights\":[{\"ident\":\"TK1980\",\"date\":\"2026-06-12\",\"confidence\":\"high\"}]}\n```")
	legs, err := x.Extract(context.Background(), "body")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(legs) != 1 {
		t.Errorf("legs = %+v", legs)
	}
}

func TestExtract_PromptIncludesToday(t *testing.T) {
	x, l := newExtractor(`{"flights":[]}`)
	if _, err := x.Extract(context.Background(), "body"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(l.lastPrompt, "2026-05-22") {
		t.Errorf("prompt missing today's date: %q", l.lastPrompt)
	}
}

func TestStripCodeFence(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "hello"},
		{"```\nhello\n```", "hello"},
		{"```json\n{\"x\":1}\n```", `{"x":1}`},
		{"```", ""},
	}
	for _, c := range cases {
		if got := stripCodeFence(c.in); got != c.want {
			t.Errorf("stripCodeFence(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
