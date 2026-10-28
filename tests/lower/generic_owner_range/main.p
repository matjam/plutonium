// A range over a vec.Vec[T] in generic code over [T owner any] yields views,
// *T (sections 8.7, 12.10). An instance with a value type, Bump[int], does
// not keep the views yet, so the compiler rejects it (E2500) instead of
// producing wrong code. Bump[Conn] is fine. The runtime target is
// tests/lower/generic_owner_range_views.
package main

import "pluto/vec"

type Conn owner struct{ id int }

func Bump[T owner any](v *vec.Vec[T], f func(*T)) {
	for _, x := range v {
		f(x)
	}
}

func main() {
	v := vec.New[Conn]()
	v.Push(Conn{1})
	Bump(&v, func(c *Conn) { c.id += 10 })
	println(v.Ptr(0).id)
	w := vec.New[int]()
	w.Push(3)
	Bump(&w, func(p *int) { *p += 10 })
	println(w.At(0))
}
