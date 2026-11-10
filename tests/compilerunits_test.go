package tests

import (
	"os"
	"strings"
	"testing"
)

// compilerUnit is a compiler package with _test.p unit tests (the API-level
// tests ported from the Go implementation at the go-cutover tag).
type compilerUnit struct {
	pkg   string
	flags []string
	// heavy packages link much of the compiler: their test build takes
	// minutes and up to 10 GB (build), so they run only when
	// PLUTO_UNIT_HEAVY is set.
	heavy bool
	// checkOnly stops the build as optimisation starts (exit status 130),
	// after the escape, move and race checks: the unit's tests are not
	// run, only checked to build.
	checkOnly bool
}

var compilerUnits = []compilerUnit{
	{pkg: "compat"},
	{pkg: "compat/fs"},
	{pkg: "internal/regexp"},
	{pkg: "internal/testpanic"},
	{pkg: "internal/testrand"},
	{pkg: "internal/scratch"},
	{pkg: "internal/ssa"},
	{pkg: "internal/opt"},
	{pkg: "internal/arch/amd64"},
	// typesprobe links types2 and pluto/testing with generation checks: a
	// check that the escape analysis accepts them, which the heavy escape2
	// builds make only in an hour. The analysis takes 7 min and 10 GB, and
	// code generation 4 min more, so the build stops before it.
	{pkg: "internal/escape2/typesprobe", checkOnly: true},
	{pkg: "internal/escape2/domain", heavy: true},
	{pkg: "internal/escape2/summary", heavy: true},
	{pkg: "internal/escape2/solve", heavy: true},
	{pkg: "internal/escape2/sched", heavy: true},
	{pkg: "internal/escape2/eir", heavy: true},
	{pkg: "internal/escape2", heavy: true},
	{pkg: "internal/lsp", heavy: true},
	{pkg: "internal/build", heavy: true},
}

// TestCompilerUnits runs pluto test on each compiler package that has unit
// tests. The light packages run in parallel in about 8 min (typesprobe the
// longest, 7 min alone; the others about 90 s); the heavy ones about an
// hour, one at a time.
func TestCompilerUnits(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the compiler packages' unit tests")
	}
	heavy := os.Getenv("PLUTO_UNIT_HEAVY") != ""
	for _, u := range compilerUnits {
		t.Run(u.pkg, func(t *testing.T) {
			if u.heavy {
				if !heavy {
					t.Skip("set PLUTO_UNIT_HEAVY=1 to run (minutes and up to 10 GB)")
				}
			} else {
				t.Parallel()
			}
			args := append([]string{"test"}, u.flags...)
			args = append(args, "./compiler/"+u.pkg)
			var env []string
			want := 0
			if u.checkOnly {
				env, want = []string{"PLUTO_PROFILE_PHASE=opt"}, 130
			}
			out, status := runPluto(t, "..", env, args...)
			if status != want {
				t.Fatalf("pluto %s: exit status %d\n%s", strings.Join(args, " "), status, out)
			}
		})
	}
}
