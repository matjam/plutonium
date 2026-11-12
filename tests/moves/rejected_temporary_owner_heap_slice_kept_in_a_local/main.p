package main

import "pluto/vec"

func mk() vec.Vec[int] {
	var v vec.Vec[int]
	v.Push(7)
	return v
}

// The temporary Vec is dropped at the end of the statement, and its
// buffer with it (E3009, spec section 18.3).
func main() {
	s := mk().Slice()
	println(s[0])
}
