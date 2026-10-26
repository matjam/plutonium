package main

import (
	"pluto/arch/x86"
	"unsafe"
)

var _ unsafe.Pointer
var _ x86.InterruptFrame

func main() { _ = x86.InterruptGate(3, 0x08, 0) }
