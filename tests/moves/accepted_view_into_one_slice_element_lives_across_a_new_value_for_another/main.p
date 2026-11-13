package main

import "pluto/vec"

// A new value for element 1 of the slice drops the box element 1 held,
// not the box of element 0: the view into element 0's object lives on
// (language spec sections 18.3, 18.6).

type L owner struct{ x int }

type pair owner struct {
	a owned[L]
	n int
}

func main() {
	var v vec.Vec[owned[L]]
	v.Push(new(L))
	v.Push(new(L))
	s := v.Slice()
	q := &s[0].x
	s[1] = new(L)
	s[1].x = 3
	println(*q)

	var w vec.Vec[pair]
	w.Push(pair{a: new(L)})
	w.Push(pair{a: new(L)})
	t := w.Slice()
	r := &t[1].a.x
	t[0].a = new(L)
	t[0] = pair{a: new(L), n: 2}
	println(*r)
}
