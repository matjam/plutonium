package main

// The twin of rejected_generic_store_may_free. A generic function or method
// is checked against every type argument the program instantiates it with
// (language spec section 18.3, D81). Here every one is int, which holds no
// owner and no counted box, so assigning to an element frees nothing, and a
// view of a local owner passed to the function, or held by the receiver,
// stays usable after the call.

import "pluto/vec"

func set[T any](data []T, x T) { data[0] = x }

type Box[T any] struct{ buf []T }

func (b *Box[T]) Put(x T) { b.buf[0] = x }

func main() {
	v := vec.New[int]()
	defer v.Free()
	v.Push(1)
	all := v.Slice()
	set(all, 2)
	println(all[0])
	b := Box[int]{buf: v.Slice()}
	b.Put(3)
	println(b.buf[0], all[0])
}
