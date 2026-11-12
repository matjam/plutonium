package main

import "pluto/vec"

// A literal that uses an owner but builds its result only from its
// parameter returns no view of the owner (language spec section 18.3).

type holder struct{ p *int }

var x = 5

func main() {
	h := new(holder)
	for i := 0; i < 2; i++ {
		var v vec.Vec[int]
		v.Push(i)
		pick := func(q *int) *int {
			r := q
			if v.Len() > 1 {
				return nil
			}
			return r
		}
		h.p = pick(&x)
		v.Free()
	}
	println(*h.p)
}
