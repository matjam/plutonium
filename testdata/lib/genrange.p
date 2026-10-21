// The generation directory's range check (rt.GenRegisteredRangeLive) gives
// the same answer as its definition (ABI section 1.3): every granule an
// extent touches, and its last byte, checked on their own. The extents
// start at every offset of a granule and end anywhere up to several
// granules on, so they cross from live allocations into neighbours, freed
// slots, the end of a large allocation and arena allocations that a
// Release or Free took back. Without generation checks both answers are
// always true, so the output is the same in either mode.
package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/rt"
	"unsafe"
)

var probes, disagree int

// definition checks each granule of [p, p+n) and the last byte on its own.
func definition(p unsafe.Pointer, n uintptr) bool {
	a := uintptr(p)
	for off := uintptr(0); off < n; off = ((a + off) &^ 15) + 16 - a {
		if !rt.GenRegisteredLive(unsafe.Add(p, off)) {
			return false
		}
	}
	return rt.GenRegisteredLive(unsafe.Add(p, n-1))
}

// probe compares the range check of every extent of up to max bytes that
// starts in the first 32 bytes after p.
func probe(name string, p unsafe.Pointer, max uintptr) {
	for off := uintptr(0); off < 32; off++ {
		q := unsafe.Add(p, off)
		for n := uintptr(1); n <= max; n++ {
			probes++
			if rt.GenRegisteredRangeLive(q, n) != definition(q, n) {
				disagree++
				println("disagree:", name, off, n)
			}
		}
	}
}

func main() {
	heap := alloc.Default()

	// Neighbouring slots of one size class, then one of them freed and
	// its slot reused.
	a := alloc.Raw(48, 16)
	b := alloc.Raw(48, 16)
	c := alloc.Raw(48, 16)
	probe("slab", a, 200)
	heap.Free(b, 48, 16)
	probe("slab freed", a, 200)
	probe("slab stale", b, 120)
	d := alloc.Raw(48, 16)
	probe("slab reused", a, 200)

	// The tail of a large allocation and the memory past it.
	big := alloc.Raw(5000, 16)
	probe("large", big, 200)
	probe("large tail", unsafe.Add(big, 4960), 120)

	// Arena allocations are registrations of their own over a chunk the
	// arena took from the heap: the check also validates that backing.
	ar := arena.NewChunked(4096)
	x := ar.Alloc(40, 8)
	m := ar.Mark()
	y := ar.Alloc(24, 8)
	z := ar.Alloc(200, 8)
	probe("arena", x, 300)
	ar.Free(z, 200, 8) // the latest allocation gives its space back
	probe("arena free", x, 300)
	probe("arena free stale", z, 100)
	ar.Release(m)
	probe("arena release", x, 300)
	probe("arena release stale", y, 100)
	w := ar.Alloc(64, 8)
	probe("arena reused", x, 300)
	probe("arena reused stale", y, 100)
	_ = w
	ar.Drop()
	probe("arena dropped", x, 100)

	heap.Free(a, 48, 16)
	heap.Free(c, 48, 16)
	heap.Free(d, 48, 16)
	heap.Free(big, 5000, 16)
	println("probes", probes, "disagree", disagree)
}
