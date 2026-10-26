package main

import (
	"pluto/arch/x86"
	"unsafe"
)

var _ unsafe.Pointer
var _ x86.InterruptFrame

//pu:interrupt
func h(f *x86.InterruptFrame) {}

func main() { g := h; _ = x86.InterruptGate(g, 0x08, 0) }
