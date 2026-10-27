// Ported from internal/escape2/domain/fieldsge_test.go at the go-cutover tag.

package domain

import (
	"compiler/internal/testrand"
	"pluto/slices"
	"pluto/testing"

	"compiler/compat"
)

// Tests of FieldsGE keys ("GE within the object"): the key's path or any
// extension of it by offset steps, no load.

// sampleKey returns a random concrete word that k stands for.
func sampleKey(r *testrand.Rand, ps Paths, k Key) []Step {
	w := sample(r, ps, k.Path(), k.GE())
	if k.FieldsGE() {
		for range r.IntN(4) {
			w = compat.Append(w, randOffset(r))
		}
	}
	return w
}

// randFlagged returns a random key of input 0 with no flag, GE or
// FieldsGE.
func randFlagged(r *testrand.Rand, ps Paths, max int) Key {
	k := walk(ps, randWord(r, max)...)
	if k.GE() {
		return k
	}
	switch r.IntN(3) {
	case 0:
		return k.WithPath(k.Path(), true)
	case 1:
		return k.WithFieldsGE()
	}
	return k
}

func showKey(ps Paths, k Key) string {
	f := ""
	switch {
	case k.GE():
		f = " GE"
	case k.FieldsGE():
		f = " FieldsGE"
	}
	return compat.Sprintf("%v%s", ps.Steps(k.Path()), f)
}

func TestFieldsGEKey(t *testing.T) {
	ps := NewPaths()
	p := walk(ps, F(1), L).Path()
	k := MakeKey(3, p, KeyFlags{FieldsGE: true, Borrowed: true})
	if !k.FieldsGE() || k.GE() || !k.Flags().FieldsGE || !k.Flags().Borrowed || k.Input() != 3 || k.Path() != p {
		t.Fatalf("MakeKey FieldsGE: %x", k)
	}
	if g := MakeKey(3, p, KeyFlags{GE: true, FieldsGE: true}); !g.GE() || g.FieldsGE() {
		t.Fatal("GE and FieldsGE: GE must win")
	}
	if g := k.WithPath(p, true); !g.GE() || g.FieldsGE() {
		t.Fatal("WithPath(ge) must clear FieldsGE")
	}
	if e := k.WithPath(Root, false); e.GE() || e.FieldsGE() || !e.Flags().Borrowed {
		t.Fatal("WithPath must clear FieldsGE and keep the other flags")
	}
	if g := k.WithPath(p, true); g.WithFieldsGE() != g {
		t.Fatal("WithFieldsGE of a GE key changed it")
	}
	if k>>45 != 0 {
		t.Fatal("a key uses bits past 44")
	}
}

func TestFieldsGECovers(t *testing.T) {
	ps := NewPaths()
	k := func(ws ...Step) Key { return walk(ps, ws...) }
	fge := func(ws ...Step) Key { return walk(ps, ws...).WithFieldsGE() }
	ge := func(ws ...Step) Key { x := walk(ps, ws...); return x.WithPath(x.Path(), true) }
	for _, c := range []struct {
		a, b Key
		want bool
	}{
		{fge(F(1)), k(F(1)), true},
		{fge(F(1)), k(F(1), F(2)), true},
		{fge(F(1)), k(F(1), E), true},
		{fge(F(1)), fge(F(1), F(2)), true},
		{fge(F(1)), k(F(1), L), false}, // a load is past the object
		{fge(F(1)), k(F(1), F(2), L), false},
		{fge(F(1)), ge(F(1)), false}, // GE is wider
		{fge(F(1)), k(F(2)), false},
		{fge(L), k(L, F(0)), true},
		{fge(L), k(F(0), L), false},
		{ge(F(1)), fge(F(1), F(2)), true},
		{ge(F(1)), fge(F(1)), true},
		{k(F(1)), fge(F(1)), false}, // exact covers no FieldsGE key
		{k(F(1), F(2)), fge(F(1), F(2)), false},
		{fge(F(1), F(2)), k(F(1), F(2)), true},
	} {
		if got := Covers(ps, c.a, c.b); got != c.want {
			t.Errorf("Covers(%s, %s) = %v, want %v", showKey(ps, c.a), showKey(ps, c.b), got, c.want)
		}
	}
	// Extending: an offset is absorbed, a load makes Deep on the path (the
	// offsets, then the load), or extends normally when the segment is
	// full.
	if x := ExtendKey(ps, fge(F(1)), E); x != fge(F(1)) {
		t.Errorf("FieldsGE + offset = %s", showKey(ps, x))
	}
	if x := ExtendKey(ps, fge(F(1)), L); x != k(F(1)).WithDeep() {
		t.Errorf("FieldsGE + load = %s", showDeep(ps, x))
	}
	if x := ExtendKey(ps, fge(F(1), F(2)), L); x != k(F(1), F(2), L) {
		t.Errorf("full FieldsGE + load = %s", showKey(ps, x))
	}
	if x := AppendTail(ps, k(L), fge(F(1))); x != fge(L, F(1)) {
		t.Errorf("AppendTail = %s", showKey(ps, x))
	}
	if x := AppendTail(ps, fge(F(0)), k(L, F(1))); x != k(F(0)).WithDeep() {
		t.Errorf("AppendTail through a FieldsGE key = %s", showDeep(ps, x))
	}
}

// TestPropFieldsGECoversExact: Covers is sound against KeyMatches on samples,
// and when it says no there is a witness, except where an exact key with a
// full last segment stands for the same pointers as a FieldsGE key, which
// Covers by design does not treat as covering it.
func TestPropFieldsGECoversExact(t *testing.T) {
	rv := testrand.New(81, 82)
	r := &rv
	ps := NewPaths()
	var keys []Key
	for range 300 {
		k := randFlagged(r, ps, 10)
		keys = compat.Append(keys, k)
		if st := ps.Steps(k.Path()); len(st) > 0 {
			p := walk(ps, st[:r.IntN(len(st))]...)
			keys = compat.Append(keys, p.WithFieldsGE(), p.WithPath(p.Path(), true))
		}
	}
	covered := 0
	for _, a := range keys {
		for _, b := range keys {
			if Covers(ps, a, b) {
				covered++
				for range 3 {
					if w := sampleKey(r, ps, b); !KeyMatches(ps, a, w) {
						t.Fatalf("Covers(%s, %s) but %v is not covered", showKey(ps, a), showKey(ps, b), w)
					}
				}
				continue
			}
			if !a.GE() && !a.FieldsGE() && b.FieldsGE() && a.Path() == b.Path() && segmentFull(ps, a.Path()) {
				continue
			}
			st := ps.Steps(b.Path())
			witness := false
			for _, tail := range [][]Step{nil, {L}, {F(0)}, {E}} {
				w := compat.Append(slices.Clip(st), tail...)
				if KeyMatches(ps, b, w) && !KeyMatches(ps, a, w) {
					witness = true
				}
			}
			if !witness {
				t.Fatalf("not Covers(%s, %s), yet no witness", showKey(ps, a), showKey(ps, b))
			}
		}
	}
	if covered < 2*len(keys) {
		t.Fatalf("only %d covering pairs", covered)
	}
}

// TestPropFieldsGEWidens is I8 for FieldsGE keys: ExtendKey, AppendTail and
// Cut cover what the key and the step stand for.
func TestPropFieldsGEWidens(t *testing.T) {
	rv := testrand.New(83, 84)
	r := &rv
	ps := NewPaths()
	for range 20000 {
		k := randFlagged(r, ps, 14)
		s := randStep(r)
		k2 := ExtendKey(ps, k, s)
		tail := randFlagged(r, ps, 8)
		k3 := AppendTail(ps, k, tail)
		c := k.WithPath(ps.Cut(k.Path()), true)
		for range 4 {
			w := sampleKey(r, ps, k)
			if ws := compat.Append(slices.Clip(w), s); !KeyMatches(ps, k2, ws) {
				t.Fatalf("%s + %d = %s misses %v", showKey(ps, k), s, showKey(ps, k2), ws)
			}
			if wt := compat.Append(slices.Clip(w), sampleKey(r, ps, tail)...); !KeyMatches(ps, k3, wt) {
				t.Fatalf("AppendTail(%s, %s) = %s misses %v", showKey(ps, k), showKey(ps, tail), showKey(ps, k3), wt)
			}
			if !KeyMatches(ps, c, w) {
				t.Fatalf("Cut of %s = %s misses %v", showKey(ps, k), showKey(ps, c), w)
			}
		}
	}
}

// TestPropFieldsGEAntichain: with FieldsGE keys in the sets, Antichain and
// CoveredBy agree with a scan of Covers.
func TestPropFieldsGEAntichain(t *testing.T) {
	rv := testrand.New(85, 86)
	r := &rv
	ps := NewPaths()
	gen := func(n int) []Key {
		var ks []Key
		for len(ks) < n {
			k := randFlagged(r, ps, 8)
			ks = compat.Append(ks, k)
			if st := ps.Steps(k.Path()); len(st) > 0 && r.IntN(2) == 0 {
				p := walk(ps, st[:r.IntN(len(st))]...)
				ks = compat.Append(ks, p.WithFieldsGE())
			}
		}
		return ks
	}
	removedFieldsGE := 0
	for range 1000 {
		in := gen(1 + r.IntN(100))
		orig := compat.Clone(in)
		out := Antichain(ps, in)
		for i, a := range out {
			for j, b := range out {
				if i != j && Covers(ps, a, b) {
					t.Fatalf("%s covers %s in the antichain", showKey(ps, a), showKey(ps, b))
				}
			}
		}
		for _, k := range orig {
			if !bruteCovered(ps, out, k) {
				t.Fatalf("%s is no longer covered", showKey(ps, k))
			}
			if !slices.Contains(out, k) && k.FieldsGE() {
				removedFieldsGE++
			}
		}
		set := SortKeys(gen(40))
		for _, k := range gen(20) {
			if CoveredBy(ps, set, k) != bruteCovered(ps, set, k) {
				t.Fatalf("CoveredBy(%s) disagrees with Covers", showKey(ps, k))
			}
		}
	}
	if removedFieldsGE == 0 {
		t.Fatal("no FieldsGE key was ever removed")
	}
}
