package main

// set stores the literal, which holds a view of v, into g. v.Push(3) may
// move v's elements, so calling g.f after it uses a view that has gone
// (language spec section 18.3).

import "pluto/vec"

type H struct{ f func() }

func (h *H) set(f func()) { h.f = f }

func main() {
	v := vec.New[int]()
	v.Push(1)
	v.Push(2)
	s := v.Slice()
	var g H
	g.set(func() { println(s[1]) })
	v.Push(3)
	g.f()
}
