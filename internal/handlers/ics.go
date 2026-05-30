package handlers

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// Hand-rolled RFC 5545 (iCalendar) renderer. No external deps — the format is
// small and well specified. We emit one VCALENDAR with one VEVENT per
// plan_part, each anchored to a VTIMEZONE for the part's IANA zone so clients
// show the correct local wall-clock time.
//
// VTIMEZONE strategy: rather than reproduce a full historical/future DST rule
// set (which we don't have without a tz-rule library), each referenced zone
// gets a VTIMEZONE whose STANDARD components capture the actual UTC offset at
// every distinct instant a feed event uses it. RFC 5545 §3.6.5 permits
// multiple observances; a client resolves an event's TZID+local-time against
// the observance whose DTSTART it falls on or after. By seeding an observance
// at (just before) each event time with that instant's real offset, the local
// time renders correctly across DST boundaries without us shipping rrules.

const icsProdID = "-//Aerly//Trip Planner//EN"

// renderICS produces the full VCALENDAR text for the given events. calName is
// the X-WR-CALNAME shown by many clients.
func renderICS(calName string, events []*store.CalendarEvent) string {
	var b strings.Builder
	writeLine(&b, "BEGIN:VCALENDAR")
	writeLine(&b, "VERSION:2.0")
	writeLine(&b, "PRODID:"+icsProdID)
	writeLine(&b, "CALSCALE:GREGORIAN")
	writeLine(&b, "METHOD:PUBLISH")
	if calName != "" {
		writeLine(&b, "X-WR-CALNAME:"+escapeText(calName))
	}

	// Collect, per IANA zone, the set of instants that need a defined offset.
	type tzUse struct {
		loc      *time.Location
		instants []time.Time
	}
	zones := map[string]*tzUse{}
	noteZone := func(tzName string, t time.Time) {
		if tzName == "" {
			return
		}
		loc, err := time.LoadLocation(tzName)
		if err != nil {
			return
		}
		z := zones[tzName]
		if z == nil {
			z = &tzUse{loc: loc}
			zones[tzName] = z
		}
		z.instants = append(z.instants, t)
	}
	for _, e := range events {
		if e.StartTZ != "" {
			noteZone(e.StartTZ, e.StartsAt)
		}
		if e.EndsAt != nil && e.EndTZ != "" {
			noteZone(e.EndTZ, *e.EndsAt)
		}
	}

	// Emit a VTIMEZONE per referenced zone, ordered for stable output.
	tzNames := make([]string, 0, len(zones))
	for name := range zones {
		tzNames = append(tzNames, name)
	}
	sort.Strings(tzNames)
	for _, name := range tzNames {
		writeVTimezone(&b, name, zones[name].loc, zones[name].instants)
	}

	for _, e := range events {
		writeVEvent(&b, e)
	}
	writeLine(&b, "END:VCALENDAR")
	return b.String()
}

func writeVTimezone(b *strings.Builder, tzName string, loc *time.Location, instants []time.Time) {
	// One STANDARD observance per distinct offset we observe, with DTSTART at
	// the earliest instant carrying that offset. This keeps the block compact
	// while still giving the client the right offset for each event time.
	type obs struct {
		offset int       // seconds east of UTC
		abbr   string    // zone abbreviation, e.g. CET/CEST
		at     time.Time // earliest instant observed at this offset
	}
	seen := map[int]*obs{}
	for _, t := range instants {
		local := t.In(loc)
		abbr, off := local.Zone()
		o := seen[off]
		if o == nil {
			seen[off] = &obs{offset: off, abbr: abbr, at: t}
		} else if t.Before(o.at) {
			o.at = t
		}
	}
	offs := make([]int, 0, len(seen))
	for off := range seen {
		offs = append(offs, off)
	}
	sort.Ints(offs)

	writeLine(b, "BEGIN:VTIMEZONE")
	writeLine(b, "TZID:"+tzName)
	for _, off := range offs {
		o := seen[off]
		// DTSTART is the local wall-clock time at the start of this observance.
		start := o.at.In(loc)
		writeLine(b, "BEGIN:STANDARD")
		writeLine(b, "DTSTART:"+start.Format("20060102T150405"))
		writeLine(b, "TZOFFSETFROM:"+formatTZOffset(o.offset))
		writeLine(b, "TZOFFSETTO:"+formatTZOffset(o.offset))
		abbr := o.abbr
		if abbr == "" {
			abbr = tzName
		}
		writeLine(b, "TZNAME:"+abbr)
		writeLine(b, "END:STANDARD")
	}
	writeLine(b, "END:VTIMEZONE")
}

func writeVEvent(b *strings.Builder, e *store.CalendarEvent) {
	writeLine(b, "BEGIN:VEVENT")
	writeLine(b, fmt.Sprintf("UID:plan-part-%d@aerly", e.PartID))
	// DTSTAMP/LAST-MODIFIED let clients detect updates (a delayed flight whose
	// part times moved re-renders on next refresh).
	writeLine(b, "DTSTAMP:"+e.UpdatedAt.UTC().Format("20060102T150405Z"))
	writeLine(b, "LAST-MODIFIED:"+e.UpdatedAt.UTC().Format("20060102T150405Z"))

	writeLine(b, dtLine("DTSTART", e.StartsAt, e.StartTZ))
	if e.EndsAt != nil {
		endTZ := e.EndTZ
		if endTZ == "" {
			endTZ = e.StartTZ
		}
		writeLine(b, dtLine("DTEND", *e.EndsAt, endTZ))
	}

	writeLine(b, "SUMMARY:"+escapeText(summaryFor(e)))
	if e.StartLabel != "" {
		writeLine(b, "LOCATION:"+escapeText(e.StartLabel))
	}
	if desc := descriptionFor(e); desc != "" {
		writeLine(b, "DESCRIPTION:"+escapeText(desc))
	}
	if e.Status == "cancelled" {
		writeLine(b, "STATUS:CANCELLED")
	} else if e.Status == "confirmed" {
		writeLine(b, "STATUS:CONFIRMED")
	} else {
		writeLine(b, "STATUS:TENTATIVE")
	}
	writeLine(b, "END:VEVENT")
}

// dtLine formats a DTSTART/DTEND property. When the zone is known we emit a
// floating local time with a TZID parameter referencing the matching
// VTIMEZONE; otherwise we fall back to UTC ("Z") so the instant is still
// unambiguous.
func dtLine(prop string, t time.Time, tzName string) string {
	if tzName != "" {
		if loc, err := time.LoadLocation(tzName); err == nil {
			return fmt.Sprintf("%s;TZID=%s:%s", prop, tzName, t.In(loc).Format("20060102T150405"))
		}
	}
	return prop + ":" + t.UTC().Format("20060102T150405Z")
}

func summaryFor(e *store.CalendarEvent) string {
	title := strings.TrimSpace(e.Title)
	typ := titleCaseType(e.Type)
	if title == "" {
		return typ
	}
	return fmt.Sprintf("%s (%s)", title, typ)
}

func descriptionFor(e *store.CalendarEvent) string {
	var parts []string
	if ref := strings.TrimSpace(e.ConfirmationRef); ref != "" {
		parts = append(parts, "Confirmation: "+ref)
	}
	if notes := strings.TrimSpace(e.Notes); notes != "" {
		parts = append(parts, notes)
	}
	return strings.Join(parts, "\n")
}

func titleCaseType(t string) string {
	if t == "" {
		return "Plan"
	}
	return strings.ToUpper(t[:1]) + t[1:]
}

// formatTZOffset renders seconds-east-of-UTC as the RFC 5545 ±HHMM(SS) form.
func formatTZOffset(secs int) string {
	sign := "+"
	if secs < 0 {
		sign = "-"
		secs = -secs
	}
	h := secs / 3600
	m := (secs % 3600) / 60
	s := secs % 60
	if s != 0 {
		return fmt.Sprintf("%s%02d%02d%02d", sign, h, m, s)
	}
	return fmt.Sprintf("%s%02d%02d", sign, h, m)
}

// escapeText escapes a value per RFC 5545 §3.3.11 (TEXT): backslash, semicolon,
// comma, and newline.
func escapeText(s string) string {
	r := strings.NewReplacer(
		"\\", "\\\\",
		";", "\\;",
		",", "\\,",
		"\r\n", "\\n",
		"\n", "\\n",
		"\r", "\\n",
	)
	return r.Replace(s)
}

// writeLine writes one content line, folding it at 75 octets per RFC 5545
// §3.1, and terminating with CRLF. Folding is byte-based with a leading space
// on continuation lines; we avoid splitting a multi-byte UTF-8 rune.
func writeLine(b *strings.Builder, line string) {
	const max = 75
	if len(line) <= max {
		b.WriteString(line)
		b.WriteString("\r\n")
		return
	}
	// First chunk up to 75 octets, subsequent chunks up to 74 (the leading
	// space counts toward the octet budget).
	i := 0
	limit := max
	for i < len(line) {
		end := i + limit
		if end > len(line) {
			end = len(line)
		} else {
			// Back off so we don't split a UTF-8 continuation byte.
			for end > i && (line[end]&0xC0) == 0x80 {
				end--
			}
		}
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(line[i:end])
		b.WriteString("\r\n")
		i = end
		limit = max - 1
	}
}
