package main

import "pluto/vec"

func mk() vec.Vec[int] {
	var v vec.Vec[int]
	v.Push(7)
	return v
}

func main() {
	p := mk().Ptr(0)
	println(*p)
}
