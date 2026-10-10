package main

import "pluto/vec"

// v[i] on a vec.Vec calls its Index method, which checks the bound: the
// trace shows the method's frame and the line of the index expression.
func main() {
	v := vec.New[int]()
	v.Push(10)
	v.Push(20)
	v[1] += 5
	println(v[0], v[1], len(v))
	println(v[len(v)])
}
