package main

import "pluto/vec"

func mk() vec.Vec[int] {
	var v vec.Vec[int]
	v.Push(7)
	return v
}

// A temporary in the header of an if statement lives until the if
// statement ends, and no longer (spec section 18.6).
func main() {
	var p *int
	if p = mk().Ptr(0); p != nil {
		println(*p)
	}
	println(*p)
}
