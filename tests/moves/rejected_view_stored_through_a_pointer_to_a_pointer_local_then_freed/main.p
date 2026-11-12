package main

import "pluto/vec"

func main() {
	var v vec.Vec[int]
	v.Push(7)
	var q *int
	pq := &q
	*pq = v.Ptr(0)
	v.Free()
	println(*q)
}
