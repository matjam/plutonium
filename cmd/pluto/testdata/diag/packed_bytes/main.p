package main

// A big-endian packed type with a byte array backing numbers its bits by
// byte (spec section 7.14).

type Bad packed be [6]byte {
	version bits[uint8, 4]
	offset  bits[uint16, 13]
	flags   bits[uint8, 3]
	length  uint16
}

type Unaligned packed be [4]byte {
	tag bits[uint8, 4]
	len uint16
}

// The fixes: a nested packed type with an integer backing, and reserved
// bits, which may be placed anywhere.
type Frag packed be uint16 {
	offset bits[uint16, 13]
	flags  bits[uint8, 3]
}

type Good packed be [4]byte {
	version bits[uint8, 4]
	_       bits[uint8, 4]
	frag    Frag
	_       uint8
}

// Little-endian and native types may cross bytes.
type Little packed le [6]byte {
	version bits[uint8, 4]
	offset  bits[uint16, 13]
	flags   bits[uint8, 3]
	length  uint16
}

// An integer backing keeps the integer rule.
type Word packed be uint32 {
	a bits[uint8, 4]
	b bits[uint16, 13]
	c bits[uint16, 15]
}

func main() {
	var b Bad
	var u Unaligned
	var g Good
	var l Little
	var w Word
	_, _, _, _, _ = b, u, g, l, w
}
