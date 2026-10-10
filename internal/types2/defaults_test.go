package types2_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/types2"
)

const defaultDecls = `package p

type Allocator interface{ Alloc(n int) int }

type heap struct{}

func (heap) Alloc(n int) int { return n }

func Default() Allocator { return heap{} }

var calls int

func New[T any](a Allocator = Default()) *T { return nil }

func Grow(n int, factor int = 2, limit int = 1 << 20) int { return n * factor }

func Sized[T any](n int = sizeOf[T]()) int { return n }

func sizeOf[T any]() int { return 0 }

func Pick[T any](x T, y T = zero[T]()) T { return y }

func zero[T any]() T {
	var z T
	return z
}

func Pair() (int, int) { return 1, 2 }

type Counter struct{ n int }

func (c *Counter) Add(d int = 1) int { return c.n + d }

type Box[T any] struct{ v T }

func (b *Box[T]) Size(n int = sizeOf[T]()) int { return n }

type Adder interface{ Add(d int) int }

// Parameters shadow package-level names in the body, but a default never
// sees them; the default here names the package-level n.
var n = 5

func Shadow(x int, y int = n) int { return y }

// A function literal in a default declares its own parameters.
func Apply(x int, f func(int) int = func(x int) int { return x + 1 }) int { return f(x) }
`

func TestDefaults(t *testing.T) {
	pkg, _ := mustCheck(t, defaultDecls+`
func use() {
	_ = New[int]()
	_ = New[int](heap{})
	_ = Grow(1)
	_ = Grow(1, 3)
	_ = Grow(1, 3, 4)
	_ = Grow(Pair())
	_ = Sized[int]()
	_ = Pick(1)
	_ = Pick("a", "b")
	var c Counter
	_ = c.Add()
	_ = c.Add(2)
	var b Box[string]
	_ = b.Size()
	var a Adder = &c // defaults do not change the method's type
	_ = a.Add(3)
	var f func(int) int = c.Add
	_ = f(4)
	var g func(int, int, int) int = Grow
	_ = g(1, 2, 3)
	defer Grow(1)
	_ = Shadow(1)
	_ = Apply(1)
}
`)
	grow := pkg.Scope().Lookup("Grow").(*types2.Func)
	if got := len(grow.Defaults()); got != 2 {
		t.Errorf("Grow has %d defaults, want 2", got)
	}
	if got := grow.Type().(*types2.Signature).Params().Len(); got != 3 {
		t.Errorf("Grow's type has %d parameters, want 3", got)
	}
	if d := pkg.Scope().Lookup("Pair").(*types2.Func).Defaults(); d != nil {
		t.Errorf("Pair has defaults %v", d)
	}
	// Identity ignores defaults.
	g2 := types2.NewSignatureType(nil, nil, nil, grow.Type().(*types2.Signature).Params(), grow.Type().(*types2.Signature).Results(), false)
	if !types2.Identical(grow.Type(), g2) {
		t.Errorf("Grow's type is not identical to the same signature without defaults")
	}
}

func TestDefaultErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"missing default after one", "func f(a int = 1, b int) {}",
			"parameter b has no default value but follows parameter a, which has one"},
		{"unnamed missing default", "func f(int = 1, string) {}",
			"parameter #2 has no default value but follows parameter #1, which has one"},
		{"variadic", "func f(a int = 1, b ...int) {}",
			"a variadic function cannot have default parameter values"},
		{"result", "func f() (r int = 1) { return }", "result parameters cannot have default values"},
		{"receiver", "type T struct{}\nfunc (t T = T{}) M() {}", "a receiver cannot have a default value"},
		{"function literal", "var f = func(a int = 1) {}",
			"default parameter values are allowed only in function and method declarations"},
		{"function type", "type F func(a int = 1)",
			"default parameter values are allowed only in function and method declarations"},
		{"interface method", "type I interface{ M(a int = 1) }",
			"default parameter values are allowed only in function and method declarations"},
		{"not assignable", "func f(a int = \"x\") {}", "cannot use \"x\" (untyped string constant) as int value in default value of a"},
		{"refers to parameter", "func f(a int, b int = a) {}",
			"a default value cannot refer to parameter a (defaults are evaluated by the caller)"},
		{"refers to receiver", "type T struct{ n int }\nfunc (t T) M(b int = t.n) {}",
			"a default value cannot refer to parameter t"},
		{"refers to parameter in literal", "func f(a int, g func() int = func() int { return a }) {}",
			"a default value cannot refer to parameter a"},
		{"too few arguments", "func f(a int, b int = 1) {}\nfunc g() { f() }", "not enough arguments in call to f"},
		{"function value", "func f(a int, b int = 1) {}\nfunc g() { h := f; h(1) }", "not enough arguments in call to h"},
		{"method value", "type T struct{}\nfunc (T) M(a int = 1) {}\nfunc g() { m := T{}.M; m() }",
			"not enough arguments in call to m"},
		{"method expression", "type T struct{}\nfunc (T) M(a int = 1) {}\nfunc g() { T.M(T{}) }",
			"not enough arguments in call to T.M"},
		{"interface call", "type T struct{}\nfunc (T) M(a int = 1) {}\ntype I interface{ M(a int) }\nfunc g() { var i I = T{}; i.M() }",
			"not enough arguments in call to i.M"},
		{"initialization cycle", "var x = f()\nfunc f(a int = x) int { return a }", "initialization cycle"},
		{"cannot infer from omitted","func f[T any](a T = *new(T)) {}\nfunc g() { f() }", "cannot infer T"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { mustFail(t, "package p\n\n"+c.src+"\n", c.want) })
	}
}
