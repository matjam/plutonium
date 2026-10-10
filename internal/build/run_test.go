package build_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

// compileAndRun builds src as a main package and runs it, returning its
// standard error (where print and println write) and exit status.
func compileAndRun(t *testing.T, src string, optLevel int) (string, int) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "prog")
	var listing bytes.Buffer
	cfg := &build.Config{Dir: dir, Output: out, Stdlib: plutonium.Stdlib(), OptLevel: optLevel}
	if os.Getenv("PLUTO_LISTING") != "" {
		cfg.Listing = &listing
	}
	if err := build.Build(cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	if listing.Len() > 0 {
		t.Logf("listing:\n%s", listing.String())
	}
	cmd := exec.Command(out)
	var stderr, stdout bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	err := cmd.Run()
	status := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		status = exitErr.ExitCode()
		if status < 0 {
			t.Fatalf("program died: %v\nstderr: %s", err, stderr.String())
		}
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String() + stderr.String(), status
}

func TestReturnStatus(t *testing.T) {
	_, status := compileAndRun(t, "package main\n\nfunc main() int64 { return 42 }\n", 0)
	if status != 42 {
		t.Fatalf("exit status %d, want 42", status)
	}
}
