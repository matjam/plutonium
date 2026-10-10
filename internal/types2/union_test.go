package types2_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/parser"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

const unionDecls = `package p

type Errno enum int32 { OK; EPERM; EIO = 5; EINTR = 4 }

func (e Errno) Error() string { return "errno" }

type DeviceState union {
	Off
	Probing struct{ attempt int }
	Ready   struct {
		irq  uint8
		base uintptr
	}
	Failed Errno
}

type Result[T any] union {
	Ok  T
	Err Errno
}

type Option[T any] union {
	None
	Some T
}

type Kind enum { Small; Large }

type Shape union {
	Circle struct{ r float64 }
	Rect   struct{ w, h float64 }
	Tagged Kind
	Nothing
}

func (s Shape) Area() float64 {
	switch v := s.(type) {
	case Circle:
		return 3 * v.r * v.r
	case Rect:
		return v.w * v.h
	case Tagged, Nothing:
		return 0
	}
}

type Areaer interface{ Area() float64 }

type PTE packed union uint64 {
	present bits[bool, 1]

	Swapped case false {
		slot bits[uint64, 63]
	}
	Mapped case true {
		writable bits[bool, 1]
		user     bits[bool, 1]
		_        bits[uint, 9]
		frame    bits[uint64, 40]
		_        bits[uint, 11]
		nx       bits[bool, 1]
	}
}

type Desc packed union uint8 {
	kind Kind
	S case Kind.Small { n bits[uint8, 7] }
	L case Kind.Large {}
}

type List union {
	Nil
	Cons struct {
		head int
		tail *List
	}
}

func Map[T, U any](r Result[T], f func(T) U) Result[U] {
	switch v := r.(type) {
	case Ok:
		return Result[U].Ok(f(v))
	case Result[T].Err:
		return Result[U].Err(v)
	}
}
`

func TestUnions(t *testing.T) {
	pkg, info := mustCheck(t, unionDecls+`
var st DeviceState

func use(st DeviceState, r Result[int], pte PTE, d Desc) (int, error) {
	st = DeviceState.Off
	st = DeviceState.Probing{attempt: 1}
	st = DeviceState.Ready{irq: 11, base: 0xFEB0_0000}
	st = DeviceState.Failed(Errno.EIO)
	mk := Result[string].Ok
	_ = mk("x")
	switch s := st.(type) {
	case DeviceState.Off:
	case Probing:
		_ = s.attempt
	case Ready:
		_ = s.irq
	case Failed:
		var e error = s
		_ = e
	}
	switch st.(type) {
	case Off, Probing:
	default:
	}
	n := 0
	switch v := r.(type) {
	case Ok:
		n = v
	case Err:
		if v != Errno.EINTR {
			return 0, v
		}
	}
	if v, ok := r.(Result[int].Ok); ok {
		n += v
	}
	p, ok := st.(DeviceState.Probing)
	_, _ = p.attempt, ok
	_ = st == DeviceState.Off && r != Result[int].Ok(1)
	var a Areaer = Shape.Circle{r: 1}
	var x any = st
	switch x.(type) {
	case DeviceState:
	}
	switch e := pte.(type) {
	case PTE.Mapped:
		n += int(e.frame)
		e.frame++
		pte = PTE.Mapped(e)
	case Swapped:
		_ = e.slot
	}
	_ = pte.present
	pte = PTE(uint64(1))
	pte = PTE.Mapped{writable: true, frame: 7}
	_ = uint64(pte)
	switch e := d.(type) {
	case S:
		_ = e.n
	case L:
		_ = e.kind
	}
	o := Option[Result[int]].Some(r)
	_ = Map(r, func(i int) string { return "" })
	var tail List
	l := List.Cons{head: 1, tail: &tail}
	_, _, _ = a, o, l
	return n, nil
}

// Unions with identical variants convert explicitly.
type A union { X int; Y }
type B union { X int; Y }

func conv(a A) B { return B(a) }

func last(o Option[int]) int {
	switch v := o.(type) {
	case Some:
		return v
	case None:
		return 0
	}
}
`)
	// Every variant name in the code, bare or qualified, is recorded as a
	// use of the variant object, and each declaration as its definition.
	uses := map[string]int{}
	for id, obj := range info.Uses {
		if v, ok := obj.(*types2.Variant); ok {
			if v.Name() != id.Name {
				t.Errorf("%s recorded as a use of variant %s", id.Name, v.Name())
			}
			uses[id.Name]++
		}
	}
	for name, want := range map[string]int{"Off": 4, "Probing": 4, "Ok": 6, "Mapped": 3, "S": 1} {
		if uses[name] != want {
			t.Errorf("%d uses of variant %s, want %d", uses[name], name, want)
		}
	}
	defs := 0
	for _, obj := range info.Defs {
		if v, ok := obj.(*types2.Variant); ok {
			defs++
			if s := v.String(); !strings.HasPrefix(s, "variant ") {
				t.Errorf("variant object string %q", s)
			}
		}
	}
	if defs != 22 {
		t.Errorf("%d variant definitions, want 22", defs)
	}
	if s := pkg.Scope().Lookup("DeviceState").Type().Underlying().String(); s != "union{Off; Probing struct{attempt int}; Ready struct{irq uint8; base uintptr}; Failed test.Errno}" {
		t.Errorf("DeviceState underlying = %s", s)
	}
	if s := pkg.Scope().Lookup("Desc").Type().Underlying().String(); s != "packed union uint8 { kind bits[test.Kind, 1]; S case 0 { n bits[uint8, 7] }; L case 1 { } }" {
		t.Errorf("Desc underlying = %s", s)
	}
}

// TestUnionImported checks variant selection, switches and export rules
// across packages.
func TestUnionImported(t *testing.T) {
	fset := token.NewFileSet()
	parse := func(name, src string) *ast.File {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	conf := types2.Config{Importer: unsafeOnly{}}
	dev, err := conf.Check("dev", fset, []*ast.File{parse("dev.p", "package dev\n\ntype State union { Off; On int; hidden }\n\ntype R[T any] union { Ok T; Err int }\n")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	conf.Importer = pkgImporter{"dev": dev}
	check := func(src string) error {
		_, err := conf.Check("main", fset, []*ast.File{parse("main.p", "package main\n\nimport \"dev\"\n\n"+src)}, nil)
		return err
	}
	if err := check("func f(s dev.State, r dev.R[int]) int {\n\ts = dev.State.On(1)\n\ts = dev.State.Off\n\tswitch v := s.(type) {\n\tcase On:\n\t\treturn v\n\tcase dev.State.Off:\n\tdefault:\n\t}\n\tif v, ok := r.(dev.R[int].Ok); ok {\n\t\treturn v\n\t}\n\t_ = dev.R[string].Ok{}\n\treturn 0\n}\n"); err == nil || !strings.Contains(err.Error(), "invalid composite literal type string") {
		t.Fatalf("got %v", err)
	}
	if err := check("func f(s dev.State, r dev.R[int]) int {\n\ts = dev.State.On(1)\n\tswitch v := s.(type) {\n\tcase On:\n\t\treturn v\n\tcase dev.State.Off:\n\tdefault:\n\t}\n\tif v, ok := r.(dev.R[int].Ok); ok {\n\t\treturn v\n\t}\n\treturn 0\n}\n"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range []struct{ src, want string }{
		{"var s = dev.State.hidden", "cannot refer to unexported variant hidden of union dev.State"},
		{"var s = dev.State.hidden{}", "cannot refer to unexported variant hidden of union dev.State"},
		{"func f(s dev.State) {\n\tswitch s.(type) {\n\tcase dev.State.hidden:\n\tdefault:\n\t}\n}", "cannot refer to unexported variant hidden"},
		{"func f(s dev.State) {\n\tswitch s.(type) {\n\tcase Off, On:\n\t}\n}", "missing case in type switch on s (dev.State): hidden"},
		{"func f(s dev.State) {\n\tswitch s.(type) {\n\tcase hidden:\n\tdefault:\n\t}\n}", "hidden is not a variant of union dev.State"},
	} {
		if err := check(c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: got %v, want %q", c.src, err, c.want)
		}
	}
}

func TestUnionLayout(t *testing.T) {
	pkg, _ := mustCheck(t, unionDecls)
	sizes := types2.SizesFor("gc", "amd64")
	for _, c := range []struct {
		name        string
		size, align int64
		tag, off    int64
	}{
		{"DeviceState", 24, 8, 1, 8},
		{"Shape", 24, 8, 1, 8},
		{"List", 24, 8, 1, 8},
	} {
		T := pkg.Scope().Lookup(c.name).Type()
		if s, a := sizes.Sizeof(T), sizes.Alignof(T); s != c.size || a != c.align {
			t.Errorf("%s: size %d align %d, want %d %d", c.name, s, a, c.size, c.align)
		}
		tag, off := types2.UnionLayout(sizes, T.Underlying().(*types2.TaggedUnion))
		if tag != c.tag || off != c.off {
			t.Errorf("%s: tag %d payload offset %d, want %d %d", c.name, tag, off, c.tag, c.off)
		}
	}
	if s := sizes.Sizeof(pkg.Scope().Lookup("PTE").Type()); s != 8 {
		t.Errorf("PTE: size %d, want 8", s)
	}
	errno := types2.UnionVariants(pkg.Scope().Lookup("DeviceState").Type())
	if len(errno) != 4 || errno[3].Name() != "Failed" || errno[3].Index() != 3 || !errno[3].HasPayload() || errno[0].HasPayload() {
		t.Errorf("DeviceState variants: %v", errno)
	}
	pte := pkg.Scope().Lookup("PTE").Type().Underlying().(*types2.Packed)
	if !pte.IsUnion() || len(pte.Variants()) != 2 || pte.Variants()[1].TagBits()[0] != 1 {
		t.Errorf("PTE variants: %v", pte.Variants())
	}
}

func TestUnionErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"anonymous", "var x union { A int }", "expected ';'"},
		{"empty", "type U union {}", "union U has no variants"},
		{"blank", "type U union { _ int; A }", "union variant cannot be _"},
		{"duplicate", "type U union { A; A int }", "A redeclared"},
		{"tag", "type U union { A int `json:\"a\"` }", "union variant cannot have a tag"},
		{"embedded pointer", "type U union { *int }", "union variant *int needs a name"},
		{"recursive", "type U union { A; B U }", "invalid recursive type"},
		{"owned payload", "type U union { A; B owned[int] }", "union U cannot have variant B with payload owned[int] yet"},
		{"owned in struct", "type U union { A struct{ p owned[int] } }", "it holds owned[int]"},
		{"owned instance", "type R[T any] union { Ok T; No }\nvar r R[shared[int]]", "union R[shared[int]] cannot have variant Ok"},
		{"variant as type", "var v DeviceState.Ready", "DeviceState.Ready is a variant of union DeviceState, not a type"},
		{"variant on value", "func f(st DeviceState) { _ = st.Ready }", "Ready is a variant of union DeviceState, not a field"},
		{"unknown variant", "var v = DeviceState.Gone", "DeviceState.Gone undefined"},
		{"union literal", "var v = DeviceState{}", "cannot write a composite literal of union type DeviceState"},
		{"bare literal", "var v = DeviceState.Off{}", "variant Off of union DeviceState has no payload: write DeviceState.Off, not DeviceState.Off{}"},
		{"literal unknown variant", "var v = DeviceState.Gone{}", "union DeviceState has no variant Gone (its variants are Off, Probing, Ready and Failed)"},
		{"literal not union", "type S struct{}\nvar v = S.X{}", "S.X is not a type (S is not a union, so it has no variants)"},
		{"wrong payload", "var v = DeviceState.Failed(\"x\")", "cannot use \"x\" (untyped string constant) as Errno value"},
		{"call bare variant", "var v = DeviceState.Off(1)", "cannot call DeviceState.Off (value of union type DeviceState): DeviceState is not a function"},
		{"bare outside switch", "var v DeviceState = Off", "undefined: Off"},
		{"assign payload", "var v DeviceState = Errno.EIO", "cannot use Errno.EIO (constant 5 of enum type Errno) as DeviceState value"},
		{"nil", "var v DeviceState = nil", "cannot use nil as DeviceState value"},
		{"missing case", "func f(st DeviceState) {\n\tswitch st.(type) {\n\tcase Off, Ready:\n\t}\n}", "missing cases in type switch on st (DeviceState): Probing and Failed"},
		{"missing one", "func f(st DeviceState) {\n\tswitch st.(type) {\n\tcase Off, Ready, Failed:\n\t}\n}", "missing case in type switch on st (DeviceState): Probing\n"},
		{"duplicate case", "func f(st DeviceState) {\n\tswitch st.(type) {\n\tcase Off, DeviceState.Off:\n\tdefault:\n\t}\n}", "duplicate case DeviceState.Off in type switch"},
		{"nil case", "func f(st DeviceState) {\n\tswitch st.(type) {\n\tcase nil:\n\tdefault:\n\t}\n}", "a union value is never nil: the zero value of DeviceState is its first variant, Off"},
		{"type case", "func f(st DeviceState) {\n\tswitch st.(type) {\n\tcase int:\n\tdefault:\n\t}\n}", "int is not a variant of union DeviceState"},
		{"other union case", "func f(st DeviceState) {\n\tswitch st.(type) {\n\tcase Result[int].Ok:\n\tdefault:\n\t}\n}", "Result[int].Ok is a variant of Result[int], not of DeviceState"},
		{"other instance case", "func f(r Result[int]) {\n\tswitch r.(type) {\n\tcase Result[string].Ok:\n\tdefault:\n\t}\n}", "Result[string].Ok is a variant of Result[string], not of Result[int]"},
		{"case not union", "func f(st DeviceState) {\n\tswitch st.(type) {\n\tcase Errno.X:\n\tdefault:\n\t}\n}", "Errno is not a union, so Errno.X is not a variant of DeviceState"},
		{"case unknown variant", "func f(st DeviceState) {\n\tswitch st.(type) {\n\tcase DeviceState.X:\n\tdefault:\n\t}\n}", "union DeviceState has no variant X"},
		{"unused binding", "func f(st DeviceState) {\n\tswitch s := st.(type) {\n\tcase Off:\n\tdefault:\n\t}\n}", "s declared and not used"},
		{"bare assertion", "func f(st DeviceState) { _ = st.(Off) }", "write DeviceState.Off: a variant's bare name is accepted only in the cases of a type switch"},
		{"assertion non-variant", "func f(st DeviceState) { _ = st.(int) }", "int is not a variant of union DeviceState"},
		{"switch on struct", "type S struct{}\nfunc f(s S) {\n\tswitch s.(type) {\n\t}\n}", "s (variable of struct type S) is not an interface or a union"},
		{"not exhaustive return", "func f(st DeviceState) int {\n\tswitch st.(type) {\n\tcase Off:\n\t\treturn 1\n\tdefault:\n\t\tbreak\n\t}\n}", "missing return"},
		{"compare incomparable", "type U union { A []int; B }\nvar x, y U\nvar b = x == y", "union with variant A of type []int cannot be compared"},
		{"multi-name packed", "type P packed union uint8 { t bits[uint8, 1]; A, B case 0 {} }", "each variant of packed union P needs its own case clause"},
		{"packed no backing", "type P packed union { t bits[uint8, 1]; A case 0 {} }", "expected"},
		{"packed two tags", "type P packed union uint8 { t, u bits[uint8, 1]; A case 0 {} }", "packed union P needs exactly one named tag field before its variants (it has 2)"},
		{"packed tag after", "type P packed union uint8 { t bits[uint8, 1]; A case 0 {}; u bits[uint8, 1] }", "tag field u of packed union P must come before the first variant"},
		{"packed float tag", "type P packed union uint64 { t float32; A case 0 {} }", "the tag t of packed union P must be a bool, integer or enum field, not float32"},
		{"packed no variants", "type P packed union uint8 { t bits[uint8, 1] }", "packed union P has no variants"},
		{"packed duplicate value", "type P packed union uint8 { t bits[uint8, 2]; A case 1 {}; B case 2, 1 {} }", "variants A and B of packed union P both have tag value 1"},
		{"packed value too big", "type P packed union uint8 { t bits[uint8, 2]; A case 4 {} }", "case value 4 of variant A does not fit the 2-bit tag t"},
		{"packed signed too small", "type P packed union uint8 { t bits[int8, 2]; A case -3 {} }", "case value -3 of variant A does not fit the 2-bit tag t"},
		{"packed value not constant", "var c uint8\ntype P packed union uint8 { t bits[uint8, 2]; A case c {} }", "case value c (variable of type uint8) of variant A is not constant"},
		{"packed overflow", "type P packed union uint8 { t bits[uint8, 2]; A case 0 { x uint8 } }", "packed fields need 10 bits but the backing type uint8 holds 8"},
		{"packed generic", "type P[T any] packed union uint8 { t bits[uint8, 2]; A case 0 {} }", "packed union P cannot have type parameters"},
		{"packed incomplete switch", "type P packed union uint8 { t bits[uint8, 2]; A case 0 {}; B case 1, 2 {} }\nfunc f(p P) {\n\tswitch p.(type) {\n\tcase A, B:\n\t}\n}", "type switch on packed union p (P) needs a default case"},
		{"packed literal", "var p = PTE{present: true}", "cannot write a composite literal of union type PTE"},
		{"packed tag key", "var p = PTE.Mapped{present: false}", "cannot set the tag present in a literal of variant Mapped: the variant sets it to true"},
		{"packed field of union", "func f(p PTE) uint64 { return uint64(p.frame) }", "p.frame undefined"},
		{"alias", "type A = union { X int }", "union type must be the type of a type definition"},
		{"packed alias", "type A = packed union uint8 { t bits[uint8, 1]; X case 0 {} }", "packed union type must be the type of a type definition"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := unionDecls + "\n" + c.src
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.p", src, 0)
			if err != nil {
				if !strings.Contains(err.Error(), c.want) {
					t.Fatalf("parse error %v, want %q", err, c.want)
				}
				return
			}
			conf := types2.Config{Importer: unsafeOnly{}}
			_, err = conf.Check("p", fset, []*ast.File{f}, nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}
}
