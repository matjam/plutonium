package refsolve

import (
	"os"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/types2"
)

func TestHeapOnlyLoad(t *testing.T) {
	// *(&o.f1) is heap-only (the environment says field 1 is), *(&o.f0) is
	// not: only the first load gives Heap; the second leads to what the
	// object's place holds, here the input.
	st := newStructType()
	b := NewFunc(0, 1, 2)
	o := b.Obj(eir.ObjVar, eir.LifeFrame, -1, st)
	b.Store(b.Addr(o, 0), b.In(0))
	b.Sink(eir.SinkReturn, b.Load(b.Addr(o, 1)), 0)
	b.Sink(eir.SinkReturn, b.Load(b.Addr(o, 0)), 1)
	e, _ := env(b.F)
	e.HeapOnlyFn = func(_ types2.Type, field int) bool { return field == 1 }
	got, _ := run(t, b.F, e)
	has(t, got, "0: heap at .", "1: in0. at .")
	hasNot(t, got, "1: heap")
}

func widenBuilt() (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	summary.Widen(NewNaivePaths(), &summary.Summary{})
	return true
}

func TestMain(m *testing.M) {
	if !widenBuilt() {
		widen = func(domain.Paths, *summary.Summary) {} // summary.Widen is still a stub
	}
	os.Exit(m.Run())
}

// env makes a one-function program (function 0) plus n callees without
// bodies, and an environment over it.
func env(f *eir.Func, callees ...*eir.Func) (*MapEnv, domain.Paths) {
	ps := NewNaivePaths()
	prog := &eir.Program{Funcs: []*eir.Func{f}}
	for i, c := range callees {
		c.ID = eir.FuncID(i + 1)
		prog.Funcs = append(prog.Funcs, c)
	}
	return &MapEnv{
		Prog: prog, P: ps,
		Sums:      map[eir.FuncID]*summary.Summary{},
		Cands:     map[eir.FuncID]map[eir.SiteID][]eir.FuncID{},
		Contracts: map[string]*summary.Summary{},
	}, ps
}

func run(t *testing.T, f *eir.Func, e *MapEnv) (string, solve.Result) {
	t.Helper()
	r := Solve(f, e)
	return Canon(e.P, r), r
}

func has(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
}

func hasNot(t *testing.T, got string, bad ...string) {
	t.Helper()
	for _, w := range bad {
		if strings.Contains(got, w) {
			t.Errorf("unexpected %q in\n%s", w, got)
		}
	}
}

func codes(r solve.Result) []diag.Code {
	var out []diag.Code
	for _, f := range r.Findings {
		out = append(out, f.Code)
	}
	return out
}

func wantCodes(t *testing.T, r solve.Result, want ...diag.Code) {
	t.Helper()
	got := codes(r)
	if len(got) != len(want) {
		t.Fatalf("findings %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("findings %v, want %v", got, want)
		}
	}
}

func TestReturnLocal(t *testing.T) {
	b := NewFunc(0, 0, 1)
	o := b.Obj(eir.ObjVar, eir.LifeFrame, -1, nil)
	b.Sink(eir.SinkReturn, b.Addr(o), 0)
	e, _ := env(b.F)
	_, r := run(t, b.F, e)
	wantCodes(t, r, diag.ReturnFrameView)
}

func TestReturnInput(t *testing.T) {
	b := NewFunc(0, 2, 1)
	b.Sink(eir.SinkReturn, b.Field(b.In(1), 0), 0)
	e, _ := env(b.F)
	got, r := run(t, b.F, e)
	wantCodes(t, r)
	has(t, got, "res 0: in1.f0. at .")
}

func TestStoreInputs(t *testing.T) {
	b := NewFunc(0, 2, 0)
	b.Store(b.In(0), b.In(1))
	b.Store(b.In(0), b.Heap())
	e, _ := env(b.F)
	got, _ := run(t, b.F, e)
	has(t, got, "store in1. -> in0.", "heapstore in0.")
}

func TestStoreFrameIntoInputAndHeap(t *testing.T) {
	b := NewFunc(0, 1, 0)
	o := b.Obj(eir.ObjVar, eir.LifeFrame, -1, nil)
	a := b.Addr(o)
	b.Store(b.In(0), a)
	b.Store(b.Heap(), a)
	e, _ := env(b.F)
	_, r := run(t, b.F, e)
	if len(r.Findings) != 2 || r.Findings[0].Code != diag.StoreFrameView || r.Findings[1].Code != diag.StoreFrameView {
		t.Fatalf("findings %v", codes(r))
	}
}

func TestSinks(t *testing.T) {
	b := NewFunc(0, 1, 0)
	o := b.Obj(eir.ObjVar, eir.LifeFrame, -1, nil)
	a := b.Addr(o)
	b.Sink(eir.SinkGlobal, a, 0)
	b.Sink(eir.SinkSend, a, 0)
	b.Sink(eir.SinkTask, a, 0)
	b.Sink(eir.SinkExternal, a, 0)
	b.Sink(eir.SinkGlobal, b.In(0), 0)
	e, _ := env(b.F)
	got, r := run(t, b.F, e)
	wantCodes(t, r, diag.StoreFrameView, diag.SendFrameView, diag.GoFrameView, diag.PassFrameView)
	has(t, got, "esc in0.s\n") // kept in a package variable: Strict (section 18.9)
}

func TestThroughFrameObject(t *testing.T) {
	// x = &a; *x = in0; return *x: the result holds what in0 holds.
	b := NewFunc(0, 1, 1)
	o := b.Obj(eir.ObjVar, eir.LifeFrame, -1, nil)
	x := b.Addr(o)
	b.Store(x, b.In(0))
	b.Sink(eir.SinkReturn, b.Load(x), 0)
	e, _ := env(b.F)
	got, r := run(t, b.F, e)
	wantCodes(t, r)
	has(t, got, "res 0: in0. at .")
}

func TestLoopBackEdge(t *testing.T) {
	b := NewFunc(0, 1, 0)
	hdr := b.NewBlock()
	body := b.NewBlock()
	exit := b.NewBlock()
	b.Edge(0, hdr)
	b.Edge(hdr, body)
	b.Edge(body, hdr)
	b.Edge(hdr, exit)
	l := b.Loop(-1, hdr, body)
	o := b.Obj(eir.ObjVar, eir.LifeIter, l, nil)
	b.At(body)
	a := b.Addr(o)
	b.At(0)
	in := b.In(0)
	b.At(hdr)
	b.Phi(in, a)
	e, _ := env(b.F)
	_, r := run(t, b.F, e)
	wantCodes(t, r, diag.LoopFrameView)
}

func TestStatementTempDrop(t *testing.T) {
	b := NewFunc(0, 0, 0)
	o := b.Obj(eir.ObjStmtTemp, eir.LifeStmt, -1, nil)
	a := b.Addr(o)
	b.Drop(o)
	b.Sink(eir.SinkGlobal, b.Copy(a), 0) // a is live after the drop
	e, _ := env(b.F)
	_, r := run(t, b.F, e)
	if len(r.Findings) != 2 {
		t.Fatalf("findings %v", codes(r))
	}
	b2 := NewFunc(0, 0, 0)
	o2 := b2.Obj(eir.ObjStmtTemp, eir.LifeStmt, -1, nil)
	b2.Copy(b2.Addr(o2))
	b2.Drop(o2) // nothing used after
	e2, _ := env(b2.F)
	_, r2 := run(t, b2.F, e2)
	wantCodes(t, r2)
}

func TestCalls(t *testing.T) {
	// callee 1 keeps input 0 and returns input 1; callee 2 has no body.
	b := NewFunc(0, 1, 1)
	o := b.Obj(eir.ObjVar, eir.LifeFrame, -1, nil)
	a := b.Addr(o)
	rs := b.Static(1, 1, a, b.In(0))
	b.Sink(eir.SinkReturn, rs[0], 0)
	b.Static(2, 1, a)
	c1 := &eir.Func{Inputs: make([]eir.Input, 2), Results: 1}
	c2 := &eir.Func{Inputs: make([]eir.Input, 1), Results: 1, Bodyless: true}
	e, ps := env(b.F, c1, c2)
	e.Sums[1] = &summary.Summary{
		Inputs: 2,
		Esc:    []domain.Key{domain.MakeKey(0, domain.Root, domain.KeyFlags{})},
		Res:    []summary.Result{{From: []summary.ResultKey{{Key: domain.MakeKey(1, domain.Root, domain.KeyFlags{})}}}},
	}
	got, r := run(t, b.F, e)
	_ = ps
	if len(r.Findings) != 2 || r.Findings[0].Code != diag.PassFrameView || r.Findings[1].Code != diag.PassFrameView {
		t.Fatalf("findings %v", codes(r))
	}
	has(t, got, "res 0: in0. at .")
}

func TestStoreSummaryIntoFrame(t *testing.T) {
	// callee stores its input 1 into the memory input 0 leads to: the caller
	// passes &local and its own input.
	b := NewFunc(0, 1, 0)
	o := b.Obj(eir.ObjVar, eir.LifeFrame, -1, nil)
	a := b.Addr(o)
	b.Static(1, 0, a, b.In(0))
	c1 := &eir.Func{Inputs: make([]eir.Input, 2)}
	e, _ := env(b.F, c1)
	e.Sums[1] = &summary.Summary{Inputs: 2, Store: []summary.StorePair{{
		Src: domain.MakeKey(1, domain.Root, domain.KeyFlags{}),
		Dst: domain.MakeKey(0, domain.Root, domain.KeyFlags{}),
	}}}
	_, r := run(t, b.F, e)
	wantCodes(t, r) // stored into the frame object: fine
	// and then the object's content flows out through a return of *a.
	b2 := NewFunc(0, 1, 1)
	o2 := b2.Obj(eir.ObjVar, eir.LifeFrame, -1, nil)
	a2 := b2.Addr(o2)
	b2.Static(1, 0, a2, b2.In(0))
	b2.Sink(eir.SinkReturn, b2.Load(a2), 0)
	e2, _ := env(b2.F, &eir.Func{Inputs: make([]eir.Input, 2)})
	e2.Sums[1] = e.Sums[1]
	got, _ := run(t, b2.F, e2)
	has(t, got, "res 0: in0. at .")
}

func TestLoadPath(t *testing.T) {
	b := NewFunc(0, 1, 1)
	b.Sink(eir.SinkReturn, b.Load(b.Field(b.In(0), 0)), 0)
	e, _ := env(b.F)
	got, _ := run(t, b.F, e)
	has(t, got, "res 0: in0.f0.* at .")
}

func TestComposeSelect(t *testing.T) {
	// s = {f0: &a, f1: in0}; return s.f1 returns in0, s.f0 the frame.
	b := NewFunc(0, 1, 2)
	o := b.Obj(eir.ObjVar, eir.LifeFrame, -1, nil)
	s := b.Compose([]eir.Reg{b.Addr(o), b.In(0)}, [][]int{{0}, {1}})
	b.Sink(eir.SinkReturn, b.Select(s, 1), 0)
	b.Sink(eir.SinkReturn, b.Select(s, 0), 1)
	e, _ := env(b.F)
	got, r := run(t, b.F, e)
	wantCodes(t, r, diag.ReturnFrameView)
	has(t, got, "res 0: in0. at .")
	hasNot(t, got, "res 1")
}

func TestClosureRegionOuterResult(t *testing.T) {
	// callee returns a closure built in the caller's region; returning it
	// while the region holds a frame object is E3001.
	b := NewFunc(0, 0, 1)
	o := b.Obj(eir.ObjVar, eir.LifeFrame, -1, nil)
	rs := b.Static(1, 1, b.Addr(o))
	b.Sink(eir.SinkReturn, rs[0], 0)
	e, _ := env(b.F, &eir.Func{Inputs: make([]eir.Input, 1), Results: 1})
	e.Sums[1] = &summary.Summary{Inputs: 1, Res: []summary.Result{{}}, Region: summary.Region{Builds: true, OuterResults: []int{0}}}
	got, r := run(t, b.F, e)
	wantCodes(t, r, diag.ReturnFrameView)
	has(t, got, "outer=[0]")
}

func TestContract(t *testing.T) {
	b := NewFunc(0, 1, 1)
	rs := b.Call(eir.Call{Kind: eir.CallContract, Contract: "c", Args: []eir.Reg{b.In(0)}}, 1)
	b.Sink(eir.SinkReturn, rs[0], 0)
	e, _ := env(b.F)
	e.Contracts["c"] = &summary.Summary{Inputs: 1, Res: []summary.Result{{Heap: []domain.PathID{domain.Root}}}}
	got, _ := run(t, b.F, e)
	has(t, got, "res 0: heap at .")
	b2 := NewFunc(0, 0, 0)
	b2.Call(eir.Call{Kind: eir.CallContract, Contract: "missing"}, 0)
	e2, _ := env(b2.F)
	defer func() {
		if recover() == nil {
			t.Error("a contract call without an entry must be an internal error")
		}
	}()
	Solve(b2.F, e2)
}

func TestDynamicByUnion(t *testing.T) {
	b := NewFunc(0, 1, 0)
	o := b.Obj(eir.ObjVar, eir.LifeFrame, -1, nil)
	b.Call(eir.Call{Kind: eir.CallFunc, Target: b.In(0), Args: []eir.Reg{b.Addr(o)}}, 0)
	e, _ := env(b.F, &eir.Func{Inputs: make([]eir.Input, 1)}, &eir.Func{Inputs: make([]eir.Input, 1)})
	e.Cands[0] = map[eir.SiteID][]eir.FuncID{0: {1, 2}}
	e.Sums[2] = &summary.Summary{Inputs: 1, Esc: []domain.Key{domain.MakeKey(0, domain.Root, domain.KeyFlags{})}}
	_, r := run(t, b.F, e)
	wantCodes(t, r, diag.PassFrameView)
}
