package build_test

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "regenerate testdata/run/*.out by running each program with Go")

// TestRun compiles and runs every program in testdata/run and compares its
// output with the .out file next to it. The programs are written in the
// common subset of Go and Plutonium, so -update produces the expected output
// by running them with the Go toolchain.
func TestRun(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "run", "*.p"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".p")
		golden := strings.TrimSuffix(file, ".p") + ".out"
		if *update {
			out, err := runWithGo(t, file)
			if err != nil {
				t.Fatalf("%s: go run: %v\n%s", name, err, out)
			}
			if err := os.WriteFile(golden, []byte(out), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Errorf("%s: %v (run with -update to create it)", name, err)
			continue
		}
		for _, level := range []int{0, 2} {
			t.Run(name+"/O"+string(rune('0'+level)), func(t *testing.T) {
				src, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
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

// runWithGo runs a test program with the Go toolchain and returns what it
// writes to standard error (println's destination).
func runWithGo(t *testing.T, file string) (string, error) {
	dir := t.TempDir()
	src, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module prog\n\ngo 1.27\n"), 0o644); err != nil {
		return "", err
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
