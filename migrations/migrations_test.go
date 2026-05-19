package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedMigrations(t *testing.T) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var ups, downs int
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".sql") {
			continue
		}
		b, err := fs.ReadFile(FS, n)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", n, err)
		}
		if len(b) == 0 {
			t.Errorf("%s is empty", n)
		}
		switch {
		case strings.HasSuffix(n, ".up.sql"):
			ups++
		case strings.HasSuffix(n, ".down.sql"):
			downs++
		}
	}
	if ups == 0 {
		t.Error("expected at least one .up.sql migration")
	}
	if ups != downs {
		t.Errorf("mismatched migrations: %d up vs %d down", ups, downs)
	}
}
