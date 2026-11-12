package main

import "pluto/vec"

type H owner struct{ p *int }

func (h *H) Drop() {
	if h.p != nil {
		println(*h.p)
	}
}

// Drops run in reverse order of declaration: v is dropped first, then
// h's Drop reads *h.p (E3313 at the declaration of h, spec section 18.3).
func main() {
	var h H
	var v vec.Vec[int]
	v.Push(7)
	h.p = v.Ptr(0)
	println(v.Len())
}
