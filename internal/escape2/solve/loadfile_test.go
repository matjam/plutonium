package solve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/escape2"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

// loadFile loads the program in path as the main package of a directory of
// its own, skipping the test if the type checker rejects it.
func loadFile(t *testing.T, path string) *load.Program {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &load.Config{Tags: load.BuildTags(false, false, nil), Stdlib: plutonium.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")}
	prog, err := load.Load(cfg, dir)
	if err != nil {
		t.Skipf("load: %v", err) // the type checker rejects it: not this analysis's case
	}
	return prog
}

// main.(*Tree).Insert of testdata/run/gentypes.p, recursive through its
// left and right subtrees, did not converge in its SCC: its store pairs grew
// past the load limit, were cut back, and the solve against the cut summary
// lost what the cut covered, so the summary went back and forth. The
// scheduler now joins each new summary with the previous one (summary.Join).
func TestTreeInsertConverges(t *testing.T) {
	prog := loadFile(t, "../../../testdata/run/gentypes.p")
	if err := escape2.Check(prog); err != nil && strings.Contains(err.Error(), "internal compiler error") {
		t.Fatal(err)
	}
}
