package main

import "pluto/vec"

// A method value holds its receiver; calling it may free through the
// receiver as the method may (language spec section 18.3).

func main() {
	var v vec.Vec[int]
	v.Push(7)
	p := v.Ptr(0)
	g := v.Free
	g()
	println(*p)
}
