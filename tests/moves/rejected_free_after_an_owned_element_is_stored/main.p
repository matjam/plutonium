package main

import "pluto/vec"

// A value loaded through a view that itself owns memory (an element of a
// vector of vectors) looks at memory the outer owner frees: a callee that
// keeps a view of it keeps a view of the owner, so a later free of the
// owner is E3314 (language spec section 18.3).

type H struct{ s []int }

var keep H

func save(s []int) { keep.s = s }

// saveFirst keeps a view of what the first element owns: memory loaded
// through its argument.
func saveFirst(s []vec.Vec[int]) { keep.s = s[0].Slice() }

func main() {
	vv := vec.New[vec.Vec[int]]()
	in := vec.New[int]()
	in.Push(1)
	vv.Push(in)
	saveFirst(vv.Slice())
	vv.Free()
	println(keep.s[0])

	ww := vec.New[vec.Vec[int]]()
	in2 := vec.New[int]()
	in2.Push(2)
	ww.Push(in2)
	save(ww.Slice()[0].Slice())
	ww.Free()
	println(keep.s[0])
}
