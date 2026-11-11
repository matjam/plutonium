package main

import (
	"pluto/alloc"
	"pluto/vec"
)

// A call through a copy of a counted reference may free through the object
// a local reference of the same type points to, so it ends the views
// obtained through the local one, wherever the copy is held: a field of a
// local, a field behind a parameter, an argument of a helper (language spec
// section 18.3, 7.19).

type Holder struct{ r shared[vec.Vec[int]] }

func grow(h *Holder) { h.r.Push(1) }

func viaParam(h *Holder) {
	a := h.r
	sa := a.Slice()
	h.r.Push(2)
	println(sa[0])
}

func main() {
	a := alloc.Shared[vec.Vec[int]]()
	a.Push(0)
	h := Holder{r: a}
	sa := a.Slice()
	h.r.Push(1)
	println(sa[0])

	sb := a.Slice()
	grow(&h)
	println(sb[0])

	viaParam(&h)
}
