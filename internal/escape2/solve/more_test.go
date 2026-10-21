package solve

import (
	"fmt"

	"testing"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/escape2/summary/summarytest"
	"github.com/matjam/plutonium/internal/types2"
)

// regionCallee is a function that returns a closure built in the region its
// caller provides (section 11.5): its region object holds a record capturing
// its input.
func regionCallee(t *testing.T, e *testEnv) *fb {
	g := e.fn("g", 1, 1)
	reg := g.obj("region", eir.LifeCaller, -1)
	g.f.Objects[reg].Kind = eir.ObjRegion
	g.store(g.addr(reg, 0), g.input(0))
	g.ret(0, g.addr(reg))
	r := e.solve(t, g.f)
	wantFindings(t, r, "")
	e.wantSummary(t, r, "builds; outer0")
	return g
}

// Section 11.5: what a call returns in its region lives as long as the
// caller's frame; kept past the iteration of a loop the call is in, E3101.
func TestRegionLoop(t *testing.T) {
	e := newEnv()
	g := regionCallee(t, e)
	b := e.fn("f", 0, 0)
	b.f.Loops = []eir.Loop{{Header: 1, Blocks: []eir.BlockID{1}, Parent: -1}}
	keep := b.obj("keep", eir.LifeFrame, -1)
	body := b.block()
	body.Loop = 0
	b.cur = body
	res := b.static(g.f, 1, b.heap())
	b.at(4).store(b.addr(keep), res[0])
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3101@4")
	if f := r.Findings[0]; f.Obj != eir.NoObj || f.Site != 0 {
		t.Errorf("finding: %+v", f)
	}
}

// Returning the closures of a call again is allowed: the caller's caller
// provides the region. Unless the region holds a view of this frame, which
// would outlive it (E3001).
func TestRegionForward(t *testing.T) {
	e := newEnv()
	g := regionCallee(t, e)
	b := e.fn("f", 1, 1)
	res := b.static(g.f, 1, b.input(0))
	b.ret(0, res[0])
	r := e.solve(t, b.f)
	wantFindings(t, r, "")
	e.wantSummary(t, r, "outer0")

	b = e.fn("h", 0, 1)
	x := b.obj("x", eir.LifeFrame, -1)
	res = b.static(g.f, 1, b.addr(x))
	b.at(7).ret(0, res[0])
	r = e.solve(t, b.f)
	wantFindings(t, r, "E3001@7")

	// Stored in package memory: E3002.
	b = e.fn("k", 0, 0)
	res = b.static(g.f, 1, b.heap())
	b.at(8).sink(eir.SinkGlobal, res[0])
	r = e.solve(t, b.f)
	wantFindings(t, r, "E3002@8")
}

// A closure stored in the region of a function's caller captures a frame
// variable of the function: E3002 at the store (section 11.5).
func TestRegionCapturesFrame(t *testing.T) {
	e := newEnv()
	g := e.fn("g", 0, 1)
	reg := g.obj("region", eir.LifeCaller, -1)
	x := g.obj("x", eir.LifeFrame, -1)
	g.at(3).store(g.addr(reg, 0), g.addr(x))
	g.ret(0, g.addr(reg))
	r := e.solve(t, g.f)
	wantFindings(t, r, "E3002@3")
}

// A path cut at the field limit stands for every place under it: what is
// stored through it is found by loads of any field under the cut.
func TestCutPath(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 0, 2)
	x := b.obj("x", eir.LifeFrame, -1)
	s := b.obj("s", eir.LifeFrame, -1)
	b.store(b.addr(s, 0, 1, 2), b.addr(x))
	b.at(5).ret(0, b.load(b.addr(s, 0, 1)))
	b.at(6).ret(1, b.load(b.addr(s, 0, 3)))
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3001@5")
	// Through an input: the store pair's destination covers the cut.
	b = e.fn("g", 2, 0)
	b.store(b.field(b.input(0), 1, 2, 3), b.input(1))
	r = e.solve(t, b.f)
	e.wantSummary(t, r, "store 1->0.1.2+")
}

// A load of a heap-only field yields heap memory (section 18.3, plan
// invariant I6).
func TestHeapOnly(t *testing.T) {
	e := newEnv()
	ptr := types2.NewPointer(types2.Typ[types2.Int])
	st := types2.NewStruct([]*types2.Var{
		types2.NewField(0, nil, "next", ptr, false),
		types2.NewField(0, nil, "other", ptr, false),
	}, nil)
	e.heapOnly[st.String()+".0"] = true
	b := e.fn("f", 1, 2)
	s := b.typedObj("s", st)
	b.store(b.addr(s, 0), b.heap())
	b.store(b.addr(s, 1), b.heap())
	b.ret(0, b.load(b.addr(s, 0)))
	b.ret(1, b.load(b.field(b.input(0), 0)))
	b.f.Blocks[0].Instrs[len(b.f.Blocks[0].Instrs)-3].Type = st // the OpField
	r := e.solve(t, b.f)
	wantFindings(t, r, "")
	e.wantSummary(t, r, "res0 heap; res1 heap")
}

// Interface conversions box non-direct values in the frame (section 7.9):
// returning the interface returns the box (E3001); a direct value is the
// pointer it is.
func TestConv(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 1, 2)
	box := b.obj("box", eir.LifeFrame, -1)
	b.f.Objects[box].Kind = eir.ObjBox
	i1 := b.reg()
	b.emit(eir.Instr{Op: eir.OpConv, Dst: i1, X: b.input(0), Obj: box})
	i2 := b.reg()
	b.emit(eir.Instr{Op: eir.OpConv, Dst: i2, X: b.input(0), Obj: eir.NoObj})
	b.at(1).ret(0, i1)
	b.at(2).ret(1, i2)
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3001@1")
	e.wantSummary(t, r, "res1 0")
}

// A method value's record holds its receiver at field 0.
func TestMethodValue(t *testing.T) {
	e := newEnv()
	m := e.fn("T.M-fm", 1, 0)
	m.sink(eir.SinkGlobal, m.load(m.field(m.input(0), 0)))
	e.solve(t, m.f)
	b := e.fn("f", 0, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	rec := b.obj("rec", eir.LifeFrame, -1)
	mv := b.reg()
	b.emit(eir.Instr{Op: eir.OpMethodVal, Dst: mv, X: b.addr(x), Obj: rec})
	c := &eir.Call{Kind: eir.CallFunc, Target: mv}
	b.at(3).call(c)
	e.cands[c] = []eir.FuncID{m.f.ID}
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3003@3")
}

// A callee of the same SCC not solved yet is the bottom summary.
func TestBottomCallee(t *testing.T) {
	e := newEnv()
	g := e.fn("g", 1, 0)
	b := e.fn("f", 0, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	b.static(g.f, 0, b.addr(x))
	wantFindings(t, e.solve(t, b.f), "")
	// Once g keeps its input, f's solve reports the call.
	e.sums[g.f.ID] = &summary.Summary{Inputs: 1, Esc: []domain.Key{e.key(0, false)}}
	wantFindings(t, e.solve(t, b.f), "E3003@0")
}

// The witness of a finding: where the pointer is made, the store into a
// frame variable, the load back, and the return.
func TestExplain(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 0, 1)
	x := b.obj("x", eir.LifeFrame, -1)
	s := b.obj("s", eir.LifeFrame, -1)
	px := b.at(1).addr(x)
	ps := b.at(2).addr(s)
	b.store(ps, px)
	v := b.at(3).load(b.addr(s))
	b.at(4).ret(0, v)
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3001@4")
	ws := &Workspace{Arena: &summarytest.Arena{}, Sets: summarytest.NewSets()}
	got := Explain(b.f, e, ws, r.Findings[0])
	if fmt.Sprint(got) != "[1 2 3 4]" {
		t.Errorf("witness %v", got)
	}
}

// A function that returns its own closure holding the closure of a call
// passes part of its region down for it (section 11.5): allowed, unless the
// call's region holds a view of the frame (E3001).
func TestRegionNested(t *testing.T) {
	e := newEnv()
	g := regionCallee(t, e)
	b := e.fn("f", 1, 1)
	reg := b.obj("region", eir.LifeCaller, -1)
	res := b.static(g.f, 1, b.input(0))
	b.store(b.addr(reg, 0), res[0])
	b.ret(0, b.addr(reg))
	r := e.solve(t, b.f)
	wantFindings(t, r, "")
	e.wantSummary(t, r, "builds; outer0")

	b = e.fn("h", 0, 1)
	reg = b.obj("region", eir.LifeCaller, -1)
	x := b.obj("x", eir.LifeFrame, -1)
	res = b.static(g.f, 1, b.addr(x))
	b.store(b.addr(reg, 0), res[0])
	b.at(5).ret(0, b.addr(reg))
	r = e.solve(t, b.f)
	wantFindings(t, r, "E3001@5")
}

// In a package's initialization the closures a call returns are built in
// static memory (section 15.2): keeping them in package memory is allowed,
// and what the callee captured from the initialization's frame is kept
// there (E3002).
func TestRegionStaticInInit(t *testing.T) {
	e := newEnv()
	g := regionCallee(t, e)
	b := e.fn("init", 0, 0)
	b.f.Key.Init = types2.NewPackage("p", "p")
	res := b.static(g.f, 1, b.heap())
	b.at(3).sink(eir.SinkGlobal, res[0])
	x := b.obj("x", eir.LifeFrame, -1)
	b.at(4).static(g.f, 1, b.addr(x))
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3002@4")
}

// Reasons carry the position of what made each entry, and for the
// function's own sinks their kind (for messages, stage 5).
func TestReasons(t *testing.T) {
	e := newEnv()
	g := e.fn("g", 1, 0)
	g.at(5).sink(eir.SinkGlobal, g.input(0))
	r := e.solve(t, g.f)
	if rs := r.Summary.Reasons; len(rs) != 1 || rs[0].Pos != 5 || rs[0].Sink != uint8(eir.SinkGlobal) || rs[0].Site != ^uint32(0) {
		t.Fatalf("own reason: %+v", rs)
	}
	b := e.fn("f", 1, 0)
	b.at(9).static(g.f, 0, b.input(0))
	r = e.solve(t, b.f)
	if rs := r.Summary.Reasons; len(rs) != 1 || rs[0].Pos != 9 || rs[0].Site != 0 || rs[0].Callee != uint32(g.f.ID) || rs[0].Entry != 0 {
		t.Fatalf("call reason: %+v", rs)
	}
}

// A box view (section 18.7) bounds a pointer by its box variable: returning
// it is E3001; storing a frame pointer through it is a store into heap
// memory (E3002); a load through it is heap memory.
func TestBoxView(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 0, 2)
	view := b.obj("b", eir.LifeFrame, -1)
	b.f.Objects[view].Kind = eir.ObjBoxView
	x := b.obj("x", eir.LifeFrame, -1)
	b.at(1).ret(0, b.addr(view, 0))
	b.at(2).store(b.addr(view, 1), b.addr(x))
	b.at(3).ret(1, b.load(b.addr(view, 1)))
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3001@1 E3002@2")
	e.wantSummary(t, r, "res1 heap")
}

// A loop's header variable (section 18.4): a view of the loop's body kept
// in it is E3006, a view of an enclosing loop's iteration is not, and a view
// of the header variable itself is good for one iteration.
func TestLoopHeader(t *testing.T) {
	e := newEnv()
	b := e.fn("f", 0, 0)
	b.f.Loops = []eir.Loop{{Parent: -1}, {Parent: 0}}
	outer := b.obj("outer", eir.LifeIter, 0)
	head := b.obj("i", eir.LifeHeader, 1)
	body := b.obj("v", eir.LifeIter, 1)
	keep := b.obj("keep", eir.LifeFrame, -1)
	b.at(1).store(b.addr(head), b.addr(body))
	b.at(2).store(b.addr(head), b.addr(outer))
	b.at(3).store(b.addr(keep), b.addr(head))
	b.at(4).store(b.addr(body), b.addr(head))
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3006@1 E3006@3")
}

// A local sync.WaitGroup passed to a function that starts a task with it is
// E3004 (section 12.12); another frame view so passed is E3003.
func TestTaskStarter(t *testing.T) {
	e := newEnv()
	pkg := types2.NewPackage("pluto/sync", "sync")
	wgT := types2.NewNamed(types2.NewTypeName(0, pkg, "WaitGroup", nil), types2.NewStruct(nil, nil), nil)
	e.contracts["task"] = &summary.Summary{Inputs: 2, Esc: []domain.Key{e.key(0, false), e.key(1, false)}}
	b := e.fn("f", 0, 0)
	wg := b.typedObj("wg", wgT)
	x := b.obj("x", eir.LifeFrame, -1)
	b.at(5).call(&eir.Call{Kind: eir.CallContract, Contract: "task", Implicit: eir.ImplTask, Args: []eir.Reg{b.addr(wg), b.addr(x)}})
	r := e.solve(t, b.f)
	got := map[eir.ObjID]string{}
	for _, f := range r.Findings {
		got[f.Obj] = string(f.Code)
	}
	if got[wg] != "E3004" || got[x] != "E3003" {
		t.Errorf("findings %v", got)
	}
}

// A helper that starts a task with its parameter (w03): its summary's reason
// says so, and passing a local sync.WaitGroup to the helper is E3004.
func TestTaskStarterHelper(t *testing.T) {
	e := newEnv()
	pkg := types2.NewPackage("pluto/sync", "sync")
	wgT := types2.NewNamed(types2.NewTypeName(0, pkg, "WaitGroup", nil), types2.NewStruct(nil, nil), nil)
	e.contracts["task"] = &summary.Summary{Inputs: 1, Esc: []domain.Key{e.key(0, false)}}
	h := e.fn("start", 1, 0)
	h.call(&eir.Call{Kind: eir.CallContract, Contract: "task", Implicit: eir.ImplTask, Args: []eir.Reg{h.input(0)}})
	e.solve(t, h.f)
	b := e.fn("f", 0, 0)
	wg := b.typedObj("wg", wgT)
	x := b.obj("x", eir.LifeFrame, -1)
	b.at(5).static(h.f, 0, b.addr(wg))
	b.at(6).static(h.f, 0, b.addr(x))
	r := e.solve(t, b.f)
	wantFindings(t, r, "E3003@6 E3004@5")
}
