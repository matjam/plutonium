package lower_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestClosureSummaryRecursiveGroup checks that the closure analysis of a
// group of mutually recursive functions that pass func values around takes a
// time that grows with the size of the group, not exponentially: each
// function was once analysed again for every round of every function it
// calls, so 24 of them took longer than anyone waited (the parser of the
// self-hosted compiler has 59 on one cycle).
func TestClosureSummaryRecursiveGroup(t *testing.T) {
	const n = 24
	var b strings.Builder
	b.WriteString("package main\n\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "func f%d(g func() int, x int) func() int {\n\tif x == 0 {\n\t\treturn g\n\t}\n\treturn f%d(f%d(g, x-1), x-2)\n}\n\n",
			i, (i+1)%n, (i+2)%n)
	}
	b.WriteString("func main() {\n\tprintln(f0(func() int { return 1 }, 3)())\n}\n")

	done := make(chan string, 1)
	go func() { done <- lowerModeError(t, b.String(), hosted) }()
	select {
	case got := <-done:
		if got != "" {
			t.Errorf("reported:\n%s", got)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("lowering a group of mutually recursive functions that pass func values did not finish in 30 seconds")
	}
}

// TestClosureResultOfInterfaceWithMethods checks that a call of a func value
// whose result is an interface with methods returns no closure, when no
// named func type implements the interface (the parser of the self-hosted
// compiler passes p.parseType to a helper that returns the type's node, and
// returns a pointer to a list of them), and that an interface that may hold a
// func value still does.
func TestClosureResultOfInterfaceWithMethods(t *testing.T) {
	const prelude = `package main

type Node interface{ Pos() int }
type Ident struct{ n int }

func (i *Ident) Pos() int { return i.n }

var one = &Ident{1}

type P struct{}

func (p *P) ident() Node { return one }
func (p *P) any() any    { return one }

type Holder struct{ n Node }
type AnyHolder struct{ n any }

func call(f func() Node) Node  { return f() }
func callAny(f func() any) any { return f() }
`
	const user = `
func build(p *P, h *Holder) *Holder {
	var local Holder
	local.n = call(p.ident)
	*h = local
	return h
}

func main() {
	var h Holder
	println(build(&P{}, &h).n.Pos())
}
`
	if got := lowerModeError(t, prelude+user, hosted); got != "" {
		t.Errorf("an interface with methods that no func type implements held a closure:\n%s", got)
	}
	// With a named func type that implements Node, the closure may be in it.
	named := prelude + `
type Fn func() Node

func (f Fn) Pos() int { return f().Pos() }
` + user
	if got := lowerModeError(t, named, hosted); !strings.Contains(got, "E3100") {
		t.Errorf("an interface that a func type implements is not reported:\n%s", got)
	}
	anyUser := `
func build(p *P, h *AnyHolder) *AnyHolder {
	var local AnyHolder
	local.n = callAny(p.any)
	*h = local
	return h
}

func main() {
	var h AnyHolder
	println(build(&P{}, &h).n != nil)
}
`
	if got := lowerModeError(t, prelude+anyUser, hosted); !strings.Contains(got, "E3100") {
		t.Errorf("an empty interface is not reported:\n%s", got)
	}
}

// TestClosureResultOfInterfaceHoldingFuncValue checks that an interface with
// methods can hold a closure when a struct or array with a func in it
// implements it, as well as when a named func type does (the adversarial
// review's c08 and c09), and that a type that holds a func only through a
// pointer does not make it so (the translated type checker's types2.Type).
func TestClosureResultOfInterfaceHoldingFuncValue(t *testing.T) {
	const prelude = `package main

type Node interface{ Pos() int }
type Ident struct{ n int }

func (i *Ident) Pos() int { return i.n }

var one = &Ident{1}

type P struct{}

func (p *P) ident() Node { return one }

type Holder struct{ n Node }

func call(f func() Node) Node { return f() }

func build(p *P, h *Holder) *Holder {
	var local Holder
	local.n = call(p.ident)
	*h = local
	return h
}

func main() {
	var h Holder
	println(build(&P{}, &h).n.Pos())
}
`
	for name, decl := range map[string]string{
		"struct":   "type V struct{ f func() int }\nfunc (v V) Pos() int { return v.f() }\n",
		"array":    "type V [1]func() int\nfunc (v V) Pos() int { return v[0]() }\n",
		"nested":   "type W struct{ f func() int }\ntype V struct{ w W }\nfunc (v V) Pos() int { return v.w.f() }\n",
		"embedded": "type W struct{ f func() int }\nfunc (w W) Pos() int { return w.f() }\ntype V struct{ W }\n",
		"generic":  "type V[T any] struct{ f func() T }\nfunc (v V[T]) Pos() int { return 1 }\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := lowerModeError(t, prelude+decl, hosted); !strings.Contains(got, "E3100") {
				t.Errorf("an interface that a type holding a func implements is not reported:\n%s", got)
			}
		})
	}
	for name, decl := range map[string]string{
		"pointer":  "type Cfg struct{ f func() int }\ntype V struct{ c *Cfg }\nfunc (v V) Pos() int { return v.c.f() }\n",
		"ptrrecv":  "type V struct{ f func() int }\nfunc (v *V) Pos() int { return v.f() }\n",
		"slice":    "type V struct{ fs []func() int }\nfunc (v V) Pos() int { return len(v.fs) }\n",
		"noMethod": "type V struct{ f func() int }\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := lowerModeError(t, prelude+decl, hosted); got != "" {
				t.Errorf("an interface that no type holding a func in itself implements held a closure:\n%s", got)
			}
		})
	}
}
