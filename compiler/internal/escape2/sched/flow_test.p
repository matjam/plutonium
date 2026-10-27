// Ported from internal/escape2/sched/flow_test.go at the go-cutover tag.

package sched

import (
	"pluto/testing"

	"compiler/internal/escape2/eir"
	"compiler/internal/types2"
	"compiler/pu"
)

const flowSrc = `
func add(a, b int) int { return a + b }
func sub(a, b int) int { return a - b }
func mul(a, b int) int { return a * b }
func apply(f func(int, int) int, x int) int { return f(x, x) }
func pick() func(int, int) int { return add }
func hold(f func(int, int) int) {}
`

type flowFixture struct {
	pkg                            *types2.Package
	pr                             *prog
	add, sub, mul, apply, pick, hd *fbuild
	sig                            *types2.Signature
	main                           *fbuild
	applyCall                      *eir.Call
}

func newFlowFixture(t *testing.T) *flowFixture {
	x := pu.New(flowFixture{pkg: typecheck(t, flowSrc), pr: newProg()})
	x.add = x.pr.decl(lookupFunc(t, x.pkg, "add"))
	x.sub = x.pr.decl(lookupFunc(t, x.pkg, "sub"))
	x.mul = x.pr.decl(lookupFunc(t, x.pkg, "mul"))
	x.apply = x.pr.decl(lookupFunc(t, x.pkg, "apply"))
	x.pick = x.pr.decl(lookupFunc(t, x.pkg, "pick"))
	x.hd = x.pr.decl(lookupFunc(t, x.pkg, "hold"))
	x.sig = lookupFunc(t, x.pkg, "add").Signature()
	// apply calls its input f.
	f := x.apply.input(0)
	x.applyCall = pu.New(eir.Call{Kind: eir.CallFunc, Sig: x.sig, Target: f, Args: regs(x.apply.input(1), x.apply.input(1)), Results: regs(x.apply.reg())})
	x.apply.call(x.applyCall)
	// pick returns add.
	x.pick.ret(x.pick.add(eir.Instr{Op: eir.OpClosure, Callee: x.add.f.ID, Obj: eir.NoObj}, true), 0)
	x.main = x.pr.fn("main", eir.FuncKey{}, nil)
	x.main.f.External = true
	// mul is a func value of the program, stored in package memory.
	m := x.main.add(eir.Instr{Op: eir.OpClosure, Callee: x.mul.f.ID, Obj: eir.NoObj}, true)
	x.main.add(eir.Instr{Op: eir.OpSink, Sink: eir.SinkGlobal, X: m}, false)
	return x
}

func (x *flowFixture) closure(b *fbuild, g *fbuild) eir.Reg {
	return b.add(eir.Instr{Op: eir.OpClosure, Callee: g.f.ID, Obj: eir.NoObj}, true)
}

// TestFlowInputs checks that a call of an input reaches what the callers
// pass, through results too, and not every func value of the signature.
func TestFlowInputs(t *testing.T) {
	x := newFlowFixture(t)
	m := x.main
	m.callStatic(x.apply, x.closure(m, x.sub), m.heap())
	r := m.callStatic(x.pick)
	m.callStatic(x.apply, r[0], m.heap())
	got := stageB(x.pr.p).Candidates(x.apply.f, x.applyCall)
	if want := idsOf(x.add, x.sub); !sameIDs(got, want) {
		t.Errorf("candidates %v, want %v (add through pick's result, sub passed; not mul)", got, want)
	}
}

// TestFlowUnknown checks the cases where the set is not known: a value
// loaded from memory, and a function whose inputs some callers the
// program does not show may bind.
func TestFlowUnknown(t *testing.T) {
	x := newFlowFixture(t)
	m := x.main
	m.callStatic(x.apply, x.closure(m, x.sub), m.heap())
	m.callStatic(x.apply, m.load(m.heap(), x.sig), m.heap())
	got := stageB(x.pr.p).Candidates(x.apply.f, x.applyCall)
	if want := idsOf(x.add, x.sub, x.mul); !sameIDs(got, want) {
		t.Errorf("loaded from memory: candidates %v, want every func value %v", got, want)
	}

	// apply passed to the contract table: its inputs are unknown.
	y := newFlowFixture(t)
	m = y.main
	m.callStatic(y.apply, y.closure(m, y.sub), m.heap())
	m.call(pu.New(eir.Call{Kind: eir.CallContract, Contract: "pluto/rt.InitValue", Args: regs(m.heap(), y.closure(m, y.apply))}))
	got = stageB(y.pr.p).Candidates(y.apply.f, y.applyCall)
	if want := idsOf(y.add, y.sub, y.mul); !sameIDs(got, want) {
		t.Errorf("apply given to the contract table: candidates %v, want every func value %v", got, want)
	}

	// A function no call reaches never runs (the lowering oracle checks
	// that every call the generated code makes is an EIR call): its call
	// reaches nothing. Called from outside Plutonium (External), its
	// inputs are unknown.
	z := newFlowFixture(t)
	if got := stageB(z.pr.p).Candidates(z.apply.f, z.applyCall); len(got) != 0 {
		t.Errorf("apply never called: candidates %v, want none", got)
	}
	z.apply.f.External = true
	got = stageB(z.pr.p).Candidates(z.apply.f, z.applyCall)
	if want := idsOf(z.add, z.mul); !sameIDs(got, want) {
		t.Errorf("apply called from outside: candidates %v, want every func value %v", got, want)
	}
}

// TestFlowRuntimeCallers checks the functions the runtime may call although
// no EIR call reaches them: pluto/rt's, and methods named as the
// scheduler's.
func TestFlowRuntimeCallers(t *testing.T) {
	rt := types2.NewPackage("pluto/rt", "rt")
	if !calledByRuntime(pu.New(eir.Func{Pkg: rt})) {
		t.Errorf("a function of pluto/rt: not called by the runtime")
	}
	pkg := typecheck(t, `
type S struct{}
func (*S) Spawn(f func()) {}
func (*S) Other(f func()) {}
`)
	S := types2.NewPointer(lookupType(t, pkg, "S"))
	if !calledByRuntime(pu.New(eir.Func{Pkg: pkg, Key: eir.FuncKey{Obj: method(t, S, "Spawn")}})) {
		t.Errorf("a method named Spawn: not called by the runtime")
	}
	if calledByRuntime(pu.New(eir.Func{Pkg: pkg, Key: eir.FuncKey{Obj: method(t, S, "Other")}})) {
		t.Errorf("a method named Other: called by the runtime")
	}
}
