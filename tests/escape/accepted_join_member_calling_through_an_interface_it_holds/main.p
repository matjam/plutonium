package main

import "pluto/alloc"

// A call through an interface joins the methods it may call. Outer's M
// keeps what K's M keeps, but only when its receiver is an *Outer and the
// interface it holds is a *K: the join's entry for it keeps both
// conditions (spec section 18.3). use(&o) passes an *Other, whose M keeps
// nothing, so &x is not kept. With the receiver's condition lost, the
// entry stood for every receiver of Outer's layout, and &o was taken to
// be kept (types2's conversions.p and pluto/testing's sprintln).

type I interface{ M() }

var keep *int

// K's M keeps the pointer its receiver holds.
type K struct{ p *int }

func (k *K) M() { keep = k.p }

// Outer's M calls M through the interface it holds: it keeps what that
// interface's value holds when the value is a *K.
type Outer struct{ in I }

func (o *Outer) M() { o.in.M() }

// Other has Outer's layout, and its M keeps nothing.
type Other struct{ in I }

func (o *Other) M() {}

func use(i I) { i.M() }

func main() {
	var x int
	k := K{p: &x}
	o := Other{in: &k}
	use(&o)
	use(alloc.New[K]())
	use(&Outer{in: alloc.New[K]()})
	println(x)
}
