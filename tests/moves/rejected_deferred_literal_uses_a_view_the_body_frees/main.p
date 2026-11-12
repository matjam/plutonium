package main

import "pluto/vec"

func main() {
	var v vec.Vec[int]
	v.Push(7)
	p := v.Ptr(0)
	defer func() { println(*p) }()
	for i := 0; i < 100; i++ {
		v.Push(i)
	}
}
