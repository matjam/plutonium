package main

import (
	"pluto/arch/x86"
	"unsafe"
)

var _ unsafe.Pointer
var _ x86.InterruptFrame

//pu:interrupt
func h(f unsafe.Pointer, code uint32) {}

func main() {}
