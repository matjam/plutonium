// Ported from internal/escape2/report/msgdiff_test.go at the go-cutover tag.
// Differences from the Go test:
//   - It is in package escape2 (see report_helpers_test.p).
//   - testing.Short is PLUTO_LONG_TESTS: the test runs only when it is set.
//   - The corpus moved with the Go removal: cmd/pluto/testdata/diag is
//     tests/diag.
//   - Plutonium has no recover: a panic while writing the messages stops
//     the test instead of being reported as an error.
//   - The Go test was TestMessageDiff, which also listed where the messages
//     differed from internal/escape's. That analysis is gone (18358443); the
//     goldens hold the messages now, and what is checked here is what was
//     this package's own: the terms a message uses and that it is written.
//   - Each program is loaded and analysed in a region of its own, released
//     before the next: nothing else frees a program's load and analyses (Go's
//     GC did), and without it the test passed 24 GB.

package escape2

import (
	"pluto/os"
	"pluto/path/filepath"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/escape2/report"
	"compiler/internal/escape2/sched"
	"compiler/internal/load"
	"compiler/internal/scratch"
	"compiler/internal/testenv"
	"compiler/internal/types2"
	"compiler/pu"
)

// TestGoldenMessages writes this package's messages for the escape errors
// of the programs of the diagnostics goldens (tests/diag) and fails on a
// message that names the analysis's own terms or an absolute path, and on
// one that could not be written.
func TestGoldenMessages(t *testing.T) {
	if compat.Getenv("PLUTO_LONG_TESTS") == "" {
		t.Skip("loads every diagnostics golden program; set PLUTO_LONG_TESTS=1")
	}
	dirs := testenv.Glob(filepath.Join(testenv.Root(), "tests/diag/*"))
	if len(dirs) == 0 {
		t.Fatalf("no golden programs")
	}
	checked := 0
	for _, dir := range dirs {
		if skipGolden(dir) {
			continue
		}
		if goldenMessages(t, dir) {
			checked++
		}
	}
	if checked == 0 {
		t.Fatalf("no golden program loaded")
	}
}

// goldenMessages checks the messages of one golden program, in a region
// of its own, and reports whether the program loaded and was analysed.
// t's messages are written out as they are made, so they may be made in
// the region.
func goldenMessages(t *testing.T, dir string) bool {
	mem := scratch.NewRegion()
	defer mem.Release()
	use := mem.Use()
	defer use.Restore()
	return goldenIn(t, dir)
}

// goldenIn is goldenMessages in the region it made current. It is a
// function of its own so that its counted locals (an error the analysis
// returns) are released when it returns: a frame releases them after its
// deferred calls run, which would be after the region's release.
func goldenIn(t *testing.T, dir string) bool {
	name := filepath.Base(dir)
	cfg := pu.New(load.Config{Tags: load.BuildTags(false, false, nil), Stdlib: testenv.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")})
	prog, err := load.Load(cfg, dir)
	if err != nil {
		return false // a golden of a load or type error
	}
	p, res, err := Run(prog, sched.Options{})
	if err != nil {
		t.Errorf("%s: the analysis failed: %v", name, err)
		return false
	}
	for _, d := range report.Diagnostics(prog, p, res) {
		if bad := internalTerm(render(d)); bad != "" {
			t.Errorf("%s: %s: message names %q:\n%s", name, d.Pos, bad, render(d))
		}
		if d.Msg == report.Unwritten {
			t.Errorf("%s: %s: the message could not be written", name, d.Pos)
		}
	}
	return true
}

// skipGolden reports whether a golden program is built in a way the test
// does not reproduce (another subcommand, a kernel or freestanding mode).
func skipGolden(dir string) bool {
	for _, f := range []string{"flags", "command"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

// render is a diagnostic as pluto prints it, without the directory.
func render(d *diag.Diagnostic) string {
	dd := *d
	dd.Pos.Filename = filepath.Base(dd.Pos.Filename)
	return dd.Error() + "\n" + diag.Explanation(&dd)
}

// internalTerms are words of the analysis's implementation that a message
// must not use (AGENTS.md, Diagnostics): the program's terms only.
var internalTerms = []string{"register", "token", "summary", "EIR", "eir.", "%!", "Obj(", "Reg(", "In(", "SCC", "witness", "<nil>"}

// internalTerm returns the first implementation term or absolute path in
// text, or "".
func internalTerm(text string) string {
	for _, w := range internalTerms {
		if strings.Contains(text, w) {
			return w
		}
	}
	for _, f := range compat.Fields(text) {
		if strings.HasPrefix(strings.TrimLeft(f, "("), "/") && strings.Contains(f, ".p:") {
			return f
		}
	}
	return ""
}
