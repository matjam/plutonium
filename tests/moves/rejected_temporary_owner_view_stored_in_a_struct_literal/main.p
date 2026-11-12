package main

import "pluto/vec"

func mk() vec.Vec[int] {
	var v vec.Vec[int]
	v.Push(7)
	return v
}

type H struct{ p *int }

func main() {
	h := H{p: mk().Ptr(0)}
	println(*h.p)
}
