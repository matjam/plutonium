package build_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

// buildDir builds the main package in dir at optLevel and returns the
// executable's path.
func buildDir(t *testing.T, dir string, optLevel int) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "prog")
	cfg := &build.Config{Dir: dir, Output: out, Stdlib: plutonium.Stdlib(), OptLevel: optLevel}
	if err := build.Build(cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	return out
}

// runProg runs an executable and returns its combined output and status.
func runProg(t *testing.T, path string) (string, int) {
	t.Helper()
	cmd := exec.Command(path)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		if exitErr.ExitCode() < 0 {
			t.Fatalf("program died: %v\n%s", err, out.String())
		}
		return out.String(), exitErr.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	return out.String(), 0
}

func needCC(t *testing.T) {
	t.Helper()
	cc := strings.Fields(os.Getenv("CC"))
	if len(cc) == 0 {
		cc = []string{"cc"}
	}
	if _, err := exec.LookPath(cc[0]); err != nil {
		t.Skip("no C compiler")
	}
}

// TestCABI links testdata/cabi/main.p with cabi.c, compiled by the system C
// compiler, and checks calls in both directions under System V: scalars of
// every width, floats, structs of each class as arguments and results,
// memory-class structs, register exhaustion, several results, func values of
// C functions and C callbacks into //pu:export functions.
func TestCABI(t *testing.T) {
	needCC(t)
	dir := filepath.Join("..", "..", "testdata", "cabi")
	want, err := os.ReadFile(filepath.Join(dir, "expected"))
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 2} {
		t.Run("O"+string(rune('0'+level)), func(t *testing.T) {
			got, status := runProg(t, buildDir(t, dir, level))
			if status != 0 {
				t.Errorf("exit status %d", status)
			}
			if got != string(want) {
				t.Errorf("output differs\n--- got ---\n%s--- want ---\n%s", got, want)
			}
		})
	}
}

// TestObjectFiles links a package's .o file as it is.
func TestObjectFiles(t *testing.T) {
	needCC(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "add.c")
	if err := os.WriteFile(src, []byte("long add3(long a, long b, long c) { return a + b * 10 + c * 20; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cc", "-c", "-O2", "-fno-stack-protector", "-o", filepath.Join(dir, "add_amd64.o"), src).CombinedOutput(); err != nil {
		t.Fatalf("cc: %v\n%s", err, out)
	}
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	// A file-name tag that does not match leaves the object out.
	if err := os.WriteFile(filepath.Join(dir, "unused_arm64.o"), []byte("not an object"), 0o644); err != nil {
		t.Fatal(err)
	}
	prog := "package main\n\n//pu:extern add3\nfunc add3(a, b, c int) int\n\nfunc main() int64 { return int64(add3(1, 2, 3)) }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, status := runProg(t, buildDir(t, dir, 2)); status != 81 {
		t.Errorf("exit status %d, want 81", status)
	}
}
