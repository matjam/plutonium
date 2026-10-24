package summary_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/escape2/summary/summarytest"
)

// TestCutKeyDeep: a key whose path has two loads or more is cut to Deep on
// its path through the first load, which covers the key and every
// extension of it and does not stand for the pointers of the cut path (a
// closure record's pointer to a captured variable, for the key of what the
// variable holds); a key with one load is cut to GE, as before.
func TestCutKeyDeep(t *testing.T) {
	ps := summarytest.NewPaths()
	for _, ge := range []bool{false, true} {
		k := key(ps, 1, ge, 7, L, L, 4, L)
		c := summary.CutKey(ps, k)
		if !c.Deep() || c.GE() || c.Path() != ps.Of(7, L) {
			t.Fatalf("CutKey(%v GE=%v) = path %v Deep=%v GE=%v; want Deep on [7 load]", ps.Steps(k.Path()), ge, ps.Steps(c.Path()), c.Deep(), c.GE())
		}
		if !domain.Covers(ps, c, k) {
			t.Errorf("GE=%v: the cut does not cover the key", ge)
		}
		for _, w := range [][]domain.Step{{7, L, L, 4, L}, {7, L, L}, {7, L, 2, L, 1}} {
			if !domain.KeyMatches(ps, c, w) {
				t.Errorf("the cut does not stand for %v", w)
			}
		}
		for _, w := range [][]domain.Step{{7, L}, {7, L, 3}, {7}} {
			if domain.KeyMatches(ps, c, w) {
				t.Errorf("the cut stands for %v, the cut path's own pointers", w)
			}
		}
	}
	one := summary.CutKey(ps, key(ps, 1, false, 7, L, 4))
	if !one.GE() || one.Deep() || one.Path() != ps.Of(7, L) {
		t.Errorf("a key of one load: cut to path %v Deep=%v GE=%v; want GE on [7 load]", ps.Steps(one.Path()), one.Deep(), one.GE())
	}
}
