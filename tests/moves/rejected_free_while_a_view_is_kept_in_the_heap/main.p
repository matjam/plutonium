package main

import "pluto/vec"

// A call that may free what an owner holds ends every view of that memory
// (language spec section 18.3). A view stored by a call where the
// compiler does not follow it can no longer be checked, so the call that
// may free is E3314.

type H struct{ s []int }

func save(h *H, s []int) { h.s = s }

func main() {
	h := new(H)
	v := vec.New[int]()
	v.Push(1)
	save(&*h, v.Slice())
	v.Push(2)
	println(h.s[0])
}
