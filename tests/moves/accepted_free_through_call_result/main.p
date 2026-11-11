package main

import "pluto/vec"

// A call's result that may hold its argument is followed only as far as
// the callee frees through it: views used before the free, or a callee
// that only reads, are fine (language spec section 18.3).

type T owner struct {
	v vec.Vec[int]
	n int
}

func id(p *T) *T { return p }

func (t *T) Reset() { t.v = vec.Vec[int]{} }

func g(p *T) {
	q := id(p)
	q.v.Push(1)
}

// look reads through the pointer id returned.
func look(p *T) int {
	q := id(p)
	return q.v.Len()
}

func main() {
	var x T
	x.v.Push(0)
	s := x.v.Slice()
	println(look(&x))
	println(s[0])
	g(&x)

	var y T
	k := &y.n
	println(*k)
	id(&y).Reset()
}
