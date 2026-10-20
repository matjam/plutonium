package types2_test

import (
	"fmt"
	"github.com/matjam/plutonium/internal/constant"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/parser"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

const enumDecls = `package p

type Color enum {
	Red
	Green
	Blue
}

type Errno enum uint16 {
	OK     = 0
	EPERM
	ENOENT
	EIO    = 5
}

type Order enum { Less = -1; Equal; Greater }

type Big enum { Zero; Huge = 1 << 40 }

type Stringer interface{ String() string }

type Named enum { A; B }

func (n Named) String() string { return "named" }

// Shade has a method with a member's name; Shade.Dark is the member.
type Shade enum { Light; Dark }

func (s Shade) Dark() bool { return s == Shade.Dark }

type Reg packed uint8 {
	color Color
	order Order
	wide  bits[Color, 4]
}

func Max[T interface{ ~uint8 }](a, b T) T {
	if a > b {
		return a
	}
	return b
}
`

func TestEnums(t *testing.T) {
	pkg, info := mustCheck(t, enumDecls+`
var names = [len(Color)]string{Color.Red: "red", Color.Green: "green", Color.Blue: "blue"}

const n = len(Errno)

func use(c Color, raw int, s Shade) (string, bool) {
	switch c {
	case Red:
		return names[c], true
	case Color.Green, Blue:
	}
	switch c {
	case Red:
	default:
	}
	x, ok := Color(raw)
	var y, ok2 = Errno(uint16(raw))
	_, _ = y, ok2
	ok = ok && x == Color.Blue && c < Color(2) && Max(c, x) != Color.Red
	var str Stringer = c
	_ = str.String() + Named.A.String() + Color.Red.String()
	for v := range Color {
		_ = v
	}
	var r Reg
	r.color = Color.Blue
	_ = 1 << c
	_ = uint8(c) + 1
	_ = s.Dark()
	return Shade.Dark.String(), ok
}

// An exhaustive switch with no default case is a terminating statement.
func word(c Color) string {
	switch c {
	case Red:
		return "red"
	case Green, Blue:
		panic("cool")
	}
}

func local() {
	type Dir enum { North; South }
	_ = Dir.South.String()
}
`)

	scope := pkg.Scope()
	enumOf := func(name string) *types2.Enum {
		t.Helper()
		en := types2.EnumOf(scope.Lookup(name).Type())
		if en == nil {
			t.Fatalf("%s is not an enum", name)
		}
		return en
	}
	members := func(en *types2.Enum) string {
		var s []string
		for i := 0; i < en.Len(); i++ {
			m := en.Member(i)
			s = append(s, fmt.Sprintf("%s=%s", m.Name(), m.Val()))
		}
		return strings.Join(s, " ")
	}

	cases := []struct {
		name, under, members string
	}{
		{"Color", "uint8", "Red=0 Green=1 Blue=2"},
		{"Errno", "uint16", "OK=0 EPERM=1 ENOENT=2 EIO=5"},
		{"Order", "int8", "Less=-1 Equal=0 Greater=1"},
		{"Big", "uint64", "Zero=0 Huge=1099511627776"},
	}
	for _, c := range cases {
		en := enumOf(c.name)
		if got := scope.Lookup(c.name).Type().Underlying().String(); got != c.under {
			t.Errorf("%s: underlying %s, want %s", c.name, got, c.under)
		}
		if got := members(en); got != c.members {
			t.Errorf("%s: members %s, want %s", c.name, got, c.members)
		}
		if en.GeneratedString() == nil {
			t.Errorf("%s: no generated String method", c.name)
		}
	}
	if enumOf("Named").GeneratedString() != nil {
		t.Errorf("Named declares String, but one was generated")
	}
	if v := scope.Lookup("n").(*types2.Const).Val(); constant.MakeInt64(4).String() != v.String() {
		t.Errorf("len(Errno) = %s, want 4", v)
	}

	// Bare case names resolve to the members.
	found := 0
	for id, obj := range info.Uses {
		if id.Name == "Red" || id.Name == "Blue" {
			if c, ok := obj.(*types2.Const); ok && c.Type() == scope.Lookup("Color").Type() {
				found++
			}
		}
	}
	if found < 4 {
		t.Errorf("found %d uses of Color members, want at least 4", found)
	}

	// Packed enum fields take the bits their values need.
	reg := scope.Lookup("Reg").Type().Underlying().(*types2.Packed)
	for i, want := range []struct{ off, width int64 }{{0, 2}, {2, 2}, {4, 4}} {
		f := reg.Field(i)
		if f.Offset() != want.off || f.Width() != want.width || !f.IsBits() {
			t.Errorf("Reg field %s: offset %d width %d bits %v, want %d %d true", f.Var().Name(), f.Offset(), f.Width(), f.IsBits(), want.off, want.width)
		}
	}
}

func TestEnumErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"duplicate value", "type E enum { A; B = 0 }", "enum member B has the same value 0 as A"},
		{"duplicate implicit value", "type E enum { A = 1; B = 0; C }", "enum member C has the same value 1 as A"},
		{"duplicate name", "type E enum { A; A }", "A redeclared"},
		{"blank member", "type E enum { _; A }", "enum member cannot be _"},
		{"no zero", "type E enum { A = 1; B }", "enum E has no member with value 0"},
		{"empty", "type E enum {}", "enum E has no members"},
		{"overflow explicit", "type E enum uint8 { A; B = 256 }", "enum member E.B value 256 overflows uint8"},
		{"overflow continue", "type E enum uint8 { A; B = 255; C }", "enum member E.C value 256 overflows uint8"},
		{"too big", "type E enum { A = -1; Z = 0; B = 1 << 63 }", "values do not fit in a 64-bit integer"},
		{"not constant", "var v = 1\ntype E enum { A = v }", "value v (variable of type int) is not constant"},
		{"float value", "type E enum { A = 1.5 }", "is not an integer"},
		{"string value", "type E enum { A = \"x\" }", "is not an integer"},
		{"base not integer", "type E enum string { A }", "underlying type string must be an integer type"},
		{"anonymous", "var x enum { A }", "enum type must be the type of a type definition"},
		{"alias", "type E = enum { A }", "enum type must be the type of a type definition"},
		{"generic", "type E[T any] enum { A }", "enum type E cannot have type parameters"},
		{"self reference", "type E enum { A; B = len(E) }", "invalid use of E in the declaration of enum E"},
		{"self member", "type E enum { A; B = E.A + 1 }", "invalid recursive type: E refers to itself"},
		{"non-exhaustive", "func f(c Color) {\n\tswitch c {\n\tcase Red:\n\t}\n}", "missing cases in switch on c (Color): Green, Blue (add them or a default case)"},
		{"exhaustive switch with a break", "func f(c Color) int {\n\tswitch c {\n\tcase Red:\n\t\treturn 1\n\tcase Green, Blue:\n\t\tbreak\n\t}\n}", "missing return"},
		{"exhaustive switch clause without return", "func f(c Color) int {\n\tswitch c {\n\tcase Red:\n\t\treturn 1\n\tcase Green, Blue:\n\t}\n}", "missing return"},
		{"non-exhaustive one", "func f(c Color) {\n\tswitch c {\n\tcase Red, Color.Green:\n\t}\n}", "missing case in switch on c (Color): Blue (add it or a default case)"},
		{"bare name outside switch", "func f() Color { return Red }", "undefined: Red (did you mean Color.Red?)"},
		{"bare name comparison", "func f(c Color) bool { return c == Blue }", "undefined: Blue (did you mean Color.Blue?)"},
		{"bare name nested in case", "func f(c Color) {\n\tswitch c {\n\tcase (Red):\n\tdefault:\n\t}\n}", "undefined: Red"},
		{"member on value", "func f(c Color) Color { return c.Red }", "c.Red undefined (Red is a member of enum Color: write Color.Red)"},
		{"unknown member", "var c = Color.Purple", "Color.Purple undefined"},
		{"member as type", "var c Color.Red", "Color.Red is not a type"},
		{"constant not a member", "var c = Color(3)", "cannot convert 3 (untyped int constant) to enum type Color: no member has that value"},
		{"non-constant single value", "func f(n int) Color { return Color(n) }", "cannot convert non-constant n (variable of type int) to enum type Color without a check"},
		{"non-constant nested", "func f(n int) (Color, bool) {\n\tc, ok := Color(int(Color(n)))\n\treturn c, ok\n}", "without a check"},
		{"float conversion", "func f(x float64) { c, ok := Color(x); _, _ = c, ok }", "only integers convert to an enum"},
		{"untyped assignment", "var c Color = 1", "cannot use 1 (untyped int constant) as Color value"},
		{"untyped argument", "func g(Color) {}\nfunc f() { g(2) }", "cannot use 2 (untyped int constant) as Color value"},
		{"untyped comparison", "func f(c Color) bool { return c == 1 }", "cannot use 1 (untyped int constant) as Color value: name a member (Color.Red) or convert explicitly"},
		{"untyped case", "func f(c Color) {\n\tswitch c {\n\tcase 1:\n\tdefault:\n\t}\n}", "cannot use 1 (untyped int constant) as Color value"},
		{"untyped shift", "func f(n uint) Color { return 1 << n }", "cannot use 1 << n (untyped int value) as Color value"},
		{"addition", "func f(c Color) Color { return c + c }", "operator + not defined on c (variable of enum type Color)"},
		{"increment", "func f(c Color) { c++ }", "operator + not defined on c"},
		{"negation", "func f(c Order) Order { return -c }", "operator - not defined on c"},
		{"bitwise", "func f(c Color) Color { return c | Color.Blue }", "operator | not defined on c"},
		{"shift operand", "func f(c Color) Color { return c << 1 }", "shifted operand c (variable of enum type Color) is an enum value"},
		{"range over value", "func f(c Color) {\n\tfor x := range c {\n\t\t_ = x\n\t}\n}", "range over the enum type instead"},
		{"range over non-enum type", "func f() {\n\tfor x := range int {\n\t\t_ = x\n\t}\n}", "cannot range over a type: int is not an enum type"},
		{"range two variables", "func f() {\n\tfor x, y := range Color {\n\t\t_, _ = x, y\n\t}\n}", "permits only one iteration variable"},
		{"len of non-enum type", "const n = len(int)", "invalid argument for built-in len: int is not an enum type"},
		{"packed too narrow", "type R packed uint8 { c bits[Color, 1] }", "bits[Color, 1]: enum Color needs 2 bits"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustFail(t, "package p\n\ntype Color enum { Red; Green; Blue }\n\ntype Order enum { Less = -1; Equal; Greater }\n\n"+c.src, c.want)
		})
	}
}

// TestEnumImported checks member selection, exhaustiveness and export rules
// across packages.
func TestEnumImported(t *testing.T) {
	fset := token.NewFileSet()
	parse := func(name, src string) *ast.File {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	conf := types2.Config{Importer: unsafeOnly{}}
	gfx, err := conf.Check("gfx", fset, []*ast.File{parse("gfx.p", "package gfx\n\ntype Color enum { Red; Green; Blue; hidden }\n")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	conf.Importer = pkgImporter{"gfx": gfx}

	check := func(src string) error {
		_, err := conf.Check("main", fset, []*ast.File{parse("main.p", "package main\n\nimport \"gfx\"\n\n"+src)}, nil)
		return err
	}
	if err := check("type C = gfx.Color\n\nfunc f(c gfx.Color) bool {\n\tswitch c {\n\tcase Red, gfx.Color.Green, C.Blue:\n\tdefault:\n\t}\n\treturn c == gfx.Color.Red\n}\n"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range []struct{ src, want string }{
		{"var c = gfx.Color.hidden", "cannot refer to unexported member hidden of enum Color"},
		{"func f(c gfx.Color) {\n\tswitch c {\n\tcase Red, Green, Blue:\n\t}\n}", "missing case in switch on c (gfx.Color): hidden"},
		{"func f(c gfx.Color) {\n\tswitch c {\n\tcase hidden:\n\tdefault:\n\t}\n}", "undefined: hidden"},
	} {
		if err := check(c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: got %v, want %q", c.src, err, c.want)
		}
	}
}

// pkgImporter imports packages checked earlier in a test, and unsafe.
type pkgImporter map[string]*types2.Package

func (m pkgImporter) Import(path string) (*types2.Package, error) {
	if p := m[path]; p != nil {
		return p, nil
	}
	return unsafeOnly{}.Import(path)
}
