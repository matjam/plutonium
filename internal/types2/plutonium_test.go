package types2_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/parser"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// checkSrc type-checks a single-file package and returns the package, the
// recorded type information and the first error.
func checkSrc(t *testing.T, src string) (*types2.Package, *types2.Info, error) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.p", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := &types2.Info{
		Types:      make(map[ast.Expr]types2.TypeAndValue),
		Defs:       make(map[*ast.Ident]types2.Object),
		Uses:       make(map[*ast.Ident]types2.Object),
		Selections: make(map[*ast.SelectorExpr]*types2.Selection),
		Instances:  make(map[*ast.Ident]types2.Instance),
	}
	conf := types2.Config{Importer: unsafeOnly{}}
	pkg, err := conf.Check("test", fset, []*ast.File{f}, info)
	return pkg, info, err
}

// unsafeOnly is an importer that knows only the unsafe package.
type unsafeOnly struct{}

func (unsafeOnly) Import(path string) (*types2.Package, error) {
	if path == "unsafe" {
		return types2.Unsafe, nil
	}
	return nil, fmt.Errorf("no package %q", path)
}

func mustCheck(t *testing.T, src string) (*types2.Package, *types2.Info) {
	t.Helper()
	pkg, info, err := checkSrc(t, src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return pkg, info
}

func mustFail(t *testing.T, src, want string) {
	t.Helper()
	_, _, err := checkSrc(t, src)
	if err == nil {
		t.Fatalf("expected error containing %q, got none", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestCheckGoSubset(t *testing.T) {
	mustCheck(t, `package p

type Pair[K comparable, V any] struct {
	k K
	v V
}

func (p *Pair[K, V]) Key() K { return p.k }

type Shape interface{ Area() float64 }

type Rect struct{ w, h float64 }

func (r *Rect) Area() float64 { return r.w * r.h }

func Max[T ~int | ~int64 | ~float64](a, b T) T {
	if a > b {
		return a
	}
	return b
}

const (
	A = iota
	B
	C = 1 << 62
)

func f() int64 {
	p := Pair[int, string]{k: 1, v: "x"}
	var s Shape = &Rect{2, 3}
	_ = s.Area()
	x := Max(3, 4)
	var arr [4]int
	sl := arr[1:3]
	for i, v := range sl {
		_ = i + v
	}
	return int64(p.Key() + x + C)
}
`)
}

// TestComplexInConstraints checks that complex types may still name type
// set terms, as constraints shared with Go code do, and that untyped
// complex constants with a real value convert as in Go.
func TestComplexInConstraints(t *testing.T) {
	mustCheck(t, `package p

type Number interface {
	~int | ~float64 | ~complex64 | ~complex128
}

func Sq[T Number](x T) T { return x * x }

var a = Sq(3)
var b float64 = 2 + 0i
var c = real(1 + 2i)
`)
}

func TestForbiddenConstructs(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"map", "package p\nvar m map[int]int", "map types are not permitted"},
		{"chan", "package p\nvar c chan int", "channel types are not permitted"},
		{"go", "package p\nfunc f() {}\nfunc g() { go f() }", "go statements are not permitted"},
		{"select", "package p\nfunc g() { select {} }", "select statements are not permitted"},
		{"reflect", "package p\nimport \"reflect\"\nvar _ = reflect.TypeOf", "import of reflect is not permitted"},
		{"recover", "package p\nfunc f() { defer func() { _ = recover() }() }", "recover is not permitted"},
		{"complex variable", "package p\nfunc f(c complex128) complex128 { return c * c }", "complex numbers are not permitted"},
		{"complex constant", "package p\nfunc f() { x := 2i; _ = x }", "complex numbers are not permitted"},
		{"complex type argument", "package p\nfunc id[T any](x T) T { return x }\nvar _ = id(2i)", "complex numbers are not permitted"},
		{"complex conversion", "package p\nvar f float64\nvar _ = complex(f, f)", "complex numbers are not permitted"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { mustFail(t, c.src, c.want) })
	}
}

func TestPlutoniumTypes(t *testing.T) {
	pkg, info := mustCheck(t, `package p

import "unsafe"

type ControlReg packed uint32 {
	mode     bits[uint, 4]
	enable   bits[bool, 1]
	priority bits[uint, 3]
}

type StatusReg packed be uint32 {
	flags bits {
		ENABLE
		READY
	}
	mode bits[uint, 4]
}

type Header packed {
	version uint8
	flags   uint8
	length  uint32
}

type Padded packed {
	hdr Header
	seq aligned[uint32, 4]
}

type UART struct {
	data   volatile[uint8]
	status volatile[uint8]
}

var pageTable aligned[[4096]byte, 4096]
var q ring[int64, 8]

func use(u *UART) uint8 {
	for u.status&1 == 0 {
	}
	u.data = 'x'
	var r ControlReg
	r.mode = 5
	r.enable = true
	var s StatusReg
	s.flags.ENABLE = true
	pageTable[0] = 1
	ok := q.Push(42)
	v, ok2 := q.Pop()
	_ = v
	raw := uint32(r)
	r = ControlReg(raw)
	r2 := ControlReg{mode: 3, priority: 1}
	_ = r2 == r
	_, _ = ok, ok2
	p := (*volatile[uint32])(unsafe.Pointer(uintptr(0xFEE000B0)))
	*p = 0
	return u.data + uint8(r.mode) + uint8(q.Len())
}
`)
	sizes := types2.SizesFor("gc", "amd64")
	for _, c := range []struct {
		name        string
		size, align int64
	}{
		{"ControlReg", 4, 4},
		{"StatusReg", 4, 4},
		{"Header", 6, 1},
		{"Padded", 12, 4},
		{"UART", 2, 1},
	} {
		typ := pkg.Scope().Lookup(c.name).Type()
		if got := sizes.Sizeof(typ); got != c.size {
			t.Errorf("Sizeof(%s) = %d, want %d", c.name, got, c.size)
		}
		if got := sizes.Alignof(typ); got != c.align {
			t.Errorf("Alignof(%s) = %d, want %d", c.name, got, c.align)
		}
	}
	pt := pkg.Scope().Lookup("pageTable").Type()
	if got := sizes.Alignof(pt); got != 4096 {
		t.Errorf("Alignof(pageTable) = %d, want 4096", got)
	}
	q := pkg.Scope().Lookup("q").Type()
	if got := sizes.Sizeof(q); got != 16+8*8 {
		t.Errorf("Sizeof(q) = %d, want %d", got, 16+8*8)
	}

	// Reading a volatile field yields its element type.
	for e, tv := range info.Types {
		if sel, ok := e.(*ast.SelectorExpr); ok && sel.Sel.Name == "status" {
			if tv.Type.String() != "uint8" {
				t.Errorf("u.status has type %s, want uint8", tv.Type)
			}
		}
	}
}

func TestPlutoniumTypeErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"bits too wide", "package p\ntype R packed uint8 { a bits[uint8, 9] }", "uint8 holds only 8 bits"},
		{"backing too small", "package p\ntype R packed uint8 { a bits[uint, 5]; b bits[uint, 4] }", "need 9 bits"},
		{"ring not power of two", "package p\nvar r ring[int, 6]", "not a power of two"},
		{"volatile struct", "package p\ntype S struct{ a int }\nvar v volatile[S]", "must be an integer, bool or pointer"},
		{"positional packed literal", "package p\ntype R packed uint8 { a bits[uint, 4] }\nvar r = R{1}", "must be field: value"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { mustFail(t, c.src, c.want) })
	}
}

const genericMethodDecls = `package p

type S struct{ x int }

func (s S) Apply[U any](f func(int) U) U { return f(s.x) }

func (s *S) Id[U any]() int { return s.x }

type G[T any] struct{ v T }

func (g G[T]) With[U any](u U) (T, U) { return g.v, u }

type I interface{ Id() int }
`

func TestGenericMethods(t *testing.T) {
	_, info := mustCheck(t, genericMethodDecls+`
func f() {
	var s S
	_ = s.Apply[string](func(int) string { return "" })
	_ = s.Apply(func(int) bool { return true })
	_, _ = G[int]{}.With(1.5)
	var h func(func(int) int) int = s.Apply
	_ = h
	m := (*S).Id[bool]
	_ = m
}
`)
	// Each instance records the method's own type arguments only.
	want := map[string][]string{
		"Apply": {"string", "bool", "int"},
		"With":  {"float64"},
		"Id":    {"bool"},
	}
	got := map[string][]string{}
	for id, inst := range info.Instances {
		if _, ok := want[id.Name]; !ok {
			continue
		}
		for i := 0; i < inst.TypeArgs.Len(); i++ {
			got[id.Name] = append(got[id.Name], inst.TypeArgs.At(i).String())
		}
	}
	for name, w := range want {
		g := got[name]
		sort.Strings(g)
		sort.Strings(w)
		if strings.Join(g, ",") != strings.Join(w, ",") {
			t.Errorf("%s: type arguments %v, want %v", name, g, w)
		}
	}
}

func TestGenericMethodErrors(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"implements", "var _ I = &S{}",
			"*S does not implement I (method Id has type parameters, so it cannot implement an interface method)"},
		{"assert", "func f(x I) { _ = x.(*S) }",
			"impossible type assertion: x.(*S)\n\t*S does not implement I (method Id has type parameters"},
		{"method value", "var _ = S{}.Apply", "cannot use generic method S{}.Apply without instantiation"},
		{"method expression", "var _ = S.Apply", "cannot use generic method S.Apply without instantiation"},
		{"pointer method expression", "var _ = (*S).Id", "cannot use generic method (*S).Id without instantiation"},
		{"generic receiver", "var _ = G[int]{}.With", "cannot use generic method G[int]{}.With without instantiation"},
		{"too many type arguments", "var _ = S{}.Apply[int, int]", "got 2 type arguments but want 1"},
		{"cannot infer", "func f(s *S) { _ = s.Id() }", "cannot infer U"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { mustFail(t, genericMethodDecls+c.body, c.want) })
	}
}

func TestCheckSizes(t *testing.T) {
	pkg, _ := mustCheck(t, `package p

type S struct {
	a uint8
	b int64
	c uint16
}
`)
	s := pkg.Scope().Lookup("S").Type()
	sizes := types2.SizesFor("gc", "amd64")
	if got := sizes.Sizeof(s); got != 24 {
		t.Errorf("Sizeof(S) = %d, want 24", got)
	}
	if got := sizes.Alignof(s); got != 8 {
		t.Errorf("Alignof(S) = %d, want 8", got)
	}
}

// rtSrc stands in for pluto/rt: the checker needs only its Allocator type.
const rtSrc = `package rt

import "unsafe"

type Allocator interface {
	Alloc(size, align uintptr) unsafe.Pointer
	Free(p unsafe.Pointer, size, align uintptr)
}
`

// withRT is an importer that knows unsafe and a stub pluto/rt.
type withRT struct{ rt *types2.Package }

func (w withRT) Import(path string) (*types2.Package, error) {
	if path == "pluto/rt" {
		return w.rt, nil
	}
	return unsafeOnly{}.Import(path)
}

// checkWithRT type-checks src with pluto/rt available.
func checkWithRT(t *testing.T, src string) (*types2.Info, error) {
	t.Helper()
	fset := token.NewFileSet()
	parse := func(name, src string) *ast.File {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return f
	}
	rt, err := (&types2.Config{Importer: unsafeOnly{}}).Check("pluto/rt", fset, []*ast.File{parse("rt.p", rtSrc)}, nil)
	if err != nil {
		t.Fatalf("pluto/rt: %v", err)
	}
	info := &types2.Info{
		Types: make(map[ast.Expr]types2.TypeAndValue),
		Uses:  make(map[*ast.Ident]types2.Object),
	}
	conf := types2.Config{Importer: withRT{rt}}
	_, err = conf.Check("test", fset, []*ast.File{parse("test.p", src)}, info)
	return info, err
}

func TestAllocatorArguments(t *testing.T) {
	info, err := checkWithRT(t, `package p

import (
	"pluto/rt"
	"unsafe"
)

type arena struct{}

func (*arena) Alloc(size, align uintptr) unsafe.Pointer { return nil }
func (*arena) Free(p unsafe.Pointer, size, align uintptr) {}

type T struct{ a, b int }

const c int8 = 3

func f(a rt.Allocator, ar *arena, v T) {
	var p1 *T = new(T, a)
	var p2 *T = new(T, ar)
	var p3 *T = new(v, a)
	var p4 *T = new(T{1, 2}, ar)
	var p5 *int = new(42, a)
	var p6 *int8 = new(c, a)
	var s1 []int = make([]int, 2, a)
	var s2 []int = make([]int, 2, 4, ar)
	var s3 []byte = make([]byte, 2, 4.0)
	var s4 []byte = make([]byte, 2, c)
	_, _, _, _, _, _, _, _, _, _ = p1, p2, p3, p4, p5, p6, s1, s2, s3, s4
}
`)
	if err != nil {
		t.Fatal(err)
	}
	// The recorded signatures carry the allocator as a final parameter.
	sigs := map[string]int{}
	for e, tv := range info.Types {
		id, ok := e.(*ast.Ident)
		if !ok || (id.Name != "new" && id.Name != "make") {
			continue
		}
		sig, ok := tv.Type.(*types2.Signature)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%s/%d", id.Name, sig.Params().Len())
		if n := sig.Params().Len(); n > 0 && sig.Params().At(n-1).Type().String() == "pluto/rt.Allocator" {
			key += "/alloc"
		}
		sigs[key]++
	}
	want := map[string]int{"new/2/alloc": 6, "make/3/alloc": 1, "make/4/alloc": 1, "make/3": 2}
	for k, n := range want {
		if sigs[k] != n {
			t.Errorf("%s: %d calls recorded, want %d (all: %v)", k, sigs[k], n, sigs)
		}
	}
}

func TestAllocatorArgumentErrors(t *testing.T) {
	const decls = "package p\nimport \"pluto/rt\"\nvar a rt.Allocator\ntype T struct{ x int }\n"
	cases := []struct{ name, body, want string }{
		{"new not an allocator", "var _ = new(T, 3)", "cannot use 3"},
		{"new value not an allocator", "var _ = new(T{}, \"x\")", "allocator argument to new"},
		{"new too many", "var _ = new(T, a, a)", "expected 1 and an optional allocator, found 3"},
		{"new untyped nil", "var _ = new(nil, a)", "use of untyped nil"},
		{"make not an allocator", "var _ = make([]int, 2, \"x\")", "allocator argument to make"},
		{"make cap then non-allocator", "var _ = make([]int, 2, 3, 4)", "allocator argument to make"},
		{"make too many", "var _ = make([]int, 1, 2, a, a)", "expects 2 or 3 arguments and an optional allocator; found 5"},
		{"make bad capacity before allocator", "var _ = make([]int, 1, \"x\", a)", "cannot convert \"x\""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := checkWithRT(t, decls+c.body)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v does not contain %q", err, c.want)
			}
		})
	}
	// Without pluto/rt there is no allocator type to check against.
	mustFail(t, "package p\ntype T struct{}\nvar x int\nvar _ = new(T, x)", "needs package pluto/rt")
}
