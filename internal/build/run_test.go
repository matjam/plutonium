package build_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// compileAndRun builds src as a main package and runs it, returning its
// standard error (where print and println write) and exit status.
func compileAndRun(t *testing.T, src string, optLevel int) (string, int) {
	t.Helper()
	return compileAndRunFiles(t, map[string]string{"main.p": src}, optLevel)
}

// compileAndRunFiles is compileAndRun for a program of several files,
// given by their paths relative to the main package's directory, such as
// a module: a pluto.mod and the packages under it.
func compileAndRunFiles(t *testing.T, files map[string]string, optLevel int) (string, int) {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(dir, "prog")
	if compiler := os.Getenv("PLUTO_TEST_COMPILER"); compiler != "" {
		buildWith(t, compiler, dir, out, optLevel)
	} else {
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

// buildWith builds the main package in dir with the compiler binary named
// by PLUTO_TEST_COMPILER, such as the self-hosted compiler, in place of
// this package's Build, and the standard library under lib/.
func buildWith(t *testing.T, compiler, dir, out string, optLevel int) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "lib"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(compiler, "build", "-O"+strconv.Itoa(optLevel), "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PLUTOROOT="+root)
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, msg)
	}
}

func TestReturnStatus(t *testing.T) {
	testguard.Parallel(t)
	_, status := compileAndRun(t, "package main\n\nfunc main() int64 { return 42 }\n", 0)
	if status != 42 {
		t.Fatalf("exit status %d, want 42", status)
	}
}
