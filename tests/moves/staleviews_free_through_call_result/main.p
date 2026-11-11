package main

import "pluto/vec"

// A call's result may hold what its argument points to, as the escape
// analysis says: a callee that frees through that result frees through
// the argument (language spec section 18.3, A local owner's memory).

type T owner struct {
	v vec.Vec[int]
	n int
}

func id(p *T) *T { return p }

func (t *T) Reset() { t.v = vec.Vec[int]{} }

// g frees through the pointer id returned, which is p.
func g(p *T) {
	q := id(p)
	q.v.Push(1)
}

func main() {
	// Through a callee.
	var x T
	x.v.Push(0)
	s := x.v.Slice()
	g(&x)
	println(s[0])

	// In the same function.
	var y T
	k := &y.n
	id(&y).Reset()
	println(*k)
}
