// An interface value made in an instance of generic code over [T owner
// any] and one made in plain code hold the same library container, with
// itabs whose methods differ (sections 8.7, 10.9, 12.10). Asserting either
// to the container type finds it.
package main

import (
	"pluto/iter"
	"pluto/rt"
	"pluto/vec"
)

type Valuer[T owner any] interface {
	Values() iter.Seq[rt.View[T]]
}

type IntValuer interface {
	Values() iter.Seq[int]
}

func Bump[T owner any](v *vec.Vec[T], f func(*T)) {
	var i Valuer[T] = v
	if w, ok := i.(*vec.Vec[T]); ok {
		for p := range w.Values() {
			f(p)
		}
	}
	var a any = i
	switch x := a.(type) {
	case *vec.Vec[T]:
		for p := range x.Values() {
			f(p)
		}
	}
}

func Sum(v *vec.Vec[int]) int {
	var i IntValuer = v
	n := 0
	if w, ok := i.(*vec.Vec[int]); ok {
		for x := range w.Values() {
			n += x
		}
	}
	switch x := i.(type) {
	case *vec.Vec[int]:
		n += x.Len()
	}
	return n
}

func main() {
	w := vec.New[int]()
	w.Push(3)
	w.Push(4)
	println(Sum(&w))
	Bump(&w, func(p *int) { *p += 10 })
	println(w.At(0), w.At(1), Sum(&w))
}
