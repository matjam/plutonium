package main

import (
	"pluto/arch/x86"
	"unsafe"
)

var _ unsafe.Pointer
var _ x86.InterruptFrame

type T struct{}

//pu:interrupt
func (T) h(f *x86.InterruptFrame) {}

func main() {}
