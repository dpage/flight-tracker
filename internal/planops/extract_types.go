package planops

import "context"

// Document is one binary attachment forwarded to the LLM alongside the prompt
// (typically a PDF ticket). It mirrors emailingest.Document; planops owns its
// own copy so the capture path has no dependency cycle with the email service
// (emailingest depends on planops, never the reverse).
type Document struct {
	Data      []byte
	MediaType string
	Filename  string
}

// FlightFields are the resolver/manual-fallback inputs for a flight part —
// the same surface the email flights path uses.
type FlightFields struct {
	Ident           string
	Date            string // YYYY-MM-DD (departure)
	OriginIATA      string
	DestIATA        string
	DepartTimeLocal string // HH:MM
	ArriveDate      string // YYYY-MM-DD
	ArriveTimeLocal string // HH:MM
}

// HasManualDetails reports whether every field needed to insert the flight
// without provider data is present.
func (f FlightFields) HasManualDetails() bool {
	return f.OriginIATA != "" && f.DestIATA != "" &&
		f.DepartTimeLocal != "" && f.ArriveDate != "" && f.ArriveTimeLocal != ""
}

// ExtractedPart is one timeline entry the extractor pulled from an
// email/paste/upload, of any plan type. Type selects which fields carry
// meaning. StartDate/EndDate are YYYY-MM-DD local; StartTime/EndTime are HH:MM
// 24h local. Confidence is "high"|"medium"|"low".
type ExtractedPart struct {
	Type       string // flight|train|hotel|ground|dining|excursion
	Confidence string

	StartDate  string
	StartTime  string
	EndDate    string
	EndTime    string
	StartLabel string
	EndLabel   string

	Flight FlightFields // Type=="flight"

	// Hotel (Type=="hotel"). StartDate/EndDate are check-in/out days.
	HotelName string
	Address   string
	Phone     string
	RoomType  string

	// Train (Type=="train").
	Operator  string
	ServiceNo string
	Class     string

	// Ground (Type=="ground").
	Provider string
	Vehicle  string

	// Dining (Type=="dining").
	ReservationName string

	// Excursion (Type=="excursion").
	ExcursionTitle string
}

// ExtractedPlan groups the parts of one booking into a single plan (PRD §6.3:
// one round-trip email → one plan with several parts).
type ExtractedPlan struct {
	Type            string
	Title           string
	ConfirmationRef string
	Parts           []ExtractedPart
}

// Extractor is the LLM seam Propose calls — implemented by
// *emailingest.Extractor. Narrowed to an interface so planops doesn't pull the
// whole ingest service in and so tests can stub it.
type Extractor interface {
	ExtractPlans(ctx context.Context, body string, docs []Document) ([]ExtractedPlan, error)
}
