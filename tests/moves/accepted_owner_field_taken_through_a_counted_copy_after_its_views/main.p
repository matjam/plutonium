package main

import "pluto/alloc"

// A view into an owner field of a counted object used before take empties
// that field through another copy, and one made again after it.

type L owner struct{ x int }
type N owner struct{ c owned[L] }

func main() {
	s := alloc.Shared[N]()
	s.c = new(L)
	s.c.x = 7
	p := &s.c.x
	println(*p)
	t := s
	l := take(&t.c)
	println(l.x)
	s.c = new(L)
	q := &s.c.x
	println(*q)
}
