package main

import "pluto/vec"

// A pointer to an element, obtained through a pointer to a vec field,
// looks at the vec's heap memory, which a push may free (language spec
// section 18.3).

type B owner struct{ v vec.Vec[int] }

func main() {
	var b B
	p := &b.v
	p.Push(1)
	e := p.Ptr(0)
	b.v.Push(2)
	println(*e)
}
