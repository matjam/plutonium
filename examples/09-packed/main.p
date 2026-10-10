package main

import "pluto/fmt"

// Test of packed types with bitfield access, endianness, and flag groups.
//
// Build: pluto build -o packed_test ./examples/09-packed/
// Run:   ./packed_test
// Expected output:
//   mode=5
//   enable=1
//   priority=7
//   mode=3
//   enable=0
//   priority=2
//   mode=1
//   priority=5
//   net_version=2
//   net_flags=9
//   ENABLE=1
//   READY=0
//   ERROR=1
//   OVERFLOW=0
//   ENABLE=0
//   ERROR=0

type ControlReg packed uint32 {
	mode     bits[uint, 4]
	enable   bits[bool, 1]
	priority bits[uint, 3]
}

// Big-endian packed type — byte-swap is emitted on read/write
type NetHeader packed be uint32 {
	version  bits[uint, 4]
	flags    bits[uint, 8]
	reserved bits[uint, 20]
}

// Named flag group — bits { ... } sugar for multiple 1-bit bools
type StatusReg packed uint32 {
	flags bits {
		ENABLE
		READY
		ERROR
		OVERFLOW
	}
	mode bits[uint, 4]
}

func testControlReg() {
	var reg ControlReg
	reg.mode = 5
	reg.enable = true
	reg.priority = 7

	fmt.Printf("mode=%d\n", reg.mode)
	fmt.Printf("enable=%d\n", reg.enable)
	fmt.Printf("priority=%d\n", reg.priority)

	// Overwrite fields — verify masking and non-interference
	reg.mode = 3
	reg.enable = false
	reg.priority = 2

	fmt.Printf("mode=%d\n", reg.mode)
	fmt.Printf("enable=%d\n", reg.enable)
	fmt.Printf("priority=%d\n", reg.priority)

	// Test overflow wrapping: 17 in 4 bits = 17 & 0xF = 1
	reg.mode = 17
	// Test overflow: 13 in 3 bits = 13 & 0x7 = 5
	reg.priority = 13

	fmt.Printf("mode=%d\n", reg.mode)
	fmt.Printf("priority=%d\n", reg.priority)
}

func testNetHeader() {
	var net NetHeader
	net.version = 2
	net.flags = 9

	fmt.Printf("net_version=%d\n", net.version)
	fmt.Printf("net_flags=%d\n", net.flags)
}

func testStatusReg() {
	var status StatusReg
	status.flags.ENABLE = true
	status.flags.ERROR = true

	fmt.Printf("ENABLE=%d\n", status.flags.ENABLE)
	fmt.Printf("READY=%d\n", status.flags.READY)
	fmt.Printf("ERROR=%d\n", status.flags.ERROR)
	fmt.Printf("OVERFLOW=%d\n", status.flags.OVERFLOW)

	// Clear flags
	status.flags.ENABLE = false
	status.flags.ERROR = false

	fmt.Printf("ENABLE=%d\n", status.flags.ENABLE)
	fmt.Printf("ERROR=%d\n", status.flags.ERROR)
}

func main() int64 {
	testControlReg()
	testNetHeader()
	testStatusReg()
	return 0
}
