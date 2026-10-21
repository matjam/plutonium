package domain

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// F is field step i, for tests.
func F(i int) Step { return Step(i) }

const L = StepLoad
const E = StepElem

// walk extends Root by steps, as a key of input 0, and returns the key.
func walk(ps Paths, steps ...Step) Key {
	k := MakeKey(0, Root, KeyFlags{})
	for _, s := range steps {
		k = ExtendKey(ps, k, s)
	}
	return k
}

func TestExtendWithinLimits(t *testing.T) {
	ps := NewPaths()
	k := walk(ps, F(1), L, F(2), E, L)
	if k.GE() {
		t.Fatal("a path within the limits was cut")
	}
	if got, want := ps.Steps(k.Path()), []Step{F(1), L, F(2), E, L}; !slices.Equal(got, want) {
		t.Fatalf("steps %v, want %v", got, want)
	}
	if ps.Loads(k.Path()) != 2 {
		t.Fatalf("loads %d, want 2", ps.Loads(k.Path()))
	}
	// Interning: the same steps give the same path.
	if k2 := walk(ps, F(1), L, F(2), E, L); k2 != k {
		t.Fatalf("same steps, keys %x and %x", k, k2)
	}
	if walk(ps, F(1), L, F(3)).Path() == walk(ps, F(1), L, F(2)).Path() {
		t.Fatal("different fields share a path")
	}
}

func TestExtendFieldNest(t *testing.T) {
	ps := NewPaths()
	two := walk(ps, F(1), F(2))
	// A third offset step keeps the path of the full segment, which
	// stands for every offset inside it.
	for _, s := range []Step{F(3), E, F(0)} {
		if k := ExtendKey(ps, two, s); k != two {
			t.Errorf("extending a full segment by %d gave %x, want %x", s, k, two)
		}
	}
	// After a load the segment is empty again.
	k := walk(ps, F(1), F(2), F(3), L, F(4), F(5))
	if got, want := ps.Steps(k.Path()), []Step{F(1), F(2), L, F(4), F(5)}; !slices.Equal(got, want) || k.GE() {
		t.Fatalf("steps %v ge %v, want %v", got, k.GE(), want)
	}
}

func TestExtendCut(t *testing.T) {
	ps := NewPaths()
	// A fourth load cuts back through the second load.
	k := walk(ps, F(1), L, F(2), L, L, L)
	if got, want := ps.Steps(k.Path()), []Step{F(1), L, F(2), L}; !slices.Equal(got, want) || !k.GE() {
		t.Fatalf("steps %v ge %v, want %v and GE", got, k.GE(), want)
	}
	// So does a ninth step.
	k = walk(ps, F(1), L, F(2), F(3), L, F(4), F(5), L)
	if k.GE() {
		t.Fatal("eight steps were cut")
	}
	k = ExtendKey(ps, k, F(6))
	if got, want := ps.Steps(k.Path()), []Step{F(1), L, F(2), F(3), L}; !slices.Equal(got, want) || !k.GE() {
		t.Fatalf("ninth step: steps %v ge %v, want %v and GE", got, k.GE(), want)
	}
	// A GE key is not extended.
	if k2 := ExtendKey(ps, k, L); k2 != k {
		t.Fatal("a GE key was extended")
	}
}

func TestCutAndPrefix(t *testing.T) {
	ps := NewPaths()
	k := walk(ps, F(1), L, F(2), L)
	c := ps.Cut(k.Path())
	if got, want := ps.Steps(c), []Step{F(1), L}; !slices.Equal(got, want) {
		t.Fatalf("Cut steps %v, want %v", got, want)
	}
	if !ps.HasPrefix(k.Path(), c) || !ps.HasPrefix(k.Path(), k.Path()) || !ps.HasPrefix(k.Path(), Root) {
		t.Fatal("HasPrefix misses a prefix")
	}
	if ps.HasPrefix(c, k.Path()) {
		t.Fatal("HasPrefix of a longer path")
	}
	if ps.HasPrefix(k.Path(), walk(ps, F(2)).Path()) {
		t.Fatal("HasPrefix of an unrelated path")
	}
	noLoad := walk(ps, F(1), E).Path()
	if ps.Cut(noLoad) != noLoad {
		t.Fatal("Cut of a path without loads changed it")
	}
}

func TestPathMatches(t *testing.T) {
	ps := NewPaths()
	full := walk(ps, F(1), F(2), L).Path()
	for _, c := range []struct {
		w    []Step
		ge   bool
		want bool
	}{
		{[]Step{F(1), F(2), L}, false, true},
		{[]Step{F(1), F(2), F(3), E, L}, false, true}, // absorbed by the full segment
		{[]Step{F(1), F(2)}, false, false},
		{[]Step{F(1), F(2), L, F(0)}, false, false},
		{[]Step{F(1), F(2), L, F(0)}, true, true},
		{[]Step{F(1), F(3), L}, true, false},
	} {
		if got := PathMatches(ps, full, c.ge, c.w); got != c.want {
			t.Errorf("PathMatches(%v, ge=%v, %v) = %v, want %v", ps.Steps(full), c.ge, c.w, got, c.want)
		}
	}
}

func TestExtendInvalidStep(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("no panic for an invalid step")
		}
	}()
	NewPaths().Extend(Root, -3)
}

// randStep returns a random step from a small alphabet, so that walks
// share prefixes.
func randStep(r *rand.Rand) Step {
	switch n := r.IntN(6); n {
	case 0, 1:
		return L
	case 2:
		return E
	default:
		return F(n - 3)
	}
}

func randWord(r *rand.Rand, max int) []Step {
	w := make([]Step, r.IntN(max+1))
	for i := range w {
		w[i] = randStep(r)
	}
	return w
}

func randOffset(r *rand.Rand) Step {
	if r.IntN(4) == 0 {
		return E
	}
	return F(r.IntN(3))
}

// sample returns a random concrete word that key k stands for: its steps,
// with random offsets inside full segments and, for a GE key, a random
// suffix.
func sample(r *rand.Rand, ps Paths, p PathID, ge bool) []Step {
	var w []Step
	seg := 0
	pad := func() {
		if seg == MaxFieldNest {
			for range r.IntN(3) {
				w = append(w, randOffset(r))
			}
		}
	}
	for _, s := range ps.Steps(p) {
		if s == L {
			pad()
			seg = 0
		} else {
			seg++
		}
		w = append(w, s)
	}
	pad()
	if ge {
		w = append(w, randWord(r, 5)...)
	}
	return w
}

// TestPropExtendWidens is invariant I8 for Extend: for every word w a key
// stands for and every step s, w+s is one the extended key stands for; and
// when Extend neither cuts nor absorbs, it stands for nothing else.
func TestPropExtendWidens(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	ps := NewPaths()
	for range 20000 {
		k := walk(ps, randWord(r, 14)...)
		s := randStep(r)
		k2 := ExtendKey(ps, k, s)
		for range 4 {
			w := sample(r, ps, k.Path(), k.GE())
			ws := append(slices.Clip(w), s)
			if !PathMatches(ps, k2.Path(), k2.GE(), ws) {
				t.Fatalf("%v (ge %v) + %d = %v (ge %v) misses %v", ps.Steps(k.Path()), k.GE(), s, ps.Steps(k2.Path()), k2.GE(), ws)
			}
		}
		if !k.GE() && !k2.GE() && k2 != k && !fullSegment(ps.Steps(k2.Path())) {
			for range 4 {
				v := sample(r, ps, k2.Path(), false)
				if len(v) == 0 || v[len(v)-1] != s || !PathMatches(ps, k.Path(), false, v[:len(v)-1]) {
					t.Fatalf("exact extension %v + %d stands for %v", ps.Steps(k.Path()), s, v)
				}
			}
		}
	}
}

// fullSegment reports whether the last segment of steps is full.
func fullSegment(steps []Step) bool {
	n := 0
	for i := len(steps) - 1; i >= 0 && steps[i] != L; i-- {
		n++
	}
	return n == MaxFieldNest
}

// TestPropWalkCovers: a key built by walking a concrete word stands for it,
// whatever the limits cut.
func TestPropWalkCovers(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	ps := NewPaths()
	for range 20000 {
		w := randWord(r, 16)
		k := walk(ps, w...)
		if !PathMatches(ps, k.Path(), k.GE(), w) {
			t.Fatalf("walk of %v = %v (ge %v) does not stand for it", w, ps.Steps(k.Path()), k.GE())
		}
	}
}

// TestPropCutWidens is I8 for Cut, the cap's widening.
func TestPropCutWidens(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	ps := NewPaths()
	for range 20000 {
		k := walk(ps, randWord(r, 14)...)
		c := ps.Cut(k.Path())
		if !ps.HasPrefix(k.Path(), c) || ps.Loads(c) > 1 {
			t.Fatalf("Cut(%v) = %v", ps.Steps(k.Path()), ps.Steps(c))
		}
		for range 4 {
			w := sample(r, ps, k.Path(), k.GE())
			if !PathMatches(ps, c, true, w) {
				t.Fatalf("Cut(%v) = %v misses %v", ps.Steps(k.Path()), ps.Steps(c), w)
			}
		}
	}
}

// TestPropAppendWidens: re-rooting a key on another covers the
// concatenation of what they stand for.
func TestPropAppendWidens(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	ps := NewPaths()
	for range 20000 {
		a := walk(ps, randWord(r, 8)...)
		b := walk(ps, randWord(r, 8)...)
		k := AppendKey(ps, a, b.Path(), b.GE())
		for range 4 {
			w := append(sample(r, ps, a.Path(), a.GE()), sample(r, ps, b.Path(), b.GE())...)
			if !PathMatches(ps, k.Path(), k.GE(), w) {
				t.Fatalf("Append(%v ge %v, %v ge %v) = %v ge %v misses %v", ps.Steps(a.Path()), a.GE(), ps.Steps(b.Path()), b.GE(), ps.Steps(k.Path()), k.GE(), w)
			}
		}
	}
}

// TestPropCoversExact: Covers(a, b) holds exactly when every word b stands
// for is one a stands for. Soundness is checked on samples of b; for
// precision, when Covers fails a witness is b's own steps, or those followed
// by a load when b is GE.
func TestPropCoversExact(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 10))
	ps := NewPaths()
	var keys []Key
	for range 300 {
		k := walk(ps, randWord(r, 10)...)
		if r.IntN(3) == 0 {
			k = k.WithPath(k.Path(), true)
		}
		keys = append(keys, k)
		// Some prefixes, so that coverage happens often.
		if st := ps.Steps(k.Path()); len(st) > 0 {
			p := walk(ps, st[:r.IntN(len(st))]...)
			keys = append(keys, p.WithPath(p.Path(), true))
		}
	}
	covered := 0
	for _, a := range keys {
		for _, b := range keys {
			if Covers(ps, a, b) {
				covered++
				for range 3 {
					w := sample(r, ps, b.Path(), b.GE())
					if !PathMatches(ps, a.Path(), a.GE(), w) {
						t.Fatalf("Covers(%v ge %v, %v ge %v) but %v is not covered", ps.Steps(a.Path()), a.GE(), ps.Steps(b.Path()), b.GE(), w)
					}
				}
				continue
			}
			w := ps.Steps(b.Path())
			ok := !PathMatches(ps, a.Path(), a.GE(), w)
			if b.GE() {
				ok = ok || !PathMatches(ps, a.Path(), a.GE(), append(slices.Clip(w), L))
			}
			if !ok {
				t.Fatalf("not Covers(%v ge %v, %v ge %v), yet no witness", ps.Steps(a.Path()), a.GE(), ps.Steps(b.Path()), b.GE())
			}
		}
	}
	if covered < len(keys)*2 {
		t.Fatalf("only %d covering pairs: the test does not exercise Covers", covered)
	}
}

// TestPathsBounded: the interner holds the paths the limits allow over an
// alphabet and no more. Closing Root under Extend by every step of the
// alphabet finds them all; random walks of any length add none.
func TestPathsBounded(t *testing.T) {
	ps := NewPaths()
	alphabet := []Step{L, E, F(0), F(1), F(2)}
	for p := 0; p < ps.Len(); p++ {
		for _, s := range alphabet {
			ps.Extend(PathID(p), s)
		}
	}
	n := ps.Len()
	r := rand.New(rand.NewPCG(11, 12))
	for range 200000 {
		walk(ps, randWord(r, 30)...)
	}
	if ps.Len() != n {
		t.Fatalf("paths grew from %d to %d on random walks", n, ps.Len())
	}
	t.Logf("%d paths over 5 steps, %d bytes", n, PathBytes(ps))
}

func ExamplePathMatches() {
	ps := NewPaths()
	k := walk(ps, F(0), L, F(1), F(2), F(3))
	fmt.Println(ps.Steps(k.Path()), k.GE())
	fmt.Println(PathMatches(ps, k.Path(), k.GE(), []Step{F(0), L, F(1), F(2), F(3)}))
	// Output:
	// [0 -2 1 2] false
	// true
}
