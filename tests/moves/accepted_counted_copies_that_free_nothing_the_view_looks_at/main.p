package main

import (
	"pluto/alloc"
	"pluto/vec"
)

// Calls given a copy of a counted reference, or that use package
// variables, that free nothing the view looks at: the view stays valid
// (language spec section 18.3).

type L owner struct{ x int }
type N owner struct {
	c owned[L]
	y int
}

var cache shared[N]

func size(b shared[vec.Vec[int]]) int { return b.Len() }

func reset() { cache.c = new(L) }

func main() {
	cache = alloc.Shared[N]()
	a := alloc.Shared[vec.Vec[int]]()
	a.Push(7)
	s := a.Slice()
	println(size(a))
	reset()
	println(s[0])

	a.Push(8)
	s = a.Slice()
	println(s[1])

	n := alloc.Shared[N]()
	n.c = new(L)
	n.y = 3
	m := n
	p := &n.y
	m.c = new(L)
	n.c = new(L)
	println(*p)
	q := &n.c.x
	*q = 4
	println(n.c.x)
}
