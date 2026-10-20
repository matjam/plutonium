package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestDigestStable builds a few programs of the corpus twice: the digests of
// a compiler must not change from one run to the next, or comparing two
// compilers with them says nothing. The corpus includes a program that is
// rejected (its digest is of the diagnostics) and one that is built.
func TestDigestStable(t *testing.T) {
	testguard.Parallel(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	progs := corpus(root)
	if len(progs) < 200 {
		t.Fatalf("only %d programs in the corpus", len(progs))
	}
	var rejected, built int
	for _, p := range progs {
		if !strings.HasPrefix(p.name, "diag/escape") && p.name != "examples/07-hello" {
			continue
		}
		a, b := digest(root, p, 2), digest(root, p, 2)
		if a != b {
			t.Errorf("%s: %s, then %s", p.name, a, b)
		}
		if strings.HasPrefix(a, "error") {
			rejected++
		} else {
			built++
		}
	}
	if rejected == 0 || built == 0 {
		t.Errorf("digested %d rejected and %d built programs; want some of each", rejected, built)
	}
}
