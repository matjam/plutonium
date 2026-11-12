package main

import "pluto/vec"

// A callee may call a func value it is given, so passing a literal counts
// as calling it (language spec section 18.3).

func apply(f func()) { f() }

func main() {
	var v vec.Vec[int]
	v.Push(7)
	p := v.Ptr(0)
	apply(func() { v.Free() })
	println(*p)
}
