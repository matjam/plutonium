// A range over the values of a hash.Map[K, T] in generic code over
// [T owner any] yields views, *T (sections 8.7, 10.9, 12.10), in every
// instance. The views stay views through a second generic function and a
// closure: AddAll[int] changes the values in place, as AddAll[Conn] does.
package main

import (
	"pluto/hash"
	"pluto/vec"
)

type Conn owner struct{ id int }

func Each[T owner any](m *hash.Map[string, T], f func(*T)) {
	for _, p := range m.All() {
		f(p)
	}
}

func AddAll[T owner any](m *hash.Map[string, T], v *vec.Vec[T], f func(*T)) {
	Each(m, f)
	g := func() {
		for p := range m.Values() {
			f(p)
		}
		for p := range v.Values() {
			f(p)
		}
	}
	g()
}

func main() {
	m := hash.NewMap[string, Conn](0)
	m.Set("a", Conn{1})
	cv := vec.New[Conn]()
	cv.Push(Conn{2})
	AddAll(&m, &cv, func(c *Conn) { c.id += 10 })
	println(m.Ptr("a").id, cv.Ptr(0).id)
	n := hash.NewMap[string, int](0)
	n.Set("a", 3)
	iv := vec.New[int]()
	iv.Push(4)
	AddAll(&n, &iv, func(p *int) { *p += 10 })
	println(n.At("a"), iv.At(0))
}
