package domain

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// randKeys returns random keys over few inputs and flags, with many GE keys
// on prefixes of others, so that covering is common.
func randKeys(r *rand.Rand, ps Paths, n int) []Key {
	keys := make([]Key, 0, n)
	for len(keys) < n {
		f := KeyFlags{Borrowed: r.IntN(4) == 0, Counts: r.IntN(6) == 0}
		k := MakeKey(r.IntN(3), Root, f)
		w := randWord(r, 8)
		if r.IntN(3) == 0 && len(w) > 0 {
			w = w[:r.IntN(len(w))]
			k = k.WithPath(k.Path(), true)
		}
		for _, s := range w {
			k = ExtendKey(ps, k, s)
		}
		if r.IntN(4) == 0 {
			k = k.WithPath(k.Path(), true)
		}
		keys = append(keys, k)
	}
	return keys
}

func bruteCovered(ps Paths, set []Key, k Key) bool {
	for _, a := range set {
		if Covers(ps, a, k) {
			return true
		}
	}
	return false
}

// TestPropAntichain: Antichain keeps a subset, sorted, in which no key
// covers another, and that covers every key it was given: the same
// concretization, smaller.
func TestPropAntichain(t *testing.T) {
	r := rand.New(rand.NewPCG(51, 52))
	ps := NewPaths()
	removed := 0
	for range 2000 {
		in := randKeys(r, ps, 1+r.IntN(150))
		orig := slices.Clone(in)
		out := Antichain(ps, in)
		removed += len(orig) - len(out)
		if !slices.IsSorted(out) {
			t.Fatal("not sorted")
		}
		for i, a := range out {
			if !slices.Contains(orig, a) {
				t.Fatalf("key %x not in the input", a)
			}
			for j, b := range out {
				if i != j && Covers(ps, a, b) {
					t.Fatalf("%x covers %x in the antichain", a, b)
				}
			}
		}
		for _, k := range orig {
			if !bruteCovered(ps, out, k) {
				t.Fatalf("%x is no longer covered", k)
			}
			if CoveredBy(ps, out, k) != true {
				t.Fatalf("CoveredBy misses %x", k)
			}
		}
	}
	if removed == 0 {
		t.Fatal("no key was ever removed: the test does not exercise Antichain")
	}
}

// TestPropCoveredBy agrees with a scan of Covers, for keys in and out of
// the set.
func TestPropCoveredBy(t *testing.T) {
	r := rand.New(rand.NewPCG(53, 54))
	ps := NewPaths()
	yes, no := 0, 0
	for range 500 {
		set := SortKeys(randKeys(r, ps, 1+r.IntN(80)))
		for _, k := range randKeys(r, ps, 40) {
			want := bruteCovered(ps, set, k)
			if CoveredBy(ps, set, k) != want {
				t.Fatalf("CoveredBy(%x) = %v, want %v", k, !want, want)
			}
			if want {
				yes++
			} else {
				no++
			}
		}
	}
	if yes == 0 || no == 0 {
		t.Fatalf("covered %d, not covered %d: the test exercises one side only", yes, no)
	}
}

func TestJoinKeysAndCoversAll(t *testing.T) {
	r := rand.New(rand.NewPCG(55, 56))
	ps := NewPaths()
	var dst []Key
	for range 500 {
		a := Antichain(ps, randKeys(r, ps, 1+r.IntN(40)))
		b := Antichain(ps, randKeys(r, ps, 1+r.IntN(40)))
		dst = JoinKeys(ps, dst, a, b)
		if !CoversAll(ps, dst, a) || !CoversAll(ps, dst, b) {
			t.Fatal("the join does not cover its operands")
		}
		for _, k := range dst {
			if !slices.Contains(a, k) && !slices.Contains(b, k) {
				t.Fatalf("the join holds %x, in neither operand", k)
			}
		}
		if CoversAll(ps, a, dst) != CoversAll(ps, a, b) {
			t.Fatal("a covers a ∪ b exactly when it covers b")
		}
	}
}

// TestKeyPacking pins the bit layout of api.go, which the cache files of D44
// will depend on.
func TestKeyPacking(t *testing.T) {
	ps := NewPaths()
	p := walk(ps, F(3), L).Path()
	k := MakeKey(255, p, KeyFlags{GE: true, Counts: true})
	if k.Input() != 255 || k.Path() != p || !k.GE() || k.Flags() != (KeyFlags{GE: true, Counts: true}) {
		t.Fatalf("round trip: %x", k)
	}
	k2 := k.WithPath(Root, false)
	if k2.Input() != 255 || k2.Path() != Root || k2.GE() || !k2.Flags().Counts {
		t.Fatalf("WithPath: %x", k2)
	}
}
