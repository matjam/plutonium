package main

import "pluto/vec"

// An owner frees what it holds when it is dropped at the end of its block,
// is dropped, or is given a new value. A view of that memory stored by a
// call where the compiler does not follow it can no longer be checked
// there, so each of these is E3314 (language spec section 18.3).

type H struct{ s []int }

var keep H

func save(s []int) { keep.s = s }

func main() {
	{
		v := vec.New[int]()
		v.Push(1)
		save(v.Slice())
	}
	println(keep.s[0])

	w := vec.New[int]()
	w.Push(1)
	save(w.Slice())
	w.Drop()
	println(keep.s[0])

	x := vec.New[int]()
	x.Push(1)
	save(x.Slice())
	x = vec.New[int]()
	println(keep.s[0])
	x.Push(2)
}
