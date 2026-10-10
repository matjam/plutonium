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

func TestParseEnumTypes(t *testing.T) {
	src := `package p

type Color enum {
	Red
	Green // second
	Blue
}

type Errno enum uint16 {
	// No error.
	OK = 0
	EPERM
	EIO = 2 + 3
}

type Order enum { Less = -1; Equal; Greater }

type Empty enum {}

func f(c Color) {
	switch c {
	case Red, Color.Blue:
	}
	for x := range Color {
		_ = x
	}
}
`
	f, err := ParseFile(token.NewFileSet(), "enum.p", src, ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var enums []*ast.EnumType
	ast.Inspect(f, func(n ast.Node) bool {
		if e, ok := n.(*ast.EnumType); ok {
			enums = append(enums, e)
		}
		return true
	})
	if len(enums) != 4 {
		t.Fatalf("got %d enum types, want 4", len(enums))
	}
	names := func(e *ast.EnumType) string {
		var s []string
		for _, m := range e.Members {
			s = append(s, m.Name.Name)
		}
		return strings.Join(s, " ")
	}
	color, errno, order, empty := enums[0], enums[1], enums[2], enums[3]
	if color.Base != nil || names(color) != "Red Green Blue" {
		t.Errorf("Color: base %v, members %q", color.Base, names(color))
	}
	if c := color.Members[1].Comment; c == nil || c.Text() != "second\n" {
		t.Errorf("Green comment = %v, want \"second\"", c)
	}
	if id, ok := errno.Base.(*ast.Ident); !ok || id.Name != "uint16" {
		t.Errorf("Errno base = %#v, want uint16", errno.Base)
	}
	if d := errno.Members[0].Doc; d == nil || d.Text() != "No error.\n" {
		t.Errorf("OK doc = %v, want \"No error.\"", d)
	}
	if errno.Members[1].Value != nil || errno.Members[1].Assign.IsValid() {
		t.Errorf("EPERM has a value")
	}
	if _, ok := errno.Members[2].Value.(*ast.BinaryExpr); !ok {
		t.Errorf("EIO value = %T, want *ast.BinaryExpr", errno.Members[2].Value)
	}
	if u, ok := order.Members[0].Value.(*ast.UnaryExpr); !ok || u.Op != token.SUB || names(order) != "Less Equal Greater" {
		t.Errorf("Order: first value %#v, members %q", order.Members[0].Value, names(order))
	}
	if len(empty.Members) != 0 || empty.End() != empty.Rbrace+1 {
		t.Errorf("Empty: %d members, end %d", len(empty.Members), empty.End())
	}

	for _, bad := range []struct{ src, want string }{
		{"package p\ntype E enum { 1 }", "expected '}'"},
		{"package p\ntype E enum { A = }", "expected operand"},
		{"package p\nvar enum = 1", "expected 'IDENT'"},
	} {
		if _, err := ParseFile(token.NewFileSet(), "bad.p", bad.src, 0); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%q: got error %v, want %q", bad.src, err, bad.want)
		}
	}
}

func TestParseUnionTypes(t *testing.T) {
	src := `package p

type DeviceState union {
	Off
	Probing struct{ attempt int } // retrying
	Ready   struct {
		irq  uint8
		base uintptr
	}
	// Failed holds the error.
	Failed syscall.Errno
	A, B   int
}

type Result[T any, E any] union {
	Ok  T
	Err E
}

type PTE packed union uint64 {
	present bits[bool, 1]

	Swapped case false {
		slot bits[uint64, 63]
	}
	Mapped case true { frame bits[uint64, 40] }
}

type Desc packed union le [8]byte {
	kind, sub bits[uint8, 4]
	Code case 1, 2 {}
	Data case kind(3) {
		flags bits { R W }
	}
}

type Old packed union { u uint8 }

type union int

type U2 union
type U3 union[int]
type U4 = union

func f(union int) int {
	var x union = union(union)
	return int(x) + union
}
`
	f, err := ParseFile(token.NewFileSet(), "union.p", src, ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var unions []*ast.UnionType
	var packed []*ast.PackedType
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.UnionType:
			unions = append(unions, n)
		case *ast.PackedType:
			packed = append(packed, n)
		}
		return true
	})
	if len(unions) != 4 {
		t.Fatalf("got %d union types, want 4", len(unions))
	}
	dev, res, pte, desc := unions[0], unions[1], unions[2], unions[3]
	if dev.Packed.IsValid() || dev.Pos() != dev.Union || len(dev.Fields.List) != 5 {
		t.Fatalf("DeviceState: packed %v, %d variants", dev.Packed, len(dev.Fields.List))
	}
	if off := dev.Fields.List[0]; off.Names != nil || off.Type.(*ast.Ident).Name != "Off" {
		t.Errorf("Off variant = %#v, want a lone name", off)
	}
	if c := dev.Fields.List[1].Comment; c == nil || c.Text() != "retrying\n" {
		t.Errorf("Probing comment = %v", c)
	}
	if d := dev.Fields.List[3].Doc; d == nil || d.Text() != "Failed holds the error.\n" {
		t.Errorf("Failed doc = %v", d)
	}
	if n := len(dev.Fields.List[4].Names); n != 2 {
		t.Errorf("A, B: %d names", n)
	}
	if len(res.Fields.List) != 2 {
		t.Errorf("Result: %d variants", len(res.Fields.List))
	}

	if !pte.Packed.IsValid() || pte.Pos() != pte.Packed || pte.Order != ast.NativeEndian {
		t.Errorf("PTE: packed %v order %v", pte.Packed, pte.Order)
	}
	if id, ok := pte.Backing.(*ast.Ident); !ok || id.Name != "uint64" {
		t.Errorf("PTE backing = %#v", pte.Backing)
	}
	if len(pte.Fields.List) != 3 {
		t.Fatalf("PTE: %d fields", len(pte.Fields.List))
	}
	if _, ok := pte.Fields.List[0].Type.(*ast.IndexListExpr); !ok {
		t.Errorf("PTE tag type = %T", pte.Fields.List[0].Type)
	}
	vc, ok := pte.Fields.List[1].Type.(*ast.VariantCase)
	if !ok || len(vc.Values) != 1 || len(vc.Fields.List) != 1 || vc.Pos() != vc.Case || vc.End() != vc.Fields.Closing+1 {
		t.Fatalf("Swapped = %#v", pte.Fields.List[1].Type)
	}
	if desc.Order != ast.LittleEndian || len(desc.Fields.List) != 3 {
		t.Errorf("Desc: order %v, %d fields", desc.Order, len(desc.Fields.List))
	}
	if vc := desc.Fields.List[1].Type.(*ast.VariantCase); len(vc.Values) != 2 || len(vc.Fields.List) != 0 {
		t.Errorf("Code: %d values, %d fields", len(vc.Values), len(vc.Fields.List))
	}
	if vc := desc.Fields.List[2].Type.(*ast.VariantCase); len(vc.Values) != 1 {
		t.Errorf("Data: %d values", len(vc.Values))
	} else if _, ok := vc.Fields.List[0].Type.(*ast.BitsGroup); !ok {
		t.Errorf("Data flags = %T", vc.Fields.List[0].Type)
	}
	// packed union { ... } is a packed type backed by a type named union.
	if len(packed) != 1 || packed[0].Backing.(*ast.Ident).Name != "union" {
		t.Errorf("packed union {...}: %d packed types", len(packed))
	}

	for _, bad := range []struct{ src, want string }{
		{"package p\ntype U union { 1 }", "expected '}'"},
		{"package p\ntype U packed union uint8 { A case { } }", "expected operand"},
		{"package p\ntype U packed union uint8 { A case 1 x }", "expected '{'"},
		{"package p\nvar x union { A int }", "expected ';'"},
	} {
		if _, err := ParseFile(token.NewFileSet(), "bad.p", bad.src, 0); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%q: got error %v, want %q", bad.src, err, bad.want)
		}
	}
}

// TestParseTypeConstructorExprs checks that type constructors with constant
// arguments parse in expression position, as composite literal types and
// conversions, not only in declarations.
func TestParseTypeConstructorExprs(t *testing.T) {
	src := `package p

func f(x uint32) {
	r := ring[int, 8]{}
	a := aligned[uint32, 4](x)
	b := aligned[[4]byte, 16]{1, 2}
	m := Map[string, []int]{}
	_, _, _, _ = r, a, b, m
}
`
	f, err := ParseFile(token.NewFileSet(), "ctor.p", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var lists []*ast.IndexListExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if ix, ok := n.(*ast.IndexListExpr); ok {
			lists = append(lists, ix)
		}
		return true
	})
	if len(lists) != 4 {
		t.Fatalf("got %d instance expressions, want 4", len(lists))
	}
	for i, want := range []string{"8", "4", "16"} {
		lit, ok := lists[i].Indices[1].(*ast.BasicLit)
		if !ok || lit.Value != want {
			t.Errorf("instance %d: second argument %#v, want constant %s", i, lists[i].Indices[1], want)
		}
	}
	if _, ok := lists[3].Indices[1].(*ast.ArrayType); !ok {
		t.Errorf("Map[string, []int]: second argument %T, want *ast.ArrayType", lists[3].Indices[1])
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
	for _, dir := range []string{"lib", "examples", "testdata", "bench", "cmd"} {
		n := len(files)
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".p") && !strings.Contains(path, "invalid") {
				files = append(files, path)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == n {
			t.Errorf("no .p files under %s", dir)
		}
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
