package main

import "pluto/vec"

// A func value that may hold a function without a body is a call whose
// callees are not known: it may free through everything it is given
// (language spec section 18.3, A local owner's memory). It does not keep
// the pointer (pu:noescape), but nothing says it frees nothing.

//pu:noescape
func ext(v *vec.Vec[int])

func look(v *vec.Vec[int]) { println(v.Len()) }

func main() {
	v := vec.New[int]()
	v.Push(0)
	s := v.Slice()
	f := look
	if v.Len() > 0 {
		f = ext
	}
	f(&v)
	println(s[0])
}
