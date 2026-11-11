package main

// Packed fields the compiler cannot access yet (roadmap item 238): each
// is valid by spec section 7.14, and each stops the build with E2500
// rather than compiling to wrong code.

// An array field that does not start on a byte.
type A packed le [4]byte {
	x bits[uint8, 4]
	a [2]byte
}

// An array of several-byte elements in a big-endian type laid out by byte.
type B packed be [4]byte {
	h [2]uint16
}

// An array field in a big-endian integer.
type C packed be uint32 {
	a [2]byte
	b uint16
}

// An array field of a volatile packed value.
type V packed uint32 {
	a [2]byte
	b uint16
}

// A 64-bit field whose bytes span nine bytes.
type D packed le [12]byte {
	x bits[uint8, 4]
	y uint64
}

// A big-endian integer that does not start on a byte.
type Inner packed be uint16 {
	a uint16
}

type E packed [3]byte {
	x bits[uint8, 3]
	i Inner
}

func main() {
	var a A
	var b B
	var c C
	var d D
	var e E
	var v volatile[V]
	println(a.a[0], b.h[0], c.a[0], d.y, e.i.a, v.a[0])
}
