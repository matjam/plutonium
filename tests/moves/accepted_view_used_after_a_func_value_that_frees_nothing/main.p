package main

import "pluto/vec"

// A func value whose candidates all have bodies is followed through them:
// neither frees through its argument, so the view stays usable (language
// spec section 18.3, A local owner's memory).

func look(v *vec.Vec[int]) { println(v.Len()) }

func again(v *vec.Vec[int]) { println(v.Len()) }

func main() {
	v := vec.New[int]()
	v.Push(7)
	s := v.Slice()
	f := look
	f(&v)
	if v.Len() > 0 {
		f = again
	}
	f(&v)
	println(s[0])
}
