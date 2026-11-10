package main

// The twin of tests/escape/release_values_drops_frame_elements:
// rt.ReleaseValues on frame memory whose elements' Drop keeps nothing, and
// on static memory, which may be kept anywhere (language spec sections
// 18.3 and 18.6). ZeroValues drops nothing.

import (
	"pluto/rt"
	"unsafe"
)

type P owner struct{ n int }

func (p *P) Drop() { p.n = 0 }

type O owner struct{ n int }

var keep *O

func (o *O) Drop() { keep = o }

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
