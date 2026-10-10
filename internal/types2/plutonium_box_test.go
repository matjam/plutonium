package types2_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/parser"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// TestBoxes checks the types owned, shared, synced and weak (language spec
// section 5.6): they select and index like the pointer they act as, compare
// by address and with nil, convert to a *T view and to unsafe.Pointer, and
// may be embedded like *T.
func TestBoxes(t *testing.T) {
	pkg, info := mustCheck(t, `package p

import "unsafe"

type Node owner struct {
	val  int
	next owned[Node]
}

func (n *Node) Val() int { return n.val }

type Style struct{ size int }

func (s *Style) Apply() int { return s.size }

type Widget owner struct {
	owned[Style] // embedded box: Style's methods are promoted
}

type Panel struct {
	shared[Style] // a shared box copies and counts
}

type Valuer interface{ Val() int }

func use() {
	n := new(Node)
	n.val = 1
	n.next = new(Node)
	_ = n.next.val + n.Val() + (*n).val
	var v *Node = (*Node)(n)
	_ = &*n == v
	_ = n == nil
	var e owned[Node]
	_ = e != nil
	_ = unsafe.Pointer(n)
	var w Widget
	_ = w.Apply() + w.size
	var p Panel
	_ = p.Apply()
	var vi Valuer = v
	_ = vi
	arr := new([3]int)
	arr[1] = 2
	_ = len(arr) + arr[1]
	for i, x := range arr {
		_, _ = i, x
	}
	_ = arr[:]
	var s shared[Node]
	wk := weak[shared[Node]](s)
	s2, ok := wk.Upgrade()
	_, _ = s2, ok
	var sy synced[Node]
	_ = weak[synced[Node]](sy)
}
`)
	_ = info
	for name, want := range map[string]bool{"Widget": true, "Panel": false} {
		if got := types2.IsOwner(pkg.Scope().Lookup(name).Type()); got != want {
			t.Errorf("IsOwner(%s) = %v, want %v", name, got, want)
		}
	}

	mustFail(t, `package p

type R interface{ Read() int }

var b owned[R]
`, "owned cannot hold an interface type (R): use an owning interface")

	mustFail(t, `package p

type R interface{ Read() int }

func f() { _ = new(R) }
`, "owned cannot hold an interface type (R)")

	mustFail(t, `package p

type Node struct{}

var w weak[Node]
`, "weak takes shared[T] or synced[T], not Node")

	mustFail(t, `package p

type Style struct{}

type Widget struct {
	owned[Style]
}
`, "Widget must be declared owner: its embedded field Style is an owned[Style], which is an owner")

	// A value-receiver method of an owner consumes it and never satisfies
	// an interface method (G18).
	mustFail(t, `package p

type Buf owner struct{ b []byte }

func (b Buf) Finish() string { return "" }

type Finisher interface{ Finish() string }

func f(b Buf) { var x Finisher = b; _ = x }
`, "method Finish has a value receiver of owner type Buf")

	mustFail(t, `package p

type Node struct{}

func f(p *Node) { var o owned[Node] = owned[Node](p); _ = o }
`, "cannot convert p")
}

// TestOwnerComparison checks that owners compare when their underlying type
// does (G13).
func TestOwnerComparison(t *testing.T) {
	mustCheck(t, `package p

type Fd owner int32

func (f *Fd) Drop() {}

func same(f, g *Fd) bool { return *f == *g }
`)
}

// TestOwnerConversion checks G14: a value converts into an owner type only
// in the package that declares it; an owner converts to a plain value, and
// between owner types, anywhere.
func TestOwnerConversion(t *testing.T) {
	fset := token.NewFileSet()
	parse := func(name, src string) *ast.File {
		f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	sys := parse("sys.p", `package sys

type Fd owner int32

func (f *Fd) Drop() {}

func Open() Fd { return Fd(3) } // allowed: Fd's own package

type Other owner int32
`)
	conf := types2.Config{Importer: unsafeOnly{}}
	sysPkg, err := conf.Check("sys", fset, []*ast.File{sys}, nil)
	if err != nil {
		t.Fatal(err)
	}
	imp := mapImporter{"sys": sysPkg}
	check := func(src string) error {
		conf := types2.Config{Importer: imp}
		_, err := conf.Check("main", fset, []*ast.File{parse("main.p", src)}, nil)
		return err
	}
	if err := check(`package main

import "sys"

func f() int32 {
	fd := sys.Open()
	o := sys.Other(fd) // owner to owner: moves
	_ = o
	g := sys.Open()
	return int32(g) // owner to plain value: reads
}
`); err != nil {
		t.Fatal(err)
	}
	err = check(`package main

import "sys"

func f() { fd := sys.Fd(3); _ = fd }
`)
	if err == nil || !strings.Contains(err.Error(), "cannot convert 3 (untyped int constant) to owner type sys.Fd outside package sys") {
		t.Fatalf("got %v", err)
	}
}

type mapImporter map[string]*types2.Package

func (m mapImporter) Import(path string) (*types2.Package, error) {
	if p := m[path]; p != nil {
		return p, nil
	}
	return unsafeOnly{}.Import(path)
}
