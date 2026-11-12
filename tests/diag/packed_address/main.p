package main

// The address of a packed field that is not whole bytes of plain memory
// cannot be taken (section 7.14, decision D131): with &, by slicing an
// array field, or by calling a method with a pointer receiver.

type Inner packed le [2]byte {
	lo uint8
	hi uint8
}

func (i *Inner) Set() { i.lo = 1 }

type A packed le [5]byte {
	x  bits[uint8, 4]
	a  [2]byte
	in Inner
	_  bits[uint8, 4]
}

type BE packed be [4]byte {
	n uint16
	b bool
	_ uint8
}

type C packed be uint32 {
	a [2]byte
	b uint16
}

type V packed uint32 {
	a [2]byte
	b uint16
}

type F packed uint8 {
	on   bits[bool, 1]
	mode bits[uint8, 4]
	_    bits[uint8, 3]
}

func main() {
	var a A
	var be BE
	var c C
	var v volatile[V]
	var f F
	p := &a.a
	s := a.a[:]
	q := &a.a[0]
	a.in.Set()
	n := &be.n
	b := &be.b
	ca := &c.a
	cb := &(c.b)
	va := &v.a
	m := &f.mode
	_, _, _, _, _, _, _, _, _ = p, s, q, n, b, ca, cb, va, m
}
