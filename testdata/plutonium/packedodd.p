package main

// Packed fields off a byte whose bytes reach the end of a backing of an
// odd number of bytes (spec section 7.14): no power-of-two load fits inside
// the storage, so the field is read and written in parts.

// A uint16 at bit 3 of three bytes.
type A packed le [3]byte {
	a bits[uint8, 3]
	b uint16
	_ bits[uint8, 5]
}

// The same as a bit field.
type B packed le [3]byte {
	a bits[uint8, 3]
	b bits[uint16, 16]
	_ bits[uint8, 5]
}

// The same with no backing: the fields need 19 bits, so three bytes.
type N packed le {
	a bits[uint8, 3]
	b uint16
}

// A uint32 at bit 1 of five bytes.
type W packed le [5]byte {
	f bits[bool, 1]
	w uint32
	_ bits[uint8, 7]
}

// A signed field at bit 5 of three bytes.
type S packed le [3]byte {
	_ bits[uint8, 5]
	s int16
	_ bits[uint8, 3]
}

func dump(b []byte) {
	for _, c := range b {
		print(c, " ")
	}
	println()
}

func main() {
	var x A
	x.b = 0x1234
	x.a = 5
	println(x.b, x.a)
	r := [3]byte(x)
	dump(r[:])

	// Clearing the field keeps the bits around it.
	x = A([3]byte{0xFF, 0xFF, 0xFF})
	x.b = 0
	r = [3]byte(x)
	dump(r[:])
	println(x.b, x.a)

	var y B
	y.b = 0x1234
	y.a = 5
	println(y.b, y.a)
	r = [3]byte(y)
	dump(r[:])
	y.b++
	println(y.b, y.a)

	var n N
	n.b = 0xBEEF
	n.a = 2
	println(n.b, n.a)
	r = [3]byte(n)
	dump(r[:])

	var w W
	w.w = 0x89ABCDEF
	w.f = true
	println(w.w, w.f)
	r5 := [5]byte(w)
	dump(r5[:])
	w = W([5]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	w.w = 0
	r5 = [5]byte(w)
	dump(r5[:])
	println(w.w, w.f)

	var s S
	s.s = -2
	println(s.s)
	r = [3]byte(s)
	dump(r[:])
	s = S([3]byte{0, 0, 0xFF})
	println(s.s)
}
