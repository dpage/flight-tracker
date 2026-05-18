// Package web exposes the Vite-built SPA as an embedded filesystem so the Go
// binary can serve it without any extra files on disk.
//
// The dist/ directory is populated by `npm run build`; in a fresh checkout
// before the first build the embed pattern still matches the placeholder
// .keep file below so the binary compiles.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the SPA assets rooted at the dist/ directory.
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
