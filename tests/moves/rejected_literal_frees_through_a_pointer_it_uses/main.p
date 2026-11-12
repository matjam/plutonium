package main

import "pluto/vec"

func main() {
	var v vec.Vec[int]
	v.Push(7)
	p := v.Ptr(0)
	pv := &v
	f := func() {
		q := pv
		q.Push(8)
	}
	f()
	println(*p)
}
