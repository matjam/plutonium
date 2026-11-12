package main

import "pluto/vec"

func main() {
	var v vec.Vec[int]
	v.Push(7)
	v.Push(8)
	for _, x := range v.Slice() {
		v.Free()
		println(x)
	}
}
