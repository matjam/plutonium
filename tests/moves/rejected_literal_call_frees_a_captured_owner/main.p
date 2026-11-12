package main

import "pluto/vec"

// Calling a function literal does what its body does to the variables it
// uses, at the call: v.Free() in the literal ends the view p (language
// spec section 18.3).

func main() {
	var v vec.Vec[int]
	v.Push(7)
	p := v.Ptr(0)
	f := func() { v.Free() }
	f()
	println(*p)
}
