package main

import (
	"pluto/arch/x86"
	"unsafe"
)

var _ unsafe.Pointer
var _ x86.InterruptFrame

func h(f *x86.InterruptFrame) {}

func main() { _ = x86.TrapGate(h, 0x08, 0) }
