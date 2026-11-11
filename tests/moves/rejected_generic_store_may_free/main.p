package main

// Generic code takes the strictest rule any type argument needs (language
// spec section 18.3, D81). A generic function that assigns to an element
// of a slice it is given drops the old element, which frees memory when
// the type argument is an owner or a counted box. So a view of a local
// owner passed to it ends at the call, as it does for the same function
// written for that type.

import (
	"pluto/alloc"
	"pluto/vec"
)

func set[T any](data []T, x T) { data[0] = x }

func main() {
	v := vec.New[shared[vec.Vec[int]]]()
	v.Push(alloc.Shared[vec.Vec[int]]())
	all := v.Slice()
	set(all, alloc.Shared[vec.Vec[int]]())
	println(len(all))
}
