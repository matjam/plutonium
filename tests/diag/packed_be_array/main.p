package main

// An array field whose elements are wider than one byte is not allowed in a
// big-endian packed type, laid out by byte or integer-backed (section 7.14,
// decision D131). Arrays of single bytes and reserved fields are allowed.

type B packed be [12]byte {
	h [2]uint16
	m [1][2]int32
}

type C packed be uint32 {
	a [2]uint16
}

type OK packed be uint32 {
	a [2]uint8
	_ [1]uint16
}

func main() {
	var b B
	var c C
	var ok OK
	println(b.h[0], c.a[0], ok.a[1])
}
