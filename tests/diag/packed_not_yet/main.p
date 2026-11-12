package main

// Packed fields the compiler cannot access (roadmap item 238): each stops
// the build with E2500 rather than compiling to wrong code. Section 7.14
// does not say in which order the bytes of an array field's elements are
// stored in big-endian storage, or what the address of a packed field that
// does not hold whole bytes is.

// An array of several-byte elements in a big-endian type laid out by byte.
type B packed be [4]byte {
	h [2]uint16
}

// An array field in a big-endian integer.
type C packed be uint32 {
	a [2]byte
	b uint16
}

// An array field that does not start on a byte.
type A packed le [4]byte {
	x bits[uint8, 4]
	a [2]byte
	_ bits[uint16, 12]
}

func main() {
	var a A
	var b B
	var c C
	println(b.h[0], c.a[0])
	p := &a.a
	s := a.a[:]
	println(p[0], s[0])
}
