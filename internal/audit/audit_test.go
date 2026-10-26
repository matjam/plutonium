package audit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

func sourceProgram(t *testing.T, src string) *load.Program {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := filepath.Abs("../../lib")
	if err != nil {
		t.Fatal(err)
	}
	p, err := load.LoadPackage(&load.Config{Stdlib: os.DirFS(lib), Tags: load.BuildTags(false, false, nil), Sizes: types2.SizesFor("gc", "amd64")}, dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("closed report") }

// TestAuditFailures checks that Write returns the writer's error. The
// reports themselves are checked through pluto vet -unsafe in tests.
func TestAuditFailures(t *testing.T) {
	t.Parallel()
	p := sourceProgram(t, "package p\n//pu:nochecks\nfunc f() {}\n")
	if err := Write(failedWriter{}, p); err == nil || err.Error() != "closed report" {
		t.Fatalf("writer failure: %v", err)
	}
}
