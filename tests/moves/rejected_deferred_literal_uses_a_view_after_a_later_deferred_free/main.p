package main

import "pluto/vec"

// Deferred calls run in reverse order where the function returns: v.Free()
// runs first, then the literal reads *p (language spec sections 12.19 and
// 18.3).

func main() {
	var v vec.Vec[int]
	v.Push(7)
	p := v.Ptr(0)
	defer func() { println(*p) }()
	defer v.Free()
}
