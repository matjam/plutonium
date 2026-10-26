package main

import (
	"pluto/arch/x86"
	"unsafe"
)

var _ unsafe.Pointer
var _ x86.InterruptFrame

func main() { defer x86.TrapGate(nil, 0x08, 0) }
