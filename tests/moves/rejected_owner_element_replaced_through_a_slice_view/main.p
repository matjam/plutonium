package main

import "pluto/vec"

// An element of a slice of boxes given a new value drops the old box:
// a view into its object is stale (language spec sections 18.3, 18.6).

type L owner struct{ x int }

func main() {
	var v vec.Vec[owned[L]]
	v.Push(new(L))
	s := v.Slice()
	s[0].x = 7
	q := &s[0].x
	s[0] = new(L)
	println(*q)
}
