package build_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLib compiles and runs every program in testdata/lib, which exercise
// the standard library, and compares the output with the .out file next to
// it. These programs import pluto/... packages, so Go cannot run them as
// they are; their expected output was checked against Go running the same
// code with the imports pointed at Go's packages (or at shims for
// Plutonium-only APIs), or by hand where no Go equivalent exists.
func TestLib(t *testing.T) {
	runGoldenDir(t, "lib")
}

// TestPlutonium compiles and runs every program in testdata/plutonium,
// which exercise language features Go does not have (enums, type
// constructor expressions). Go cannot run them, so their .out files were
// checked by hand.
func TestPlutonium(t *testing.T) {
	runGoldenDir(t, "plutonium")
}

// runGoldenDir builds and runs each testdata/<dir>/*.p at -O0 and -O2 and
// compares its output with the .out file next to it.
func runGoldenDir(t *testing.T, dir string) {
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", dir, "*.p"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no programs in testdata/%s", dir)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".p")
		want, err := os.ReadFile(strings.TrimSuffix(file, ".p") + ".out")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, level := range []int{0, 2} {
			t.Run(name+"/O"+string(rune('0'+level)), func(t *testing.T) {
				got, status := compileAndRun(t, string(src), level)
				if status != 0 {
					t.Errorf("exit status %d", status)
				}
				if got != string(want) {
					t.Errorf("output differs\n--- got ---\n%s\n--- want ---\n%s", got, want)
				}
			})
		}
	}
}
