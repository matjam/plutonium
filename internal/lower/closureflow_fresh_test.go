package lower_test

import (
	"strings"
	"testing"
)

// TestClosureStoredInFreshAllocation checks that a closure stored in memory
// the function has just allocated (new(T), &T{}, alloc.New) is not taken for
// one stored through any pointer: the pool of values that every read through a
// pointer may see. The translated type checker builds a pointer with
// pu.New(v) (p := alloc.New[T](); *p = v; return p), which made every value
// passed to it, a method value among them, part of what a function returning
// an interface holds. Storing through a pointer the function was given is
// still seen.
func TestClosureStoredInFreshAllocation(t *testing.T) {
	const prelude = `package main

import "pluto/alloc"

type Node interface{ Pos() int }
type Ident struct{ n int }

func (i *Ident) Pos() int { return i.n }

// Fn holds a func value, so a Node may refer to one.
type Fn struct{ f func() int }

func (f *Fn) Pos() int { return f.f() }

var one = &Ident{1}

type W struct{ f func() int }

type P struct{ n int }

func (p *P) get() int { return p.n }

func newW[T any](v T) *T {
	p := alloc.New[T]()
	*p = v
	return p
}

func mkNew(f func() int) *W {
	w := alloc.New[W]()
	w.f = f
	return w
}

func mkLit(f func() int) *W {
	w := &W{}
	w.f = f
	return w
}

func pick(p *P, n Node) Node {
	_ = newW(W{p.get})
	_ = mkNew(p.get)
	_ = mkLit(p.get)
	var r Node = n
	return r
}

func pickStore(w *W, p *P, n Node) Node {
	w.f = p.get
	var r Node = n
	return r
}
`
	const user = `
func main() {
	var p P
	var keep Node
	for i := 0; i < 3; i++ {
		keep = USE
	}
	println(keep.Pos())
}
`
	if got := lowerModeError(t, prelude+strings.Replace(user, "USE", "pick(&p, one)", 1), hosted); got != "" {
		t.Errorf("a closure stored in a new allocation was taken for one stored through any pointer:\n%s", got)
	}
	got := lowerModeError(t, prelude+strings.Replace(user, "USE", "pickStore(alloc.New[W](), &p, one)", 1), hosted)
	if !strings.Contains(got, "E3101") {
		t.Errorf("a closure stored through a pointer parameter is not reported:\n%s", got)
	}
}
