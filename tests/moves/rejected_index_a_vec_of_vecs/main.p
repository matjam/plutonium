package main

import "pluto/vec"

// vv[0] reads an element of a container of owners, which would be a copy
// (spec section 11.11). The error is at vv[0], not in the library.
func main() {
	vv := vec.New[vec.Vec[int]]()
	defer vv.Free()
	vv.Push(vec.New[int]())
	w := vv[0]
	println(w.Len())
}
