package main

import "pluto/vec"

func mk() vec.Vec[int] {
	var v vec.Vec[int]
	v.Push(7)
	return v
}

var keep *int

func main() {
	keep = mk().Ptr(0)
	println(*keep)
}
