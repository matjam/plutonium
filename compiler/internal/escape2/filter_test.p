// Ported from internal/escape2/filter_test.go at the go-cutover tag.

package escape2

import (
	"pluto/strings"
	"pluto/testing"
)

// The programs of the type filters' tests (D51, solve/filter.go): a callee
// that keeps only what passes an assertion, or a method reached through an
// interface, keeps nothing of a caller's value of another dynamic type.
// Each program's main calls f, whose frame objects are the ones judged.
const filterDecls = `package main

type Namer interface{ Name() string }

type N struct{ s string }

func (n *N) Name() string { return n.s }

// V is a value type that is not direct and implements Namer.
type V struct{ a, b int }

func (v V) Name() string { return "v" }

var kept Namer

// keepNamers keeps the operands that are Namers, and nothing else.
func keepNamers(args ...any) {
	for _, a := range args {
		switch a := a.(type) {
		case Namer:
			kept = a
		case int:
			_ = a
		}
	}
}

// middle passes its operands on: the filter is carried through its summary.
func middle(args ...any) { keepNamers(args...) }

type holder struct{ v any }

// keepFrom keeps a Namer held in a struct its caller points to.
func keepFrom(h *holder) {
	if n, ok := h.v.(Namer); ok {
		kept = n
	}
}

type Stringer interface{ String() string }

// A's String keeps its receiver; B's and C's keep nothing of theirs but
// C's keeps what its receiver's field points to.
type A struct{ p *int }

var keepA *A

func (a *A) String() string {
	keepA = a
	return "a"
}

type B struct{ n, m int }

func (b B) String() string { return "b" }

type C struct {
	p *int
	q int
}

var keepP *int

func (c C) String() string {
	keepP = c.p
	return "c"
}

func show(s Stringer) string { return s.String() }

// ga and gc make A's and C's String candidates of show.
var ga A
var gc C

func main() {
	f()
	_ = show(&ga)
	_ = show(gc)
	_ = show(B{})
}
`

func TestTypeFilters(t *testing.T) {
	for _, c := range []struct {
		name, f  string
		rejected bool
	}{
		// An assertion to an interface: only boxes of a type that
		// implements it pass.
		{"box of a string to a Namer case", `s := "x"
	keepNamers(s, 1)`, false},
		{"box of a frame value that is a Namer", `var v V
	keepNamers(v)`, true},
		{"pointer to a frame Namer", `var n N
	keepNamers(&n)`, true},
		{"through a middle function, a string", `s := "x"
	middle(s)`, false},
		{"through a middle function, a Namer", `var v V
	middle(v)`, true},
		{"held in a struct, a string", `h := holder{v: "x"}
	keepFrom(&h)`, false},
		{"held in a struct, a Namer", `var v V
	h := holder{v: v}
	keepFrom(&h)`, true},
		// A call through an interface: a candidate's effects on its
		// receiver apply only to a receiver of its type.
		{"a value whose String keeps nothing", `var b B
	_ = show(b)`, false},
		{"a pointer whose String keeps it", `var a A
	_ = show(&a)`, true},
		{"a value whose String keeps what it points to", `x := 1
	_ = show(C{p: &x})`, true},
		{"a value of another type pointing to the frame", `x := 1
	var b B
	b.n = x
	_ = show(b)`, false},
	} {
		src := strings.Replace(filterDecls, "func main() {", "func f() {\n\t"+c.f+"\n}\n\nfunc main() {", 1)
		err := Check(loadSrc(t, src))
		if isInternal(err) && err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := len(sites(err)) > 0; got != c.rejected {
			t.Errorf("%s: rejected %v, want %v (%v)", c.name, got, c.rejected, err)
		}
	}
}
