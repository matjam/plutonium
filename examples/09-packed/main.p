package main

import "pluto/fmt"

// Packed types: exact bit layouts for hardware registers and wire formats.

type ControlReg packed uint32 {
	mode     bits[uint, 4]
	enable   bits[bool, 1]
	priority bits[uint, 3]
}

// A big-endian header: stored byte-swapped, fields numbered from the
// least significant bit of the logical value.
type NetHeader packed be uint32 {
	version  bits[uint, 4]
	flags    bits[uint, 8]
	reserved bits[uint, 20]
}

// A group of named one-bit flags.
type StatusReg packed uint32 {
	flags bits {
		ENABLE
		READY
		ERROR
		OVERFLOW
	}
	mode bits[uint, 4]
}

// Byte-level fields at unaligned offsets.
type Header packed {
	version uint8
	kind    uint8
	length  uint32
}

// Signed bit fields read sign-extended.
type Delta packed uint16 {
	dx bits[int8, 5]
	dy bits[int8, 5]
}

func main() {
	var reg ControlReg
	reg.mode = 5
	reg.enable = true
	reg.priority = 7
	fmt.Printf("mode=%d enable=%t priority=%d raw=%#x\n", reg.mode, reg.enable, reg.priority, uint32(reg))

	// Writes keep only the field's bits.
	reg.mode = 17     // 17 & 0xF = 1
	reg.priority = 13 // 13 & 0x7 = 5
	reg.enable = false
	fmt.Printf("mode=%d enable=%t priority=%d raw=%#x\n", reg.mode, reg.enable, reg.priority, uint32(reg))

	// A raw register value becomes a typed view by conversion.
	view := ControlReg(0x1A)
	fmt.Printf("view mode=%d enable=%t\n", view.mode, view.enable)

	var net NetHeader
	net.version = 2
	net.flags = 9
	fmt.Printf("net version=%d flags=%d\n", net.version, net.flags)

	var status StatusReg
	status.flags.ENABLE = true
	status.flags.ERROR = true
	fmt.Printf("ENABLE=%t READY=%t ERROR=%t OVERFLOW=%t raw=%#x\n",
		status.flags.ENABLE, status.flags.READY, status.flags.ERROR, status.flags.OVERFLOW, uint32(status))

	h := Header{version: 1, kind: 7, length: 0x01020304}
	fmt.Printf("header version=%d kind=%d length=%#x\n", h.version, h.kind, h.length)

	var d Delta
	d.dx = -3
	d.dy = 12
	fmt.Printf("delta dx=%d dy=%d\n", d.dx, d.dy)
}
