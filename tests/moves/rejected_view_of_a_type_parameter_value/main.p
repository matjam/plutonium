package main

// Generic code takes the strictest rule any type argument needs (language
// spec section 18.3, D81). A value of a type parameter converted to an
// ordinary interface is a view of its variable for every type argument:
// for a map, a channel or a box the interface holds the variable's word
// without a count, so a new value of the variable ends it. The rule holds
// for a type parameter whose type set has only values too.

import (
	"pluto/alloc"
	"pluto/vec"
)

func use(x any) { _ = x }

func show[T any](x T) {
	var z T
	i := any(x)
	x = z
	use(i)
}

func showValue[T int | float64](x T) {
	var z T
	i := any(x)
	x = z
	use(i)
}

// A T local has drop glue when some type of its type set has it: the end
// of its block releases what it holds and ends the views of it.
func scope[T any](t T) {
	var p *T
	{
		u := t
		p = &u
	}
	use(*p)
}

type Slicer interface{ Slice() []int }

// A method called on a T value may return what the value holds, as a
// counted box's Slice does, so its result is a view of the variable.
func method[T Slicer](t T) {
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

// A method called on a T value may free what an earlier result looks at,
// as (*Holder).Reset does.
func methodFrees[T Resetter](t T) {
	s := t.Slice()
	t.Reset()
	println(len(s))
}

func main() {
	show[map[int]int](map[int]int{1: 1})
	showValue[int](1)
	scope[map[int]int](map[int]int{1: 1})
	method[shared[vec.Vec[int]]](alloc.Shared[vec.Vec[int]]())
	h := Holder{v: vec.New[int]()}
	methodFrees(&h)
}
