package main

import (
	"pluto/arena"
	"unsafe"
)

// Memory a local arena hands out is a view of the arena (spec 18.3), so it
// can not be used after the arena is dropped or moved.

func takeArena(a arena.Chunked) {}

func main() {
	a1 := arena.NewChunked(4096)
	p1 := (*int)(a1.Alloc(8, 8))
	a1.Drop()
	_ = *p1

	a2 := arena.NewChunked(4096)
	p2 := a2.Alloc(16, 8)
	takeArena(a2)
	_ = p2

	// Fine: the address as a uintptr is not a view.
	a3 := arena.NewChunked(4096)
	addr := uintptr(a3.Alloc(8, 8))
	a3.Drop()
	_ = unsafe.Pointer(addr)
}
