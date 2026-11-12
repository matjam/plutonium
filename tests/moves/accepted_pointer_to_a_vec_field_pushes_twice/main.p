package main

import "pluto/vec"

// A pointer to a field of a local owner points at the field's storage,
// which a call that frees through it leaves where it was, holding the
// current value (language spec section 18.3). A slice of what the field
// holds is a view of its heap memory, made again after the call.

type B owner struct {
	n int
	v vec.Vec[int]
}

type C owner struct{ b B }

func main() {
	var b B
	p := &b.v
	p.Push(1)
	p.Push(2)
	println(p.Len())

	var c C
	q := &c.b
	r := &c.b.v
	r.Push(1)
	q.v.Push(2)
	r.Push(3)
	s := r.Slice()
	println(s[2] + q.n)
}
