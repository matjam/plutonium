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

// TestLocalClosureCandidates checks that a call of a func value the
// function made itself, from known literals through copies and phis,
// reaches only those literals; with any other source it is matched by
// signature.
func TestLocalClosureCandidates(t *testing.T) {
	pkg := typecheck(t, `
func f(func()) {}
func other() {}
`)
	sig := lookupFunc(t, pkg, "other").Signature()
	for _, mixed := range []bool{false, true} {
		pr := newProg()
		other := pr.decl(lookupFunc(t, pkg, "other"))
		l1 := pr.fn("lit1", eir.FuncKey{}, sig)
		l2 := pr.fn("lit2", eir.FuncKey{}, sig)
		fb := pr.decl(lookupFunc(t, pkg, "f"))
		fb.f.Facts.FuncValues = []eir.FuncID{other.f.ID}
		a := fb.add(eir.Instr{Op: eir.OpClosure, Callee: l1.f.ID, Obj: eir.NoObj}, true)
		b := fb.add(eir.Instr{Op: eir.OpClosure, Callee: l2.f.ID, Obj: eir.NoObj}, true)
		bc := fb.add(eir.Instr{Op: eir.OpCopy, X: b}, true)
		args := []eir.Reg{a, bc}
		if mixed {
			args = append(args, fb.input(0))
		}
		target := fb.add(eir.Instr{Op: eir.OpPhi, Args: args}, true)
		c := &eir.Call{Kind: eir.CallFunc, Sig: sig, Target: target, Implicit: eir.ImplDefer}
		fb.call(c)
		want := idsOf(l1, l2)
		if mixed {
			want = idsOf(other, l1, l2) // every func() value
		}
		if got := stageB(pr.p).Candidates(fb.f, c); !sameIDs(got, want) {
			t.Errorf("mixed %v: candidates %v, want %v", mixed, got, want)
		}
	}
}

// TestDeferredClosureCandidates checks the deferred call of a literal: the
// closure is stored in a frame temporary and loaded at the exit. While only
// those loads and stores reach the temporary, the call reaches the literal
// alone; once its address goes elsewhere, it is matched by signature.
func TestDeferredClosureCandidates(t *testing.T) {
	pkg := typecheck(t, `
func f() {}
func other() {}
func keep(*func()) {}
`)
	sig := lookupFunc(t, pkg, "other").Signature()
	for _, escapes := range []bool{false, true} {
		pr := newProg()
		other := pr.decl(lookupFunc(t, pkg, "other"))
		keep := pr.decl(lookupFunc(t, pkg, "keep"))
		lit := pr.fn("lit", eir.FuncKey{}, sig)
		fb := pr.decl(lookupFunc(t, pkg, "f"))
		fb.f.Facts.FuncValues = []eir.FuncID{other.f.ID}
		tmp := fb.obj(eir.Object{Kind: eir.ObjTemp, Type: sig, Life: eir.LifeFrame, Loop: -1})
		cl := fb.add(eir.Instr{Op: eir.OpClosure, Callee: lit.f.ID, Obj: eir.NoObj}, true)
		fb.store(fb.addr(tmp), cl, sig)
		if escapes {
			fb.callStatic(keep, fb.addr(tmp))
		}
		target := fb.load(fb.addr(tmp), sig)
		c := &eir.Call{Kind: eir.CallFunc, Sig: sig, Target: target, Implicit: eir.ImplDefer}
		fb.call(c)
		want := idsOf(lit)
		if escapes {
			want = idsOf(other, lit)
		}
		if got := stageB(pr.p).Candidates(fb.f, c); !sameIDs(got, want) {
			t.Errorf("address escapes %v: candidates %v, want %v", escapes, got, want)
		}
	}
}

// TestInternIdentical checks that identical types intern to one ID even
// where their text differs (parameter names, struct tags), and that types
// that differ do not.
func TestInternIdentical(t *testing.T) {
	pkg := typecheck(t, `
type F1 func(x int, s string) (n int)
type F2 func(int, string) int
type F3 func(int, string) bool
type S1 struct { p *int "tag" ; g func(a int) }
type S2 struct { p *int ; g func(b int) }
`)
	tt := newTypeTable()
	u := func(n string) types2.Type { return lookupType(t, pkg, n).Underlying() }
	if tt.intern(u("F1")) != tt.intern(u("F2")) {
		t.Errorf("func(x int, s string) (n int) and func(int, string) int: two IDs")
	}
	if tt.intern(u("F2")) == tt.intern(u("F3")) {
		t.Errorf("func(int, string) int and func(int, string) bool: one ID")
	}
	if tt.intern(u("S1")) == tt.intern(u("S2")) {
		t.Errorf("structs with different tags: one ID")
	}
	if _, ok := tt.lookup(types2.NewSignatureType(nil, nil, nil,
		types2.NewTuple(types2.NewParam(0, nil, "q", types2.Typ[types2.Int]), types2.NewParam(0, nil, "", types2.Typ[types2.String])),
		types2.NewTuple(types2.NewParam(0, nil, "", types2.Typ[types2.Int])), false)); !ok {
		t.Errorf("an identical signature made apart is not found")
	}
}

// TestFuncValueCandidates checks func-value calls: by signature, a generic
// signature standing for its instantiations, and nothing for a call in code
// that is never instantiated.
func TestFuncValueCandidates(t *testing.T) {
	pkg := typecheck(t, `
func f1(int) int { return 0 }
func f2(int) int { return 0 }
func f3(string) int { return 0 }
func f4(bool) int { return 0 }
func apply[T any](g func(T) int, x T) int { return g(x) }
func lit[U any]() func(U) int { return func(U) int { return 0 } }
`)
	for _, instantiate := range []bool{false, true} {
		pr := newProg()
		b1 := pr.decl(lookupFunc(t, pkg, "f1"))
		pr.decl(lookupFunc(t, pkg, "f2"))
		b3 := pr.decl(lookupFunc(t, pkg, "f3"))
		b4 := pr.decl(lookupFunc(t, pkg, "f4"))
		ap := lookupFunc(t, pkg, "apply")
		ab := pr.decl(ap)
		litF := lookupFunc(t, pkg, "lit")
		pr.decl(litF)
		// The literal in lit[U]: func(U) int, a func value.
		litSig := litF.Signature().Results().At(0).Type().(*types2.Signature)
		lb := pr.fn("lit literal", eir.FuncKey{}, litSig)
		main := pr.fn("main", eir.FuncKey{}, nil)
		// f2 is never a value; the literal is one (OpClosure).
		main.f.Facts.FuncValues = []eir.FuncID{b1.f.ID, b3.f.ID, b4.f.ID}
		main.add(eir.Instr{Op: eir.OpClosure, Callee: lb.f.ID, Obj: eir.NoObj}, true)
		if instantiate {
			main.f.Facts.Instances = []eir.Instance{
				{Obj: ap, Args: []types2.Type{types2.Typ[types2.Int]}},
				{Obj: ap, Args: []types2.Type{types2.Typ[types2.String]}},
				{Obj: litF, Args: []types2.Type{types2.Typ[types2.String]}},
			}
		}
		gsig := ap.Signature().Params().At(0).Type().(*types2.Signature)
		c := &eir.Call{Kind: eir.CallFunc, Sig: gsig, Target: ab.input(0)}
		ab.call(c)
		intSig := lookupFunc(t, pkg, "f1").Signature()
		c2 := &eir.Call{Kind: eir.CallFunc, Sig: intSig, Target: main.heap()}
		main.call(c2)
		f := stageB(pr.p)
		var want []eir.FuncID
		if instantiate {
			// apply[int] and apply[string]: f1, f3 and lit[string]'s literal;
			// never f4, whose func(bool) int no instantiation gives.
			want = idsOf(b1, b3, lb)
		}
		if got := f.Candidates(ab.f, c); !sameIDs(got, want) {
			t.Errorf("instantiated %v: generic call: candidates %v, want %v", instantiate, got, want)
		}
		// A func(int) int call: f1 only (lit's literal is func(string) int
		// where instantiated, and made nowhere otherwise).
		if got, want := f.Candidates(main.f, c2), idsOf(b1); !sameIDs(got, want) {
			t.Errorf("instantiated %v: func(int) int call: candidates %v, want %v", instantiate, got, want)
		}
	}
}
