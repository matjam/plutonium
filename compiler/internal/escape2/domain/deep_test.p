// Ported from internal/escape2/domain/deep_test.go at the go-cutover tag.

package domain

import (
	"compiler/internal/testrand"
	"pluto/slices"
	"pluto/testing"

	"compiler/compat"
)

// Tests of Deep keys ("anywhere in the object, then load"): the key's path,
// any offset steps, one load, then any steps; never the path's pointers or
// their offsets themselves.

// sampleAny returns a random concrete word that k stands for, whatever its
// flags.
func sampleAny(r *testrand.Rand, ps Paths, k Key) []Step {
	if !k.Deep() {
		return sampleKey(r, ps, k)
	}
	w := sample(r, ps, k.Path(), false)
	for range r.IntN(3) {
		w = compat.Append(w, randOffset(r))
	}
	w = compat.Append(w, L)
	return compat.Append(w, randWord(r, 4)...)
}

// randAny returns a random key of input 0: exact, GE, FieldsGE or Deep.
func randAny(r *testrand.Rand, ps Paths, max int) Key {
	k := walk(ps, randWord(r, max)...)
	if k.GE() {
		return k
	}
	switch r.IntN(4) {
	case 0:
		return k.WithPath(k.Path(), true)
	case 1:
		return k.WithFieldsGE()
	case 2:
		return k.WithDeep()
	}
	return k
}

// randAnyPrefixed returns keys of every kind, with some on prefixes of each
// other's paths, so that covering pairs are common.
func randAnyPrefixed(r *testrand.Rand, ps Paths, n, max int) []Key {
	var keys []Key
	for len(keys) < n {
		k := randAny(r, ps, max)
		keys = compat.Append(keys, k)
		if st := ps.Steps(k.Path()); len(st) > 0 {
			p := walk(ps, st[:r.IntN(len(st))]...)
			keys = compat.Append(keys, p.WithDeep(), p.WithFieldsGE(), p.WithPath(p.Path(), true))
		}
	}
	return keys
}

func TestDeepKey(t *testing.T) {
	ps := NewPaths()
	p := walk(ps, F(1), L).Path()
	k := MakeKey(3, p, KeyFlags{Deep: true, Counts: true})
	if !k.Deep() || k.GE() || k.FieldsGE() || !k.Flags().Deep || !k.Flags().Counts || k.Input() != 3 || k.Path() != p {
		t.Fatalf("MakeKey Deep: %x", k)
	}
	if g := MakeKey(3, p, KeyFlags{GE: true, Deep: true}); !g.GE() || g.Deep() {
		t.Fatal("GE and Deep: GE must win")
	}
	if g := MakeKey(3, p, KeyFlags{FieldsGE: true, Deep: true}); !g.GE() || g.Deep() || g.FieldsGE() {
		t.Fatal("FieldsGE and Deep: their union is GE")
	}
	if g := k.WithPath(p, true); !g.GE() || g.Deep() {
		t.Fatal("WithPath(ge) must clear Deep")
	}
	if e := k.WithPath(Root, false); e.GE() || e.Deep() || !e.Flags().Counts {
		t.Fatal("WithPath must clear Deep and keep the other flags")
	}
	if k.WithFieldsGE() != k || k.WithDeep() != k {
		t.Fatal("WithFieldsGE or WithDeep changed a Deep key")
	}
	if g := k.WithPath(p, true); g.WithDeep() != g {
		t.Fatal("WithDeep of a GE key changed it")
	}
	if f := k.WithPath(p, false).WithFieldsGE().WithDeep(); !f.Deep() || f.FieldsGE() || f.Path() != p {
		t.Fatal("WithDeep of a FieldsGE key must be Deep on its path")
	}
	if k>>46 != 0 {
		t.Fatal("a key uses bits past 45")
	}
}

func TestDeepCovers(t *testing.T) {
	ps := NewPaths()
	k := func(ws ...Step) Key { return walk(ps, ws...) }
	fge := func(ws ...Step) Key { return walk(ps, ws...).WithFieldsGE() }
	ge := func(ws ...Step) Key { x := walk(ps, ws...); return x.WithPath(x.Path(), true) }
	deep := func(ws ...Step) Key { return walk(ps, ws...).WithDeep() }
	for _, c := range []struct {
		a, b Key
		want bool
	}{
		// Covered by the GE key at the root, and by GE on a prefix.
		{ge(), deep(), true},
		{ge(F(1)), deep(F(1), F(2)), true},
		{ge(F(1)), deep(F(1)), true},
		// Covers any path that starts with a load from the object.
		{deep(), k(L), true},
		{deep(), k(F(1), L), true},
		{deep(), k(F(1), F(2), L, F(0)), true},
		{deep(), ge(L), true},
		{deep(), ge(E, L), true},
		{deep(), fge(L), true},
		{deep(), fge(F(0), L, F(1)), true},
		{deep(), deep(F(1)), true},
		{deep(), deep(L), true},
		{deep(L), k(L, L), true},
		{deep(L), k(L, F(0), L, F(1)), true},
		// Never the root itself, nor its offsets.
		{deep(), k(), false},
		{deep(), k(F(1)), false},
		{deep(), k(F(1), F(2)), false},
		{deep(), fge(), false},
		{deep(), fge(F(0)), false},
		{deep(), ge(), false},
		{deep(), ge(F(1)), false},
		{deep(L), k(L), false},
		{deep(L), k(L, F(1)), false},
		{deep(L), ge(L), false},
		// Other objects.
		{deep(F(1)), k(F(2), L), false},
		{deep(F(1)), deep(F(2)), false},
		{deep(F(1)), deep(), false},
		{deep(L), deep(), false},
		// Nothing narrower covers a Deep key.
		{k(), deep(), false},
		{k(L), deep(), false},
		{fge(), deep(), false},
		{ge(L), deep(), false},
		{fge(), k(L), false},
	} {
		if got := Covers(ps, c.a, c.b); got != c.want {
			t.Errorf("Covers(%s, %s) = %v, want %v", showDeep(ps, c.a), showDeep(ps, c.b), got, c.want)
		}
	}
	// Extending: a Deep key stands for every extension; a load through a
	// FieldsGE key is Deep on its path.
	for _, s := range []Step{L, E, F(0)} {
		if x := ExtendKey(ps, deep(F(1)), s); x != deep(F(1)) {
			t.Errorf("Deep + %d = %s", s, showDeep(ps, x))
		}
	}
	if x := ExtendKey(ps, fge(F(1)), L); x != deep(F(1)) {
		t.Errorf("FieldsGE + load = %s", showDeep(ps, x))
	}
	if x := AppendTail(ps, k(L), deep(F(1))); x != deep(L, F(1)) {
		t.Errorf("AppendTail(L, Deep F1) = %s", showDeep(ps, x))
	}
	if x := AppendTail(ps, deep(F(0)), k(L, F(1))); x != deep(F(0)) {
		t.Errorf("AppendTail through a Deep key = %s", showDeep(ps, x))
	}
	if x := AppendTail(ps, deep(F(0)), ge(L)); x != deep(F(0)) {
		t.Errorf("AppendTail of a GE tail on a Deep key = %s", showDeep(ps, x))
	}
	if x := AppendTail(ps, fge(F(0)), deep()); x != deep(F(0)) {
		t.Errorf("AppendTail of Deep on a FieldsGE key = %s", showDeep(ps, x))
	}
	if x := AppendTail(ps, ge(F(0)), deep()); x != ge(F(0)) {
		t.Errorf("AppendTail of Deep on a GE key = %s", showDeep(ps, x))
	}
}

func showDeep(ps Paths, k Key) string {
	if k.Deep() {
		return showKey(ps, k.WithPath(k.Path(), false)) + " Deep"
	}
	return showKey(ps, k)
}

// TestPropDeepCoversExact: Covers is sound against KeyMatches on samples,
// and when it says no there is a witness (but for the one case Covers leaves
// out by design, an exact key with a full last segment and its FieldsGE
// twin).
func TestPropDeepCoversExact(t *testing.T) {
	rv := testrand.New(91, 92)
	r := &rv
	ps := NewPaths()
	keys := randAnyPrefixed(r, ps, 300, 10)
	covered, deepCovered := 0, 0
	for _, a := range keys {
		for _, b := range keys {
			if Covers(ps, a, b) {
				covered++
				if a.Deep() || b.Deep() {
					deepCovered++
				}
				for range 4 {
					if w := sampleAny(r, ps, b); !KeyMatches(ps, a, w) {
						t.Fatalf("Covers(%s, %s) but %v is not covered", showDeep(ps, a), showDeep(ps, b), w)
					}
				}
				continue
			}
			if !a.GE() && !a.FieldsGE() && !a.Deep() && b.FieldsGE() && a.Path() == b.Path() && segmentFull(ps, a.Path()) {
				continue
			}
			st := ps.Steps(b.Path())
			witness := false
			for _, tail := range [][]Step{nil, {L}, {F(0)}, {E}, {L, L}, {F(0), L}, {E, L}, {L, F(0)}} {
				w := compat.Append(slices.Clip(st), tail...)
				if KeyMatches(ps, b, w) && !KeyMatches(ps, a, w) {
					witness = true
					break
				}
			}
			if !witness {
				t.Fatalf("not Covers(%s, %s), yet no witness", showDeep(ps, a), showDeep(ps, b))
			}
		}
	}
	if covered < 2*len(keys) || deepCovered < len(keys)/4 {
		t.Fatalf("only %d covering pairs, %d with a Deep key", covered, deepCovered)
	}
}

// TestPropDeepWidens is invariant I8 for Deep keys: ExtendKey, AppendTail,
// AppendKey and the cut of a summary cap (Cut with GE) cover what the key
// and the step or tail stand for.
func TestPropDeepWidens(t *testing.T) {
	rv := testrand.New(93, 94)
	r := &rv
	ps := NewPaths()
	for range 20000 {
		k := randAny(r, ps, 14)
		s := randStep(r)
		k2 := ExtendKey(ps, k, s)
		tail := randAny(r, ps, 8)
		k3 := AppendTail(ps, k, tail)
		k4 := AppendKey(ps, k, tail.Path(), true)
		c := k.WithPath(ps.Cut(k.Path()), true)
		for range 4 {
			w := sampleAny(r, ps, k)
			if ws := compat.Append(slices.Clip(w), s); !KeyMatches(ps, k2, ws) {
				t.Fatalf("%s + %d = %s misses %v", showDeep(ps, k), s, showDeep(ps, k2), ws)
			}
			if wt := compat.Append(slices.Clip(w), sampleAny(r, ps, tail)...); !KeyMatches(ps, k3, wt) {
				t.Fatalf("AppendTail(%s, %s) = %s misses %v", showDeep(ps, k), showDeep(ps, tail), showDeep(ps, k3), wt)
			}
			if wt := compat.Append(slices.Clip(w), sample(r, ps, tail.Path(), true)...); !KeyMatches(ps, k4, wt) {
				t.Fatalf("AppendKey(%s, %v, GE) = %s misses %v", showDeep(ps, k), ps.Steps(tail.Path()), showDeep(ps, k4), wt)
			}
			if !KeyMatches(ps, c, w) {
				t.Fatalf("Cut of %s = %s misses %v", showDeep(ps, k), showDeep(ps, c), w)
			}
			if d := k.WithDeep(); !k.GE() {
				// WithDeep is k followed by offsets, a load and more.
				wd := compat.Append(slices.Clip(w), randOffset(r), L)
				if !KeyMatches(ps, d, wd) {
					t.Fatalf("WithDeep of %s = %s misses %v", showDeep(ps, k), showDeep(ps, d), wd)
				}
			}
		}
	}
}

// TestPropDeepMonotone: the operations on keys are monotone under Covers:
// when a covers b, a's result covers b's, for ExtendKey by a step that cuts
// neither path, AppendTail on either side, WithDeep and WithFieldsGE.
func TestPropDeepMonotone(t *testing.T) {
	rv := testrand.New(95, 96)
	r := &rv
	ps := NewPaths()
	keys := randAnyPrefixed(r, ps, 200, 6)
	tails := randAnyPrefixed(r, ps, 20, 3)
	pairs := 0
	for _, a := range keys {
		for _, b := range keys {
			if !Covers(ps, a, b) {
				continue
			}
			pairs++
			if !Covers(ps, a.WithDeep(), b.WithDeep()) {
				t.Fatalf("WithDeep: %s covers %s, not after", showDeep(ps, a), showDeep(ps, b))
			}
			if !Covers(ps, a.WithFieldsGE(), b.WithFieldsGE()) {
				t.Fatalf("WithFieldsGE: %s covers %s, not after", showDeep(ps, a), showDeep(ps, b))
			}
			for _, s := range []Step{L, E, F(0), F(1)} {
				_, cutA := ps.Extend(a.Path(), s)
				_, cutB := ps.Extend(b.Path(), s)
				if cutA || cutB {
					continue
				}
				if x, y := ExtendKey(ps, a, s), ExtendKey(ps, b, s); !Covers(ps, x, y) {
					t.Fatalf("ExtendKey %d: %s covers %s, not %s over %s", s, showDeep(ps, a), showDeep(ps, b), showDeep(ps, x), showDeep(ps, y))
				}
			}
			for _, tl := range tails {
				fits := func(x Key) bool {
					return len(ps.Steps(x.Path()))+len(ps.Steps(tl.Path())) <= MaxSteps && ps.Loads(x.Path())+ps.Loads(tl.Path()) <= MaxLoads
				}
				if !fits(a) || !fits(b) {
					continue // a cut on the way is not monotone (paths.go)
				}
				if x, y := AppendTail(ps, a, tl), AppendTail(ps, b, tl); !Covers(ps, x, y) {
					t.Fatalf("AppendTail(_, %s): %s covers %s, not %s over %s", showDeep(ps, tl), showDeep(ps, a), showDeep(ps, b), showDeep(ps, x), showDeep(ps, y))
				}
				if x, y := AppendTail(ps, tl, a), AppendTail(ps, tl, b); !Covers(ps, x, y) {
					t.Fatalf("AppendTail(%s, _): %s covers %s, not %s over %s", showDeep(ps, tl), showDeep(ps, a), showDeep(ps, b), showDeep(ps, x), showDeep(ps, y))
				}
			}
		}
	}
	if pairs < len(keys) {
		t.Fatalf("only %d covering pairs", pairs)
	}
}

// TestPropDeepAntichain: with Deep keys in the sets, Antichain and CoveredBy
// agree with a scan of Covers.
func TestPropDeepAntichain(t *testing.T) {
	rv := testrand.New(97, 98)
	r := &rv
	ps := NewPaths()
	removedDeep, removedByDeep := 0, 0
	for range 1000 {
		in := randAnyPrefixed(r, ps, 1+r.IntN(100), 8)
		orig := compat.Clone(in)
		out := Antichain(ps, in)
		for i, a := range out {
			for j, b := range out {
				if i != j && Covers(ps, a, b) {
					t.Fatalf("%s covers %s in the antichain", showDeep(ps, a), showDeep(ps, b))
				}
			}
		}
		for _, k := range orig {
			if !bruteCovered(ps, out, k) {
				t.Fatalf("%s is no longer covered", showDeep(ps, k))
			}
			if slices.Contains(out, k) {
				continue
			}
			if k.Deep() {
				removedDeep++
			}
			for _, a := range out {
				if a.Deep() && Covers(ps, a, k) {
					removedByDeep++
					break
				}
			}
		}
		set := SortKeys(randAnyPrefixed(r, ps, 40, 8))
		for _, k := range randAnyPrefixed(r, ps, 20, 8) {
			if CoveredBy(ps, set, k) != bruteCovered(ps, set, k) {
				t.Fatalf("CoveredBy(%s) disagrees with Covers", showDeep(ps, k))
			}
		}
	}
	if removedDeep == 0 || removedByDeep == 0 {
		t.Fatalf("Deep keys removed %d, removed by a Deep key %d", removedDeep, removedByDeep)
	}
}
