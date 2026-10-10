package types2_test

import (
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
	conf := types2.Config{}
	pkg, err := conf.Check("test", fset, []*ast.File{f}, info)
	return pkg, info, err
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
