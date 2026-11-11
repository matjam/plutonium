package main

// A box of a generic type converted to an interface: the itab adapter
// calls the method of the box's object with its type arguments (item 208).

import (
	"pluto/alloc"
	"pluto/vec"
)

type S interface{ Slice() []int }

func viaIface(t S, z S) {
	s := t.Slice()
	println(len(s), s[0])
	t = z
	println(t == nil)
}

func viaParam[T S](t T) {
	s := t.Slice()
	println(len(s))
}

func main() {
	c := alloc.Shared[vec.Vec[int]]()
	c.Push(4)
	c.Push(5)
	viaIface(c, nil)
	viaParam(c)
	o := alloc.Owned[vec.Vec[int]]()
	o.Push(6)
	var i S = o
	println(len(i.Slice()))
}
