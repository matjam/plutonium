package sched

import (
	"testing"

	"github.com/matjam/plutonium/internal/escape2/domain"
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
	hasOther := false
	for _, x := range g.deps[it.f.ID] {
		hasOther = hasOther || x == other.f.ID
	}
	if !hasOther {
		t.Errorf("iter depends on %v: a deferred call keeps its edges to its candidates (other), for its fallback", g.deps[it.f.ID])
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

// TestCanRead checks what a solve may read while D49 is on: final
// summaries and members of the current SCC, not a function of a later SCC;
// and that a member read outside the call graph's deps is still solved
// before its reader settles: the driver re-solves the solves that read a
// summary when it grows, whatever the graph says.
func TestCanRead(t *testing.T) {
	var order []eir.FuncID
	withFakes(t, &order)
	saved := deferralOn
	deferralOn = true
	defer func() { deferralOn = saved }()
	pr := newProg()
	fs := map[string]*fbuild{}
	for _, n := range []string{"leaf", "a", "b", "c", "main"} {
		fs[n] = pr.fn(n, eir.FuncKey{}, nil)
	}
	// a → b → c → a, a → leaf, main → a.
	fs["a"].callStatic(fs["b"])
	fs["a"].callStatic(fs["leaf"])
	fs["b"].callStatic(fs["c"])
	fs["c"].callStatic(fs["a"])
	fs["main"].callStatic(fs["a"])
	inner := solveFunc
	var checked bool
	solveFunc = func(f *eir.Func, en solve.Env, ws *solve.Workspace) solve.Result {
		e := en
		r := inner(f, en, ws)
		if f.ID == fs["b"].f.ID {
			// b passes nothing on: c's growth reaches a only through a's
			// own read of c.
			r.Summary.Esc = []domain.Key{domain.Key(f.ID)}
		}
		if f.ID == fs["c"].f.ID && e.Summary(fs["a"].f.ID) != nil {
			// A key c has only once a has a summary: it reaches a late.
			r.Summary.Esc = append(r.Summary.Esc, 1000)
		}
		if f.ID != fs["a"].f.ID {
			return r
		}
		if !checked {
			checked = true
			for n, want := range map[string]bool{"leaf": true, "b": true, "c": true, "main": false} {
				if got := e.CanRead(f, fs[n].f.ID); got != want {
					t.Errorf("CanRead(a, %s) = %v, want %v", n, got, want)
				}
			}
		}
		// a also reads c, which its call graph does not cover.
		if cs := e.Summary(fs["c"].f.ID); cs != nil {
			have := map[domain.Key]bool{}
			for _, k := range r.Summary.Esc {
				have[k] = true
			}
			for _, k := range cs.Esc {
				if !have[k] {
					r.Summary.Esc = append(r.Summary.Esc, k)
				}
			}
		}
		return r
	}
	res := RunWith(pr.p, Options{})
	if !checked {
		t.Fatalf("a was not solved")
	}
	have := map[domain.Key]bool{}
	for _, k := range res.Summaries[fs["a"].f.ID].Esc {
		have[k] = true
	}
	for _, k := range res.Summaries[fs["c"].f.ID].Esc {
		if !have[k] {
			t.Errorf("a's summary lacks %v of c's: a was not solved again after c, which it read, grew", k)
		}
	}
}

// TestDeferralCallbackArg checks the callback pattern: a call of a func
// input whose argument is a call's result (underIs's f(under(typ))) is
// deferred; what the argument holds is the solver's to check.
func TestDeferralCallbackArg(t *testing.T) {
	pkg := typecheck(t, `
func under(x *int) *int { return x }
func underIs(x *int, f func(*int) bool) bool { return f(under(x)) }
`)
	pr := newProg()
	un := pr.decl(lookupFunc(t, pkg, "under"))
	ui := pr.decl(lookupFunc(t, pkg, "underIs"))
	u := ui.callStatic(un, ui.input(0))[0]
	fc := &eir.Call{Kind: eir.CallFunc, Sig: ui.f.Inputs[1].Type.(*types2.Signature), Target: ui.input(1), Args: []eir.Reg{u}, Results: []eir.Reg{ui.reg()}}
	ui.call(fc)
	f := stageB(pr.p)
	d := newDeferral(pr.p, f.c, f.c.flow)
	if !d.deferred(ui.f, fc) {
		t.Errorf("underIs's call of f with a call's result as argument is not deferred")
	}
	// A range body over an iterator f was given: f(body), body a closure
	// whose record is in f's frame. Deferred, it would fall to the
	// conservative default; it is resolved in f.
	body := pr.fn("body", eir.FuncKey{}, nil)
	o := ui.obj(eir.Object{Type: types2.Typ[types2.Int]})
	cl := ui.add(eir.Instr{Op: eir.OpClosure, Callee: body.f.ID, Obj: o}, true)
	rc := &eir.Call{Kind: eir.CallFunc, Sig: fc.Sig, Target: ui.input(1), Args: []eir.Reg{cl}, Results: []eir.Reg{ui.reg()}}
	ui.call(rc)
	f = stageB(pr.p)
	d = newDeferral(pr.p, f.c, f.c.flow)
	if d.deferred(ui.f, rc) {
		t.Errorf("a call of f with a closure of f's frame as argument is deferred")
	}
	if !d.deferred(ui.f, fc) {
		t.Errorf("the callback call is no longer deferred")
	}
}

// TestDeferralFrameArg checks a deferred call whose argument, bound in a
// caller, may point into the caller's frame: the caller cannot defer it
// further, so it depends on the call's candidates (the fallback); a
// caller passing only its inputs does not.
func TestDeferralFrameArg(t *testing.T) {
	pkg := typecheck(t, `
func apply(f func(func(int) bool), fn func(int) bool) { f(fn) }
func caller(f func(func(int) bool)) {}
func plain(f func(func(int) bool), fn func(int) bool) {}
func runner(fn func(int) bool) {}
func other(int) bool { return true }
`)
	pr := newProg()
	ap := pr.decl(lookupFunc(t, pkg, "apply"))
	cl := pr.decl(lookupFunc(t, pkg, "caller"))
	pl := pr.decl(lookupFunc(t, pkg, "plain"))
	run := pr.decl(lookupFunc(t, pkg, "runner"))
	other := pr.decl(lookupFunc(t, pkg, "other"))
	fc := &eir.Call{Kind: eir.CallFunc, Sig: run.f.Sig, Target: ap.input(0), Args: []eir.Reg{ap.input(1)}}
	ap.call(fc)
	// caller passes a closure with a record of its frame as the argument.
	body := pr.fn("body", eir.FuncKey{}, other.f.Sig)
	body.f.Inputs = append(body.f.Inputs, eir.Input{Type: types2.Typ[types2.UnsafePointer]})
	o := cl.obj(eir.Object{Type: types2.Typ[types2.Int]})
	lit := cl.add(eir.Instr{Op: eir.OpClosure, Callee: body.f.ID, Obj: o, Args: []eir.Reg{cl.addr(o)}}, true)
	cl.callStatic(ap, cl.input(0), lit)
	// plain passes its inputs on.
	pl.callStatic(ap, pl.input(0), pl.input(1))
	main := pr.fn("main", eir.FuncKey{}, nil)
	main.f.External = true
	main.f.Facts.FuncValues = []eir.FuncID{run.f.ID, other.f.ID}
	rv := main.add(eir.Instr{Op: eir.OpClosure, Callee: run.f.ID, Obj: eir.NoObj}, true)
	main.callStatic(cl, rv)

	f := stageB(pr.p)
	d := newDeferral(pr.p, f.c, f.c.flow)
	if !d.deferred(ap.f, fc) {
		t.Fatalf("apply's call of its input is not deferred")
	}
	if !d.argInputs[ap.f.ID][1] || !d.argInputs[pl.f.ID][1] {
		t.Errorf("argument inputs: apply %v, plain %v; want input 1 in both", d.argInputs[ap.f.ID], d.argInputs[pl.f.ID])
	}
	has := false
	for _, x := range d.deps(cl.f) {
		has = has || x == run.f.ID
	}
	if !has {
		t.Errorf("caller depends on %v, want the deferred call's candidates (runner): it passes a frame pointer as its argument", d.deps(cl.f))
	}
	if got := d.deps(pl.f); len(got) != 0 {
		t.Errorf("plain depends on %v, want nothing", got)
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
	for _, id := range []eir.FuncID{all.f.ID, allRef.f.ID} {
		for _, x := range g.deps[id] {
			if x == other.f.ID || x == lit.f.ID {
				t.Errorf("%s depends on %s", pr.p.Funcs[id].Name, pr.p.Funcs[x].Name)
			}
		}
	}
}
