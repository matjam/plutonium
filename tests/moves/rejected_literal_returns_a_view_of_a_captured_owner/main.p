package main

import "pluto/vec"

// What a literal returns may be a view of a variable it uses (language
// spec section 18.3).

func main() {
	var v vec.Vec[int]
	v.Push(7)
	get := func() *int { return v.Ptr(0) }
	p := get()
	v.Free()
	println(*p)
}
