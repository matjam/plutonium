package sched

import (
	"testing"

	"github.com/matjam/plutonium/internal/escape2/eir"
)

// TestDeferral checks D49's scheduling side: a call of a func-value input is
// deferred, its function gets no edge to its candidates, a caller depends
// on the literal it passes, passing an input on re-defers it, and an
// argument of unknown origin depends on the deferred call's candidates.
func TestDeferral(t *testing.T) {
	pkg := typecheck(t, `
func iter(yield func(int) bool) { yield(1) }
func pass(y func(int) bool) { iter(y) }
func other(int) bool { return true }
`)
	pr := newProg()
	iterF := lookupFunc(t, pkg, "iter")
	it := pr.decl(iterF)
	ps := pr.decl(lookupFunc(t, pkg, "pass"))
	other := pr.decl(lookupFunc(t, pkg, "other"))
	lit := pr.fn("body", eir.FuncKey{}, other.f.Sig)
	// iter calls its input.
	yc := &eir.Call{Kind: eir.CallFunc, Sig: other.f.Sig, Target: it.input(0), Results: []eir.Reg{it.reg()}}
	it.call(yc)
	// pass gives its input to iter.
	ps.callStatic(it, ps.input(0))
	main := pr.fn("main", eir.FuncKey{}, nil)
	main.f.External = true
	main.f.Facts.FuncValues = []eir.FuncID{other.f.ID}
	l := main.add(eir.Instr{Op: eir.OpClosure, Callee: lit.f.ID, Obj: eir.NoObj}, true)
	main.callStatic(it, l)
	main.callStatic(ps, l)
	m2 := pr.fn("main2", eir.FuncKey{}, nil)
	m2.f.External = true
	m2.callStatic(it, m2.load(m2.heap(), other.f.Sig))

	f := stageB(pr.p)
	d := newDeferral(pr.p, f.c, f.c.flow)
	if !d.deferred(it.f, yc) {
		t.Fatalf("iter's call of its input is not deferred")
	}
	if got := d.inputs[ps.f.ID][0]; len(got) != 1 || got[0].call != yc {
		t.Errorf("pass's input 0 stands for %v, want iter's call (re-deferred)", got)
	}
	g := buildGraphD(pr.p, f.rule, d)
	for _, x := range g.deps[it.f.ID] {
		if x == lit.f.ID || x == other.f.ID {
			t.Errorf("iter depends on %d: a deferred call has no edges in its function", x)
		}
	}
	if got := d.deps(main.f); !sameIDs(got, idsOf(lit)) {
		t.Errorf("main depends on %v, want only the literal it passes %v", got, idsOf(lit))
	}
	// main2 passes a value loaded from memory: the deferred call's
	// candidates (its flow set: everything passed to iter's input, or the
	// signature's func values where unknown).
	got := d.deps(m2.f)
	has := map[eir.FuncID]bool{}
	for _, x := range got {
		has[x] = true
	}
	if !has[other.f.ID] {
		t.Errorf("main2 depends on %v, want the deferred call's candidates (other among them)", got)
	}
	if len(d.deps(ps.f)) != 0 {
		t.Errorf("pass depends on %v, want nothing: it passes its own input on", d.deps(ps.f))
	}
}
