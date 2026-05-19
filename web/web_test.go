package web

import (
	"io/fs"
	"testing"
)

func TestFS(t *testing.T) {
	sub, err := FS()
	if err != nil {
		t.Fatalf("FS(): %v", err)
	}
	if sub == nil {
		t.Fatal("FS() returned a nil filesystem")
	}
	// The dist directory always contains at least the .keep placeholder
	// (before a build) or the built SPA assets (after `npm run build`).
	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		t.Fatalf("ReadDir dist: %v", err)
	}
	if len(entries) == 0 {
		t.Error("embedded dist filesystem is empty")
	}
}
