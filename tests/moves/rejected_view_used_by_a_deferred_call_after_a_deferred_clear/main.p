package main

// Deferred calls run last in, first out: the deferred clear(s) runs
// before the deferred call registered ahead of it, which uses a view into
// an element of s that the clear has dropped (language spec section 18.3
// and section 9.10).

import "pluto/vec"

type L owner struct{ x int }

func show(p *int) { println(*p) }

func use() {
	var v vec.Vec[owned[L]]
	v.Push(new(L))
	s := v.Slice()
	q := &s[0].x
	defer show(q)
	defer clear(s)
	println(*q)
}

func main() { use() }
