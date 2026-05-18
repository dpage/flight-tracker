// Package migrations exposes the embedded .sql migration files.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
