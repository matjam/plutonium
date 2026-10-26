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

// buildFiles writes files to a temporary directory, builds it with cfg's
// settings and runs it. It returns the directory, the program's combined
// output and its status.
func buildFiles(t *testing.T, files map[string][]byte, cfg build.Config) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Dir = dir
	cfg.Output = filepath.Join(dir, "prog")
	cfg.Stdlib = plutonium.Stdlib()
	if err := buildCfg(&cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	cmd := exec.Command(cfg.Output)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	status := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		status = exitErr.ExitCode()
		if status < 0 {
			t.Fatalf("program died: %v\noutput: %s", err, out.String())
		}
	} else if err != nil {
		t.Fatal(err)
	}
	return dir, out.String(), status
}
