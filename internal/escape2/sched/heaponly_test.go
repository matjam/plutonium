package sched

import (
	"testing"

	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/types2"
)

const hoSrc = `
type Node struct{ n int; next *int }
type Alt Node
type Raw struct{ n int; next *int }
type Other struct{ q *int }
func set(p *Node, x *int) { p.next = x }
func get() *int { return nil }
func sink(q **int) {}
`

type hoFixture struct {
	pkg               *types2.Package
	node, alt, raw    types2.Type
	other             types2.Type
	intPtr            types2.Type
	pr                *prog
	main              *fbuild
	nodeObj, localInt eir.ObjID
}

func newHoFixture(t *testing.T) *hoFixture {
	pkg := typecheck(t, hoSrc)
	x := &hoFixture{pkg: pkg, node: lookupType(t, pkg, "Node"), alt: lookupType(t, pkg, "Alt"),
		raw: lookupType(t, pkg, "Raw"), other: lookupType(t, pkg, "Other"), pr: newProg()}
	x.intPtr = types2.NewPointer(types2.Typ[types2.Int])
	x.main = x.pr.fn("main", eir.FuncKey{}, nil)
	x.main.f.External = true
	x.nodeObj = x.main.obj(eir.Object{Kind: eir.ObjVar, Type: x.node, Life: eir.LifeFrame, Loop: -1})
	x.localInt = x.main.obj(eir.Object{Kind: eir.ObjVar, Type: types2.Typ[types2.Int], Life: eir.LifeFrame, Loop: -1})
	return x
}

func (x *hoFixture) heapOnly(T types2.Type, field int) bool {
	return stageB(x.pr.p).HeapOnly(T, field)
}

// TestHeapOnlyStores checks direct stores, and that types with the same
// underlying struct share a class.
func TestHeapOnlyStores(t *testing.T) {
	x := newHoFixture(t)
	m := x.main
	m.store(m.field(m.addr(x.nodeObj), 1), m.heap(), x.intPtr)
	for _, T := range []types2.Type{x.node, x.alt, x.raw} {
		if !x.heapOnly(T, 1) {
			t.Errorf("%s.next: not heap-only after a heap store", T)
		}
	}
	m.store(m.addr(x.nodeObj, 1), m.addr(x.localInt), x.intPtr)
	for _, T := range []types2.Type{x.node, x.alt, x.raw} {
		if x.heapOnly(T, 1) {
			t.Errorf("%s.next: heap-only after a store of a frame address", T)
		}
	}
	if !x.heapOnly(x.other, 0) {
		t.Errorf("Other.q: never written, but not heap-only")
	}
}

// TestHeapOnlyParams checks inputs: heap only if every static caller passes
// heap.
func TestHeapOnlyParams(t *testing.T) {
	x := newHoFixture(t)
	set := x.pr.decl(lookupFunc(t, x.pkg, "set"))
	set.store(set.field(set.input(0), 1), set.input(1), x.intPtr)
	m := x.main
	m.callStatic(set, m.addr(x.nodeObj), m.heap())
	if !x.heapOnly(x.node, 1) {
		t.Errorf("Node.next: every caller passes heap, but not heap-only")
	}
	m.callStatic(set, m.addr(x.nodeObj), m.addr(x.localInt))
	if x.heapOnly(x.node, 1) {
		t.Errorf("Node.next: a caller passes a frame address, but heap-only")
	}
}

// TestHeapOnlyParamOfFuncValue checks that a function used as a func value
// has inputs of unknown source.
func TestHeapOnlyParamOfFuncValue(t *testing.T) {
	x := newHoFixture(t)
	set := x.pr.decl(lookupFunc(t, x.pkg, "set"))
	set.store(set.field(set.input(0), 1), set.input(1), x.intPtr)
	x.main.callStatic(set, x.main.addr(x.nodeObj), x.main.heap())
	x.main.f.Facts.FuncValues = []eir.FuncID{set.f.ID}
	if x.heapOnly(x.node, 1) {
		t.Errorf("Node.next: set is a func value, its callers unknown, but heap-only")
	}
}

// TestHeapOnlyResults checks call results, and loads of a heap-only class.
func TestHeapOnlyResults(t *testing.T) {
	x := newHoFixture(t)
	get := x.pr.decl(lookupFunc(t, x.pkg, "get"))
	get.ret(get.heap(), 0)
	m := x.main
	r := m.callStatic(get)
	m.store(m.field(m.addr(x.nodeObj), 1), r[0], x.intPtr)
	// Other.q gets what Node.next holds.
	otherObj := m.obj(eir.Object{Kind: eir.ObjVar, Type: x.other, Life: eir.LifeFrame, Loop: -1})
	v := m.load(m.field(m.addr(x.nodeObj), 1), x.intPtr)
	m.store(m.field(m.addr(otherObj), 0), v, x.intPtr)
	if !x.heapOnly(x.node, 1) || !x.heapOnly(x.other, 0) {
		t.Errorf("Node.next %v, Other.q %v: want both heap-only", x.heapOnly(x.node, 1), x.heapOnly(x.other, 0))
	}
	// A second return of a frame address withdraws both.
	get.ret(get.addr(get.obj(eir.Object{Kind: eir.ObjVar, Type: types2.Typ[types2.Int], Life: eir.LifeFrame, Loop: -1})), 0)
	if x.heapOnly(x.node, 1) || x.heapOnly(x.other, 0) {
		t.Errorf("Node.next %v, Other.q %v: want neither heap-only", x.heapOnly(x.node, 1), x.heapOnly(x.other, 0))
	}
}

// TestHeapOnlyUnknownAddress checks stores through addresses of unknown
// place: they reach a class only if its field's address escapes.
func TestHeapOnlyUnknownAddress(t *testing.T) {
	x := newHoFixture(t)
	sink := x.pr.decl(lookupFunc(t, x.pkg, "sink"))
	local := sink.obj(eir.Object{Kind: eir.ObjVar, Type: types2.Typ[types2.Int], Life: eir.LifeFrame, Loop: -1})
	sink.store(sink.input(0), sink.addr(local), x.intPtr)
	m := x.main
	m.store(m.field(m.addr(x.nodeObj), 1), m.heap(), x.intPtr)
	if !x.heapOnly(x.node, 1) {
		t.Errorf("Node.next: no address of it escapes, but not heap-only")
	}
	m.callStatic(sink, m.field(m.addr(x.nodeObj), 1))
	if x.heapOnly(x.node, 1) {
		t.Errorf("Node.next: its address reaches a store of a frame address, but heap-only")
	}
}

// TestHeapOnlyFieldWrites checks the struct writes the facts list.
func TestHeapOnlyFieldWrites(t *testing.T) {
	x := newHoFixture(t)
	m := x.main
	v := m.add(eir.Instr{Op: eir.OpCompose, Args: []eir.Reg{m.addr(x.localInt)}, Fields: [][]int{{1}}, Type: x.node}, true)
	m.f.Facts.FieldWrites = []eir.FieldWrite{{Type: x.node, From: v}}
	if x.heapOnly(x.alt, 1) {
		t.Errorf("Alt.next: a Node with a frame address is written, but heap-only")
	}
}

// TestHeapOnlyGeneric checks that a generic struct's writes reach its
// instances.
func TestHeapOnlyGeneric(t *testing.T) {
	pkg := typecheck(t, `
type Box[T any] struct{ p *T }
func put[T any](b *Box[T], x *T) { b.p = x }
`)
	x := newHoFixture(t)
	put := lookupFunc(t, pkg, "put")
	tp := put.Signature().TypeParams().At(0)
	boxT := put.Signature().Params().At(0).Type().(*types2.Pointer).Elem()
	boxInt, err := types2.Instantiate(nil, lookupType(t, pkg, "Box"), []types2.Type{types2.Typ[types2.Int]}, false)
	if err != nil {
		t.Fatal(err)
	}
	pb := x.pr.decl(put)
	pb.f.External = true // its inputs are of unknown source
	pb.store(pb.field(pb.input(0), 0), pb.input(1), types2.NewPointer(tp))
	x.main.f.Facts.Instances = []eir.Instance{{Obj: put, Args: []types2.Type{types2.Typ[types2.Int]}}}
	if x.heapOnly(boxInt, 0) {
		t.Errorf("Box[int].p: the generic put stores unknown pointers, but heap-only")
	}
	if x.heapOnly(boxT, 0) {
		t.Errorf("Box[T].p: heap-only")
	}
}
