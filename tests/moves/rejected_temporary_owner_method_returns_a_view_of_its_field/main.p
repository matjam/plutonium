package main

import "pluto/vec"

type B owner struct{ v vec.Vec[int] }

func (b *B) First() *int { return b.v.Ptr(0) }

func mk() B {
	var b B
	b.v.Push(7)
	return b
}

func main() {
	p := mk().First()
	println(*p)
}
