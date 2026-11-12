package main

import "pluto/alloc"

// A function gives the owner a new value through a copy of the reference
// in a package variable (language spec section 18.3).

type L owner struct{ x int }
type N owner struct{ c owned[L] }

var g shared[N]

func reset() { g.c = new(L) }

func main() {
	s := alloc.Shared[N]()
	s.c = new(L)
	s.c.x = 7
	g = s
	p := &s.c.x
	reset()
	println(*p)
}
