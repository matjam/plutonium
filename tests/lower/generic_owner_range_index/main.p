// A range over a vec.Vec[T] in generic code over [T owner any] that binds
// only the index, or nothing, uses no view of an element, so its instances
// compile for an owner type and for a value type alike. A range that binds
// the element gets a view in both (tests/lower/generic_owner_range).
package main

import "pluto/vec"

type Conn owner struct{ id int }

func Sum[T owner any](v *vec.Vec[T]) int {
	n := 0
	for i := range v {
		n += i
	}
	for range v {
		n += 10
	}
	for i, _ := range v {
		n += 100 * i
	}
	return n
}

func main() {
	v := vec.New[Conn]()
	v.Push(Conn{1})
	v.Push(Conn{2})
	w := vec.New[int]()
	w.Push(3)
	w.Push(4)
	w.Push(5)
	println(Sum(&v), Sum(&w))
}
