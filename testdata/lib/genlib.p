// Standard library generics used from generic code: hash.Map and vec.Vec
// instantiated with type parameters of the caller, with keys of a type
// declared in a function, allocation with explicit allocators in generic
// functions, and the map's default allocator argument through a generic
// wrapper.
package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/hash"
	"pluto/vec"
)

func Count[K comparable](keys []K) hash.Map[K, int] {
	m := hash.NewMap[K, int](8)
	for _, k := range keys {
		p, _ := m.Upsert(k)
		*p++
	}
	return m
}

func Distinct[T comparable](xs []T, a alloc.Allocator) int {
	var s hash.Set[T]
	s.Init(4, a)
	defer s.Free()
	for _, x := range xs {
		s.Add(x)
	}
	return s.Len()
}

func Collect[T any](xs []T, keep func(T) bool) vec.Vec[T] {
	v := vec.New[T]()
	for _, x := range xs {
		if keep(x) {
			v.Push(x)
		}
	}
	return v
}

func NewIn[T any](v T, a alloc.Allocator) *T {
	p := alloc.New[T](a) // a view of memory the arena owns
	*p = v
	return p
}

func MakeIn[T any](n int, v T, a alloc.Allocator) vec.Vec[T] {
	s := vec.Make[T](n, a)
	for i := range n {
		s[i] = v
	}
	return s
}

func main() {
	m := Count([]string{"a", "b", "a", "c", "a"})
	n, ok := m.Get("a")
	println(m.Len(), n, ok, m.Has("z"))
	m.Free()

	type id int16
	km := Count([]id{12, 12, 21})
	k, _ := km.Get(id(12))
	println(km.Len(), k)
	km.Free()

	type key struct {
		x, y int
	}

	ar := arena.NewFixed(4096)
	println(Distinct([]int{3, 1, 3, 2, 1}, &ar), Distinct([]string{"x", "x"}, alloc.Default()))

	v := Collect([]int{1, 2, 3, 4, 5, 6}, func(x int) bool { return x%2 == 0 })
	println(v.Len(), v.At(2))
	v.Free()

	p := NewIn(key{7, 8}, &ar)
	s := MakeIn(3, "s", &ar)
	println(p.x, p.y, s.Len(), s[2], ar.Used() > 0)
}
