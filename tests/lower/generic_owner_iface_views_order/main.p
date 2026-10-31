// A library container converted to an interface in generic code over
// [T owner any] keeps its views (sections 8.7, 10.9, 12.10): Valuer[T] is
// implemented by *vec.Vec[T], whose Values yields *T there. Bump[int]
// calls the Values of the instance with views through its itab, and Sum
// calls the plain Values, which yields ints, through IntValuer. Each itab
// has its own adapter. Here the instance with views is lowered before the
// plain one.
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
	for p := range i.Values() {
		f(p)
	}
}

func Sum(v *vec.Vec[int]) int {
	var i IntValuer = v
	n := 0
	for x := range i.Values() {
		n += x
	}
	return n
}

func main() {
	w := vec.New[int]()
	w.Push(3)
	w.Push(4)
	Bump(&w, func(p *int) { *p += 10 })
	println(w.At(0), w.At(1), Sum(&w))
}
