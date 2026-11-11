package main

// Generic code takes the strictest rule any type argument needs (language
// spec section 18.3, D81). A generic function or method that assigns to an
// element of a slice drops the old element, which frees memory when the
// type argument is an owner or a counted box. So a view of a local owner
// passed to it, or held by its receiver, ends at the call, as it does for
// the same function written for that type.

import (
	"pluto/alloc"
	"pluto/vec"
)

func set[T any](data []T, x T) { data[0] = x }

type Box[T any] struct{ buf []T }

func (b *Box[T]) Put(x T) { b.buf[0] = x }

func main() {
	v := vec.New[shared[vec.Vec[int]]]()
	v.Push(alloc.Shared[vec.Vec[int]]())
	all := v.Slice()
	set(all, alloc.Shared[vec.Vec[int]]())
	println(len(all))
	b := Box[shared[vec.Vec[int]]]{buf: v.Slice()}
	b.Put(alloc.Shared[vec.Vec[int]]())
	println(len(b.buf))
}
