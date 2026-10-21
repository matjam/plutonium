package solve

import (
	"testing"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/escape2/summary/summarytest"
)

// Section 18.3, E3001: returning a view of the frame.
func TestReturnFrame(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 0, 1)
	x := b.obj("x", eir.LifeFrame, -1)
	b.at(10).ret(0, b.addr(x))
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3001@10")
	e.wantSummary(t, r, "")
}

// Returning what the caller passed is allowed, and is in the summary.
func TestReturnInput(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 2, 2)
	p := b.input(0)
	q := b.input(1)
	b.ret(0, b.field(p, 1))
	b.ret(1, b.load(q))
	r := e.solve(t, b.f)
	wantFindings(t, r, "")
	e.wantSummary(t, r, "res0 0.1; res1 1*")
}

// E3002, E3005, E3004: package memory, channels, tasks; and inputs that go
// there are kept (Esc).
func TestSinks(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 1, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	px := b.addr(x)
	b.at(1).sink(eir.SinkGlobal, px)
	b.at(2).sink(eir.SinkSend, px)
	b.at(3).sink(eir.SinkTask, px)
	b.at(4).sink(eir.SinkExternal, px)
	b.at(5).sink(eir.SinkGlobal, b.load(b.input(0)))
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3002@1 E3003@4 E3004@3 E3005@2")
	e.wantSummary(t, r, "esc 0*")
}

// Storing through a pointer the function received (E3002), into heap memory
// (E3002), and the store edges between inputs, including those of one input
// (plan invariant I4(b), the selfSafe defect R4 §1).
func TestStores(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 2, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	p := b.input(0)
	q := b.input(1)
	b.at(1).store(p, b.addr(x))
	b.at(2).store(b.heap(), b.addr(x))
	b.at(3).store(p, q)
	b.at(4).store(b.field(p, 2), b.load(p))
	b.at(5).store(b.heap(), q)
	b.at(6).store(b.load(q), b.heap())
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3002@1 E3002@2")
	e.wantSummary(t, r, "esc 1; heapstore 1*; store 0*->0.2; store 1->0")
}

// Memory of the frame is followed: a pointer stored in a local and loaded
// back is still the pointer (E3001), field by field.
func TestFrameMemory(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 1, 2)
	x := b.obj("x", eir.LifeFrame, -1)
	s := b.obj("s", eir.LifeFrame, -1)
	b.store(b.addr(s, 0), b.addr(x))
	b.store(b.addr(s, 1), b.input(0))
	whole := b.load(b.addr(s))
	b.at(7).ret(0, b.sel(whole, 1))
	b.at(8).ret(1, b.load(b.addr(s, 0)))
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3001@8")
	e.wantSummary(t, r, "res0 0")
}

// Struct values in registers are told apart by field.
func TestStructValues(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 1, 2)
	x := b.obj("x", eir.LifeFrame, -1)
	v := b.compose([][]int{{0}, {1}}, b.addr(x), b.input(0))
	b.at(1).ret(0, b.sel(v, 1))
	b.at(2).ret(1, v)
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3001@2")
	e.wantSummary(t, r, "res0 0; res1.1 0")
}

// E3003: passing a view of the frame to a function that keeps it; a
// callee's Esc on an input becomes the caller's.
func TestCallKeeps(t *testing.T) {
	e := newEnv()
	g := e.fn("g", 1, 0)
	g.sink(eir.SinkGlobal, g.load(g.input(0)))
	if r := e.solve(t, g.f); e.describe(r.Summary) != "esc 0*" {
		t.Fatalf("g: %s", e.describe(r.Summary))
	}
	b := e.fn("f", 1, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	y := b.obj("y", eir.LifeFrame, -1)
	px := b.addr(x)
	b.store(b.addr(y), px)
	b.at(5).static(g.f, 0, b.addr(y)) // keeps *(&y) = &x
	b.at(6).static(g.f, 0, px)        // keeps *(&x): nothing
	b.at(7).static(g.f, 0, b.input(0))
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3003@5")
	e.wantSummary(t, r, "esc 0*")
	if f := r.Findings[0]; f.Callee != g.f.ID || f.Entry != 0 || f.Site != 0 || f.Obj != x {
		t.Errorf("finding: %+v", f)
	}
}

// A callee's store edges are applied to the caller's memory with the store
// check: an iteration's variable kept in a frame variable (E3006), and
// between inputs, a store pair of the caller.
func TestCallStores(t *testing.T) {
	e := newEnv()
	g := e.fn("g", 2, 0)
	g.store(g.input(0), g.input(1))
	e.solve(t, g.f)
	b := e.fn("f", 2, 0)
	b.f.Loops = []eir.Loop{{Header: 0, Parent: -1}}
	s := b.obj("s", eir.LifeFrame, -1)
	x := b.obj("x", eir.LifeIter, 0)
	b.at(3).static(g.f, 0, b.addr(s), b.addr(x))
	b.at(4).static(g.f, 0, b.field(b.input(1), 3), b.input(0))
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3006@3")
	e.wantSummary(t, r, "store 0->1.3")
}

// The callee stores the caller's frame pointer through a pointer of the
// caller's caller: E3003 at the call.
func TestCallStoresIntoCaller(t *testing.T) {
	e := newEnv()
	g := e.fn("g", 2, 0)
	g.store(g.input(0), g.input(1))
	e.solve(t, g.f)
	b := e.fn("f", 1, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	b.at(9).static(g.f, 0, b.input(0), b.addr(x))
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3003@9")
}

// Results of a call hold what the callee's summary says.
func TestCallResults(t *testing.T) {
	e := newEnv()
	g := e.fn("g", 1, 1)
	g.ret(0, g.load(g.input(0)))
	e.solve(t, g.f)
	b := e.fn("f", 0, 1)
	x := b.obj("x", eir.LifeFrame, -1)
	y := b.obj("y", eir.LifeFrame, -1)
	b.store(b.addr(y), b.addr(x))
	res := b.static(g.f, 1, b.addr(y))
	b.at(4).ret(0, res[0])
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3001@4")
}

// Section 18.4: a view of an iteration's storage reaching the next iteration
// through a back edge of SSA is E3006; leaving the loop with it is allowed.
func TestBackEdge(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 0, 1)
	b.f.Loops = []eir.Loop{{Header: 1, Blocks: []eir.BlockID{1, 2}, Parent: -1}}
	x := b.obj("x", eir.LifeIter, 0)
	entry := b.cur
	head := b.block()
	body := b.block()
	exit := b.block()
	head.Loop, body.Loop = 0, 0
	entry.Succs = []eir.BlockID{1}
	head.Preds = []eir.BlockID{0, 2}
	head.Succs = []eir.BlockID{2, 3}
	body.Preds = []eir.BlockID{1}
	body.Succs = []eir.BlockID{1}
	exit.Preds = []eir.BlockID{1}
	nilv := b.reg()
	phi := b.reg()
	b.cur = body
	px := b.addr(x)
	b.cur = head
	b.at(20).emit(eir.Instr{Op: eir.OpPhi, Dst: phi, Args: []eir.Reg{nilv, px}})
	b.cur = exit
	b.at(30).sink(eir.SinkGlobal, b.heap())
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3006@20")

	// The same value leaving by the exit only: no back edge.
	e2 := newEnv()
	b = e2.fn("f", 0, 0)
	b.f.Loops = []eir.Loop{{Header: 1, Blocks: []eir.BlockID{1, 2}, Parent: -1}}
	x = b.obj("x", eir.LifeIter, 0)
	entry = b.cur
	head = b.block()
	body = b.block()
	exit = b.block()
	head.Loop, body.Loop = 0, 0
	entry.Succs = []eir.BlockID{1}
	head.Preds = []eir.BlockID{0, 2}
	head.Succs = []eir.BlockID{2}
	body.Preds = []eir.BlockID{1}
	body.Succs = []eir.BlockID{1, 3}
	exit.Preds = []eir.BlockID{2}
	b.cur = body
	px = b.addr(x)
	b.cur = exit
	out := b.reg()
	b.emit(eir.Instr{Op: eir.OpPhi, Dst: out, Args: []eir.Reg{px}})
	b.at(31).emit(eir.Instr{Op: eir.OpLoad, Dst: b.reg(), X: out})
	r = e2.solve(t, b.f)
	wantFindings(t, r, "")
}

// Section 18.4: keeping a pointer to an iteration's variable in a variable
// declared outside the loop body is E3006; inside the same loop it is not;
// a variable of an outer loop's iteration outlives an inner loop's.
func TestLoopStores(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 0, 0)
	b.f.Loops = []eir.Loop{{Parent: -1}, {Parent: 0}}
	outside := b.obj("outside", eir.LifeFrame, -1)
	outer := b.obj("outer", eir.LifeIter, 0)
	inner := b.obj("inner", eir.LifeIter, 1)
	inner2 := b.obj("inner2", eir.LifeIter, 1)
	b.at(1).store(b.addr(outside), b.addr(outer))
	b.at(2).store(b.addr(outer), b.addr(inner))
	b.at(3).store(b.addr(inner2), b.addr(inner))
	b.at(4).store(b.addr(inner), b.addr(outer))
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3006@1 E3006@2")
}

// E3009: a view of a statement temporary kept past its statement, in a
// register live after the drop, or in longer-lived memory.
func TestStmtTemp(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 0, 0)
	tmp := b.obj("tmp", eir.LifeStmt, -1)
	v := b.obj("v", eir.LifeFrame, -1)
	p := b.addr(tmp)
	q := b.addr(tmp)
	b.at(2).store(b.addr(v), q)
	b.at(3).emit(eir.Instr{Op: eir.OpDrop, Obj: tmp})
	b.at(4).emit(eir.Instr{Op: eir.OpLoad, Dst: b.reg(), X: p})
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3009@2 E3009@3")

	e2 := newEnv()
	b = e2.fn("f", 0, 0)
	tmp = b.obj("tmp", eir.LifeStmt, -1)
	p = b.addr(tmp)
	b.emit(eir.Instr{Op: eir.OpLoad, Dst: b.reg(), X: p})
	b.at(3).emit(eir.Instr{Op: eir.OpDrop, Obj: tmp})
	r = e2.solve(t, b.f)
	wantFindings(t, r, "")
}

// Dynamic calls: the union over every candidate (stage 2).
func TestDynamicUnion(t *testing.T) {
	e := newEnv()
	keep := e.fn("keep", 1, 0)
	keep.sink(eir.SinkGlobal, keep.input(0))
	e.solve(t, keep.f)
	use := e.fn("use", 1, 0)
	use.load(use.input(0))
	e.solve(t, use.f)
	b := e.fn("f", 1, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	c := &eir.Call{Kind: eir.CallIface, Target: b.addr(x), Results: nil}
	b.at(5).call(c)
	e.cands[c] = []eir.FuncID{use.f.ID, keep.f.ID}
	c2 := &eir.Call{Kind: eir.CallFunc, Target: b.heap(), Args: []eir.Reg{b.input(0)}}
	b.at(6).call(c2)
	e.cands[c2] = []eir.FuncID{use.f.ID, keep.f.ID}
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3003@5")
	e.wantSummary(t, r, "esc 0")
}

// A closure: its record holds the captures; a call through the func value
// binds the literal's record input to the target (rules.go, binding).
func TestClosure(t *testing.T) {
	e := newEnv()
	// lit keeps what its first capture points to.
	lit := e.fn("lit", 1, 0)
	lit.sink(eir.SinkGlobal, lit.load(lit.field(lit.input(0), 0)))
	e.solve(t, lit.f)
	b := e.fn("f", 0, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	rec := b.obj("rec", eir.LifeFrame, -1)
	b.f.Objects[rec].Kind = eir.ObjClosure
	clo := b.reg()
	b.emit(eir.Instr{Op: eir.OpClosure, Dst: clo, Obj: rec, Args: []eir.Reg{b.addr(x)}, Callee: lit.f.ID})
	c := &eir.Call{Kind: eir.CallFunc, Target: clo}
	b.at(8).call(c)
	e.cands[c] = []eir.FuncID{lit.f.ID}
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3003@8")
}

// A function with no body and no summary keeps every input (section 18.3).
func TestBodylessDefault(t *testing.T) {
	e := newEnv()
	ext := e.fn("ext", 1, 1)
	ext.f.Bodyless = true
	b := e.fn("f", 1, 1)
	x := b.obj("x", eir.LifeFrame, -1)
	b.at(2).static(ext.f, 1, b.addr(x))
	res := b.static(ext.f, 1, b.input(0))
	b.ret(0, res[0])
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3003@2")
	e.wantSummary(t, r, "esc 0+; res0 0+; res0 heap")
}

// A contract call uses the contract's summary; one without an entry is an
// internal compiler error.
func TestContract(t *testing.T) {
	e := newEnv()
	e.contracts["keep"] = &summary.Summary{Inputs: 1, Esc: []domain.Key{e.key(0, false)}}
	b := e.fn("f", 0, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	b.at(3).call(&eir.Call{Kind: eir.CallContract, Contract: "keep", Args: []eir.Reg{b.addr(x)}})
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3003@3")

	b = e.fn("g", 0, 0)
	b.call(&eir.Call{Kind: eir.CallContract, Contract: "missing"})
	defer func() {
		if recover() == nil {
			t.Error("no internal compiler error for a missing contract")
		}
	}()
	e.solve(t, b.f)
}

// Copy cycles are collapsed and the result is the same as without.
func TestCycleCollapse(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 1, 1)
	in := b.input(0)
	x := b.obj("x", eir.LifeFrame, -1)
	px := b.addr(x)
	a, c, d := b.reg(), b.reg(), b.reg()
	b.emit(eir.Instr{Op: eir.OpPhi, Dst: a, Args: []eir.Reg{in, d, px}})
	b.emit(eir.Instr{Op: eir.OpCopy, Dst: c, X: a})
	b.emit(eir.Instr{Op: eir.OpCopy, Dst: d, X: c})
	b.at(9).ret(0, d)
	ws := &Workspace{Arena: &summarytest.Arena{}, Sets: summarytest.NewSets()}
	s := newSolver(b.f, e, ws)
	s.build()
	s.run()
	if s.merged == 0 {
		t.Error("no cycle collapsed")
	}
	if s.find(uint32(a)) != s.find(uint32(c)) || s.find(uint32(c)) != s.find(uint32(d)) {
		t.Error("cycle members not merged")
	}
	r := Result{Summary: s.summary(), Findings: s.findings}
	wantFindings(t, r, "E3001@9")
	e.wantSummary(t, r, "res0 0")
}
