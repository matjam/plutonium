package main

import "pluto/alloc"

// A view obtained through a counted reference held in a local variable is
// bounded by that reference as a view of an owner is (spec 18.3): it may
// not be used after the variable is given a new reference.

type T struct {
	n    int
	tail []int
}

func main() {
	s := alloc.Shared[T]()
	p := &s.n
	s = alloc.Shared[T]()
	*p = 1

	r := alloc.Shared[T]()
	q := &r.n
	*q = 2 // fine: r still holds the same reference
	println(r.n, s.n)
}
