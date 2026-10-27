// Ported from internal/escape2/sched/heaponly_writes_test.go at the go-cutover tag.

package sched

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/escape2/eir"
	"compiler/internal/types2"
	"compiler/pu"
)

// composeNode composes a Node whose next holds the address of the
// fixture's frame int.
func (x *hoFixture) composeNode() eir.Reg {
	m := x.main
	return m.add(eir.Instr{Op: eir.OpCompose, Args: regs(m.addr(x.localInt)), Fields: compat.Append[[]int](nil, compat.Append[int](nil, 1)), Type: x.node}, true)
}

// TestHeapOnlyBoxWrite checks that boxing a struct value into an interface
// writes it: the box is a place of its type (review3
// r16_heaponly_via_iface_assert_value).
func TestHeapOnlyBoxWrite(t *testing.T) {
	x := newHoFixture(t)
	m := x.main
	v := x.composeNode()
	if !x.heapOnly(x.node, 1) {
		t.Errorf("Node.next: a composed value written nowhere, but not heap-only")
	}
	box := m.obj(eir.Object{Kind: eir.ObjBox, Type: x.node, Life: eir.LifeFrame, Loop: -1})
	m.add(eir.Instr{Op: eir.OpConv, X: v, Type: x.node, To: types2.NewInterfaceType(nil, nil), Obj: box}, true)
	if x.heapOnly(x.node, 1) {
		t.Errorf("Node.next: a Node with a frame address is boxed, but heap-only")
	}
}

// TestHeapOnlyRecordWrite checks that a method value's receiver and a
// closure's captures are written into their records.
func TestHeapOnlyRecordWrite(t *testing.T) {
	for _, op := range []eir.Op{eir.OpMethodVal, eir.OpClosure} {
		x := newHoFixture(t)
		m := x.main
		v := x.composeNode()
		// The records' types as eir makes them: the receiver's type, the
		// closure's func type.
		rt := x.node
		if op == eir.OpClosure {
			rt = types2.NewSignatureType(nil, nil, nil, nil, nil, false)
		}
		rec := m.obj(eir.Object{Kind: eir.ObjTemp, Type: rt, Life: eir.LifeFrame, Loop: -1})
		in := eir.Instr{Op: op, Obj: rec, Callee: eir.NoFunc}
		if op == eir.OpMethodVal {
			in.X = v
		} else {
			in.Args = regs(v)
		}
		m.add(in, true)
		if x.heapOnly(x.node, 1) {
			t.Errorf("%v: Node.next: a Node with a frame address is written into a record, but heap-only", op)
		}
	}
}

// TestHeapOnlyWholeAddress checks that the address of a whole struct
// escaping does not make the classes within it escaped: a store through an
// address of unknown place does not reach them, and a store through the
// escaped address takes a field step into the class.
func TestHeapOnlyWholeAddress(t *testing.T) {
	x := newHoFixture(t)
	sink := x.pr.decl(lookupFunc(t, x.pkg, "sink"))
	local := sink.obj(eir.Object{Kind: eir.ObjVar, Type: types2.Typ[types2.Int], Life: eir.LifeFrame, Loop: -1})
	sink.store(sink.input(0), sink.addr(local), x.intPtr)
	set := x.pr.decl(lookupFunc(t, x.pkg, "set"))
	set.store(set.field(set.input(0), 1), set.input(1), x.intPtr)
	m := x.main
	m.callStatic(set, m.addr(x.nodeObj), m.heap())
	m.callStatic(sink, m.addr(m.obj(eir.Object{Kind: eir.ObjVar, Type: x.intPtr, Life: eir.LifeFrame, Loop: -1})))
	if !x.heapOnly(x.node, 1) {
		t.Errorf("Node.next: only a whole Node's address escapes, but not heap-only")
	}
	m.callStatic(set, m.addr(x.nodeObj), m.addr(x.localInt))
	if x.heapOnly(x.node, 1) {
		t.Errorf("Node.next: a frame address is stored through the escaped address, but heap-only")
	}
}

// TestHeapOnlyContractResult checks a contract's result: rt.CopyValue(p)
// holds what p points to.
func TestHeapOnlyContractResult(t *testing.T) {
	for _, heap := range []bool{true, false} {
		x := newHoFixture(t)
		m := x.main
		if heap {
			m.store(m.field(m.addr(x.nodeObj), 1), m.heap(), x.intPtr)
		} else {
			m.store(m.field(m.addr(x.nodeObj), 1), m.addr(x.localInt), x.intPtr)
		}
		c := pu.New(eir.Call{Kind: eir.CallContract, Contract: "pluto/rt.CopyValue", Callee: eir.NoFunc,
			Args: regs(m.field(m.addr(x.nodeObj), 1)), Results: regs(m.reg())})
		m.call(c)
		otherObj := m.obj(eir.Object{Kind: eir.ObjVar, Type: x.other, Life: eir.LifeFrame, Loop: -1})
		m.store(m.field(m.addr(otherObj), 0), c.Results[0], x.intPtr)
		if got := x.heapOnly(x.other, 0); got != heap {
			t.Errorf("Other.q holds a copy of Node.next (heap-only %v): heap-only %v", heap, got)
		}
	}
}

// TestHeapOnlyPlainVarParts checks a frame variable whose address is also
// stepped into: loads through a part leave it plain (what is loaded from
// it is what was stored), a store through a part does not.
func TestHeapOnlyPlainVarParts(t *testing.T) {
	for _, partStore := range []bool{false, true} {
		x := newHoFixture(t)
		m := x.main
		v := m.obj(eir.Object{Kind: eir.ObjVar, Type: x.intPtr, Life: eir.LifeFrame, Loop: -1})
		m.store(m.addr(v), m.heap(), x.intPtr)
		part := m.field(m.addr(v), eir.AnyStep)
		m.load(part, types2.Typ[types2.Uintptr])
		if partStore {
			m.store(part, m.addr(x.localInt), x.intPtr)
		}
		m.store(m.field(m.addr(x.nodeObj), 1), m.load(m.addr(v), x.intPtr), x.intPtr)
		if got := x.heapOnly(x.node, 1); got == partStore {
			t.Errorf("store through a part %v: Node.next heap-only %v", partStore, got)
		}
	}
}

// TestHeapOnlyCounted checks that a field holding a counted box is never
// heap-only: what a load of it reads is bounded by its holder's count.
func TestHeapOnlyCounted(t *testing.T) {
	pkg := typecheck(t, `
type C struct{ p synced[int] }
`)
	pr := newProg()
	m := pr.fn("main", eir.FuncKey{}, nil)
	m.f.External = true
	c := lookupType(t, pkg, "C")
	o := m.obj(eir.Object{Kind: eir.ObjVar, Type: c, Life: eir.LifeFrame, Loop: -1})
	m.store(m.field(m.addr(o), 0), m.heap(), c.Underlying().(*types2.Struct).Field(0).Type())
	if stageB(pr.p).HeapOnly(c, 0) {
		t.Errorf("C.p: holds a synced box, but heap-only")
	}
}

// TestHeapOnlyClosureCapturesClosure checks that a closure captured by
// another closure is a write of a func value, which holds no struct field,
// and does not make the analysis give up on every class (the nested
// closures of pluto/rt.heapSortFuncs did, so no field was heap-only and
// an arena's current chunk, section 18.3's own example, was not).
func TestHeapOnlyClosureCapturesClosure(t *testing.T) {
	for _, static := range []bool{true, false} {
		x := newHoFixture(t)
		m := x.main
		m.store(m.field(m.addr(x.nodeObj), 1), m.heap(), x.intPtr)
		sig := types2.NewSignatureType(nil, nil, nil, nil, nil, false)
		inner := x.pr.fn("inner", eir.FuncKey{}, sig)
		outer := x.pr.fn("outer", eir.FuncKey{}, sig)
		in := eir.Instr{Op: eir.OpClosure, Callee: inner.f.ID, Obj: eir.NoObj}
		if !static {
			in.Obj = m.obj(eir.Object{Kind: eir.ObjClosure, Type: sig, Life: eir.LifeFrame, Loop: -1})
			in.Args = regs(m.addr(x.localInt))
		}
		c := m.add(in, true)
		rec := m.obj(eir.Object{Kind: eir.ObjClosure, Type: sig, Life: eir.LifeFrame, Loop: -1})
		m.add(eir.Instr{Op: eir.OpClosure, Callee: outer.f.ID, Obj: rec, Args: regs(c)}, true)
		if !x.heapOnly(x.node, 1) {
			t.Errorf("static=%v: Node.next: only heap is stored, but not heap-only", static)
		}
	}
}

// TestHeapOnlyCapturedScalarJoin checks that a closure capturing a join of
// values that hold no pointers (a phi of register 0, as of a captured
// uintptr in pluto/alloc/debug) does not make the analysis give up on
// every class.
func TestHeapOnlyCapturedScalarJoin(t *testing.T) {
	x := newHoFixture(t)
	m := x.main
	m.store(m.field(m.addr(x.nodeObj), 1), m.heap(), x.intPtr)
	phi := m.add(eir.Instr{Op: eir.OpPhi, Args: regs(0, 0)}, true)
	sig := types2.NewSignatureType(nil, nil, nil, nil, nil, false)
	lit := x.pr.fn("lit", eir.FuncKey{}, sig)
	rec := m.obj(eir.Object{Kind: eir.ObjClosure, Type: sig, Life: eir.LifeFrame, Loop: -1})
	m.add(eir.Instr{Op: eir.OpClosure, Callee: lit.f.ID, Obj: rec, Args: regs(phi)}, true)
	if !x.heapOnly(x.node, 1) {
		t.Errorf("Node.next: only heap is stored, but not heap-only")
	}
}
