package main

import "pluto/vec"

func mk() vec.Vec[int] {
	var v vec.Vec[int]
	v.Push(7)
	return v
}

func id[T any](x T) T { return x }

func main() {
	p := id(mk().Ptr(0))
	println(*p)
}
