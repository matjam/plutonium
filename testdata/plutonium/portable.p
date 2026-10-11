package main

import "unsafe"

// The portable constraint (language spec section 8.9): a generic function
// that sends values between address spaces accepts exactly the types built
// from numbers, booleans, enums, packed types and arrays, structs and unions
// of them. Here the "other address space" is a byte buffer: a portable value
// means the same after a copy of its bytes, which is what makes Encode and
// Decode correct for every T that satisfies the constraint.

type Kind enum uint8 {
	Ping
	Data
	Close
}

type Flags packed uint16 {
	urgent bits[bool, 1]
	level  bits[uint, 4]
}

type Header struct {
	kind Kind
	seq  uint32
	f    Flags
}

type Shape union {
	Empty
	Dot struct{ x, y int32 }
	Box struct{ w, h float64 }
}

type Message struct {
	hdr    Header
	name   [8]byte
	nameN  uint8
	shape  Shape
	window ring[uint16, 4]
	addr   uintptr
	ok     bool
}

type Celsius float32

// Encode copies the bytes of v into buf and returns their number.
func Encode[T portable](buf []byte, v T) int {
	n := int(unsafe.Sizeof(v))
	src := unsafe.Slice((*byte)(unsafe.Pointer(&v)), n)
	copy(buf, src)
	return n
}

// Decode makes a value of type T from the bytes in buf.
func Decode[T portable](buf []byte) T {
	var v T
	n := int(unsafe.Sizeof(v))
	dst := unsafe.Slice((*byte)(unsafe.Pointer(&v)), n)
	copy(dst, buf[:n])
	return v
}

// RoundTrip sends v through a buffer and back; it is itself portable-generic,
// so it may pass its T on to Encode and Decode.
func RoundTrip[T portable](v T) T {
	var buf [256]byte
	n := Encode(buf[:], v)
	return Decode[T](buf[:n])
}

// Packet is a generic type whose body must be portable.
type Packet[T portable] struct {
	hdr  Header
	body T
}

func Wrap[T portable](seq uint32, body T) Packet[T] {
	return Packet[T]{hdr: Header{kind: Kind.Data, seq: seq}, body: body}
}

func main() {
	println(RoundTrip(true))
	println(RoundTrip(int64(-5)))
	println(RoundTrip(uint8(200)))
	println(RoundTrip(2.5))
	println(RoundTrip(Celsius(36.5)))
	println(RoundTrip(uintptr(0x1000)))
	println(RoundTrip(Kind.Close) == Kind.Close)

	f := RoundTrip(Flags{urgent: true, level: 9})
	println(f.urgent, f.level)

	h := RoundTrip(Header{kind: Kind.Data, seq: 77, f: Flags{level: 3}})
	println(h.kind == Kind.Data, h.seq, h.f.level)

	arr := RoundTrip([4]int16{1, -2, 3, -4})
	println(arr[0], arr[1], arr[2], arr[3])

	var m Message
	m.hdr = Header{kind: Kind.Ping, seq: 9}
	copy(m.name[:], "pluto")
	m.nameN = 5
	m.shape = Shape.Box{w: 1.5, h: 2.5}
	m.window.Push(11)
	m.window.Push(22)
	m.addr = 0xdead
	m.ok = true
	got := RoundTrip(m)
	println(got.hdr.seq, got.nameN, got.name[0], got.name[4], got.ok, got.addr)
	if b, ok := got.shape.(Shape.Box); ok {
		println(b.w, b.h)
	}
	println(got.window.Len())

	p := RoundTrip(Wrap(5, [2]uint16{7, 8}))
	println(p.hdr.seq, p.body[0], p.body[1])
	q := RoundTrip(Wrap(6, Shape.Dot{x: -1, y: 2}))
	if d, ok := q.body.(Shape.Dot); ok {
		println(q.hdr.seq, d.x, d.y)
	}
}
