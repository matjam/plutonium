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

// TestCutKeyDeepOneLoad: a Deep key of one load, what a loaded pointer leads
// to, is cut to Deep on its path through the load, never GE: GE would stand
// for the loaded pointer itself, and a store of what *p holds into *p would
// read, cut, as p stored into *p (types2's convertibleTo, whose closures
// store into *cause what *cause held; a caller passing &cause was then told
// cause is kept).
func TestCutKeyDeepOneLoad(t *testing.T) {
	ps := summarytest.NewPaths()
	for _, steps := range [][]domain.Step{{7, L}, {7, L, 4}, {7, L, 4, 2}} {
		k := key(ps, 1, false, steps...).WithDeep()
		c := CutKey(ps, k)
		if !c.Deep() || c.GE() || c.Path() != ps.Of(7, L) {
			t.Fatalf("CutKey(%s Deep) = path %v Deep=%v GE=%v; want Deep on [7 load]", stepsString(steps), ps.Steps(c.Path()), c.Deep(), c.GE())
		}
		if !domain.Covers(ps, c, k) {
			t.Errorf("%s: the cut does not cover the key", stepsString(steps))
		}
		if w := compat.AppendPlain(cloneSteps(steps), L); !domain.KeyMatches(ps, c, w) {
			t.Errorf("%s: the cut does not stand for %s", stepsString(steps), stepsString(w))
		}
		for _, w := range [][]domain.Step{{7, L}, {7, L, 4}, {7}} {
			if domain.KeyMatches(ps, c, w) {
				t.Errorf("%s: the cut stands for %s, the loaded pointer or its offsets", stepsString(steps), stepsString(w))
			}
		}
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

// TestWidenKeepsDeepFilter: a widening that cuts keys short of their type
// filter (D51) cuts them at the filter's pointer instead, and keeps it. A
// join of the Write methods called through an interface held entries of a
// member's own calls through interfaces, filtered past the cut; dropped,
// they stood for every member's receiver, and pluto/testing's buffer, held
// by an appendWriter whose Write keeps nothing, was taken for kept.
func TestWidenKeepsDeepFilter(t *testing.T) {
	ps := summarytest.NewPaths()
	const id = domain.FilterID(5)
	s := &Summary{Inputs: 1}
	var orig []domain.Key
	for j := 0; j < 70; j++ {
		k := key(ps, 0, false, 0, L, 1, L, 2, L, domain.Step(j)).WithFilterRaw(id, 5)
		orig = compat.AppendPlain(orig, k)
		s.Esc = compat.AppendPlain(s.Esc, k)
	}
	Widen(ps, s)
	if len(s.Esc) != 1 {
		t.Fatalf("Widen left %d keys; want 1", len(s.Esc))
	}
	c := s.Esc[0]
	if f, pos := c.Filter(); f != id || pos != 5 {
		t.Fatalf("the widened key has filter %d at %d; want %d at 5", f, pos, id)
	}
	if !c.GE() || c.Path() != ps.Of(0, L, 1, L, 2, L) {
		t.Errorf("the widened key is path %v GE=%v Deep=%v; want GE on [0 load 1 load 2 load]", ps.Steps(c.Path()), c.GE(), c.Deep())
	}
	for _, k := range orig {
		if !domain.Covers(ps, c, k) {
			t.Fatalf("the widened key does not cover %v", ps.Steps(k.Path()))
		}
	}
	// The cuts alone: past the filter's pointer, never short of it.
	k := key(ps, 0, false, 7, L, 3, L, 4, L).WithDeep().WithFilterRaw(id, 4)
	for _, c := range []domain.Key{CutKey(ps, k), rootKey(ps, k)} {
		if f, pos := c.Filter(); f != id || pos != 4 || !c.Deep() || c.Path() != ps.Of(7, L, 3, L) {
			t.Errorf("cut of [7 load 3 load 4 load] Deep, filter at 4: path %v Deep=%v filter %d at %d; want Deep on [7 load 3 load], filter at 4", ps.Steps(c.Path()), c.Deep(), f, pos)
		}
		if !domain.Covers(ps, c, k) {
			t.Errorf("the cut does not cover the key")
		}
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
