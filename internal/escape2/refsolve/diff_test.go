package refsolve

import (
	"os"
	"strconv"
	"testing"

	"github.com/matjam/plutonium/internal/escape2/domain"
)

// cases is the number of random cases of each test: REFSOLVE_CASES, default
// 3000.
func cases() int {
	if s := os.Getenv("REFSOLVE_CASES"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return 3000
}

// TestRandomReferenceOnly runs the reference solver on random inputs with the
// naive paths: it must terminate, be deterministic, and produce summaries
// that are antichains with entries in range.
func TestRandomReferenceOnly(t *testing.T) {
	for seed := int64(0); seed < int64(cases()); seed++ {
		ps := NewNaivePaths()
		c := Random(seed, ps)
		r1 := Canon(ps, Solve(c.F, c.Env))
		r2 := Canon(ps, Solve(c.F, c.Env))
		if r1 != r2 {
			t.Fatalf("not deterministic:\n%s\n%s\n%s", c.Dump(), r1, r2)
		}
		sm := Solve(c.F, c.Env).Summary
		for _, k := range sm.Esc {
			for _, o := range sm.Esc {
				if k != o && domain.Covers(ps, o, k) {
					t.Fatalf("Esc is not an antichain: %s covers %s\n%s", CanonKey(ps, o), CanonKey(ps, k), c.Dump())
				}
			}
			if k.Input() >= len(c.F.Inputs) {
				t.Fatalf("key of input %d, function has %d", k.Input(), len(c.F.Inputs))
			}
		}
	}
}

// TestDifferential runs solve.Solve and the reference solver on random
// inputs and on hand-built ones, and requires equal summaries and findings. A
// disagreement is a bug in one of them.
func TestDifferential(t *testing.T) {
	if !Available() {
		t.Skip("solve.Solve or domain is not built yet")
	}
	for seed := int64(0); seed < int64(cases()); seed++ {
		c := Random(seed, domain.NewPaths())
		if d := Compare(c); d != "" {
			t.Fatalf("solve and refsolve differ on seed %d:\n%s\n%s", seed, d, c.Dump())
		}
	}
}
