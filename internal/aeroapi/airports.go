package aeroapi

import "github.com/dpage/flight-tracker/internal/airports"

// LookupIATA is a thin re-export of airports.Lookup, kept here so existing
// aeroapi code paths don't need to import a second package.
func LookupIATA(code string) (lat, lon float64, ok bool) {
	return airports.Lookup(code)
}
