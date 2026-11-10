package main

// rt.ReleaseValues(p, n) runs the drop glue of the n values at p, each
// Drop method with the element's address (language spec sections 18.3 and
// 18.6). A Drop that keeps its receiver keeps the address of frame memory
// here, which is E3003 at the call.

import (
	"pluto/rt"
	"unsafe"
)

type O owner struct{ n int }

var keep *O

func (o *O) Drop() { keep = o }

//pu:noinline
func f() {
	var buf [2]int // no drop glue of its own
	p := (*O)(unsafe.Pointer(&buf[0]))
	rt.ReleaseValues(p, 1)
}

func main() { f() }
