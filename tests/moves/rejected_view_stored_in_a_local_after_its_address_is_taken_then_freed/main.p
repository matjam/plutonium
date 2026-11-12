package main

import "pluto/vec"

// ps points to s, so it sees the view s.p gets after ps is made: freeing
// w ends that view (language spec section 18.3, A local owner's memory).

type S struct{ p *int }

func main() {
	var v vec.Vec[int]
	v.Push(7)
	var w vec.Vec[int]
	w.Push(8)
	s := S{p: v.Ptr(0)}
	ps := &s
	s.p = w.Ptr(0)
	w.Free()
	println(*ps.p)
}
