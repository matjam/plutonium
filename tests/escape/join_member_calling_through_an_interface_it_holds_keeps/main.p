package main

import "pluto/alloc"

// The twin of accepted_join_member_calling_through_an_interface_it_holds:
// use(&o) passes an *Outer holding a *K, whose M keeps &x.

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
	o := Outer{in: &k}
	use(&o)
	use(alloc.New[K]())
	use(&Outer{in: alloc.New[K]()})
	println(x)
}
