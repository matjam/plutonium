package main

import "pluto/vec"

type B owner struct{ v vec.Vec[int] }

func main() {
	var v vec.Vec[int]
	v.Push(7)
	v.Push(8)
	s := v.Slice()
	for _, x := range s {
		v.Push(x)
	}
	var b B
	b.v.Push(1)
	b.v.Push(2)
	for _, x := range b.v.Slice() {
		b.v.Push(x)
	}
}
