package main

import "pluto/vec"

// What a literal returns may be a view of a variable it uses, reached
// through a local of the literal (language spec section 18.3).

func main() {
	var v vec.Vec[int]
	v.Push(7)
	x := 5
	pick := func(q *int) *int {
		r := q
		if v.Len() > 0 {
			r = v.Ptr(0)
		}
		return r
	}
	p := pick(&x)
	v.Free()
	println(*p)
}
