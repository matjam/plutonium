package main

import "pluto/vec"

type S struct{ p *int }

func main() {
	var v vec.Vec[int]
	v.Push(7)
	var s S
	ps := &s
	ps.p = v.Ptr(0)
	v.Free()
	println(*s.p)
}
