package main

import "pluto/vec"

func show(p *int) { println(*p) }

func main() {
	var v vec.Vec[int]
	v.Push(7)
	p := v.Ptr(0)
	defer show(p)
	defer v.Drop()
}
