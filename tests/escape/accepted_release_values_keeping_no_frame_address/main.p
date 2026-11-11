package main

// The twin of tests/escape/release_values_drops_frame_elements:
// rt.ReleaseValues on frame memory whose elements' Drop keeps nothing, and
// on static memory whose elements' Drop keeps a pointer it loads from the
// owner (language spec sections 18.3 and 18.6). A Drop that keeps its
// receiver itself is E3002 at the Drop, wherever the owner lives
// (tests/escape/rejected_drop_keeps_its_receiver). ZeroValues drops
// nothing.

import (
	"pluto/rt"
	"unsafe"
)

type P owner struct{ n int }

func (p *P) Drop() { p.n = 0 }

type O owner struct{ p *int }

var keep *int

func (o *O) Drop() { keep = o.p }

//pu:noinline
func frame() {
	var buf [2]int
	p := (*P)(unsafe.Pointer(&buf[0]))
	rt.ReleaseValues(p, 1)
	q := (*O)(unsafe.Pointer(&buf[1]))
	rt.ZeroValues(q, 1)
}

var heap [2]int

//pu:noinline
func static() {
	p := (*O)(unsafe.Pointer(&heap[0]))
	rt.ReleaseValues(p, 1)
}

func main() {
	frame()
	static()
	println(keep != nil)
}
