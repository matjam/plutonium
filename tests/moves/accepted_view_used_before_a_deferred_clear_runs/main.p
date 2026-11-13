package main

// A deferred clear(s) runs when the function returns, so a view into an
// element of s stays usable for the rest of the body (language spec
// section 18.3 and section 9.10).

import "pluto/vec"

type L owner struct{ x int }

func use() {
	var v vec.Vec[owned[L]]
	v.Push(new(L))
	s := v.Slice()
	q := &s[0].x
	defer clear(s)
	println(*q)
	*q = 2
	println(*q)
}

func main() { use() }
