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
