package build_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// TestBuildTest builds the test binaries of standard library packages with
// _test.p files at each optimisation level and runs them, so that
// pluto/testing is also compiled under PLUTO_SSACHECK. cmd/pluto tests the
// command's output.
func TestBuildTest(t *testing.T) {
	testguard.Parallel(t)
	for _, pkg := range []string{"pluto/strings", "pluto/sort", "pluto/path/filepath", "pluto/flag"} {
		for _, level := range []int{0, 2} {
			t.Run(pkg+"/O"+string(rune('0'+level)), func(t *testing.T) {
				testguard.Parallel(t)
				out := filepath.Join(t.TempDir(), "test")
				cfg := &build.Config{Output: out, Stdlib: plutonium.Stdlib(), OptLevel: level}
				bin, err := build.BuildTest(cfg, pkg, &build.TestOptions{Verbose: true})
				if err != nil {
					t.Fatalf("build: %v", err)
				}
				if bin.Path != pkg || len(bin.Tests) == 0 {
					t.Fatalf("binary for %q with tests %v", bin.Path, bin.Tests)
				}
				got, err := exec.Command(out).CombinedOutput()
				if err != nil {
					t.Fatalf("%v\n%s", err, got)
				}
				for _, name := range bin.Tests {
					if !strings.Contains(string(got), "--- PASS: "+name+" (") {
						t.Errorf("no PASS line for %s in:\n%s", name, got)
					}
				}
				if !strings.HasSuffix(string(got), "\nPASS\n") {
					t.Errorf("output does not end in PASS:\n%s", got)
				}
			})
		}
	}
}

// fatalTest fails a test with Fatalf, which must leave the test function at
// once and let the next test run, and keeps values that live across the
// jump back to the runner in registers and on the stack.
const fatalTest = `package p

import "pluto/testing"

func sum(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += i
	}
	return s
}

func TestFatal(t *testing.T) {
	a, b, c := sum(3), sum(4), sum(5)
	t.Run("inner", func(t *testing.T) {
		t.Fatalf("stop %d %d %d", a, b, c)
		t.Error("not reached")
	})
	t.Logf("after %d", a+b+c)
	t.FailNow()
	t.Error("not reached")
}

func TestNext(t *testing.T) {
	t.Log("next ran")
}
`

func TestBuildTestFailNow(t *testing.T) {
	testguard.Parallel(t)
	dir := t.TempDir()
	files := map[string]string{"pluto.mod": "module example.com/p\n", "p.p": "package p\n", "p_test.p": fatalTest}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want := `=== RUN   TestFatal
=== RUN   TestFatal/inner
    stop 3 6 10
--- FAIL: TestFatal/inner (T)
    after 19
--- FAIL: TestFatal (T)
=== RUN   TestNext
    next ran
--- PASS: TestNext (T)
FAIL
`
	for _, level := range []int{0, 2} {
		out := filepath.Join(dir, "test")
		cfg := &build.Config{Output: out, Stdlib: plutonium.Stdlib(), OptLevel: level}
		if _, err := build.BuildTest(cfg, dir, &build.TestOptions{Verbose: true}); err != nil {
			t.Fatalf("build: %v", err)
		}
		got, err := exec.Command(out).CombinedOutput()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Errorf("-O%d: exit %v, want status 1", level, err)
		}
		if s := regexp.MustCompile(`\(\d+\.\d+s\)`).ReplaceAllString(string(got), "(T)"); s != want {
			t.Errorf("-O%d output:\n%s\nwant:\n%s", level, s, want)
		}
	}
}

func TestBuildTestNoTestFiles(t *testing.T) {
	testguard.Parallel(t)
	cfg := &build.Config{Output: filepath.Join(t.TempDir(), "test"), Stdlib: plutonium.Stdlib()}
	bin, err := build.BuildTest(cfg, "pluto/bits", &build.TestOptions{})
	if !errors.Is(err, build.ErrNoTestFiles) || bin == nil || bin.Path != "pluto/bits" {
		t.Errorf("BuildTest(pluto/bits) = %v, %v; want ErrNoTestFiles", bin, err)
	}
}
