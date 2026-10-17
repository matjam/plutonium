package main

import "pluto/fmt"

// Packed types backed by bytes number bits by byte: position p is bit p%8 of
// byte p/8. A byte-level field of several bytes is stored in the stated byte
// order, whatever the target (spec section 7.14, Byte order).

type Frag packed be uint16 {
	offset bits[uint16, 13]
	flags  bits[uint8, 3]
}

type IPv4Header packed be [20]byte {
	ihl     bits[uint8, 4]
	version bits[uint8, 4]
	tos     uint8
	length  uint16
	id      uint16
	frag    Frag
	ttl     uint8
	proto   uint8
	sum     uint16
	src     uint32
	dst     uint32
}

// The same fields, little-endian: a nested type keeps its own byte order.
type FragLE packed le uint16 {
	offset bits[uint16, 13]
	flags  bits[uint8, 3]
}

type HeaderLE packed le [8]byte {
	ihl     bits[uint8, 4]
	version bits[uint8, 4]
	length  uint16
	frag    FragLE
	id      uint16
}

// Bit fields may cross bytes when the order is little-endian or native.
type Wide packed le [4]byte {
	a bits[uint8, 3]
	b bits[uint16, 12]
	c bits[uint8, 1]
	d uint16
}

// Small byte-backed types are held in a register; their fields still follow
// the stated order.
type Pair packed be [4]byte {
	x uint16
	y uint16
}

type Tiny packed be [2]byte {
	hi bits[uint8, 4]
	lo bits[uint8, 4]
	n  uint8
}

// No backing: sized from the fields.
type Rec packed be {
	tag uint8
	n   uint16
	m   uint32
}

type Native packed [4]byte {
	x uint16
	y bits[uint8, 4]
	z bits[uint8, 4]
	w uint8
}

func main() {
	var h IPv4Header
	h.version = 4
	h.ihl = 5
	h.tos = 0x10
	h.length = 0x0102
	h.id = 0xABCD
	h.frag.offset = 0x1234
	h.frag.flags = 2
	h.ttl = 64
	h.proto = 6
	h.sum = 0xBEEF
	h.src = 0x0A000001
	h.dst = 0xC0A80102
	fmt.Printf("% x\n", [20]byte(h))
	fmt.Println(h.ihl, h.version, h.tos, h.length, h.id, h.frag.offset, h.frag.flags, h.ttl, h.proto, h.sum)
	fmt.Printf("%#x %#x %#x\n", h.src, h.dst, uint16(h.frag))

	// A header received from the wire.
	raw := [20]byte{0x45, 0, 0, 0x28, 0x12, 0x34, 0x40, 0x00, 0x40, 0x11, 0, 0, 10, 0, 0, 1, 10, 0, 0, 2}
	g := IPv4Header(raw)
	fmt.Println(g.ihl, g.version, g.length, g.id, g.frag.offset, g.frag.flags, g.ttl, g.proto)
	g.frag.flags = 0
	g.frag.offset = 185
	g.ttl--
	fmt.Printf("% x\n", [20]byte(g))
	f := g.frag
	f.flags = 7
	g.frag = f
	gb := [20]byte(g)
	fmt.Printf("%#x % x\n", uint16(g.frag), gb[6:8])

	var l HeaderLE
	l.ihl = 5
	l.version = 4
	l.length = 0x0102
	l.frag.offset = 0x1234
	l.frag.flags = 2
	l.id = 0xABCD
	fmt.Printf("% x\n", [8]byte(l))
	fmt.Println(l.length, l.frag.offset, l.frag.flags, l.id)

	var w Wide
	w.a = 5
	w.b = 0xABC
	w.c = 1
	w.d = 0x0102
	fmt.Printf("% x\n", [4]byte(w))
	fmt.Println(w.a, w.b, w.c, w.d)

	var p Pair
	p.x = 0x0102
	p.y = 0x0304
	fmt.Printf("% x\n", [4]byte(p))
	fmt.Println(p.x, p.y)
	q := Pair([4]byte{0xAA, 0xBB, 0xCC, 0xDD})
	fmt.Printf("%#x %#x\n", q.x, q.y)

	var t Tiny
	t.hi = 0xA
	t.lo = 0x5
	t.n = 9
	fmt.Printf("% x %d %d %d\n", [2]byte(t), t.hi, t.lo, t.n)

	var r Rec
	r.tag = 1
	r.n = 0x0203
	r.m = 0x04050607
	fmt.Printf("% x\n", [7]byte(r))
	fmt.Println(r.tag, r.n, r.m)

	var n Native
	n.x = 0x0102
	n.y = 0xA
	n.z = 0x5
	n.w = 7
	fmt.Printf("% x\n", [4]byte(n))
	fmt.Println(n.x, n.y, n.z, n.w)
}
