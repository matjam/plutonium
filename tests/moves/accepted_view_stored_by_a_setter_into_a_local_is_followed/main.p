package main

// set stores the literal into the memory its receiver points to. The
// receiver is a local the compiler follows, so the view of v the literal
// holds goes into g, and only uses of g after v.Push(3) are errors, not
// the call to set (language spec section 18.3).

import "pluto/vec"

type H struct{ f func() }

func (h *H) set(f func()) { h.f = f }

func main() {
	v := vec.New[int]()
	v.Push(1)
	var h H
	h.set(func() { println(v.Len()) })
	v.Push(2)
	s := v.Slice()
	h.f()
	println(s[0])
	var g H
	g.set(func() { println(s[1]) })
	g.f()
	v.Push(3)
	println(v.Len())
}
