package solve

import (
	"testing"

	"github.com/matjam/plutonium/internal/escape2/eir"
)

// deferEnv makes body (it keeps its argument) and iter(p, yield), which
// calls yield(p) as a deferred call (D49), with body as the call's only
// candidate (the fallback).
func deferEnv(t *testing.T) (*testEnv, *fb, *fb) {
	e := newEnv()
	body := e.fn("body", 2, 0) // (x, record)
	body.sink(eir.SinkGlobal, body.input(0))
	e.solve(t, body.f)
	iter := e.fn("iter", 2, 0)
	c := &eir.Call{Kind: eir.CallFunc, Target: iter.input(1), Args: []eir.Reg{iter.input(0)}}
	iter.call(c)
	e.deferred[c] = true
	e.cands[c] = []eir.FuncID{body.f.ID}
	return e, body, iter
}

// The deferred call is in iter's summary, not the join of its candidates.
func TestDeferredSummary(t *testing.T) {
	e, _, iter := deferEnv(t)
	r := e.solve(t, iter.f)
	wantFindings(t, r, "")
	e.wantSummary(t, r, "defer f1@0 1(0)")
}

// A caller that passes a closure of its own resolves the call against it:
// the closure keeps the caller's frame pointer.
func TestDeferredResolvedToClosure(t *testing.T) {
	e, body, iter := deferEnv(t)
	e.solve(t, iter.f)
	b := e.fn("f", 0, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	rec := b.obj("rec", eir.LifeFrame, -1)
	b.f.Objects[rec].Kind = eir.ObjClosure
	clo := b.reg()
	b.emit(eir.Instr{Op: eir.OpClosure, Dst: clo, Obj: rec, Callee: body.f.ID})
	b.at(4).static(iter.f, 0, b.addr(x), clo)
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3003@4")
}

// A caller that passes its own inputs defers the call again.
func TestDeferredAgain(t *testing.T) {
	e, _, iter := deferEnv(t)
	e.solve(t, iter.f)
	b := e.fn("g", 2, 0)
	b.static(iter.f, 0, b.load(b.input(0)), b.input(1))
	r := e.solve(t, b.f)
	wantFindings(t, r, "")
	e.wantSummary(t, r, "defer f1@0 1(0*)")
}

// A function value the caller cannot resolve (a static one) gets the join
// of the origin call's candidates.
func TestDeferredFallback(t *testing.T) {
	e, _, iter := deferEnv(t)
	e.solve(t, iter.f)
	b := e.fn("f", 1, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	b.at(4).static(iter.f, 0, b.addr(x), b.heap())
	b.static(iter.f, 0, b.input(0), b.heap())
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3003@4")
	e.wantSummary(t, r, "esc 0")
}

// A deferred call of the function's own whose argument holds a frame
// pointer is kept as by a bodyless callee: it reads no candidate.
func TestDeferredOwnFrameArgument(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 1, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	p := b.obj("p", eir.LifeFrame, -1)
	b.store(b.addr(p), b.addr(x))
	c := &eir.Call{Kind: eir.CallFunc, Target: b.input(0), Args: []eir.Reg{b.load(b.addr(p))}}
	b.at(6).call(c)
	e.deferred[c] = true
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3003@6")
	e.wantSummary(t, r, "esc 0+")
}

// A caller that passes its own input as the function value but a frame
// pointer as the argument cannot defer the call again: it applies the
// origin call's candidates, which keep the frame pointer.
func TestDeferredFrameArgumentFallsBack(t *testing.T) {
	e, _, iter := deferEnv(t)
	e.solve(t, iter.f)
	b := e.fn("g", 1, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	b.at(3).static(iter.f, 0, b.addr(x), b.input(0))
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3003@3")
	e.wantSummary(t, r, "")
}

// A recursive function defers its own deferred calls through its recursive
// call: those are not calls of its own code, so where they cannot be
// deferred further they fall back to the candidates, never to keeping what
// they pass (testdata/run/gentypes.p, (*Tree).Walk).
func TestDeferredRecursive(t *testing.T) {
	e := newEnv()
	body := e.fn("body", 2, 0)
	e.solve(t, body.f)
	walk := e.fn("walk", 2, 0) // (t, f)
	rec := &eir.Call{Kind: eir.CallStatic, Callee: walk.f.ID, Args: []eir.Reg{walk.load(walk.field(walk.input(0), 0)), walk.input(1)}}
	walk.call(rec)
	c := &eir.Call{Kind: eir.CallFunc, Target: walk.input(1), Args: []eir.Reg{walk.load(walk.field(walk.input(0), 1))}}
	walk.call(c)
	e.deferred[c] = true
	e.cands[c] = []eir.FuncID{body.f.ID}
	var r Result
	for range 6 {
		r = e.solve(t, walk.f)
	}
	for _, k := range r.Summary.Esc {
		t.Errorf("walk keeps %s", e.keyString(k))
	}
	if len(r.Summary.Deferred) == 0 {
		t.Errorf("walk defers nothing: %s", e.describe(r.Summary))
	}
}

// A function no caller binds (External) has nobody to defer to: it keeps
// what it passes.
func TestDeferredExternal(t *testing.T) {
	e, _, iter := deferEnv(t)
	iter.f.External = true
	r := e.solve(t, iter.f)
	e.wantSummary(t, r, "esc 0+; esc 1+")
}

// A literal the scheduler did not order before the resolving function is
// not read: the call falls back to the origin call's candidates, and where
// those cannot be read either, keeps what it passes.
func TestDeferredUnread(t *testing.T) {
	e, body, iter := deferEnv(t)
	e.solve(t, iter.f)
	e.unread[body.f.ID] = true
	b := e.fn("f", 1, 0)
	rec := b.obj("rec", eir.LifeFrame, -1)
	b.f.Objects[rec].Kind = eir.ObjClosure
	clo := b.reg()
	b.emit(eir.Instr{Op: eir.OpClosure, Dst: clo, Obj: rec, Callee: body.f.ID})
	b.static(iter.f, 0, b.input(0), clo)
	r := e.solve(t, b.f)
	e.wantSummary(t, r, "esc 0+")
}
