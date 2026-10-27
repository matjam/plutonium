package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDigestStable builds a few programs of the corpus twice: the digests of
// a compiler must not change from one run to the next, or comparing two
// compilers with them says nothing. The programs include ones that are
// rejected (their digest is of the diagnostics) and ones that are built.
// It needs a compiler: $PLUTO_TEST_COMPILER or pluto at the repository
// root; without one it skips.
func TestDigestStable(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	progs := corpus(root)
	if len(progs) < 200 {
		t.Fatalf("only %d programs in the corpus", len(progs))
	}
	compiler, err := filepath.Abs(defaultCompiler(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(compiler); err != nil {
		t.Skip("no compiler: set PLUTO_TEST_COMPILER or build the self-hosted compiler as ./pluto")
	}
	lib := filepath.Join(root, "lib")
	var rejected, built int
	for _, p := range progs {
		if !strings.HasPrefix(p.name, "diag/escape") && p.name != "examples/07-hello" && p.name != "trace/index" {
			continue
		}
		a, b := digest(compiler, lib, root, p, 2), digest(compiler, lib, root, p, 2)
		if a != b {
			t.Errorf("%s: %s, then %s", p.name, a, b)
		}
		switch {
		case strings.HasPrefix(a, "status"):
			rejected++
		case strings.HasPrefix(a, "object"):
			built++
		default:
			t.Errorf("%s: %s", p.name, a)
		}
	}
	if rejected == 0 || built < 2 {
		t.Errorf("digested %d rejected and %d built programs; want some of each", rejected, built)
	}
}
