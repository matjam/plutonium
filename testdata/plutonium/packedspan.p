package main

// Packed fields that are not whole bytes of their storage (spec section
// 7.14): array fields that do not start on a byte, array fields of a
// volatile value (section 7.15), fields whose bytes span more than eight
// bytes or a window wider than the storage, big-endian integer packed
// fields that do not start on a byte, and nested packed fields larger than
// eight bytes. Each test writes fields, reads them back, and reads the
// storage as bytes, which the layout rules fix.

// a: bits 4-19; a[0] is bits 4-11, a[1] bits 12-19.
type A packed le [4]byte {
	x bits[uint8, 4]
	a [2]byte
	y bits[uint8, 4]
	z uint8
}

// h: bits 4-35, the bytes of [2]uint16 in memory.
type H packed le [5]byte {
	x bits[uint8, 4]
	h [2]uint16
	_ bits[uint8, 4]
}

// Signed elements at bit 2.
type S packed le [3]byte {
	_ bits[uint8, 2]
	s [2]int8
	_ bits[uint8, 6]
}

// Arrays of packed elements and of arrays, at bit 1.
type Nib packed uint8 {
	lo bits[uint8, 4]
	hi bits[uint8, 4]
}

type N packed le [5]byte {
	f bits[bool, 1]
	n [2]Nib
	m [2][1]byte
	_ bits[uint8, 7]
}

// An array field of an integer-backed value, used through volatile.
type V packed uint32 {
	a [2]byte
	b uint16
}

// y spans bytes 0 to 8.
type D packed le [12]byte {
	x bits[uint8, 4]
	y uint64
	z bits[int32, 28]
}

// A signed 64-bit field at bit 3 spans bytes 0 to 8.
type DS packed le [10]byte {
	_ bits[uint8, 3]
	y int64
	_ bits[uint16, 13]
}

// v spans the three bytes of the storage; no power-of-two window fits.
type S3 packed [3]byte {
	x bits[uint8, 3]
	v bits[int32, 20]
	_ bits[uint8, 1]
}

// A float64 field at bit 4.
type FD packed le [9]byte {
	x bits[uint8, 4]
	d float64
	_ bits[uint8, 4]
}

// A big-endian integer packed type. In a type laid out by byte, its bytes
// are the groups of eight bits from its first bit, most significant first.
type Inner packed be uint16 {
	lo bits[uint8, 4]
	hi bits[uint16, 12]
}

type E packed [3]byte {
	x bits[uint8, 3]
	i Inner
	_ bits[uint8, 5]
}

// The same Inner on a byte, for comparison.
type E0 packed [3]byte {
	i Inner
	x uint8
}

type In32 packed be uint32 {
	a uint8
	b bits[uint32, 24]
}

type E32 packed le [5]byte {
	x bits[uint8, 5]
	i In32
	_ bits[uint8, 3]
}

// A 64-bit big-endian integer at bit 4 spans nine bytes.
type In64 packed be uint64 {
	v uint64
}

type E64 packed le [9]byte {
	x bits[uint8, 4]
	i In64
	_ bits[uint8, 4]
}

// An array of big-endian integer packed elements at bit 4.
type EA packed le [5]byte {
	x  bits[uint8, 4]
	is [2]Inner
	_  bits[uint8, 4]
}

// A nested packed field larger than eight bytes, off a byte and on one.
type Big packed le [10]byte {
	a uint32
	b [6]byte
}

type Outer packed le [11]byte {
	x bits[uint8, 4]
	g Big
	_ bits[uint8, 4]
}

type OuterBE packed be [12]byte {
	n uint16
	g Big
}

func dump(b []byte) {
	for _, c := range b {
		print(c, " ")
	}
	println()
}

func mkA() A {
	var a A
	a.a[1] = 0x42
	return a
}

func mkV() V {
	var v V
	v.a[1] = 3
	v.b = 4
	return v
}

func testA() {
	var a A
	a.x = 5
	a.a[0] = 0xAB
	a.a[1] = 0xCD
	a.y = 3
	a.z = 0x7E
	b := [4]byte(a)
	dump(b[:])
	println(a.x, a.a[0], a.a[1], a.y, a.z)
	c := a.a
	println(c[0], c[1], len(a.a))
	a.a = [2]byte{1, 2}
	b = [4]byte(a)
	dump(b[:])
	i := 1
	a.a[i] = 0xFF
	b = [4]byte(a)
	dump(b[:])
	a.a[i]--
	a.a[0] += 0x10
	for k, e := range a.a {
		print(k, ":", e, " ")
	}
	println()
	println(a.x, a.y, a.z, mkA().a[1])
	a2 := A{x: 1, a: [2]byte{2, 3}, y: 4, z: 5}
	b = [4]byte(a2)
	dump(b[:])
	println(a2.a == [2]byte{2, 3}, a.a == [2]byte{2, 3})
}

func testH() {
	var h H
	h.x = 0xA
	h.h[0] = 0x1234
	h.h[1] = 0xBEEF
	b := [5]byte(h)
	dump(b[:])
	println(h.h[0], h.h[1], h.x)
	for i := range 2 {
		h.h[i] += 1
	}
	println(h.h[0], h.h[1])

	var s S
	s.s[0] = -1
	s.s[1] = -100
	sb := [3]byte(s)
	dump(sb[:])
	println(s.s[0], s.s[1])
	j := 1
	s.s[j] = 50
	println(s.s[0], s.s[j])

	var n N
	n.f = true
	n.n[0].lo = 1
	n.n[1].hi = 0xF
	n.m[1][0] = 0x81
	nb := [5]byte(n)
	dump(nb[:])
	println(n.f, n.n[0].lo, n.n[1].hi, n.m[1][0])
	k := 0
	n.n[k].hi = 9
	println(n.n[0].hi, n.n[0].lo, uint8(n.n[0]))
	m := n.m
	println(m[1][0])
}

func testV() {
	var v volatile[V]
	v.a[1] = 7
	v.b = 0x0102
	v.a[0] = 9
	println(v.a[0], v.a[1], v.b, uint32(V(v)))
	i := 1
	v.a[i] = 3
	x := v.a
	println(x[0], x[1], uint32(V(v)))
	v.a = [2]byte{4, 5}
	println(v.a[0], v.a[1], v.b, uint32(V(v)))
	v.a[i]++
	println(v.a[i], mkV().a[1], mkV().b)
}

func testD() {
	var d D
	d.x = 0xF
	d.y = 0x0123456789ABCDEF
	d.z = -5
	b := [12]byte(d)
	dump(b[:])
	println(d.x, d.y, d.z)
	d.y = 0xFFFFFFFFFFFFFFFF
	b = [12]byte(d)
	dump(b[:])
	d.y = 1
	println(d.x, d.y, d.z)

	var s DS
	s.y = -2
	sb := [10]byte(s)
	dump(sb[:])
	println(s.y)
	s.y = 0x7FFFFFFFFFFFFFFF
	sb = [10]byte(s)
	dump(sb[:])
	println(s.y)

	var t S3
	t.x = 7
	t.v = -3
	tb := [3]byte(t)
	dump(tb[:])
	println(t.x, t.v)
	t.v = 0x7FFFF
	tb = [3]byte(t)
	dump(tb[:])
	println(t.x, t.v)

	var f FD
	f.x = 1
	f.d = -2.5
	fb := [9]byte(f)
	dump(fb[:])
	println(int(f.d*2), f.x)
}

func testE() {
	var e E
	e.x = 7
	e.i.lo = 5
	e.i.hi = 0xABC
	b := [3]byte(e)
	dump(b[:])
	println(e.x, e.i.lo, e.i.hi, uint16(e.i))
	w := e.i
	println(uint16(w), w.hi)
	e.i = Inner(0x1234)
	b = [3]byte(e)
	dump(b[:])
	println(e.i.lo, e.i.hi, e.x)
	e.i.hi++
	println(uint16(e.i))

	var e0 E0
	e0.i = Inner(0xABC5)
	b = [3]byte(e0)
	dump(b[:])

	var e32 E32
	e32.x = 0x1F
	e32.i.a = 0x11
	e32.i.b = 0xAABBCC
	b5 := [5]byte(e32)
	dump(b5[:])
	println(e32.i.a, e32.i.b, uint32(e32.i), e32.x)

	var e64 E64
	e64.x = 0xA
	e64.i.v = 0x0102030405060708
	b9 := [9]byte(e64)
	dump(b9[:])
	println(e64.i.v, uint64(e64.i), e64.x)

	var ea EA
	ea.x = 3
	ea.is[0] = Inner(0x1234)
	ea.is[1].hi = 0xFED
	ea.is[1].lo = 0xC
	b5 = [5]byte(ea)
	dump(b5[:])
	j := 1
	println(uint16(ea.is[0]), uint16(ea.is[j]), ea.is[j].hi, ea.is[0].lo)
	ea.is[j].lo = 1
	println(uint16(ea.is[1]), ea.x)
}

func testBig() {
	var o Outer
	o.x = 9
	o.g.a = 0xDEADBEEF
	o.g.b[5] = 0x11
	b := [11]byte(o)
	dump(b[:])
	g := o.g
	println(g.a, g.b[5], o.g.a, o.x)
	o.g = Big{a: 1, b: [6]byte{1, 2, 3, 4, 5, 6}}
	b = [11]byte(o)
	dump(b[:])
	println(o.g.b[2], o.x)

	var q OuterBE
	q.n = 0x0102
	q.g.a = 0x01020304
	q.g.b[0] = 9
	qb := [12]byte(q)
	dump(qb[:])
	h := q.g
	println(h.a, h.b[0], q.n)
	q.g = Big{a: 7}
	qb = [12]byte(q)
	dump(qb[:])
}

func main() {
	testA()
	testH()
	testV()
	testD()
	testE()
	testBig()
}
