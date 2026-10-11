package main

import (
	"pluto/bits"
	"pluto/fmt"
	"unsafe"
)

// Describing hardware registers and wire formats exactly.
//
// A packed type fixes the position of every field, bit by bit (language spec
// section 7.14). Fields are numbered from the least significant bit of the
// value, so the layout reads the same on any machine. `bits[T, N]` is an
// N-bit field read as T, `bits { A B C }` is a group of named one-bit flags,
// and `be` or `le` says in which byte order the whole value is stored. A
// packed value converts to and from an integer or byte array of its size, so
// a raw register value becomes a typed view with a conversion and no casts.
// The same description serves a device's status register, a descriptor in
// memory, and a header on the network.

// A device status register: three flags, a channel number and a counter.
type StatusReg packed uint32 {
	flags bits {
		READY
		ERROR
		BUSY
	}
	channel bits[uint, 4]
	count   bits[uint, 8]
}

// A network header. `be` stores the 32-bit value byte-swapped, as it travels
// on the wire; the fields still count from the value's low bit.
type NetHeader packed be uint32 {
	version bits[uint, 4]
	flags   bits[uint, 8]
	length  bits[uint, 20]
}

// A descriptor read by a DMA engine: byte-level fields with no padding, and a
// reserved byte that is always zero.
type Descriptor packed {
	addr   uint32
	length uint16
	kind   uint8
	_      uint8
}

func main() {
	var s StatusReg
	s.flags.READY = true
	s.flags.BUSY = true
	s.channel = 5
	s.count = 200
	fmt.Printf("status word 0x%08x ready=%t error=%t channel=%d count=%d\n",
		uint32(s), s.flags.READY, s.flags.ERROR, s.channel, s.count)

	// A value read from a register is a plain integer until it is converted.
	raw := uint32(0x0000642d)
	view := StatusReg(raw)
	fmt.Printf("raw %#x: busy=%t, %d bits set\n", raw, view.flags.BUSY, bits.OnesCount32(raw))

	// A write keeps only the low bits of the value: 19 & 0xf is 3.
	view.channel = 19
	fmt.Println("channel is", view.channel)

	var h NetHeader
	h.version = 4
	h.length = 1500
	wire := [4]byte(h)
	fmt.Printf("header on the wire: % x\n", wire[:])

	// The same four bytes read back from the network.
	got := NetHeader(wire)
	fmt.Printf("version %d, length %d\n", got.version, got.length)

	var d Descriptor
	fmt.Printf("descriptor is %d bytes, no padding\n", unsafe.Sizeof(d))
}
