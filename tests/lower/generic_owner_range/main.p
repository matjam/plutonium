// A range over a vec.Vec[T] in generic code over [T owner any] yields views,
// *T (sections 8.7, 12.10), in every instance: Bump[int] gets pointers to
// the elements, as Bump[Conn] does.
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
	v.Push(Conn{2})
	Bump(&v, func(c *Conn) { c.id += 10 })
	println(v.Ptr(0).id, v.Ptr(1).id)
	w := vec.New[int]()
	w.Push(3)
	w.Push(4)
	Bump(&w, func(p *int) { *p += 10 })
	println(w.At(0), w.At(1))
}
