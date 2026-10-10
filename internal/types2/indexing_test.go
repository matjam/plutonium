package types2_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// indexingPrelude declares containers for the indexing tests: Vec has the
// full set of methods on pointer receivers, RO only Index, WO only SetIndex.
const indexingPrelude = `package p

type Vec struct{ data []int }

func (v *Vec) Index(i int) int          { return v.data[i] }
func (v *Vec) SetIndex(i int, x int)    { v.data[i] = x }
func (v *Vec) IndexOk(i int) (int, bool) { return 0, false }
func (v *Vec) Len() int                 { return len(v.data) }

type RO struct{}

func (RO) Index(k string) float64 { return 0 }

type WO struct{}

func (*WO) SetIndex(k string, v int) {}

type P struct{ x, y int }

type PV struct{}

func (*PV) Index(i int) P       { return P{} }
func (*PV) SetIndex(i int, p P) {}

type Indexable[K, V any] interface {
	Index(k K) V
	SetIndex(k K, v V)
}

type Ints []int

func (Ints) Index(i int) string { return "" }

`

func TestIndexing(t *testing.T) {
	_, info := mustCheck(t, indexingPrelude+`
func f(v *Vec, r RO, w WO) {
	var u Vec
	a := u[0]
	u[1] = a
	u[2] += 3
	u[3]++
	b, ok := u[4]
	_, _ = b, ok
	c := v[5]
	v[6] = c
	n := len(u) + len(v)
	_ = n
	var x float64 = r["k"]
	_ = x
	w["k"] = 1
	var s Ints
	var e int = s[0] // built-in indexing, not Index
	_ = e
}

func g[M Indexable[K, V], K, V any](m M, k K, v V) V {
	m[k] = v
	return m[k]
}

func h[C interface{ Len() int }](c C) int { return len(c) }

var _ = g[*Vec, int, int]
var _ = h[*Vec]
`)
	// The checker records the element type for reads and writes.
	for e, tv := range info.Types {
		ix, ok := e.(*ast.IndexExpr)
		if !ok {
			continue
		}
		if id, ok := ix.X.(*ast.Ident); ok && id.Name == "r" {
			if tv.Type.String() != "float64" {
				t.Errorf("r[k] has type %s, want float64", tv.Type)
			}
		}
	}
}

func TestIndexesByMethod(t *testing.T) {
	cases := []struct {
		T    types2.Type
		want bool
	}{
		{types2.Typ[types2.String], false},
		{types2.NewSlice(types2.Typ[types2.Int]), false},
		{types2.NewArray(types2.Typ[types2.Int], 3), false},
		{types2.NewPointer(types2.NewArray(types2.Typ[types2.Int], 3)), false},
		{types2.NewPointer(types2.Typ[types2.Int]), true},
		{types2.NewStruct(nil, nil), true},
	}
	for _, c := range cases {
		if got := types2.IndexesByMethod(c.T); got != c.want {
			t.Errorf("IndexesByMethod(%s) = %v, want %v", c.T, got, c.want)
		}
	}
	if types2.LenByMethod(types2.Typ[types2.String]) || !types2.LenByMethod(types2.NewStruct(nil, nil)) {
		t.Errorf("LenByMethod is wrong for string or struct")
	}
}

func TestRangesByMethod(t *testing.T) {
	cases := []struct {
		T    types2.Type
		want bool
	}{
		{types2.Typ[types2.String], false},
		{types2.Typ[types2.Int], false},
		{types2.NewSlice(types2.Typ[types2.Int]), false},
		{types2.NewMap(types2.Typ[types2.Int], types2.Typ[types2.Int]), false},
		{types2.NewPointer(types2.NewArray(types2.Typ[types2.Int], 3)), false},
		{types2.NewSignatureType(nil, nil, nil, nil, nil, false), false},
		{types2.Typ[types2.Float64], true},
		{types2.NewPointer(types2.NewStruct(nil, nil)), true},
		{types2.NewStruct(nil, nil), true},
		{types2.NewWrapper(types2.WrapRing, types2.Typ[types2.Int], 4), true},
		{types2.NewWrapper(types2.WrapVolatile, types2.Typ[types2.Int], 0), false},
	}
	for _, c := range cases {
		if got := types2.RangesByMethod(c.T); got != c.want {
			t.Errorf("RangesByMethod(%s) = %v, want %v", c.T, got, c.want)
		}
	}
}

// TestRangeOverContainer checks the iteration variables' types of a range
// over containers whose All methods return a Seq2, through a pointer and a
// value receiver, and over a type parameter constrained by a method.
func TestRangeOverContainer(t *testing.T) {
	mustCheck(t, `package p

type Seq2[K, V any] func(yield func(K, V) bool)

type M struct{}

func (*M) All() Seq2[string, float64] { return nil }

type L struct{}

func (L) All() func(func(int) bool) { return nil }

func f(m M, p *M, l L) {
	for k, v := range m {
		var _ string = k
		var _ float64 = v
	}
	for k := range p {
		var _ string = k
	}
	for x := range l {
		var _ int = x
	}
}

func g[C interface{ All() Seq2[int, bool] }](c C) {
	for i, b := range c {
		var _ int = i
		var _ bool = b
	}
}
`)
}

func TestIndexingErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"no methods", "type T struct{}\nfunc f(x T) { _ = x[0] }", "cannot index x"},
		{"read without Index", "func f(w *WO) int { return w[\"k\"] }", "cannot read w[\"k\"]: *WO has no method Index"},
		{"assign without SetIndex", "func f(r RO) { r[\"k\"] = 1 }", "cannot assign to r[\"k\"]: RO has no method SetIndex"},
		{"comma-ok without IndexOk", "func f(r RO) { v, ok := r[\"k\"]; _, _ = v, ok }", "cannot use comma-ok on r[\"k\"]: RO has no method IndexOk"},
		{"compound without SetIndex", "func f(r RO) { r[\"k\"] += 1 }", "RO has no method SetIndex"},
		{"key type", "func f(v *Vec) int { return v[\"x\"] }", "cannot use \"x\" (untyped string constant) as int value in index"},
		{"element type", "func f(v *Vec) { v[0] = \"x\" }", "cannot use \"x\" (untyped string constant) as int value in assignment"},
		{"address", "func f(v *Vec) *int { return &v[0] }", "cannot take address of v[0]: it is a copy returned by the Index method of *Vec"},
		{"field assign", "func f(v *PV) { v[0].x = 1 }", "cannot assign to v[0].x: v[0] is a copy returned by the Index method of *PV"},
		{"range", "func f(v *Vec) {\n\tfor x := range v {\n\t\t_ = x\n\t}\n}", "*Vec has no method All returning an iterator"},
		{"range struct", "type S struct{}\nfunc f(s S) {\n\tfor range s {\n\t}\n}", "S has no method All returning an iterator"},
		{"range bad All", "type S struct{}\nfunc (S) All() int { return 0 }\nfunc f(s S) {\n\tfor range s {\n\t}\n}", "method All of S must have the form All() iter.Seq[V]"},
		{"range All with argument", "type S struct{}\nfunc (S) All(n int) func(func(int) bool) { return nil }\nfunc f(s S) {\n\tfor range s {\n\t}\n}", "must have the form"},
		{"range unaddressable", "type S struct{}\nfunc (*S) All() func(func(int) bool) { return nil }\nfunc mk() S { return S{} }\nfunc f() {\n\tfor range mk() {\n\t}\n}", "method All of S has a pointer receiver, and mk() is not addressable"},
		{"range too many variables", "type S struct{}\nfunc (S) All() func(func(int) bool) { return nil }\nfunc f(s S) {\n\tfor k, v := range s {\n\t\t_, _ = k, v\n\t}\n}", "permits only one iteration variable"},
		{"range float", "func f(x float64) {\n\tfor range x {\n\t}\n}", "cannot range over x (variable of type float64)"},
		{"pointer receiver", "func mk() Vec { return Vec{} }\nfunc f() int { return mk()[0] }", "method Index of Vec has a pointer receiver, and mk() is not addressable"},
		{"bad shape", "type B struct{}\nfunc (B) Index(i, j int) int { return 0 }\nfunc f(b B) int { return b[0] }", "method Index must have the form Index(k K) V"},
		{"bad comma-ok", "type B struct{}\nfunc (B) IndexOk(i int) (int, int) { return 0, 0 }\nfunc f(b B) { v, ok := b[0]; _, _ = v, ok }", "must have the form IndexOk(k K) (V, bool)"},
		{"bad setter", "type B struct{}\nfunc (B) SetIndex(i int) {}\nfunc f(b B) { b[0] = 1 }", "must have the form SetIndex(k K, v V)"},
		{"variadic", "type B struct{}\nfunc (B) Index(i ...int) int { return 0 }\nfunc f(b B) int { return b[0] }", "may not be generic or variadic"},
		{"mismatch", "type B struct{}\nfunc (B) Index(i int) int { return 0 }\nfunc (*B) SetIndex(i int, v int64) {}\nfunc f(b B) int { return b[0] }", "methods Index and SetIndex use different key or element types"},
		{"len without Len", "func f(r RO) int { return len(r) }", "invalid argument: r (variable of struct type RO) for built-in len"},
		{"bad Len", "type B struct{}\nfunc (B) Len() int64 { return 0 }\nfunc f(b B) int { return len(b) }", "for built-in len"},
		{"field named Index", "type B struct{ Index int }\nfunc f(b B) int { return b[0] }", "cannot index b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustFail(t, indexingPrelude+c.src, c.want)
		})
	}
}
