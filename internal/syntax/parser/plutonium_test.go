package parser

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
)

func TestParsePackedTypes(t *testing.T) {
	src := `package p

type ControlReg packed uint32 {
	mode     bits[uint, 4]
	flags    bits {
		CLK_ENABLE
		SMOKE
	}
	priority bits[uint, 2]
	_        bits[uint, 1]
}

type Header packed be {
	version uint8
	flags   uint8
	length  uint32
}

type Block packed le [512]byte {
	magic uint32
	seq   aligned[uint32, 4]
}

var r = ControlReg{}
var x packed { a, b uint8 }
`
	fset := token.NewFileSet()
	f, err := ParseFile(fset, "packed.p", src, 0)
	if err != nil {
		t.Fatal(err)
	}

	var packed []*ast.PackedType
	ast.Inspect(f, func(n ast.Node) bool {
		if p, ok := n.(*ast.PackedType); ok {
			packed = append(packed, p)
		}
		return true
	})
	if len(packed) != 4 {
		t.Fatalf("got %d packed types, want 4", len(packed))
	}

	ctrl := packed[0]
	if ctrl.Order != ast.NativeEndian {
		t.Errorf("ControlReg order = %v, want native", ctrl.Order)
	}
	if id, ok := ctrl.Backing.(*ast.Ident); !ok || id.Name != "uint32" {
		t.Errorf("ControlReg backing = %#v, want uint32", ctrl.Backing)
	}
	if n := len(ctrl.Fields.List); n != 4 {
		t.Fatalf("ControlReg has %d fields, want 4", n)
	}
	group, ok := ctrl.Fields.List[1].Type.(*ast.BitsGroup)
	if !ok || len(group.Names) != 2 || group.Names[1].Name != "SMOKE" {
		t.Errorf("flags field = %#v, want bits group {CLK_ENABLE SMOKE}", ctrl.Fields.List[1].Type)
	}
	if _, ok := ctrl.Fields.List[0].Type.(*ast.IndexListExpr); !ok {
		t.Errorf("mode field type = %T, want *ast.IndexListExpr for bits[uint, 4]", ctrl.Fields.List[0].Type)
	}

	if packed[1].Order != ast.BigEndian || packed[1].Backing != nil {
		t.Errorf("Header: order %v backing %v, want be and no backing", packed[1].Order, packed[1].Backing)
	}
	if packed[2].Order != ast.LittleEndian {
		t.Errorf("Block order = %v, want le", packed[2].Order)
	}
	if _, ok := packed[2].Backing.(*ast.ArrayType); !ok {
		t.Errorf("Block backing = %T, want array type", packed[2].Backing)
	}
}

func TestParseGoSyntax(t *testing.T) {
	src := `package main

type Pair[K comparable, V any] struct {
	k K
	v V
}

type Node[T any] struct{ next *Node[T]; val T }

type Number interface {
	~int | ~int64 | ~float64
}

func Map[T, U any](xs []T, f func(T) U, out []U) {
	for i, x := range xs {
		out[i] = f(x)
	}
}

func (p *Pair[K, V]) Key() K { return p.k }

const (
	A = iota
	B
	C = 1 << iota
)

func f() (a, b int) {
	m := Pair[int, string]{k: 1, v: "x"}
	_ = m
	var arr [4]int
	s := arr[1:3:4]
	_ = s
	var v volatile[uint8]
	_ = v
outer:
	for i := 0; i < 10; i++ {
		for j := range 10 {
			if j == 3 { continue outer }
		}
	}
	goto end
end:
	return 1, 2
}
`
	fset := token.NewFileSet()
	if _, err := ParseFile(fset, "go.p", src, 0); err != nil {
		t.Fatal(err)
	}
}

// TestParseGenericMethods checks that methods may declare type parameters
// and that interface methods may not.
func TestParseGenericMethods(t *testing.T) {
	ok := `package p

func (v *Vec[T]) Map[U any](f func(T) U) Vec[U] { return Vec[U]{} }

func (s S) Pair[A, B any, C comparable](a A, b B, c C) {}

func f() {
	_ = v.Map[int](g)
	_ = v.Map(g)
	_ = s.Pair[int, string, bool]
}
`
	fset := token.NewFileSet()
	f, err := ParseFile(fset, "ok.p", ok, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range []int{1, 3} {
		fd := f.Decls[i].(*ast.FuncDecl)
		if fd.Recv == nil || fd.Type.TypeParams.NumFields() != n {
			t.Errorf("%s: %d type parameters, want %d", fd.Name.Name, fd.Type.TypeParams.NumFields(), n)
		}
	}

	bad := `package p

type I interface {
	M[T any](x T) T
}
`
	_, err = ParseFile(token.NewFileSet(), "bad.p", bad, 0)
	want := "interface method must have no type parameters: a method with type parameters is never called through an interface"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("got error %v, want %q", err, want)
	}
}

// TestParseDefaults checks that parameters may have default values, that
// each defaulted parameter gets a field of its own, and that a default
// shared by several names is rejected.
func TestParseDefaults(t *testing.T) {
	src := `package p

func New[T any](a Allocator = Default()) *T

func Grow(n, m int, factor int = 2, s []int = []int{1, 2}) {}

func Unnamed(int, string = "x")

var f = func(x int = 1) {}
`
	fset := token.NewFileSet()
	f, err := ParseFile(fset, "ok.p", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	type param struct {
		names string
		def   bool
	}
	describe := func(fl *ast.FieldList) []param {
		var out []param
		for _, fld := range fl.List {
			var names []string
			for _, n := range fld.Names {
				names = append(names, n.Name)
			}
			if fld.Default != nil && !fld.Assign.IsValid() {
				t.Errorf("default without the position of its '='")
			}
			out = append(out, param{strings.Join(names, ","), fld.Default != nil})
		}
		return out
	}
	cases := []struct {
		decl int
		want []param
	}{
		{0, []param{{"a", true}}},
		{1, []param{{"n,m", false}, {"factor", true}, {"s", true}}},
		{2, []param{{"", false}, {"", true}}},
	}
	for _, c := range cases {
		fd := f.Decls[c.decl].(*ast.FuncDecl)
		got := describe(fd.Type.Params)
		if len(got) != len(c.want) {
			t.Fatalf("%s: fields %v, want %v", fd.Name.Name, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: field %d is %v, want %v", fd.Name.Name, i, got[i], c.want[i])
			}
		}
		if last := fd.Type.Params.List[len(fd.Type.Params.List)-1]; last.End() != last.Default.End() {
			t.Errorf("%s: field ends at %d, want the end of its default", fd.Name.Name, last.End())
		}
	}
	// The checker, not the parser, rejects defaults outside declarations.
	lit := f.Decls[3].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Values[0].(*ast.FuncLit)
	if lit.Type.Params.List[0].Default == nil {
		t.Errorf("function literal parameter lost its default")
	}

	for _, bad := range []string{
		"package p\n\nfunc f(a, b int = 1) {}\n",
		"package p\n\nfunc f(a = 1, b int) {}\n",
	} {
		_, err := ParseFile(token.NewFileSet(), "bad.p", bad, 0)
		const want = "a default value must follow a single parameter name and its type"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got error %v, want %q", bad, err, want)
		}
	}
}

// TestParseRepoSources parses every .p file shipped in the repository.
func TestParseRepoSources(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	var files []string
	for _, dir := range []string{"pluto", "examples", "testdata"} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".p") && !strings.Contains(path, "invalid") {
				files = append(files, path)
			}
			return nil
		})
	}
	if len(files) == 0 {
		t.Skip("no .p files found")
	}
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		if _, err := ParseFile(fset, path, src, ParseComments); err != nil {
			t.Errorf("%v", err)
		}
	}
}
