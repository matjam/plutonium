package main

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

func main() {
	var p P
	var keep Node
	for i := 0; i < 3; i++ {
		keep = pickStore(alloc.New[W](), &p, one)
	}
	println(keep.Pos())
}
