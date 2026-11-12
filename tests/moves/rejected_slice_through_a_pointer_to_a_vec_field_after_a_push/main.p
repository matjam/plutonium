package main

import "pluto/vec"

// A slice obtained through a pointer to a vec field looks at the vec's
// heap memory, which a push through the pointer may free (language spec
// section 18.3).

type B owner struct{ v vec.Vec[int] }

func main() {
	var b B
	p := &b.v
	p.Push(1)
	s := p.Slice()
	p.Push(2)
	println(s[0])
}
