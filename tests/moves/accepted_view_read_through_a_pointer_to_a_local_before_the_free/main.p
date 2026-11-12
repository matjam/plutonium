package main

import "pluto/vec"

type S struct{ p *int }

func main() {
	var v vec.Vec[int]
	v.Push(7)
	var s S
	s.p = v.Ptr(0)
	ps := &s
	println(*ps.p)
	q := v.Ptr(0)
	pq := &q
	println(**pq)
	v.Free()
}
