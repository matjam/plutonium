package main

// Packed fields the compiler cannot access (roadmap item 238): each stops
// the build with E2500 rather than compiling to wrong code. Section 7.14
// does not say what the address of a packed field that does not hold
// whole bytes is.

// An array field that does not start on a byte.
type A packed le [4]byte {
	x bits[uint8, 4]
	a [2]byte
	_ bits[uint16, 12]
}

func main() {
	var a A
	p := &a.a
	s := a.a[:]
	println(p[0], s[0])
}
