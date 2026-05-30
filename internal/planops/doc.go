// Package planops holds the shared business logic for capturing and committing
// plans, the generalized successor to internal/flightops. Both the HTTP ingest
// endpoints and the email-ingest Service call into it, so all capture methods
// (manual / paste / upload / email) converge on one code path.
//
// This file is the Wave 0a placeholder so later waves can add their files
// without touching a shared one:
//
//   - Propose / Commit (Wave 2A — ingestion + rebooking match)
//   - hoteltimes.go    (Wave 1E — smart hotel check-in/out times)
package planops
