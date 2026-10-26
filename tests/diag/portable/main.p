package main

// The portable constraint (language spec section 8.9): the type argument
// must be built only from numbers, booleans, enums, packed types and arrays,
// structs and unions of them.

func Send[T portable](port int, v T) {}

type Header struct {
	kind uint8
	len  uint32
}

// A string field: its bytes live in the sender's address space.
type Named struct {
	id   uint32
	name string
}

// A pointer field.
type Linked struct {
	id   uint32
	next *Linked
}

type Reader interface{ Read(p []byte) int }

// An owner is not portable, even when it is built from numbers.
type Frame owner uintptr

// The cause is found at any depth.
type Inner struct {
	n   int
	buf []byte
}

type Outer struct {
	hdr   Header
	parts [2]Inner
}

func demo(r Reader) {
	Send(1, Header{})
	Send(1, Named{})
	Send(1, Linked{})
	Send(1, r)
	Send(1, Frame(0))
	Send(1, Outer{})
}

func main() {}
