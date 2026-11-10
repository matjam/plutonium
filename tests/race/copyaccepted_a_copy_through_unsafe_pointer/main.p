package main

import (
	"pluto/sync"
	"unsafe"
)

type Counter struct {
	mu sync.Mutex
	n  int
}

var total Counter

func main() {
	// Memory reached through unsafe.Pointer is the programmer's
	// responsibility (section 19.3, What is not checked).
	c := *(*Counter)(unsafe.Pointer(&total))
	println(c.n)
}
