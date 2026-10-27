package main

import "pluto/vec"

// A vector of vectors: the explicit type argument vec.Vec[int] is a type,
// not a read of an element of a vec.Vec.
func main() {
	v := vec.New[vec.Vec[int]]()
	defer v.Free()
	println(v.Len())
}
