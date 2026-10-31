// The views of an instance of generic code over [T owner any] reach a
// method value, a method expression and a type switch case of the library
// container (sections 8.7, 10.9, 12.10). The plain instance, which yields
// ints, is used in the same program.
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
	g := v.Values
	for p := range g() {
		f(p)
	}
	h := (*vec.Vec[T]).Values
	for p := range h(v) {
		f(p)
	}
	var a any = v
	switch x := a.(type) {
	case IntValuer:
		println("plain")
	case Valuer[T]:
		for p := range x.Values() {
			f(p)
		}
	}
}

func Sum(v *vec.Vec[int]) int {
	n := 0
	g := v.Values
	for x := range g() {
		n += x
	}
	h := (*vec.Vec[int]).Values
	for x := range h(v) {
		n += x
	}
	var a any = v
	switch x := a.(type) {
	case Valuer[int]:
		println("views")
	case IntValuer:
		for y := range x.Values() {
			n += y
		}
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
