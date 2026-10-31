package sched

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/solve"
	"compiler/internal/escape2/summary"
	"compiler/internal/types2"
	"compiler/pu"
)

// TestJoinConj checks the filter a join through an interface gives a
// member's entry of its receiver that has a filter of its own further down
// its path (spec 18.3): the receiver's, with the member's own as its
// conjunct, so the entry stands neither for every receiver nor for every
// destination of the member's own call. An entry without a filter of its
// own takes the receiver's alone.
func TestJoinConj(t *testing.T) {
	pkg := typecheck(t, `
type J interface{ N(p *int) }
type B struct{ j J }
func (b *B) M(p *int) { b.j.N(p) }
`)
	pb := types2.NewPointer(lookupType(t, pkg, "B"))
	pr := newProg()
	bm := pr.decl(method(t, pb, "M"))

	ps := domain.NewPaths()
	fs := ps.Filters()
	own := fs.Intern(domain.One(domain.AtomIs, types2.Typ[types2.Int]))
	load0, _ := ps.Extend(domain.Root, domain.StepLoad)
	// The member keeps its receiver's loaded pointer when it is of type
	// int (a filter past the receiver's load), and its receiver.
	k := domain.MakeKey(0, load0, domain.KeyFlags{}).WithFilterRaw(own, 1)
	k0 := domain.MakeKey(0, domain.Root, domain.KeyFlags{})
	sums := map[eir.FuncID]*summary.Summary{
		bm.f.ID: pu.New(summary.Summary{Inputs: 2, Esc: compat.Append[domain.Key](nil, k, k0)}),
	}
	s := join(pr.p, ps, funcIDs(bm.f.ID), true, false, func(id eir.FuncID) *summary.Summary { return sums[id] })
	fb := solve.RecvFilterID(fs, pr.p, bm.f.ID, true)
	if fb == 0 {
		t.Fatalf("B.M has no receiver filter")
	}
	if len(s.Esc) != 2 {
		t.Fatalf("the join has %d keys; want 2", len(s.Esc))
	}
	for _, e := range s.Esc {
		id, pos := e.Filter()
		in, rel := fs.InnerOf(id)
		switch {
		case pos != 0 || fs.Outer(id) != fb:
			t.Errorf("a joined key has filter %d at %d; want B's (%d) at 0", fs.Outer(id), pos, fb)
		case e.Path() == load0 && (in != own || rel != 1):
			t.Errorf("the loaded pointer's key has conjunct %d at %d; want its own filter (%d) at 1", in, rel, own)
		case e.Path() == domain.Root && in != 0:
			t.Errorf("the receiver's key has conjunct %d at %d; want none", in, rel)
		}
	}
}

// TestJoinInSolveOverlay checks that a join read in a parallel solve
// (worker.Summary) makes its paths in an overlay, never in the interner,
// which the round's solves read at once: a value receiver's key is a load
// past the data word then its path (loadKey), a path the interner does not
// have. The join holds it, so it is the solve's own, made in the solve's
// overlay, and not cached; once the path is interned, the join is cached.
func TestJoinInSolveOverlay(t *testing.T) {
	pkg := typecheck(t, `
type I interface{ M() }
type V struct{ a, b *int }
func (v V) M() {}
type W struct{ a, b *int }
func (w W) M() {}
`)
	pr := newProg()
	vm := pr.decl(method(t, lookupType(t, pkg, "V"), "M"))
	wm := pr.decl(method(t, lookupType(t, pkg, "W"), "M"))
	if !solve.ValueRecvByData(pr.p, vm.f.ID) {
		t.Fatalf("V.M's receiver is not given by its data word")
	}
	ps := domain.NewPaths()
	f1, _ := ps.Extend(domain.Root, 1)
	// Each member keeps its receiver's second field.
	k := domain.MakeKey(0, f1, domain.KeyFlags{})
	sums := map[eir.FuncID]*summary.Summary{
		vm.f.ID: pu.New(summary.Summary{Inputs: 1, Esc: compat.Append[domain.Key](nil, k)}),
		wm.f.ID: pu.New(summary.Summary{Inputs: 1, Esc: compat.Append[domain.Key](nil, k)}),
	}
	sum := func(id eir.FuncID) *summary.Summary { return sums[id] }
	v := newVirtuals(pr.p, ps)
	vid := v.of(funcIDs(vm.f.ID, wm.f.ID), true, false)[0]
	i := int(vid) - len(pr.p.Funcs)
	ix := newPathIndex(ps)
	ov := newOverlay(ix)
	n := ps.Len()
	s := v.summaryIn(ov, vid, sum)
	if ps.Len() != n {
		t.Fatalf("the join made %d paths in the interner during a round", ps.Len()-n)
	}
	if !anyPath(s, ov.isOwn) {
		t.Fatalf("the join holds no path of the solve's overlay; want the load past the data word")
	}
	for _, e := range s.Esc {
		if st := ov.Steps(e.Path()); len(st) != 2 || st[0] != domain.StepLoad || st[1] != 1 {
			t.Errorf("a joined key has path %v; want [load 1]", st)
		}
	}
	if v.cache[i] != nil {
		t.Errorf("a join holding a path of the solve's own was cached")
	}
	// Once the overlay is committed, the join finds the path in the
	// interner and is cached.
	ov.commit()
	ov2 := newOverlay(ix)
	s = v.summaryIn(ov2, vid, sum)
	if anyPath(s, ov2.isOwn) || v.cache[i] == nil {
		t.Errorf("a join of interned paths was not cached")
	}
}
