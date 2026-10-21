package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestLower runs the lowerdemo demo, built from the Go front end (the loader,
// the escape, move and race checks, and the lowerer) and from its translation
// (selfhost/internal/...), over the programs of the repository: the examples,
// the programs of testdata and the diagnostics goldens. For each, at -O0 and
// -O2, the two builds must print the same text: the errors the checks and
// lowering report, or the SSA program (as internal/ssawrite writes it after
// lowering, and every function after the optimiser's pipeline).
//
// Each program takes the translated front end minutes (its checks run over
// the whole program, the library included), so the test takes a sample: the
// examples and every LOWER_STEP'th (default 12) of the other programs;
// LOWER_STEP=1 runs all. It runs only with GO2PU_SLOW=1, and needs
// PLUTO_TEST_RSS_MB=22000: building the translated front end takes 15 GB.
// LOWER_ONLY=<substring> runs the directories whose names contain it.
func TestLower(t *testing.T) {
	testguard.Parallel(t)
	if os.Getenv("GO2PU_SLOW") == "" {
		t.Skip("builds the translated front end (set GO2PU_SLOW=1)")
	}
	if _, err := exec.LookPath("ld"); err != nil {
		t.Skip("no linker")
	}
	work := t.TempDir()
	all := typeCheckDirs(t, work)
	step := 12
	if s := os.Getenv("LOWER_STEP"); s != "" {
		if _, err := fmt.Sscan(s, &step); err != nil {
			t.Fatalf("LOWER_STEP: %v", err)
		}
	}
	var dirs []string
	n := 0
	for _, d := range all {
		if strings.Contains(d, "/gen/pkg/") || strings.Contains(d, "/typecheck_") {
			continue // packages of the library and translations: nothing to lower
		}
		if only := os.Getenv("LOWER_ONLY"); only != "" && !strings.Contains(d, only) {
			continue
		}
		isExample := strings.Contains(d, string(filepath.Separator)+"examples"+string(filepath.Separator))
		if isExample || n%step == 0 {
			dirs = append(dirs, d)
		}
		n++
	}
	t.Logf("%d directories", len(dirs))
	goBin := goBinary(t, "./testdata/lowerdemo")
	lib, err := filepath.Abs(filepath.Join(repoRoot, "lib"))
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 2} {
		// One level after the other: building the translated front end takes
		// 15 GB.
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			puBin := demoBinary(t, "lowerdemo", level, false)
			mismatched := 0
			for _, d := range dirs {
				want := runLower(t, goBin, lib, level, "digest", d)
				got := runLower(t, puBin, lib, level, "digest", d)
				if bytes.Equal(want, got) {
					continue
				}
				mismatched++
				if mismatched <= 5 {
					// The text, to see where they differ.
					w := runLower(t, goBin, lib, level, "full", d)
					g := runLower(t, puBin, lib, level, "full", d)
					t.Errorf("%s: the dumps differ\n%s", d, firstDifference(w, g))
				}
			}
			if mismatched > 0 {
				t.Errorf("%d of %d directories differ", mismatched, len(dirs))
			}
		})
	}
}

// runLower runs bin on the directory and returns its output.
func runLower(t *testing.T, bin, lib string, level int, mode, dir string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, lib, fmt.Sprint(level), mode, dir)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\n%s", filepath.Base(bin), dir, err, stderr.String())
	}
	return stdout.Bytes()
}
