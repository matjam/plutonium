package main

// The twin of rejected_view_of_a_type_parameter_value (language spec
// section 18.3, D81). An interface made from a T value is used before the
// variable gets a new value. When every type of the type set is a value,
// the end of a block releases nothing, and a view obtained from a T value
// does not look at memory the variable holds.

import (
	"pluto/alloc"
	"pluto/vec"
)

func use(x any) { _ = x }

func show[T int | float64](x T) {
	var z T
	i := any(x)
	use(i)
	x = z
}

func scope[T ~int | ~float64](t T) {
	var p *T
	{
		u := t
		p = &u
	}
	use(*p)
}

type Slicer interface{ Slice() []int }

// The result of a method called on a T value is used before the variable
// gets a new value.
func method[T Slicer](t T) {
	s := t.Slice()
	println(len(s))
	var z T
	t = z
}

type Ints []int

func (a Ints) Slice() []int { return a }

// When every type of the set is a value or a view, the result does not
// look at memory the variable holds.
func methodOfViews[T interface {
	~[]int
	Slice() []int
}](t T) {
	s := t.Slice()
	var z T
	t = z
	println(len(s))
}

type Holder owner struct{ v vec.Vec[int] }

func (h *Holder) Reset()       { h.v = vec.New[int]() }
func (h *Holder) Slice() []int { return h.v.Slice() }

type Resetter interface {
	Reset()
	Slice() []int
}

// The result of a method is used before a method that may free it.
func methodFrees[T Resetter](t T) {
	s := t.Slice()
	println(len(s))
	t.Reset()
}

func main() {
	show[int](1)
	scope[float64](2)
	method[shared[vec.Vec[int]]](alloc.Shared[vec.Vec[int]]())
	methodOfViews[Ints](Ints{1, 2})
	h := Holder{v: vec.New[int]()}
	methodFrees(&h)
}
