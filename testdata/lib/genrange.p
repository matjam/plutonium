// The generation directory's range check (rt.GenRegisteredRangeLive) gives
// the same answer as its definition (ABI section 1.3): every granule an
// extent touches, and its last byte, checked on their own. The extents
// start at every offset of a granule and end anywhere up to several
// granules on, so they cross from live allocations into neighbours, freed
// slots, the end of a large allocation and arena allocations that a
// Release took back. Without generation checks both answers are
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
	// ar is a package variable: a local arena's allocations are views of it
	// (spec 18.3), and the probe of x after Drop would not compile.
	ar = arena.NewChunked(4096)
	x := view(5)
	m := ar.Mark()
	y := view(3)
	z := ar.Alloc(200, 8)
	probe("arena", x, 300)
	ar.Free(z, 200, 8) // takes nothing back (language spec section 18.9)
	probe("arena free", x, 300)
	probe("arena free stale", z, 100)
	ar.Release(m)
	probe("arena release", x, 300)
	probe("arena release stale", y, 100)
	w := view(8)
	probe("arena reused", x, 300)
	probe("arena reused stale", y, 100)
	_ = w
	dead := take(&ar)
	dead.Drop()
	probe("arena dropped", x, 100)

	heap.Free(a, 48, 16)
	heap.Free(c, 48, 16)
	heap.Free(d, 48, 16)
	heap.Free(big, 5000, 16)
	println("probes", probes, "disagree", disagree)
	oracle()
}

var checked bool

var ar arena.Chunked

// view allocates n words in ar as a plain view, which the arena does not
// count, so a Release or Drop may take it back while the program still
// holds its address (language spec section 18.9, Live allocations).
func view(n int) unsafe.Pointer { return unsafe.Pointer(unsafe.SliceData(ar.Slice[uint64](n))) }

var wrong int

// expect checks, in a checked build, that the n bytes at p are live or
// not, both through the range check and byte by byte.
func expect(name string, i int, p unsafe.Pointer, n uintptr, live bool) {
	if !checked {
		return
	}
	if rt.GenRegisteredRangeLive(p, n) != live {
		wrong++
		println("wrong:", name, i, "range live", !live)
	}
	for off := uintptr(0); off < n; off++ {
		if rt.GenRegisteredLive(unsafe.Add(p, off)) != live {
			wrong++
			println("wrong:", name, i, "byte", off, "live", !live)
			return
		}
	}
}

// oracle checks liveness the program knows: many allocations in one arena
// chunk, so that their interval of the directory keeps a registration for
// each, across a Release, reuse, a partial Free of the latest allocation
// (which takes nothing back) and Drop; and a freed heap slot.
func oracle() {
	heap := alloc.Default()
	s := alloc.Raw(32, 16)
	heap.Free(s, 32, 16)
	checked = !rt.GenRegisteredLive(s)

	const n = 300
	ar = arena.NewChunked() // the package variable, as in main
	var first, second [n]unsafe.Pointer
	for i := range first[:n/2] {
		first[i] = view(2)
	}
	m := ar.Mark()
	for i := n / 2; i < n; i++ {
		first[i] = view(2)
	}
	for i := range first {
		expect("allocated", i, first[i], 16, true)
	}
	ar.Release(m)
	for i := range first {
		expect("released", i, first[i], 16, i < n/2)
	}
	for i := n / 2; i < n; i++ {
		second[i] = view(2)
	}
	for i := n / 2; i < n; i++ {
		expect("stale", i, first[i], 16, false)
		expect("reused", i, second[i], 16, true)
	}
	t := ar.Alloc(40, 8)
	ar.Free(unsafe.Add(t, 24), 16, 8) // takes nothing back
	expect("kept", 0, t, 40, true)
	if checked && !rt.GenRegisteredRangeLive(unsafe.Add(t, 16), 16) {
		wrong++
		println("wrong: an extent across the freed tail is not live")
	}
	dead := take(&ar)
	dead.Drop()
	for i := range first {
		expect("dropped", i, first[i], 16, false)
	}
	expect("dropped", 0, t, 40, false)
	println("wrong", wrong)
}
