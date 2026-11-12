package main

import "pluto/vec"

// The slice and a pointer to an element's slot look at the buffer, which
// a new element value does not free; a view into the new box made after
// the assignment is valid (language spec section 18.3).

type L owner struct{ x int }

func main() {
	var v vec.Vec[owned[L]]
	v.Push(new(L))
	s := v.Slice()
	r := &s[0]
	s[0] = new(L)
	s[0].x = 5
	println((*r).x)
	q := &s[0].x
	*q = 6
	println(s[0].x)
	clear(s)
	println(len(s))
	v.Set(0, new(L))
	println(v.Slice()[0].x)
}
