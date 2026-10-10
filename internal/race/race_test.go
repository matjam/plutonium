package race_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/race"
	"github.com/matjam/plutonium/internal/types2"
)

// check loads src as a main package and runs the race analysis on it.
func check(t *testing.T, src string) error {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &load.Config{
		Tags:   map[string]bool{"amd64": true, "linux": true, "hosted": true},
		Stdlib: plutonium.Stdlib(),
		Sizes:  types2.SizesFor("gc", "amd64"),
	}
	prog, err := load.Load(cfg, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return race.Check(prog)
}

// fullText returns the diagnostics in err as pluto prints them, with the
// temporary directory cut from positions.
func fullText(err error) string {
	var b strings.Builder
	diag.Print(&b, err, false)
	return regexp.MustCompile(`/\S*/main\.p`).ReplaceAllString(b.String(), "main.p")
}
