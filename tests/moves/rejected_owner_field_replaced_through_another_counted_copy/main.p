package main

import "pluto/alloc"

// An owner in the object given a new value through one copy of a counted
// reference drops the old value, which a view obtained through another
// copy looks at (language spec section 18.3).

type L owner struct{ x int }
type N owner struct{ c owned[L] }

func main() {
	s := alloc.Shared[N]()
	s.c = new(L)
	s.c.x = 7
	t := s
	p := &s.c.x
	t.c = new(L)
	println(*p)
}
