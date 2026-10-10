package main

import (
	"pluto/fmt"
	"unsafe"
)

// volatile[T] for device registers, aligned[T, N] for layout control, and
// the unsafe package for raw memory.

// A fake device: in a kernel this would sit at a fixed physical address.
type UART struct {
	data   volatile[uint8]
	status volatile[uint8]
}

var pageTable aligned[[512]uint64, 4096]

func send(u *UART, s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		for u.status&0x20 == 0 {
		}
		u.data = s[i]
		n++
	}
	return n
}

func main() {
	var dev UART
	dev.status = 0x20 // transmitter ready
	fmt.Printf("sent=%d last=%c\n", send(&dev, "hi!"), dev.data)

	// A pointer to a volatile location, as a kernel builds for MMIO.
	reg := (*volatile[uint32])(unsafe.Pointer(&pageTable[0]))
	*reg = 0xCAFE
	fmt.Printf("reg=%#x\n", *reg)

	fmt.Printf("page table aligned: %t\n", uintptr(unsafe.Pointer(&pageTable))%4096 == 0)
	fmt.Printf("sizeof(UART)=%d sizeof(pageTable)=%d\n", unsafe.Sizeof(dev), unsafe.Sizeof(pageTable))

	var val int64 = 99
	p := unsafe.Pointer(&val)
	back := (*int64)(p)
	fmt.Printf("roundtrip=%d\n", *back)
}
