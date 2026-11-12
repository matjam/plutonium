package main

import "pluto/alloc"

// An owner in the object of a counted reference given a new value drops
// the old one: a view into it is stale (language spec section 18.3).

type L owner struct{ x int }
type N owner struct{ c owned[L] }

func main() {
	s := alloc.Shared[N]()
	s.c = new(L)
	s.c.x = 7
	p := &s.c.x
	s.c = new(L)
	println(*p)
}
