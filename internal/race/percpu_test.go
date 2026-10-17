package race_test

import (
	"os"
	"path/filepath"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/race"
	"github.com/matjam/plutonium/internal/types2"
)

// checkKernel loads src as a main package built in kernel mode and runs the
// race analysis on it.
func checkKernel(t *testing.T, src string) error {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte("package main"+src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &load.Config{
		Tags:   load.BuildTags(true, true, nil),
		Stdlib: plutonium.Stdlib(),
		Sizes:  types2.SizesFor("gc", "amd64"),
	}
	prog, err := load.Load(cfg, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return race.Check(prog)
}
