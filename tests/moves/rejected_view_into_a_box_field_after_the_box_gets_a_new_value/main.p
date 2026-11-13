package main

// A new value for the box t.b drops its old object, so &t.b.x looks at
// freed memory; and Push may move the elements a slice of t.b.v looks at
// (language spec section 18.3).

import "pluto/vec"

type L owner struct {
	x int
	v vec.Vec[int]
}

type T owner struct {
	b owned[L]
}

func main() {
	t := T{b: new(L)}
	t.b.v.Push(1)
	q := &t.b.x
	s := t.b.v.Slice()
	t.b.v.Push(2)
	println(s[0])
	t.b = new(L)
	println(*q)
}
