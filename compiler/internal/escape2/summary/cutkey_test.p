// Ported from internal/escape2/summary/cutkey_test.go at the go-cutover tag.
// The Go test was in package summary_test; this one is in package summary.

package summary

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/summary/summarytest"
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
		c := CutKey(ps, k)
		if !c.Deep() || c.GE() || c.Path() != ps.Of(7, L) {
			t.Fatalf("CutKey(%v GE=%v) = path %v Deep=%v GE=%v; want Deep on [7 load]", ps.Steps(k.Path()), ge, ps.Steps(c.Path()), c.Deep(), c.GE())
		}
		if !domain.Covers(ps, c, k) {
			t.Errorf("GE=%v: the cut does not cover the key", ge)
		}
		for _, w := range [][]domain.Step{{7, L, L, 4, L}, {7, L, L}, {7, L, 2, L, 1}} {
			if !domain.KeyMatches(ps, c, w) {
				t.Errorf("the cut does not stand for %s", stepsString(w))
			}
		}
		for _, w := range [][]domain.Step{{7, L}, {7, L, 3}, {7}} {
			if domain.KeyMatches(ps, c, w) {
				t.Errorf("the cut stands for %s, the cut path's own pointers", stepsString(w))
			}
		}
	}
	one := CutKey(ps, key(ps, 1, false, 7, L, 4))
	if !one.GE() || one.Deep() || one.Path() != ps.Of(7, L) {
		t.Errorf("a key of one load: cut to path %v Deep=%v GE=%v; want GE on [7 load]", ps.Steps(one.Path()), one.Deep(), one.GE())
	}
}

// TestCutKeyNoLoad: a key with no load, a place in the input's own object,
// is cut to FieldsGE on its path: it covers the key and every offset under
// it, never a pointer found through a load (errors.As: *target = e, a store
// whose destination is the caller's variable, not what it points to). A GE
// key with no load stays GE.
func TestCutKeyNoLoad(t *testing.T) {
	ps := summarytest.NewPaths()
	for _, steps := range [][]domain.Step{nil, {3}, {3, 1}} {
		k := key(ps, 1, false, steps...)
		c := CutKey(ps, k)
		if !c.FieldsGE() || c.GE() || c.Deep() || c.Path() != k.Path() {
			t.Fatalf("CutKey(%s) = path %v FieldsGE=%v GE=%v Deep=%v; want FieldsGE on its path", stepsString(steps), ps.Steps(c.Path()), c.FieldsGE(), c.GE(), c.Deep())
		}
		if !domain.Covers(ps, c, k) || !domain.Covers(ps, c, c) {
			t.Errorf("%s: the cut does not cover the key", stepsString(steps))
		}
		if w := compat.AppendPlain(cloneSteps(steps), 2); !domain.KeyMatches(ps, c, w) {
			t.Errorf("%s: the cut does not stand for %s", stepsString(steps), stepsString(w))
		}
		if w := compat.AppendPlain(cloneSteps(steps), L); domain.KeyMatches(ps, c, w) {
			t.Errorf("%s: the cut stands for %s, through a load", stepsString(steps), stepsString(w))
		}
	}
	if c := CutKey(ps, key(ps, 1, true)); !c.GE() || c.Path() != domain.Root {
		t.Errorf("a GE key on Root: cut to path %v GE=%v", ps.Steps(c.Path()), c.GE())
	}
}

// cloneSteps copies steps to the heap, element by element.
func cloneSteps(steps []domain.Step) []domain.Step {
	var w []domain.Step
	for _, x := range steps {
		w = compat.AppendPlain(w, x)
	}
	return w
}

// stepsString formats steps as fmt's %v formats a slice of integers.
func stepsString(steps []domain.Step) string {
	b := compat.AppendString(nil, "[")
	for i, x := range steps {
		if i > 0 {
			b = compat.AppendString(b, " ")
		}
		b = compat.AppendInt(b, int64(x), 10)
	}
	b = compat.AppendString(b, "]")
	return compat.String(b)
}
