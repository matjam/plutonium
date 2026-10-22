package sched

import (
	"testing"

	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/types2"
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

// TestCanRead checks the guard on the summaries a solve reads while D49 is
// on: final ones and the function's deps may be read; a member of the
// current SCC outside its deps may not, and reading one is an internal
// error.
func TestCanRead(t *testing.T) {
	var order []eir.FuncID
	withFakes(t, &order)
	saved := deferralOn
	deferralOn = true
	defer func() { deferralOn = saved }()
	pr := newProg()
	fs := map[string]*fbuild{}
	for _, n := range []string{"leaf", "a", "b", "c"} {
		fs[n] = pr.fn(n, eir.FuncKey{}, nil)
	}
	// a → b → c → a, a → leaf.
	fs["a"].callStatic(fs["b"])
	fs["a"].callStatic(fs["leaf"])
	fs["b"].callStatic(fs["c"])
	fs["c"].callStatic(fs["a"])
	inner := solveFunc
	var checked bool
	solveFunc = func(f *eir.Func, en solve.Env, ws *solve.Workspace) solve.Result {
		e := en.(*env)
		if f.ID == fs["a"].f.ID && !checked {
			checked = true
			for n, want := range map[string]bool{"leaf": true, "b": true, "c": false} {
				if got := e.CanRead(f, fs[n].f.ID); got != want {
					t.Errorf("CanRead(a, %s) = %v, want %v", n, got, want)
				}
			}
			func() {
				defer func() {
					if recover() == nil {
						t.Errorf("a read c's summary, outside its deps, without an internal error")
					}
				}()
				e.Summary(fs["c"].f.ID)
			}()
		}
		return inner(f, en, ws)
	}
	RunWith(pr.p, Options{})
	if !checked {
		t.Fatalf("a was not solved")
	}
}

// TestDeferralRangeBody checks deferral through a closure record: a range
// body calls the yield its record captured (by value, and by reference
// through a frame object), the function that makes the body re-defers it
// through its own input, and its caller depends only on the literal it
// passes.
func TestDeferralRangeBody(t *testing.T) {
	pkg := typecheck(t, `
func iter(yield func(int) bool) { yield(1) }
func All(yield func(int) bool) {}
func AllRef(yield func(int) bool) {}
func other(int) bool { return true }
`)
	pr := newProg()
	it := pr.decl(lookupFunc(t, pkg, "iter"))
	all := pr.decl(lookupFunc(t, pkg, "All"))
	allRef := pr.decl(lookupFunc(t, pkg, "AllRef"))
	other := pr.decl(lookupFunc(t, pkg, "other"))
	sig := other.f.Sig
	recT := types2.Typ[types2.UnsafePointer]
	lit := pr.fn("lit", eir.FuncKey{}, sig)
	body := pr.fn("body", eir.FuncKey{}, sig)
	body.f.Inputs = append(body.f.Inputs, eir.Input{Type: recT})
	body2 := pr.fn("body2", eir.FuncKey{}, sig)
	body2.f.Inputs = append(body2.f.Inputs, eir.Input{Type: recT})

	yc := &eir.Call{Kind: eir.CallFunc, Sig: sig, Target: it.input(0), Results: []eir.Reg{it.reg()}}
	it.call(yc)
	// body: yield captured by value, field 0 of its record.
	bc := &eir.Call{Kind: eir.CallFunc, Sig: sig, Args: []eir.Reg{body.input(0)}, Results: []eir.Reg{body.reg()}}
	bc.Target = body.load(body.field(body.input(1), 0), sig)
	body.call(bc)
	// body2: yield captured by reference.
	b2c := &eir.Call{Kind: eir.CallFunc, Sig: sig, Args: []eir.Reg{body2.input(0)}, Results: []eir.Reg{body2.reg()}}
	b2c.Target = body2.load(body2.load(body2.field(body2.input(1), 0), recT), sig)
	body2.call(b2c)
	// All makes body capturing its input and ranges over iter.
	cl := all.add(eir.Instr{Op: eir.OpClosure, Callee: body.f.ID, Args: []eir.Reg{all.input(0)}}, true)
	all.callStatic(it, cl)
	// AllRef stores its input into a frame object body2 captures.
	o := allRef.obj(eir.Object{Type: sig})
	a := allRef.addr(o)
	allRef.store(a, allRef.input(0), sig)
	cl2 := allRef.add(eir.Instr{Op: eir.OpClosure, Callee: body2.f.ID, Args: []eir.Reg{a}}, true)
	allRef.callStatic(it, cl2)
	main := pr.fn("main", eir.FuncKey{}, nil)
	main.f.External = true
	main.f.Facts.FuncValues = []eir.FuncID{other.f.ID}
	l := main.add(eir.Instr{Op: eir.OpClosure, Callee: lit.f.ID, Obj: eir.NoObj}, true)
	main.callStatic(all, l)
	main.callStatic(allRef, l)

	f := stageB(pr.p)
	d := newDeferral(pr.p, f.c, f.c.flow)
	for _, x := range []struct {
		f *fbuild
		c *eir.Call
	}{{body, bc}, {body2, b2c}} {
		if !d.deferred(x.f.f, x.c) {
			t.Errorf("%s's call of its captured yield is not deferred", x.f.f.Name)
		}
	}
	for _, x := range []struct {
		f, body *fbuild
		c       *eir.Call
	}{{all, body, bc}, {allRef, body2, b2c}} {
		has := false
		for _, o := range d.inputs[x.f.f.ID][0] {
			has = has || o.call == x.c
		}
		if !has {
			t.Errorf("%s's input 0 stands for %v, want %s's call (re-deferred through the capture)", x.f.f.Name, d.inputs[x.f.f.ID][0], x.body.f.Name)
		}
		if got := d.deps(x.f.f); !sameIDs(got, idsOf(x.body)) {
			t.Errorf("%s depends on %v for deferred entries, want only its body %v", x.f.f.Name, got, idsOf(x.body))
		}
	}
	if got := d.deps(main.f); !sameIDs(got, idsOf(lit)) {
		t.Errorf("main depends on %v, want only the literal it passes %v", got, idsOf(lit))
	}
	g := buildGraphD(pr.p, f.rule, d)
	for _, id := range []eir.FuncID{it.f.ID, body.f.ID, body2.f.ID, all.f.ID, allRef.f.ID} {
		for _, x := range g.deps[id] {
			if x == other.f.ID || x == lit.f.ID {
				t.Errorf("%s depends on %s", pr.p.Funcs[id].Name, pr.p.Funcs[x].Name)
			}
		}
	}
}
