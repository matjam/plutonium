// Ported from internal/escape2/domain/sets_test.go at the go-cutover tag.

package domain

import (
	"pluto/slices"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/testpanic"
	"compiler/internal/testrand"
	"compiler/pu"
)

// model is the reference a set is checked against: a sorted slice without
// duplicates.
func model(xs []uint64) []uint64 {
	m := compat.Clone(xs)
	slices.Sort(m)
	return slices.Compact(m)
}

func modelUnion(a, b []uint64) []uint64 { return model(compat.Append(compat.Clone(a), b...)) }

func modelDiff(a, b []uint64) []uint64 {
	var out []uint64
	for _, x := range a {
		if _, ok := slices.BinarySearch(b, x); !ok {
			out = compat.Append(out, x)
		}
	}
	return out
}

func randSet(r *testrand.Rand, universe uint64) []uint64 {
	n := r.IntN(12)
	if r.IntN(50) == 0 {
		n = blockWords/4 + r.IntN(blockWords) // a set with a block of its own
	}
	xs := compat.Make[uint64](n)
	for i := range xs {
		xs[i] = r.Uint64N(universe)
	}
	return xs
}

// mustPanic runs f in a child process (Plutonium has no recover) and fails
// the test when f returns.
func mustPanic(t *testing.T, what string, f func()) {
	t.Helper()
	if !testpanic.Panics(f) {
		t.Fatalf("%s: no panic", what)
	}
}

func TestSetsBasic(t *testing.T) {
	s := NewSets(NewArena())
	if s.Intern(nil) != Empty || s.Len(Empty) != 0 || s.Elems(Empty) != nil || s.Has(Empty, 0) {
		t.Fatal("the empty set")
	}
	a := s.Intern([]uint64{5, 1, 5, 3})
	if got := s.Elems(a); !slices.Equal(got, []uint64{1, 3, 5}) {
		t.Fatalf("Elems %v", got)
	}
	if b := s.Intern([]uint64{3, 1, 5}); b != a {
		t.Fatal("equal sets, different IDs")
	}
	if !s.Has(a, 3) || s.Has(a, 4) || s.Has(a, 6) || s.Has(a, 0) {
		t.Fatal("Has")
	}
	b := s.Intern([]uint64{2, 3})
	u := s.Union(a, b)
	if got := s.Elems(u); !slices.Equal(got, []uint64{1, 2, 3, 5}) {
		t.Fatalf("Union %v", got)
	}
	if s.Union(b, a) != u || s.Union(a, Empty) != a || s.Union(Empty, a) != a || s.Union(a, a) != a {
		t.Fatal("Union identities")
	}
	if s.Union(u, a) != u {
		t.Fatal("Union with a subset is not the superset")
	}
	d := s.Diff(u, b)
	if d != a && !slices.Equal(s.Elems(d), []uint64{1, 5}) {
		t.Fatalf("Diff %v", s.Elems(d))
	}
	if s.Diff(a, a) != Empty || s.Diff(Empty, a) != Empty || s.Diff(a, Empty) != a {
		t.Fatal("Diff identities")
	}
	if s.Diff(a, s.Intern([]uint64{9})) != a {
		t.Fatal("Diff of a disjoint set is not the set")
	}
}

// TestPropSetsExact runs random operations against the model: every result
// has exactly the model's elements, and two IDs are equal exactly when their
// sets are. The memo is shrunk so that its entries are overwritten often.
func TestPropSetsExact(t *testing.T) {
	r := testrand.New(21, 22)
	for _, universe := range []uint64{8, 64, 1 << 40} {
		s := NewSets(NewArena()).(*sets)
		s.memo = s.memo[:4]
		ids := compat.Append([]SetID(nil), Empty)
		ms := [][]uint64{nil}
		add := func(id SetID, m []uint64) {
			if got := s.Elems(id); !slices.Equal(got, m) || s.Len(id) != len(m) {
				t.Fatalf("set %d: elems %v, want %v", id, got, m)
			}
			ids = compat.Append(ids, id)
			ms = compat.Append(ms, m)
		}
		for range 5000 {
			i, j := r.IntN(len(ids)), r.IntN(len(ids))
			switch r.IntN(3) {
			case 0:
				xs := randSet(&r, universe)
				m := model(xs)
				add(s.Intern(xs), m)
			case 1:
				add(s.Union(ids[i], ids[j]), modelUnion(ms[i], ms[j]))
			case 2:
				add(s.Diff(ids[i], ids[j]), modelDiff(ms[i], ms[j]))
			}
			x := r.Uint64N(universe)
			_, want := slices.BinarySearch(ms[i], x)
			if s.Has(ids[i], x) != want {
				t.Fatalf("Has(%v, %d) != %v", ms[i], x, want)
			}
		}
		// Hash-consing: equal IDs exactly for equal sets.
		for a := range ids {
			for b := a + 1; b < len(ids) && b < a+200; b++ {
				if (ids[a] == ids[b]) != slices.Equal(ms[a], ms[b]) {
					t.Fatalf("ids %d %d for sets %v %v", ids[a], ids[b], ms[a], ms[b])
				}
			}
		}
	}
}

// TestSetsReset: an ID from before a Reset of the table or of its arena
// panics; the table works afterwards.
func TestSetsReset(t *testing.T) {
	a := NewArena()
	s := NewSets(a)
	old := s.Intern([]uint64{1, 2})
	s.Reset()
	mustPanic(t, "Elems after Sets.Reset", func() { s.Elems(old) })
	mustPanic(t, "Union after Sets.Reset", func() { s.Union(old, Empty) })
	x := s.Intern([]uint64{1, 2})
	if got := s.Elems(x); !slices.Equal(got, []uint64{1, 2}) {
		t.Fatalf("after Reset: %v", got)
	}
	a.Reset()
	mustPanic(t, "Len after Arena.Reset", func() { s.Len(x) })
	mustPanic(t, "Has after Arena.Reset", func() { s.Has(x, 1) })
	mustPanic(t, "Diff after Arena.Reset", func() { s.Diff(x, x) })
	y := s.Intern([]uint64{7})
	if !s.Has(y, 7) || s.Len(y) != 1 {
		t.Fatal("after Arena.Reset")
	}
	// An ID of another generation never names a set of this one, even
	// after the generation counter wraps.
	for range 300 {
		s.Reset()
		z := s.Intern([]uint64{3})
		if z == y {
			t.Fatal("an ID was reused across a Reset")
		}
		y = z
	}
}

// TestCheckedArenaPoisons: with a checked arena, a slice of a set kept past
// either Reset reads poison, not a later set.
func TestCheckedArenaPoisons(t *testing.T) {
	for _, which := range []string{"sets", "arena"} {
		a := NewCheckedArena()
		s := NewSets(a)
		small := s.Elems(s.Intern([]uint64{1, 2, 3}))
		bigSet := compat.Make[uint64](blockWords)
		for i := range bigSet {
			bigSet[i] = uint64(i)
		}
		big := s.Elems(s.Intern(bigSet))
		raw := a.Alloc(3)
		raw[0] = 42
		if which == "sets" {
			s.Reset()
			a.Reset()
		} else {
			a.Reset()
			s.Reset()
		}
		s.Intern([]uint64{1, 2, 3})
		for _, kept := range [][]uint64{small, big, raw} {
			for _, x := range kept {
				if x != PoisonWord {
					t.Fatalf("%s reset: a kept slice reads %x", which, x)
				}
			}
		}
	}
}

func TestArena(t *testing.T) {
	a := NewArena()
	if a.Alloc(0) != nil {
		t.Fatal("Alloc(0)")
	}
	mustPanic(t, "Alloc(-1)", func() { a.Alloc(-1) })
	x := a.Alloc(10)
	for i := range x {
		x[i] = 7
	}
	y := a.Alloc(chunkWords) // a chunk of its own
	if len(y) != chunkWords || cap(x) != 10 {
		t.Fatal("sizes")
	}
	for range 3 * chunkWords / 100 {
		a.Alloc(100)
	}
	peak := a.Peak()
	if a.Bytes() != peak || peak < 4*chunkWords*8 {
		t.Fatalf("Bytes %d Peak %d", a.Bytes(), peak)
	}
	a.Reset()
	if a.Peak() != peak || a.Bytes() >= peak {
		t.Fatalf("after Reset: Bytes %d Peak %d", a.Bytes(), a.Peak())
	}
	// Reused memory is zeroed.
	for _, w := range a.Alloc(10) {
		if w != 0 {
			t.Fatal("reused memory not zeroed")
		}
	}
	// Reuse: the same work again holds no more.
	for range 3 * chunkWords / 100 {
		a.Alloc(100)
	}
	if a.Peak() != peak {
		t.Fatalf("peak grew on reuse: %d to %d", peak, a.Peak())
	}
}

// TestSetsBounded: the same work in every SCC holds the same memory: Reset
// reuses blocks and tables rather than growing.
func TestSetsBounded(t *testing.T) {
	a := NewArena()
	s := NewSets(a)
	work := func() {
		r := testrand.New(31, 32)
		ids := compat.Append([]SetID(nil), Empty)
		for range 3000 {
			id := s.Intern(randSet(&r, 1000))
			ids = compat.Append(ids, id, s.Union(id, ids[r.IntN(len(ids))]))
		}
	}
	work()
	first, firstArena := s.Bytes(), a.Peak()
	for range 5 {
		s.Reset()
		a.Reset()
		work()
	}
	if s.Bytes() != first || a.Peak() != firstArena {
		t.Fatalf("memory grew across Resets: sets %d to %d, arena peak %d to %d", first, s.Bytes(), firstArena, a.Peak())
	}
}

// TestSetsDeterministic: two tables given the same calls make the same IDs.
func TestSetsDeterministic(t *testing.T) {
	run := func() []SetID {
		s := NewSets(NewArena())
		r := testrand.New(41, 42)
		ids := compat.Append([]SetID(nil), Empty)
		for range 2000 {
			id := s.Intern(randSet(&r, 100))
			ids = compat.Append(ids, id, s.Union(id, ids[r.IntN(len(ids))]), s.Diff(ids[r.IntN(len(ids))], id))
		}
		return ids
	}
	if !slices.Equal(run(), run()) {
		t.Fatal("the same calls made different IDs")
	}
}

// otherArena is an Arena of another implementation, which a Sets table must
// work on too (without seeing its resets).
type otherArena struct{ Arena }

func TestSetsOtherArena(t *testing.T) {
	s := NewSets(pu.New(otherArena{NewArena()}))
	a := s.Intern([]uint64{4, 2})
	if !slices.Equal(s.Elems(s.Union(a, s.Intern([]uint64{3}))), []uint64{2, 3, 4}) {
		t.Fatal("union on another arena")
	}
}
