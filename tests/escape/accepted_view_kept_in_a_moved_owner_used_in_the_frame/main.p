package main

import "pluto/vec"

// Accepted twin of rejected_view_kept_in_a_moved_owner_stored_in_a_package_variable:
// the owner that holds the view is moved within the frame and used there
// while v lives (language spec section 18.3).

type O owner struct{ s []int }

func use(o O) int { return o.s[0] }

func main() {
	v := vec.New[int]()
	v.Push(1)
	o := O{}
	o.s = v.Slice()
	p := o
	println(p.s[0])
	println(use(p))
	v.Free()
}
