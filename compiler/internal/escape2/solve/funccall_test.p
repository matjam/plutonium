// Ported from internal/escape2/solve/funccall_test.go at the go-cutover tag.

package solve

import (
	"pluto/testing"

	"compiler/internal/escape2/eir"
	"compiler/pu"
)

// funcEnv makes two literals of one signature (u, record): leaky keeps what
// its record's first capture points to, quiet does nothing.
func funcEnv(t *testing.T) (e *testEnv, leaky, quiet *fb) {
	e = newEnv()
	leaky = e.fn("leaky", 2, 0)
	leaky.sink(eir.SinkGlobal, leaky.load(leaky.field(leaky.input(1), 0)))
	e.solve(t, leaky.f)
	quiet = e.fn("quiet", 2, 0)
	e.solve(t, quiet.f)
	return e, leaky, quiet
}

// closure emits quiet's closure capturing x, in b.
func closure(b *fb, callee *fb, x eir.ObjID) eir.Reg {
	rec := b.obj("rec", eir.LifeFrame, -1)
	b.f.Objects[rec].Kind = eir.ObjClosure
	clo := b.reg()
	b.emit(eir.Instr{Op: eir.OpClosure, Dst: clo, Obj: rec, Callee: callee.f.ID, Args: regs(b.addr(x))})
	return clo
}

// A call of a closure the function made applies that closure alone: the
// other candidate's summary never reads its record.
func TestFuncCallOwnClosure(t *testing.T) {
	e, leaky, quiet := funcEnv(t)
	b := e.fn("f", 0, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	clo := closure(b, quiet, x)
	c := pu.New(eir.Call{Kind: eir.CallFunc, Target: clo, Args: regs(b.heap())})
	b.at(4).call(c)
	e.cands[c] = funcIDs(leaky.f.ID, quiet.f.ID)
	r := e.solve(t, b.f)
	wantFindings(t, r, "")
}

// The candidates still apply to the rest of the value, with their record
// bound to it alone.
func TestFuncCallRest(t *testing.T) {
	e, leaky, quiet := funcEnv(t)
	b := e.fn("f", 1, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	clo := closure(b, quiet, x)
	slot := b.obj("slot", eir.LifeFrame, -1)
	b.store(b.addr(slot), clo)
	b.store(b.addr(slot), b.input(0))
	c := pu.New(eir.Call{Kind: eir.CallFunc, Target: b.load(b.addr(slot)), Args: regs(b.heap())})
	b.at(4).call(c)
	e.cands[c] = funcIDs(leaky.f.ID, quiet.f.ID)
	r := e.solve(t, b.f)
	wantFindings(t, r, "")
	e.wantSummary(t, r, "esc 0.0*")
}

// A closure the solve may not read is resolved by the candidates, as any
// other function value.
func TestFuncCallUnreadClosure(t *testing.T) {
	e, leaky, quiet := funcEnv(t)
	e.unread[quiet.f.ID] = true
	b := e.fn("f", 0, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	clo := closure(b, quiet, x)
	c := pu.New(eir.Call{Kind: eir.CallFunc, Target: clo, Args: regs(b.heap())})
	b.at(4).call(c)
	e.cands[c] = funcIDs(leaky.f.ID)
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3003@4")
}
