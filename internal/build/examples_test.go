package build_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestExamples builds and runs every program in examples/ and checks its
// output against examples/<name>/expected (the exact output) and its exit
// status against a first line of the form "exit N" in that file, if any.
func TestExamples(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("..", "..", "examples", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		src, err := os.ReadFile(filepath.Join(dir, "main.p"))
		if err != nil {
			continue
		}
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join(dir, "expected"))
			if err != nil {
				t.Fatalf("missing expected output: %v", err)
			}
			wantStatus := 0
			wantOut := string(want)
			if first, rest, ok := strings.Cut(wantOut, "\n"); ok && strings.HasPrefix(first, "exit ") {
				wantStatus, _ = strconv.Atoi(strings.TrimPrefix(first, "exit "))
				wantOut = rest
			}
			got, status := compileAndRun(t, string(src), 2)
			if status != wantStatus {
				t.Errorf("exit status %d, want %d", status, wantStatus)
			}
			if got != wantOut {
				t.Errorf("output differs\n--- got ---\n%s--- want ---\n%s", got, wantOut)
			}

			// With the debug allocator as the default allocator the
			// output is the same: the example misuses no memory.
			got, status = runDebugAlloc(t, buildDebugAlloc(t, string(src), 2), "leaks=off")
			if status != wantStatus {
				t.Errorf("--alloc=debug: exit status %d, want %d", status, wantStatus)
			}
			if got != wantOut {
				t.Errorf("--alloc=debug: output differs\n--- got ---\n%s--- want ---\n%s", got, wantOut)
			}
		})
	}
}
