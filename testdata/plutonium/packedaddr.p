package main

// The address of a packed field that is whole bytes of plain memory can be
// taken (section 7.14, decision D131), and an array of single bytes is
// allowed in a big-endian packed type, laid out by byte or integer-backed.

type Inner packed le [2]byte {
	lo uint8
	hi uint8
}

func (i *Inner) Set(lo, hi uint8) {
	i.lo = lo
	i.hi = hi
}

// Fields on a byte of little-endian storage laid out by byte.
type L packed le [11]byte {
	x  uint8
	n  uint16
	a  [2]byte
	in Inner
	f  float32
}

// Fields of an integer of the target's order.
type I packed uint32 {
	x uint8
	n uint16
	y uint8
}

// A big-endian type laid out by byte: single bytes, an array of bytes and
// a big-endian integer-backed field are plain memory.
type Frag packed be uint16 {
	offset bits[uint16, 13]
	flags  bits[uint8, 3]
}

type BE packed be [6]byte {
	t    uint8
	a    [2]uint8
	frag Frag
	_    uint8
}

// Arrays of single bytes in a big-endian integer: element i is the byte at
// bit 8i of the field in the value.
type C packed be uint32 {
	a [2]byte
	b uint16
}

func dump(b []byte) {
	for _, c := range b {
		print(c, " ")
	}
	println()
}

func put16(p *uint16, v uint16) { *p = v }

func main() {
	var l L
	put16(&l.n, 0xBEEF)
	pa := &l.a
	pa[1] = 7
	s := l.a[:]
	s[0] = 6
	q := &l.a[1]
	*q += 1
	l.in.Set(0x11, 0x22)
	pi := &l.in
	pi.hi++
	*(&l.f) = 1.5
	l.x = 9
	r := [11]byte(l)
	dump(r[:])
	println(l.x, l.n, l.a[0], l.a[1], l.in.lo, l.in.hi, l.f == 1.5)

	var i I
	i.x = 1
	i.y = 4
	put16(&i.n, 0x0302)
	println(uint32(i), i.n)

	var b BE
	pt := &b.t
	*pt = 0xAA
	sa := b.a[:]
	sa[0], sa[1] = 1, 2
	pf := &b.frag
	pf.offset = 0x123
	pf.flags = 5
	rb := [6]byte(b)
	dump(rb[:])
	println(b.t, b.a[0], b.a[1], b.frag.offset, b.frag.flags)

	var c C
	c.a[0] = 1
	c.a[1] = 2
	c.b = 0x0304
	k := 1
	c.a[k] += 0x10
	rc := [4]byte(c)
	dump(rc[:])
	println(c.a[0], c.a[1], c.b)
	for j, e := range c.a {
		print(j, ":", e, " ")
	}
	println()
	c = C([4]byte{0xA, 0xB, 0xC, 0xD})
	println(c.a[0], c.a[1], c.b)
	c.a = [2]byte{0x55, 0x66}
	rc = [4]byte(c)
	dump(rc[:])
}
