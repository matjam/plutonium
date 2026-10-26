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

// escape2DemoPrograms are the programs TestEscape2Demo analyzes, relative to
// the repository: directories, or single files, which the test puts in a
// directory of their own as main.p. They cover the analysis's errors (the
// review programs are rejected, each for another reason), programs it
// accepts, and the library they import (fmt, errors, vec, rings, ranges over
// functions, closures, owners and boxes).
var escape2DemoPrograms = []string{
	"examples/24-errors",
	"tests/diag/boxes_views",
	"tests/diag/owning_iface",
	"tests/diag/box_temp_views",
	"testdata/lib/fmtappend.p",
	"testdata/run/closureregions.p",
	"testdata/run/rangefuncdefer.p",
	"testdata/plutonium/ringowners.p",
	"tests/testdata/escape/review4/a7_ring_push_copy_global.p",
	"tests/testdata/escape/review4/e03_errorbuf_wrapped_by_errorf.p",
	"tests/testdata/escape/review4/lv02_closure_of_body_var_kept.p",
	"tests/testdata/escape/review4/w01_local_waitgroup_task_no_wait.p",
	"internal/escape/testdata/review3/o17_shared_field_view_returned.p",
	"internal/escape/testdata/review2/f38_fmt_named_slice_type_string.p",
}

// escape2DemoUnbuilt says why the escape2demo program, which holds the
// translated escape analysis, cannot be built: internal/escape needs more
// than 20 GB for it (its keys of escapes grow without bound on the
// translated internal/escape2/eir with the type checker), and
// internal/escape2 (PLUTO_ESCAPE=new, which runs it alone) rejects parts of
// the translated compiler: about 80 sites, nearly all E3003, most through
// fmt and the per-signature join of func() literals
// (docs/plans/2026-10-20-escape-redesign.md).
const escape2DemoUnbuilt = "the Go compiler cannot build the translated escape analysis yet"

// TestEscape2Demo runs the escape2demo program, built from the Go escape
// analysis (internal/escape2, with the loader and type checker) and from its
// translation (selfhost/internal/escape2), over escape2DemoPrograms at -O0
// and -O2. For each program the two builds must print the same text: the
// diagnostics, byte for byte, and a digest of every summary the analysis
// made. ESCAPE2DEMO_ONLY=<substring> runs the programs whose paths contain
// it.
//
// It runs only with GO2PU_ESCAPE2DEMO=1, because the demo cannot be built
// yet (escape2DemoUnbuilt); the build reads PLUTO_ESCAPE like any other.
func TestEscape2Demo(t *testing.T) {
	testguard.Parallel(t)
	if os.Getenv("GO2PU_ESCAPE2DEMO") == "" {
		t.Skip(escape2DemoUnbuilt + " (set GO2PU_ESCAPE2DEMO=1 to try)")
	}
	if _, err := exec.LookPath("ld"); err != nil {
		t.Skip("no linker")
	}
	dirs := escape2DemoDirs(t, t.TempDir())
	goBin := goBinary(t, "./testdata/escape2demo")
	lib, err := filepath.Abs(filepath.Join(repoRoot, "lib"))
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			puBin := demoBinary(t, "escape2demo", level, false)
			for _, d := range dirs {
				want, got := runEscape2Demo(t, goBin, lib, d), runEscape2Demo(t, puBin, lib, d)
				if !bytes.Equal(want, got) {
					t.Errorf("%s: the outputs differ\n%s", d, firstDifference(want, got))
				}
			}
		})
	}
}

// runEscape2Demo runs bin on dir and returns its output.
func runEscape2Demo(t *testing.T, bin, lib, dir string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, lib, dir)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\n%s", filepath.Base(bin), dir, err, stderr.String())
	}
	return stdout.Bytes()
}

// escape2DemoDirs returns the directories of escape2DemoPrograms, making a
// directory in work for each single-file program.
func escape2DemoDirs(t *testing.T, work string) []string {
	t.Helper()
	only := os.Getenv("ESCAPE2DEMO_ONLY")
	var dirs []string
	for _, p := range escape2DemoPrograms {
		if only != "" && !strings.Contains(p, only) {
			continue
		}
		src, err := filepath.Abs(filepath.Join(repoRoot, p))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(p, ".p") {
			dirs = append(dirs, src)
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(work, strings.TrimSuffix(filepath.Base(p), ".p"))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.p"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, dir)
	}
	return dirs
}
