package emailingest

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeLLM struct {
	response   string
	err        error
	lastPrompt string
	lastDocs   []Document
}

func (f *fakeLLM) Complete(ctx context.Context, prompt string, docs []Document) (string, error) {
	f.lastPrompt = prompt
	f.lastDocs = docs
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
	legs, err := x.Extract(context.Background(), "body text here", nil)
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
	legs, err := x.Extract(context.Background(), "body", nil)
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
	legs, err := x.Extract(context.Background(), "body", nil)
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
	legs, err := x.Extract(context.Background(), "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(legs) != 0 {
		t.Errorf("legs = %+v, want empty (both out of 2y window)", legs)
	}
}

func TestExtract_BadJSON(t *testing.T) {
	x, _ := newExtractor("this is not json")
	_, err := x.Extract(context.Background(), "body", nil)
	if err == nil {
		t.Error("expected JSON error")
	}
}

func TestExtract_LLMError(t *testing.T) {
	x, l := newExtractor("")
	l.err = errors.New("boom")
	_, err := x.Extract(context.Background(), "body", nil)
	if err == nil {
		t.Error("expected LLM error to propagate")
	}
}

func TestExtract_StripsCodeFences(t *testing.T) {
	x, _ := newExtractor("```json\n{\"flights\":[{\"ident\":\"TK1980\",\"date\":\"2026-06-12\",\"confidence\":\"high\"}]}\n```")
	legs, err := x.Extract(context.Background(), "body", nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(legs) != 1 {
		t.Errorf("legs = %+v", legs)
	}
}

func TestExtract_PromptIncludesToday(t *testing.T) {
	x, l := newExtractor(`{"flights":[]}`)
	if _, err := x.Extract(context.Background(), "body", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(l.lastPrompt, "2026-05-22") {
		t.Errorf("prompt missing today's date: %q", l.lastPrompt)
	}
}

func TestExtract_PassesDocsThrough(t *testing.T) {
	x, l := newExtractor(`{"flights":[]}`)
	want := Document{Data: []byte("%PDF-1.4 content"), MediaType: "application/pdf", Filename: "ticket.pdf"}
	if _, err := x.Extract(context.Background(), "body", []Document{want}); err != nil {
		t.Fatal(err)
	}
	if len(l.lastDocs) != 1 {
		t.Fatalf("docs not forwarded: %+v", l.lastDocs)
	}
	got := l.lastDocs[0]
	if got.Filename != want.Filename {
		t.Errorf("filename = %q, want %q", got.Filename, want.Filename)
	}
	if got.MediaType != want.MediaType {
		t.Errorf("mediaType = %q, want %q", got.MediaType, want.MediaType)
	}
	if !bytes.Equal(got.Data, want.Data) {
		t.Errorf("data mismatch")
	}
}

func TestExtract_ManualFieldsParsed(t *testing.T) {
	x, _ := newExtractor(`{"flights":[{
		"ident":"TK1980","date":"2026-06-12","confidence":"high",
		"origin_iata":"ist","dest_iata":"LHR",
		"depart_time":"22:30","arrive_date":"2026-06-13","arrive_time":"01:15"
	}]}`)
	legs, err := x.Extract(context.Background(), "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(legs) != 1 {
		t.Fatalf("len(legs) = %d", len(legs))
	}
	g := legs[0]
	if g.OriginIATA != "IST" || g.DestIATA != "LHR" {
		t.Errorf("IATAs = %q/%q", g.OriginIATA, g.DestIATA)
	}
	if g.DepartTimeLocal != "22:30" || g.ArriveTimeLocal != "01:15" {
		t.Errorf("times = %q/%q", g.DepartTimeLocal, g.ArriveTimeLocal)
	}
	if g.ArriveDate != "2026-06-13" {
		t.Errorf("arrive_date = %q", g.ArriveDate)
	}
	if !g.HasManualDetails() {
		t.Errorf("HasManualDetails() = false, want true")
	}
}

func TestExtract_ManualFieldsAbsent(t *testing.T) {
	x, _ := newExtractor(`{"flights":[{"ident":"TK1980","date":"2026-06-12","confidence":"high"}]}`)
	legs, err := x.Extract(context.Background(), "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(legs) != 1 {
		t.Fatalf("len(legs) = %d", len(legs))
	}
	if legs[0].HasManualDetails() {
		t.Errorf("HasManualDetails() = true with empty extras")
	}
}

func TestExtract_DropsMalformedManualFields(t *testing.T) {
	// Garbled IATAs and times are silently dropped — the core ident/date
	// leg still comes through; the manual-fallback path simply won't fire.
	x, _ := newExtractor(`{"flights":[{
		"ident":"TK1980","date":"2026-06-12","confidence":"high",
		"origin_iata":"london","dest_iata":"JF","depart_time":"22h30","arrive_date":"13/06/2026","arrive_time":"too late"
	}]}`)
	legs, err := x.Extract(context.Background(), "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(legs) != 1 {
		t.Fatalf("len(legs) = %d", len(legs))
	}
	g := legs[0]
	if g.OriginIATA != "" || g.DestIATA != "" || g.DepartTimeLocal != "" ||
		g.ArriveTimeLocal != "" || g.ArriveDate != "" {
		t.Errorf("expected all manual fields empty, got %+v", g)
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
