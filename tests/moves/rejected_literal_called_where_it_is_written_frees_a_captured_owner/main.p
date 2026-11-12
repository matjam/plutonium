package main

import "pluto/vec"

func main() {
	var v vec.Vec[int]
	v.Push(7)
	p := v.Ptr(0)
	func() { v.Push(8) }()
	println(*p)
}
