package main

// Float and array fields of packed types (spec section 7.14, Fields). A
// float field is stored as its bits, in the type's byte order; an array
// field is its elements' bytes.

type F packed be [12]byte {
	f float32
	d float64
}

type FL packed le [9]byte {
	x bits[uint8, 4]
	f float32
	_ bits[uint8, 4]
	d uint32
}

type FW packed be uint64 {
	f float32
	n uint32
}

type Big packed le [20]byte {
	tag  uint32
	data [16]byte
}

type Mac packed be [8]byte {
	mac [6]byte
	n   uint16
}

type Pair packed uint64 {
	a [4]byte
	b uint32
}

type Halves packed le [8]byte {
	h [2]uint16
	w uint32
}

func dump(b []byte) {
	for _, c := range b {
		print(c, " ")
	}
	println()
}

func fw() FW {
	var w FW
	w.f = 0.25
	w.n = 3
	return w
}

func pair() Pair {
	var p Pair
	p.a[1] = 5
	p.b = 7
	return p
}

func main() {
	var p F
	p.f = 1.0
	p.d = -2.5
	b := [12]byte(p)
	dump(b[:])
	println(int(p.f*4), int(p.d*4))
	q := F{f: 0.5}
	println(int(q.f*4), int(q.d*4))

	var l FL
	l.x = 3
	l.f = 2.5
	l.d = 99
	lb := [9]byte(l)
	dump(lb[:])
	println(l.x, int(l.f*2), l.d)

	var w FW
	w.f = 1.0
	w.n = 1
	println(int(w.f*4), w.n)
	println(int(fw().f*4), fw().n)

	var g Big
	g.tag = 7
	g.data[3] = 9
	d := g.data
	gb := [20]byte(g)
	dump(gb[:])
	println(g.tag, d[3], len(g.data))

	var m Mac
	m.n = 0x0102
	m.mac = [6]byte{1, 2, 3, 4, 5, 6}
	x := m.mac
	println(x[0], x[5], m.n)
	mb := [8]byte(m)
	dump(mb[:])
	m2 := Mac{mac: [6]byte{9, 8, 7, 6, 5, 4}, n: 1}
	mb = [8]byte(m2)
	dump(mb[:])

	println(pair().a[1], pair().b, uint64(pair()))

	var h Halves
	h.h[1] = 0x0304
	h.w = 9
	hb := [8]byte(h)
	dump(hb[:])
	println(h.h[0], h.h[1])
}
