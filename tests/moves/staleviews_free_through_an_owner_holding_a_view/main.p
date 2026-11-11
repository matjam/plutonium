package main

import "pluto/vec"

// An owner may hold a view of another local in a field (section 18.13).
// A call given the owner's address may free through that field, which
// ends the views of the other local (section 18.3, A local owner's
// memory).

type W owner struct{ p *vec.Vec[int] }

func grow(w *W) { w.p.Push(1) }

func main() {
	y := vec.New[int]()
	y.Push(0)
	s := y.Slice()
	w := W{p: &y}
	grow(&w)
	println(s[0])
}
