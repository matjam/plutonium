package main

import "pluto/vec"

// A view stored by a call in memory the compiler follows (a local H) is
// tracked there, and a call that frees nothing may still be made while it
// is kept. Keeping the view in heap memory is E3003 (rejected_free_while_
// a_view_is_kept_in_the_heap), so the free check there is a backstop.

type H struct{ s []int }

func save(h *H, s []int) { h.s = s }

func main() {
	var h H
	v := vec.New[int]()
	v.Push(1)
	save(&h, v.Slice())
	println(v.Len())
	println(h.s[0])
}
