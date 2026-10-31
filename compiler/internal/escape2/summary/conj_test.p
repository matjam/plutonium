package summary

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/summary/summarytest"
	"compiler/internal/types2"
)

// TestConjFilter: a filter with a conjunct further down the path
// (domain.Filters.Conj), as a join gives a member's entry filtered both at
// its receiver and at a pointer of its own call through an interface
// (spec 18.3). The conjunction is narrower than its outer filter alone,
// keeps its conjunct through a union with a filter of the same conjunct,
// and stays apart from an entry of another conjunct when Widen merges the
// filters of one key.
func TestConjFilter(t *testing.T) {
	ps := summarytest.NewPaths()
	fs := ps.Filters()
	a := fs.Intern(domain.One(domain.AtomIs, types2.Typ[types2.Int]))
	b := fs.Intern(domain.One(domain.AtomIs, types2.Typ[types2.String]))
	c := fs.Intern(domain.One(domain.AtomIs, types2.Typ[types2.Bool]))
	ab := fs.Conj(a, b, 2)
	if ab == 0 || ab == a || fs.Conj(a, b, 2) != ab {
		t.Fatalf("Conj(a, b, 2) = %d (a %d); want one new filter", ab, a)
	}
	if in, rel := fs.InnerOf(ab); in != b || rel != 2 {
		t.Fatalf("the conjunct is %d at %d; want %d at 2", in, rel, b)
	}
	if f, _ := fs.Get(ab); len(f.Atoms) != 1 || f.Atoms[0].T != types2.Typ[types2.Int] {
		t.Fatalf("the conjunction's atoms are not a's")
	}
	if fs.Conj(ab, c, 1) != ab || fs.Conj(a, b, 0) != a || fs.Conj(a, 0, 2) != a {
		t.Errorf("Conj of a conjunction, at 0, or of no filter is not its outer filter")
	}
	// A conjunct has no conjunct of its own: a chain of them, one per
	// call through an interface on the way, is cut after the first.
	if in, rel := fs.InnerOf(fs.Conj(c, ab, 1)); in != a || rel != 1 {
		t.Errorf("Conj(c, ab, 1) has conjunct %d at %d; want a (%d) at 1, without b", in, rel, a)
	}
	ac := fs.Conj(a, c, 2)
	ab3 := fs.Conj(a, b, 3)
	if ac == ab || ab3 == ab {
		t.Errorf("conjunctions of other conjuncts or distances are one filter")
	}

	k := key(ps, 0, false, 0, L, 1, L, 2)
	ka, kab, kac := k.WithFilterRaw(a, 1), k.WithFilterRaw(ab, 1), k.WithFilterRaw(ac, 1)
	if !domain.Covers(ps, ka, kab) {
		t.Errorf("the outer filter alone does not cover the conjunction")
	}
	if domain.Covers(ps, kab, ka) || domain.Covers(ps, kab, kac) || domain.Covers(ps, kab, k.WithFilterRaw(ab3, 1)) {
		t.Errorf("the conjunction covers a wider key")
	}

	bb := fs.Conj(c, b, 2)
	if u := fs.Union(ab, bb); u == 0 {
		t.Errorf("the union of two conjunctions of one conjunct is no filter")
	} else if in, rel := fs.InnerOf(u); in != b || rel != 2 {
		t.Errorf("the union of two conjunctions of one conjunct has conjunct %d at %d; want %d at 2", in, rel, b)
	}
	// Conjuncts at one distance: the union of them; at two, none.
	bc := fs.Union(b, c)
	if in, rel := fs.InnerOf(fs.Union(ab, ac)); in != bc || rel != 2 {
		t.Errorf("the union of conjunctions of b and c has conjunct %d at %d; want b|c (%d) at 2", in, rel, bc)
	}
	if ku := k.WithFilterRaw(fs.Union(ab, ac), 1); !domain.Covers(ps, ku, kab) || !domain.Covers(ps, ku, kac) {
		t.Errorf("the union of two conjunctions does not cover them")
	}
	if in, _ := fs.InnerOf(fs.Union(ab, ab3)); in != 0 {
		t.Errorf("the union of conjunctions at two distances keeps a conjunct")
	}

	// Widen merges the entries of one key and one distance, with the
	// union of their conjuncts, and keeps those of another apart.
	s := &Summary{Inputs: 1, Esc: []domain.Key{kab, k.WithFilterRaw(bb, 1), kac, k.WithFilterRaw(ab3, 1)}}
	Widen(ps, s)
	if len(s.Esc) != 2 {
		t.Fatalf("Widen left %d keys; want 2", len(s.Esc))
	}
	for _, e := range s.Esc {
		id, pos := e.Filter()
		if pos != 1 {
			t.Errorf("a widened key has its filter at %d; want 1", pos)
		}
		switch in, rel := fs.InnerOf(id); {
		case rel == 2 && in == bc:
		case rel == 3 && in == b:
		default:
			t.Errorf("a widened key has conjunct %d at %d; want b|c at 2 or b at 3", in, rel)
		}
	}

	// Canonical renames the conjuncts with the filters.
	rename := fs.Canonical()
	if rename != nil {
		ab, b = rename[ab], rename[b]
	}
	if in, rel := fs.InnerOf(ab); in != b || rel != 2 {
		t.Errorf("after Canonical, the conjunct is %d at %d; want %d at 2", in, rel, b)
	}
}

// TestWidenInternsOneUnion: Widen merges the filters of a key's entries
// (D51) with one union, interned once (domain.Filters.UnionAll), with the
// union of their conjuncts. Folded pairwise, each partial union was a
// filter of its own: the joins of the compiler's self-build filled the
// table (domain.MaxFilters), and every later filter became none, so that
// what a caller passed to fmt.Errorf was taken for kept through the join
// of every String method.
func TestWidenInternsOneUnion(t *testing.T) {
	ps := summarytest.NewPaths()
	fs := ps.Filters()
	b := fs.Intern(domain.One(domain.AtomIs, types2.Typ[types2.String]))
	k := key(ps, 0, false, 0, L, 1, L, 2)
	var ty types2.Type = types2.Typ[types2.Int]
	s := &Summary{Inputs: 1}
	var orig []domain.Key
	for i := 0; i < 100; i++ {
		ty = types2.NewPointer(ty)
		f := fs.Conj(fs.Intern(domain.One(domain.AtomIs, ty)), b, 2)
		orig = compat.AppendPlain(orig, k.WithFilterRaw(f, 1))
		s.Esc = compat.AppendPlain(s.Esc, k.WithFilterRaw(f, 1))
	}
	before := fs.Intern(domain.One(domain.AtomIs, types2.Typ[types2.Bool]))
	Widen(ps, s)
	after := fs.Intern(domain.One(domain.AtomIs, types2.Typ[types2.Float64]))
	if n := int(after) - int(before) - 1; n > 2 {
		t.Errorf("Widen interned %d filters; want the union and its conjunct at most", n)
	}
	if len(s.Esc) != 1 {
		t.Fatalf("Widen left %d keys; want 1", len(s.Esc))
	}
	id, pos := s.Esc[0].Filter()
	if g, _ := fs.Get(id); pos != 1 || len(g.Atoms) != 100 {
		t.Fatalf("the merged key has a filter of %d atoms at %d; want 100 at 1", len(g.Atoms), pos)
	}
	if in, rel := fs.InnerOf(id); in != b || rel != 2 {
		t.Errorf("the merged key has conjunct %d at %d; want %d at 2", in, rel, b)
	}
	for _, o := range orig {
		if !domain.Covers(ps, s.Esc[0], o) {
			t.Fatalf("the merged key does not cover an entry")
		}
	}
}
