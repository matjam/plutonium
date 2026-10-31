// A type assertion to an interface in generic code over [T owner any]
// finds the itab of the container with views (sections 8.7, 10.9, 12.10).
// Bump[int] asserts a *vec.Vec[int] held in an any to Valuer[int], whose
// Values yields views, and Sum asserts the same type to IntValuer, whose
// Values yields ints. The program has an itab for each.
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
	var a any = v
	for p := range a.(Valuer[T]).Values() {
		f(p)
	}
}

func Sum(v *vec.Vec[int]) int {
	var a any = v
	n := 0
	for x := range a.(IntValuer).Values() {
		n += x
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
