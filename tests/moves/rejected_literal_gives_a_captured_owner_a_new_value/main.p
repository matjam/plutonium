package main

import "pluto/vec"

// The literal only gives v a new value, which drops the old one and ends
// the views of its memory (language spec section 18.3).

func main() {
	var v vec.Vec[int]
	v.Push(7)
	p := v.Ptr(0)
	set := func() { v = vec.New[int]() }
	set()
	println(*p)
}
