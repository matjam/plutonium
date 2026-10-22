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
