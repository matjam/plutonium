package main

import "pluto/vec"

func mk() vec.Vec[int] {
	var v vec.Vec[int]
	v.Push(7)
	return v
}

func show(s []int) { println(s[0]) }

// The deferred call keeps the slice, but the temporary Vec is dropped at
// the end of the defer statement.
func main() {
	defer show(mk().Slice())
	println("body")
}
