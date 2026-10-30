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
// tests. The light packages run in parallel in about 90 s (scratch the
// longest); the heavy ones about an hour, one at a time.
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
			out, status := runPluto(t, "..", nil, args...)
			if status != 0 {
				t.Fatalf("pluto %s: exit status %d\n%s", strings.Join(args, " "), status, out)
			}
		})
	}
}
