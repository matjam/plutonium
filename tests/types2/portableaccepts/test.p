package main

import "unsafe"

type Color enum uint8 {
	Red
	Green
}

type Flags packed uint16 {
	mode bits[uint, 4]
	on   bits[bool, 1]
}

type Header struct {
	kind Color
	len  uint32
	f    Flags
}

type Shape union {
	Empty
	Dot struct{ x, y int32 }
	Box struct{ w, h float64 }
}

type Frame struct {
	hdr   Header
	data  [64]byte
	ids   ring[uint32, 8]
	reg   volatile[uint32]
	page  aligned[[16]byte, 16]
	shape Shape
	addr  uintptr
	ok    bool
	r     rune
}

type Named uint16

func Send[T portable](v T) {}

func SendAll[T portable](vs [4]T) {}

func Same[T interface{ comparable; portable }](a, b T) bool { Send(a); return a == b }

type Stringer interface{ String() string }

func Show[T interface{ portable; Stringer }](v T) string { Send(v); return v.String() }

type Packet[T portable] struct {
	hdr  Header
	body T
}

type Msg struct {
	n int
	s string
}

func (m Msg) String() string { return m.s }

type File owner struct{ fd int32 }

type Handle owner uintptr

type Reader interface{ Read() int }

var sink unsafe.Pointer


func Forward[T portable](v T) { Send(v) }

func ForwardWide[T interface{ portable; ~int | ~uint8 }](v T) { Send(v) }

func ok() {
	_ = Same(1, 2)
	_ = Same(Header{}, Header{})
	_ = Show(Color.Red)
	Send(true)
	Send(7)
	Send(uint8(1))
	Send(1.5)
	Send(float32(2))
	Send(uintptr(0x1000))
	Send(Color.Red)
	Send(Flags{})
	Send(Header{})
	Send(Shape.Dot{x: 1})
	Send(Frame{})
	Send(Named(3))
	Send([3]Header{})
	Send[[2][2]int]([2][2]int{})
	Send(struct{ a, b int }{})
	SendAll([4]uint16{})
	var p Packet[Header]
	_ = p
	var q Packet[[8]byte]
	_ = q
	Forward(Frame{})
	ForwardWide(5)
	ForwardWide(uint8(5))
}
