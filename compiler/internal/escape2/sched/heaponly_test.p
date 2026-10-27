// Ported from internal/escape2/sched/heaponly_test.go at the go-cutover tag.

package sched

import (
	"pluto/testing"
	"pluto/time"

	"compiler/compat"
	"compiler/internal/escape2/eir"
	"compiler/internal/types2"
	"compiler/pu"
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
	x := pu.New(hoFixture{pkg: pkg, node: lookupType(t, pkg, "Node"), alt: lookupType(t, pkg, "Alt"),
		raw: lookupType(t, pkg, "Raw"), other: lookupType(t, pkg, "Other"), pr: newProg()})
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
	x.main.f.Facts.FuncValues = funcIDs(set.f.ID)
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
	v := m.add(eir.Instr{Op: eir.OpCompose, Args: regs(m.addr(x.localInt)), Fields: compat.Append[[]int](nil, compat.Append[int](nil, 1)), Type: x.node}, true)
	m.f.Facts.FieldWrites = compat.Append[eir.FieldWrite](nil, eir.FieldWrite{Type: x.node, From: v})
	if x.heapOnly(x.alt, 1) {
		t.Errorf("Alt.next: a Node with a frame address is written, but heap-only")
	}
}

// TestHeapOnlyPhiRing checks origins and register types through a ring of
// phis (loops make them): every register on the ring has the same sources,
// found once, not by walking every path around the ring.
func TestHeapOnlyPhiRing(t *testing.T) {
	for _, frame := range []bool{false, true} {
		x := newHoFixture(t)
		m := x.main
		const n = 60
		rs := compat.Make[eir.Reg](n)
		for i := range rs {
			rs[i] = m.reg()
		}
		for i := range rs {
			src := m.heap()
			if frame && i == n/2 {
				src = m.addr(x.localInt)
			}
			// regs[i] = phi(regs[i-1], regs[i+1], src)
			m.add(eir.Instr{Op: eir.OpPhi, Dst: rs[i], Args: regs(rs[(i+n-1)%n], rs[(i+1)%n], src)}, false)
		}
		m.store(m.field(m.addr(x.nodeObj), 1), rs[0], x.intPtr)
		if got := x.heapOnly(x.node, 1); got == frame {
			t.Errorf("frame address on the ring %v: Node.next heap-only %v", frame, got)
		}
	}
}

// TestHeapOnlyAnyStep checks places at an offset not known (eir.AnyStep,
// unsafe.Add): a store there may land in any class within the object, a
// load from there may come from any, and neither crashes the walk.
func TestHeapOnlyAnyStep(t *testing.T) {
	x := newHoFixture(t)
	m := x.main
	any := m.field(m.addr(x.nodeObj), eir.AnyStep)
	m.store(any, m.addr(x.localInt), x.intPtr)
	if x.heapOnly(x.node, 1) {
		t.Errorf("Node.next: a frame address is stored somewhere in a Node, but heap-only")
	}
	if !x.heapOnly(x.other, 0) {
		t.Errorf("Other.q: never written, but not heap-only")
	}

	y := newHoFixture(t)
	m = y.main
	m.store(m.field(m.addr(y.nodeObj), 1), m.heap(), y.intPtr)
	otherObj := m.obj(eir.Object{Kind: eir.ObjVar, Type: y.other, Life: eir.LifeFrame, Loop: -1})
	v := m.load(m.field(m.addr(y.nodeObj), eir.AnyStep), y.intPtr)
	m.store(m.field(m.addr(otherObj), 0), v, y.intPtr)
	if !y.heapOnly(y.node, 1) {
		t.Errorf("Node.next: only heap stored, but not heap-only")
	}
	if y.heapOnly(y.other, 0) {
		t.Errorf("Other.q: holds what was loaded from an unknown offset in a Node, but heap-only")
	}
}

// TestRegTypePhiDiamonds checks that inferring a register's type through
// phis is linear: 40 levels of diamonds over sources of unknown type are 2^40
// paths, which a search that explores a register once per path never
// finishes (the hang on the translated type checker).
//
// The Go test ran stage B in a goroutine and gave up after 20 seconds; here
// it runs in the test, which checks the time it took (a hang is ended by
// the test runner's timeout).
func TestRegTypePhiDiamonds(t *testing.T) {
	x := newHoFixture(t)
	m := x.main
	a := m.add(eir.Instr{Op: eir.OpHeap, Heap: eir.HeapAlloc}, true)
	b := m.add(eir.Instr{Op: eir.OpHeap, Heap: eir.HeapAlloc}, true)
	for i := 0; i < 40; i++ {
		a2 := m.add(eir.Instr{Op: eir.OpPhi, Args: regs(a, b)}, true)
		b2 := m.add(eir.Instr{Op: eir.OpPhi, Args: regs(b, a)}, true)
		a, b = a2, b2
	}
	// A store through a: its location type comes from a's type, which no
	// source has.
	m.add(eir.Instr{Op: eir.OpStore, X: a, Y: m.heap()}, false)
	start := time.Now()
	got := stageB(x.pr.p).HeapOnly(x.node, 1)
	if time.Since(start) > 20*time.Second {
		t.Fatal("stage B did not finish in 20 seconds on 40 levels of phi diamonds")
	}
	// The store's type is unknown: nothing may be called heap-only.
	if got {
		t.Errorf("Node.next heap-only after a store of unknown type")
	}
}

// TestStageBRecursiveTypes checks that stage B finishes on recursive types:
// a struct that points to itself, two that point to each other, and a
// generic type instantiated with itself, used through stores, loads and a
// loop's phis.
func TestStageBRecursiveTypes(t *testing.T) {
	pkg := typecheck(t, `
type Node struct{ next *Node; v *int }
type A struct{ b *B }
type B struct{ a *A; v *int }
type G[T any] struct{ self *G[T]; v T }
func use[T any](g *G[T]) {}
`)
	node, a, b := lookupType(t, pkg, "Node"), lookupType(t, pkg, "A"), lookupType(t, pkg, "B")
	use := lookupFunc(t, pkg, "use")
	gint, err := types2.Instantiate(nil, lookupType(t, pkg, "G"), typs(types2.Typ[types2.Int]), false)
	if err != nil {
		t.Fatal(err)
	}
	ggint, err := types2.Instantiate(nil, lookupType(t, pkg, "G"), typs(gint), false)
	if err != nil {
		t.Fatal(err)
	}
	pr := newProg()
	m := pr.fn("main", eir.FuncKey{}, nil)
	m.f.External = true
	ub := pr.decl(use)
	tp := use.Signature().TypeParams().At(0)
	gT := use.Signature().Params().At(0).Type().(*types2.Pointer).Elem()
	// use[T] walks g.self.self... in a loop: a phi ring through loads.
	g := ub.input(0)
	cur := ub.reg()
	next := ub.load(ub.field(cur, 0), types2.NewPointer(gT))
	ub.add(eir.Instr{Op: eir.OpPhi, Dst: cur, Args: regs(g, next)}, false)
	ub.store(ub.field(cur, 0), next, types2.NewPointer(gT))
	ub.store(ub.field(cur, 1), ub.load(ub.field(next, 1), tp), tp)
	m.f.Facts.Instances = insts(eir.Instance{Obj: use, Args: typs(gint)}, eir.Instance{Obj: use, Args: typs(ggint)})
	for _, T := range []types2.Type{node, a, b, gint, ggint} {
		o := m.obj(eir.Object{Kind: eir.ObjVar, Type: T, Life: eir.LifeFrame, Loop: -1})
		p := m.addr(o)
		// p.f0 = p.f0.f0: a load and store through the recursive field.
		inner := m.load(m.field(p, 0), types2.NewPointer(T))
		m.store(m.field(p, 0), m.load(m.field(inner, 0), types2.NewPointer(T)), types2.NewPointer(T))
	}
	m.callStatic(ub, m.heap())

	// The Go test ran stage B in a goroutine with a 20 second timeout: see
	// TestRegTypePhiDiamonds.
	start := time.Now()
	f := stageB(pr.p)
	for _, T := range []types2.Type{node, a, b, gint, ggint} {
		_ = f.HeapOnly(T, 0) // must answer, whatever the answer
	}
	if time.Since(start) > 20*time.Second {
		t.Fatal("stage B did not finish in 20 seconds on recursive types")
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
	boxInt, err := types2.Instantiate(nil, lookupType(t, pkg, "Box"), typs(types2.Typ[types2.Int]), false)
	if err != nil {
		t.Fatal(err)
	}
	pb := x.pr.decl(put)
	pb.f.External = true // its inputs are of unknown source
	pb.store(pb.field(pb.input(0), 0), pb.input(1), types2.NewPointer(tp))
	x.main.f.Facts.Instances = insts(eir.Instance{Obj: put, Args: typs(types2.Typ[types2.Int])})
	if x.heapOnly(boxInt, 0) {
		t.Errorf("Box[int].p: the generic put stores unknown pointers, but heap-only")
	}
	if x.heapOnly(boxT, 0) {
		t.Errorf("Box[T].p: heap-only")
	}
}
