package main

import "unsafe"

// unsafe.Sizeof and unsafe.Alignof of aligned[T, N] give the size and
// alignment of the wrapper type (spec section 7.16), wherever the operand
// comes from.

type A aligned[uint8, 16]

type S struct {
	a aligned[uint8, 16]
	b uint8
}

type P struct {
	v volatile[uint16]
	w aligned[[3]uint32, 16]
}

var g aligned[[2048]byte, 4096]

const cs = unsafe.Sizeof(g)
const ca = unsafe.Alignof(g)

func main() {
	var x aligned[uint8, 16]
	var y aligned[[3]uint32, 16]
	var a A
	var z [2]aligned[uint8, 16]
	var s S
	p := new(aligned[uint8, 16])
	var q P
	println(unsafe.Sizeof(x), unsafe.Sizeof(y), unsafe.Sizeof(a), unsafe.Sizeof(s.a), unsafe.Sizeof(*p))
	println(unsafe.Alignof(x), unsafe.Alignof(y), unsafe.Alignof(a), unsafe.Alignof(s.a), unsafe.Alignof(*p))
	println(unsafe.Sizeof(z), unsafe.Sizeof(z[1]), unsafe.Offsetof(s.b), unsafe.Sizeof(s))
	println(unsafe.Sizeof((x)), unsafe.Alignof((y)))
	println(cs, ca, unsafe.Sizeof(g))
	println(unsafe.Sizeof(q.v), unsafe.Sizeof(q.w), unsafe.Offsetof(q.w), unsafe.Sizeof(q))
	var arr [unsafe.Sizeof(x)]byte
	println(len(arr))
	// Reading the variable still gives the element type.
	println(unsafe.Sizeof(x+0), unsafe.Sizeof(y[0]))
}
