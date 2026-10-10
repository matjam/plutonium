package types2_test

import (
	"fmt"
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

func TestForbiddenConstructs(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"map", "package p\nvar m map[int]int", "map types are not permitted"},
		{"chan", "package p\nvar c chan int", "channel types are not permitted"},
		{"go", "package p\nfunc f() {}\nfunc g() { go f() }", "go statements are not permitted"},
		{"select", "package p\nfunc g() { select {} }", "select statements are not permitted"},
		{"reflect", "package p\nimport \"reflect\"\nvar _ = reflect.TypeOf", "import of reflect is not permitted"},
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
