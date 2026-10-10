package main

import (
	"pluto/alloc"
	"pluto/alloc/debug"
	"pluto/fmt"
	"pluto/hash"
	"pluto/strings"
	"pluto/vec"
	"unsafe"
)

type pair struct {
	a, b int64
}

// zeroed reports whether n bytes at p are all zero.
func zeroed(p unsafe.Pointer, n uintptr) bool {
	for i := uintptr(0); i < n; i++ {
		if *(*byte)(unsafe.Add(p, i)) != 0 {
			return false
		}
	}
	return true
}

func stats(d *debug.Allocator, what string) {
	s := d.Stats()
	fmt.Printf("%s: live %d (%d bytes), quarantined %d, guarded %d, allocs %d\n",
		what, s.Live, int(s.LiveBytes), s.Quarantined, s.Guarded, int(s.Allocs))
}

// exercise allocates, writes and frees blocks of several sizes and
// alignments from d, checking each comes back zeroed and aligned.
func exercise(d *debug.Allocator) {
	sizes := [...]uintptr{1, 7, 16, 24, 100, 4095, 4096, 4097, 10000}
	aligns := [...]uintptr{1, 8, 16, 64, 4096, 8192}
	ok := true
	for _, size := range sizes {
		for _, align := range aligns {
			p := d.Alloc(size, align)
			if p == nil || uintptr(p)%align != 0 || !zeroed(p, size) {
				fmt.Printf("bad block: size %d align %d\n", int(size), int(align))
				ok = false
				continue
			}
			for i := uintptr(0); i < size; i++ {
				*(*byte)(unsafe.Add(p, i)) = byte(i)
			}
			d.Free(p, size, align)
		}
	}
	fmt.Printf("sizes and alignments ok: %v\n", ok)
	// A zero-sized allocation is one byte, freed with size 0 or 1.
	p := d.Alloc(0, 1)
	d.Free(p, 0, 1)
	d.Free(nil, 8, 8)
}

func main() {
	// Page guards after allocations, with a small quarantine.
	o := debug.DefaultOptions()
	o.Quarantine = 64 << 10
	o.Leaks = debug.LeaksFail
	d := debug.New(o)
	exercise(d)
	stats(d, "guarded")

	// Reuse after quarantine: memory comes back zeroed.
	for i := 0; i < 200; i++ {
		p := d.Alloc(64, 8)
		if !zeroed(p, 64) {
			fmt.Printf("reused block not zeroed\n")
		}
		*(*int64)(p) = -1
		d.Free(p, 64, 8)
	}
	stats(d, "after reuse")

	// Canaries only, guard pages before allocations, no quarantine.
	o2 := debug.DefaultOptions()
	o2.CanaryBelow = 256
	o2.Underflow = true
	o2.Quarantine = 0
	d2 := debug.New(o2)
	exercise(d2)
	stats(d2, "canary")

	// The typed functions and library containers on a debug allocator.
	restore := alloc.Use(d)
	x := alloc.New[pair]()
	x.a, x.b = 3, 4
	v := vec.New[int](d)
	for i := 0; i < 1000; i++ {
		v.Push(i)
	}
	m := hash.NewMap[int, string](0, d)
	for i := 0; i < 100; i++ {
		m.Set(i, "x")
	}
	var b strings.Builder
	b.Init(d)
	b.Grow(4)
	for i := 0; i < 50; i++ {
		b.WriteString("ab")
	}
	s := make([]int64, 10)
	s[9] = 9
	stats(d, "in use")
	fmt.Printf("%d %d %d %d %d\n", x.a+x.b, v.Len(), m.Len(), len(b.String()), s[9])
	alloc.Free(x)
	v.Free()
	m.Free()
	b.Free()
	alloc.Default().Free(unsafe.Pointer(unsafe.SliceData(s)), 10*8, 8)
	fmt.Printf("alloc.ReportLeaks with the debug allocator: %d\n", alloc.ReportLeaks())
	restore.Restore()
	fmt.Printf("alloc.ReportLeaks with the slab allocator: %d\n", alloc.ReportLeaks())
	stats(d, "freed")

	d.CheckHeap()
	d2.CheckHeap()
	fmt.Printf("leaks: %d %d\n", d.ReportLeaks(), d2.ReportLeaks())
}
