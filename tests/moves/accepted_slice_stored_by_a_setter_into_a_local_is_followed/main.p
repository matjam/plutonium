package main

// set stores the slice into the local g it is called on, which the
// compiler follows: v.Push(3) ends the view g holds, and g is not used
// after it (language spec section 18.3).

import "pluto/vec"

type H struct{ p []int }

func (h *H) set(p []int) { h.p = p }

func main() {
	v := vec.New[int]()
	v.Push(1)
	v.Push(2)
	var g H
	g.set(v.Slice())
	println(g.p[1])
	v.Push(3)
	println(v.Len())
}
