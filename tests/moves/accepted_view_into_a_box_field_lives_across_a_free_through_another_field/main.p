package main

// t.b.v.Push frees and moves what the vector field v of the box's object
// holds, not the object: &t.b.x, the address of another field of that
// object, still looks at live memory (language spec section 18.3).

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
	q := &t.b.x
	t.b.v.Push(1)
	println(*q)
	*q = 2
	println(t.b.x, t.b.v.Len())
}
