package main

import "pluto/vec"

// clear of a slice of boxes drops each element: a view into the object
// of one is stale (language spec sections 18.3, 18.6).

type L owner struct{ x int }

func main() {
	var v vec.Vec[owned[L]]
	v.Push(new(L))
	s := v.Slice()
	s[0].x = 7
	q := &s[0].x
	clear(s)
	println(*q)
}
