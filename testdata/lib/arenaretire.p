// An arena that reuses one address more often than its address history has
// tags retires the exhausted granules and allocates elsewhere (abi.md
// section 1.3, Arena epochs; language spec section 18.11): a Chunked in
// a Mark/Release or Reset loop keeps going, pointers from before stay
// stale, and a Fixed whose only granule is exhausted fails its allocations
// instead of reissuing a tag. Without generation checks nothing is retired,
// so the checks below apply only to a checked build.
package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/rt"
	"unsafe"
)

type node struct{ v int }

const rounds = 40000

var checked bool
var wrong int

func fail(what string, i int) {
	wrong++
	println("wrong:", what, i)
}

func main() {
	heap := alloc.Default()
	s := alloc.Raw(32, 16)
	heap.Free(s, 32, 16)
	checked = !rt.GenRegisteredLive(s)

	// Mark/Release: every round allocates at the same place until its
	// granule is exhausted.
	a := arena.NewChunked()
	var old [4]unsafe.Pointer
	sum := 0
	for i := 0; i < rounds; i++ {
		m := a.Mark()
		p := a.New[node]()
		p.v = i
		sum += p.v
		if i%10000 == 0 {
			old[i/10000] = unsafe.Pointer(p)
		}
		a.Release(m)
	}
	for i, p := range old {
		if checked && rt.GenRegisteredLive(p) {
			fail("released pointer is live", i)
		}
	}
	a.Drop()

	// Reset.
	b := arena.NewChunked()
	for i := 0; i < rounds; i++ {
		p := b.New[node]()
		p.v = i
		sum += p.v
		b.Reset()
	}
	b.Drop()

	// A Fixed with room for allocations after the exhausted granule.
	f := arena.NewFixed(256)
	for i := 0; i < rounds; i++ {
		p := f.New[node]()
		p.v = i
		sum += p.v
		f.Reset()
	}
	f.Drop()

	// A Fixed of one granule: once it is exhausted, allocation fails.
	g := arena.NewFixed(16)
	failed := -1
	for i := 0; i < rounds && failed < 0; i++ {
		if g.Alloc(8, 8) == nil {
			failed = i
		}
		g.Reset()
	}
	if checked && (failed < 30000 || failed > 32767) {
		fail("one-granule Fixed failed at", failed)
	}
	if !checked && failed >= 0 {
		fail("unchecked Fixed failed at", failed)
	}
	g.Drop()
	println("sum", sum, "wrong", wrong)
}
