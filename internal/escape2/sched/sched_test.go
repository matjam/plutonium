package sched

import (
	"testing"

	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/types2"
)

// TestSCCOrder checks that components come callees first and hold exactly
// the mutually recursive functions.
func TestSCCOrder(t *testing.T) {
	pr := newProg()
	a := pr.fn("a", eir.FuncKey{}, nil)
	b := pr.fn("b", eir.FuncKey{}, nil)
	c := pr.fn("c", eir.FuncKey{}, nil)
	d := pr.fn("d", eir.FuncKey{}, nil)
	a.callStatic(b)
	b.callStatic(a)
	b.callStatic(c)
	d.callStatic(a)
	d.callStatic(c)
	f := stageB(pr.p)
	comps := buildGraph(pr.p, f.rule).sccs()
	pos := map[eir.FuncID]int{}
	for i, comp := range comps {
		for _, id := range comp {
			pos[id] = i
		}
	}
	if len(comps) != 3 {
		t.Fatalf("got %d components, want 3: %v", len(comps), comps)
	}
	if pos[a.f.ID] != pos[b.f.ID] {
		t.Errorf("a and b in different components: %v", comps)
	}
	if pos[c.f.ID] >= pos[a.f.ID] || pos[a.f.ID] >= pos[d.f.ID] {
		t.Errorf("components not callees first: %v", comps)
	}
}

// TestSCCDeepChain checks that a long call chain does not need a deep
// stack: Tarjan's algorithm here is iterative.
func TestSCCDeepChain(t *testing.T) {
	pr := newProg()
	const n = 200000
	fs := make([]*fbuild, n)
	for i := range fs {
		fs[i] = pr.fn("f", eir.FuncKey{}, nil)
	}
	for i := 0; i+1 < n; i++ {
		fs[i].callStatic(fs[i+1])
	}
	fs[n-1].callStatic(fs[0]) // one cycle of all
	comps := buildGraph(pr.p, unionRule{newCands(pr.p, newTypeTable(), newInstClosure(newTypeTable()))}).sccs()
	if len(comps) != 1 || len(comps[0]) != n {
		t.Fatalf("got %d components", len(comps))
	}
}

const ifaceSrc = `
type Stringer interface{ String() string }
type A struct{}
func (A) String() string { return "a" }
type B struct{}
func (*B) String() string { return "b" }
type C struct{}
func (C) String() string { return "c" }
type D struct{}
func (D) String(int) string { return "d" }
type Outer struct{ A }
type Inner struct{ p *B }
func use(s Stringer) string { return s.String() }
`

// TestIfaceCandidates checks the candidates of an interface call: methods
// of converted types and their by-value parts, never of a type that is not
// converted, and never a method of another signature.
func TestIfaceCandidates(t *testing.T) {
	pkg := typecheck(t, ifaceSrc)
	str := lookupType(t, pkg, "Stringer")
	A, B, C, D := lookupType(t, pkg, "A"), lookupType(t, pkg, "B"), lookupType(t, pkg, "C"), lookupType(t, pkg, "D")
	Outer, Inner := lookupType(t, pkg, "Outer"), lookupType(t, pkg, "Inner")
	pr := newProg()
	aS := pr.decl(method(t, A, "String"))
	bS := pr.decl(method(t, types2.NewPointer(B), "String"))
	pr.decl(method(t, C, "String"))
	pr.decl(method(t, D, "String"))
	use := pr.decl(lookupFunc(t, pkg, "use"))
	m := method(t, str, "String")
	call := &eir.Call{Kind: eir.CallIface, Method: m, Iface: str, Sig: m.Signature(), Target: use.input(0)}
	use.call(call)
	main := pr.fn("main", eir.FuncKey{}, nil)
	// Outer is converted: its promoted String (A's) is a candidate; Inner
	// is converted: its field's pointee B is a part the formatting walk
	// reaches. C is never converted; D's String has another signature.
	main.f.Facts.Converted = []types2.Type{Outer, Inner, D}
	f := stageB(pr.p)
	got := f.Candidates(use.f, call)
	if want := idsOf(aS, bS); !sameIDs(got, want) {
		t.Errorf("candidates %v, want %v", got, want)
	}
}

// TestGenericConversion checks that a conversion in a generic body is
// substituted over the instantiations of its type parameter.
func TestGenericConversion(t *testing.T) {
	pkg := typecheck(t, ifaceSrc+`
func box[T any](x T) any { return x }
`)
	str := lookupType(t, pkg, "Stringer")
	A, C := lookupType(t, pkg, "A"), lookupType(t, pkg, "C")
	box := lookupFunc(t, pkg, "box")
	tp := box.Signature().TypeParams().At(0)
	pr := newProg()
	pr.decl(method(t, A, "String"))
	cS := pr.decl(method(t, C, "String"))
	b := pr.decl(box)
	b.f.Facts.Converted = []types2.Type{tp}
	main := pr.fn("main", eir.FuncKey{}, nil)
	main.f.Facts.Instances = []eir.Instance{{Obj: box, Args: []types2.Type{C}}}
	use := pr.decl(lookupFunc(t, pkg, "use"))
	m := method(t, str, "String")
	call := &eir.Call{Kind: eir.CallIface, Method: m, Iface: str, Target: use.input(0)}
	use.call(call)
	f := stageB(pr.p)
	if got, want := f.Candidates(use.f, call), idsOf(cS); !sameIDs(got, want) {
		t.Errorf("candidates %v, want %v", got, want)
	}
}

// TestInstantiationClosure checks the closure through generic bodies, and
// that an unbounded chain is open rather than cut short.
func TestInstantiationClosure(t *testing.T) {
	pkg := typecheck(t, `
func F[T any](x T) { G[[]T](nil) }
func G[U any](x U) {}
func R[V any](x V) {}
`)
	F, G, R := lookupFunc(t, pkg, "F"), lookupFunc(t, pkg, "G"), lookupFunc(t, pkg, "R")
	T := F.Signature().TypeParams().At(0)
	U := G.Signature().TypeParams().At(0)
	V := R.Signature().TypeParams().At(0)
	pr := newProg()
	fb := pr.decl(F)
	fb.f.Facts.Instances = []eir.Instance{{Obj: G, Args: []types2.Type{types2.NewSlice(T)}}}
	rb := pr.decl(R)
	rb.f.Facts.Instances = []eir.Instance{{Obj: R, Args: []types2.Type{types2.NewSlice(V)}}}
	main := pr.fn("main", eir.FuncKey{}, nil)
	intT := types2.Typ[types2.Int]
	main.f.Facts.Instances = []eir.Instance{{Obj: F, Args: []types2.Type{intT}}, {Obj: R, Args: []types2.Type{intT}}}
	f := stageB(pr.p)
	got, ok := f.Instantiated(U)
	if !ok || len(got) != 1 || !types2.Identical(got[0], types2.NewSlice(intT)) {
		t.Errorf("G's U: %v %v, want [[]int]", got, ok)
	}
	if _, ok := f.Instantiated(V); ok {
		t.Errorf("R's V is known; want open (R[T] instantiates R[[]T])")
	}
}

// TestTypeParamCandidates checks method calls on type parameters: by the
// instantiations, or every method of the name when they are open.
func TestTypeParamCandidates(t *testing.T) {
	pkg := typecheck(t, ifaceSrc+`
func call[T Stringer](x T) string { return x.String() }
`)
	A, C := lookupType(t, pkg, "A"), lookupType(t, pkg, "C")
	callF := lookupFunc(t, pkg, "call")
	tp := callF.Signature().TypeParams().At(0)
	str := lookupType(t, pkg, "Stringer")
	m := method(t, str, "String")
	pr := newProg()
	aS := pr.decl(method(t, A, "String"))
	pr.decl(method(t, C, "String"))
	cb := pr.decl(callF)
	c := &eir.Call{Kind: eir.CallTypeParam, Method: m, Iface: tp, Args: []eir.Reg{cb.input(0)}}
	cb.call(c)
	main := pr.fn("main", eir.FuncKey{}, nil)
	main.f.Facts.Instances = []eir.Instance{{Obj: callF, Args: []types2.Type{A}}}
	f := stageB(pr.p)
	if got, want := f.Candidates(cb.f, c), idsOf(aS); !sameIDs(got, want) {
		t.Errorf("candidates %v, want %v", got, want)
	}
}

// TestFuncValueCandidates checks func-value calls: by signature, with type
// parameters matching anything.
func TestFuncValueCandidates(t *testing.T) {
	pkg := typecheck(t, `
func f1(int) int { return 0 }
func f2(int) int { return 0 }
func f3(string) int { return 0 }
func apply[T any](g func(T) int, x T) int { return g(x) }
`)
	pr := newProg()
	b1 := pr.decl(lookupFunc(t, pkg, "f1"))
	b2 := pr.decl(lookupFunc(t, pkg, "f2"))
	b3 := pr.decl(lookupFunc(t, pkg, "f3"))
	ap := lookupFunc(t, pkg, "apply")
	ab := pr.decl(ap)
	main := pr.fn("main", eir.FuncKey{}, nil)
	main.f.Facts.FuncValues = []eir.FuncID{b1.f.ID, b3.f.ID} // f2 is never a value
	gsig := ap.Signature().Params().At(0).Type().(*types2.Signature)
	c := &eir.Call{Kind: eir.CallFunc, Sig: gsig, Target: ab.input(0)}
	ab.call(c)
	intSig := lookupFunc(t, pkg, "f1").Signature()
	c2 := &eir.Call{Kind: eir.CallFunc, Sig: intSig, Target: main.heap()}
	main.call(c2)
	f := stageB(pr.p)
	if got, want := f.Candidates(ab.f, c), idsOf(b1, b3); !sameIDs(got, want) {
		t.Errorf("generic call: candidates %v, want %v", got, want)
	}
	if got, want := f.Candidates(main.f, c2), idsOf(b1); !sameIDs(got, want) {
		t.Errorf("func(int) int call: candidates %v, want %v", got, want)
	}
	_ = b2
}
