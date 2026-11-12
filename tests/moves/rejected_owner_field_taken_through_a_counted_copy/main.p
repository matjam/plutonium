package main

import "pluto/alloc"

// take through another copy of a counted reference empties the field in
// the same object, and dropping what it took frees the object a view
// points into (language spec section 18.3, 18.5).

type L owner struct{ x int }
type N owner struct{ c owned[L] }

func main() {
	s := alloc.Shared[N]()
	s.c = new(L)
	s.c.x = 7
	p := &s.c.x
	t := s
	_ = take(&t.c)
	println(*p)
}
