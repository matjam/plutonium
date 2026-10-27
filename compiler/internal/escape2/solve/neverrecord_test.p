// Ported from internal/escape2/solve/neverrecord_test.go at the go-cutover tag.

package solve

import (
	"pluto/testing"

	"compiler/internal/escape2/eir"
	"compiler/pu"
)

// neverRecordEnv makes keeper (it keeps its record) and iter(fv), which
// calls fv() as a deferred call (D49) with keeper as its only candidate.
func neverRecordEnv(t *testing.T) (*testEnv, *fb) {
	e := newEnv()
	keeper := e.fn("keeper", 1, 0) // (record)
	keeper.sink(eir.SinkGlobal, keeper.input(0))
	e.solve(t, keeper.f)
	iter := e.fn("iter", 1, 0)
	c := pu.New(eir.Call{Kind: eir.CallFunc, Target: iter.input(0)})
	iter.call(c)
	e.deferred[c] = true
	e.cands[c] = funcIDs(keeper.f.ID)
	e.solve(t, iter.f)
	return e, iter
}

// A function value points at a closure record, never at a box, variable or
// array (eir.ObjKind.NeverRecord): a pointer to a frame object of such a
// kind in the value is not a callee, so the call does not fall back to
// keeper with that object as keeper's record.
func TestNeverRecordNoFallback(t *testing.T) {
	for _, kind := range []eir.ObjKind{eir.ObjBox, eir.ObjVar, eir.ObjVarargs, eir.ObjSliceLit} {
		e, iter := neverRecordEnv(t)
		b := e.fn("f", 0, 0)
		o := b.obj("o", eir.LifeFrame, -1)
		b.f.Objects[o].Kind = kind
		b.at(4).static(iter.f, 0, b.addr(o))
		r := e.solve(t, b.f)
		wantFindings(t, r, "")
	}
}

// A frame closure record still applies its closure, and a record the
// caller cannot resolve (heap memory) still falls back.
func TestNeverRecordKeepsRecords(t *testing.T) {
	e, iter := neverRecordEnv(t)
	b := e.fn("f", 0, 0)
	rec := b.obj("rec", eir.LifeFrame, -1)
	b.f.Objects[rec].Kind = eir.ObjClosure
	clo := b.reg()
	b.emit(eir.Instr{Op: eir.OpClosure, Dst: clo, Obj: rec, Callee: e.prog.Funcs[0].ID})
	b.at(4).static(iter.f, 0, clo)
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3003@4")
}
