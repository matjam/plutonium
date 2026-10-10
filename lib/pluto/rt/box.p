package rt

import "unsafe"

// ----------------------------------------------------------------------------
// Boxes: owned[T], shared[T], synced[T] and weak[P] (language spec section
// 5.6, abi.md section 1.2). Each is the address of a heap object. In front of
// the object is a header recording the allocator it came from, so that it is
// freed to that allocator whatever the default is by then:
//
//	owned[T]:          {a Allocator}                      16 bytes
//	shared, synced:    {strong, weak int64; a Allocator}  32 bytes
//
// The allocation starts at the object's address minus the header size
// rounded up to the object's alignment (at least 8). The strong count counts
// shared or synced references; the weak count counts weak references, plus
// one held by all the strong references together, so the memory is freed
// when the last weak reference goes after the object has been dropped.
//
// The compiler emits the calls below: boxNew and boxNewWith for new and
// alloc's constructors, boxRetain and boxReleaseStrong as references are
// copied and dropped (it drops the object itself when boxReleaseStrong
// reports the last reference, then calls weakRelease), and boxFree for
// owned[T]. sync says the reference is a synced one, whose counts are
// atomic once the program is threaded (SetThreaded, as for strings).

const (
	ownedHeader   = 16
	countedHeader = 32
)

// boxHead is the header of a shared or synced object.
type boxHead struct {
	strong int64
	weak   int64
	a      Allocator
}

// boxOffset returns the distance from the start of the allocation to the
// object, and the allocation's alignment.
//
//pu:inline
func boxOffset(hdr, align uintptr) (uintptr, uintptr) {
	if align < 8 {
		align = 8
	}
	return (hdr + align - 1) &^ (align - 1), align
}

// boxNew allocates a zeroed object of size bytes with a header of hdr bytes
// from the running task's current allocator.
func boxNew(hdr, size, align uintptr) unsafe.Pointer {
	a := DefaultAllocator()
	if a == nil {
		panicMessage("no allocator: call alloc.SetDefault before allocating", "", "", "", "")
	}
	off, al := boxOffset(hdr, align)
	return boxInit(allocAt(a, off+size, al, CallerPC()), a, hdr, off)
}

// boxNewWith is boxNew with an explicit allocator.
func boxNewWith(a Allocator, hdr, size, align uintptr) unsafe.Pointer {
	if a == nil {
		panicMessage("nil allocator passed to new or make", "", "", "", "")
	}
	off, al := boxOffset(hdr, align)
	return boxInit(allocAt(a, off+size, al, CallerPC()), a, hdr, off)
}

// NewBox allocates a zeroed object of size bytes for an owned box (counted
// false) or a shared or synced one (counted true) from a, and returns its
// address. It is how pluto/alloc's Owned, Shared and Synced are written.
func NewBox(a Allocator, counted bool, size, align uintptr) unsafe.Pointer {
	if a == nil {
		panicMessage("no allocator: call alloc.SetDefault before allocating", "", "", "", "")
	}
	hdr := uintptr(ownedHeader)
	if counted {
		hdr = countedHeader
	}
	off, al := boxOffset(hdr, align)
	return boxInit(allocAt(a, off+size, al, CallerPC()), a, hdr, off)
}

// boxInit fills in the header of the allocation p, whose object is off
// bytes in, and returns the object's address.
func boxInit(p unsafe.Pointer, a Allocator, hdr, off uintptr) unsafe.Pointer {
	obj := unsafe.Add(p, off)
	// The header records its allocator to free the object to. A box from
	// a local arena must not outlive the arena, which the escape analysis
	// checks where the arena is installed (language spec section 7.1), so
	// a is hidden from it here.
	*(*Allocator)(unsafe.Add(obj, -16)) = *Pinned(&a)
	if hdr == countedHeader {
		h := (*boxHead)(unsafe.Add(obj, -countedHeader))
		h.strong = 1
		h.weak = 1
	}
	return obj
}

// boxFree returns the memory of a box whose object has been dropped to the
// allocator in its header.
func boxFree(obj unsafe.Pointer, hdr, size, align uintptr) {
	off, align := boxOffset(hdr, align)
	a := *(*Allocator)(unsafe.Add(obj, -16))
	a.Free(unsafe.Add(obj, -int(off)), off+size, align)
}

// countOf returns the address of a counted object's strong (weak false) or
// weak count.
//
//pu:inline
func countOf(obj unsafe.Pointer, weak bool) *uint64 {
	if weak {
		return (*uint64)(unsafe.Add(obj, -24))
	}
	return (*uint64)(unsafe.Add(obj, -32))
}

// addBoxCount adds delta to a count and returns the new value.
//
//pu:inline
func addBoxCount(c *uint64, delta int64, sync bool) int64 {
	if sync && atomicCounts {
		return int64(xadd64(c, delta))
	}
	*c = uint64(int64(*c) + delta)
	return int64(*c)
}

// boxRetain takes a strong reference (weak false) or a weak one to obj, if
// it is not nil.
//
//pu:inline
func boxRetain(obj unsafe.Pointer, weak, sync bool) {
	if obj != nil {
		addBoxCount(countOf(obj, weak), 1, sync)
	}
}

// boxReleaseStrong drops a strong reference and reports whether it was the
// last, in which case the caller drops the object and then calls
// weakRelease for the weak reference the strong ones held.
//
//pu:inline
func boxReleaseStrong(obj unsafe.Pointer, sync bool) bool {
	if obj == nil {
		return false
	}
	n := addBoxCount(countOf(obj, false), -1, sync)
	if n < 0 {
		panicMessage("runtime error: reference count below zero (a shared or synced reference was released twice)", "", "", "", "")
	}
	return n == 0
}

// weakRelease drops a weak reference to obj, a shared or synced object of
// size bytes, and frees its memory if it was the last.
func weakRelease(obj unsafe.Pointer, sync bool, size, align uintptr) {
	if obj == nil {
		return
	}
	n := addBoxCount(countOf(obj, true), -1, sync)
	if n > 0 {
		return
	}
	if n < 0 {
		panicMessage("runtime error: weak reference count below zero", "", "", "", "")
	}
	boxFree(obj, countedHeader, size, align)
}

// boxUpgrade takes a strong reference to obj if it still has one, and
// reports whether it did (weak[P].Upgrade). Once the strong count has
// reached zero it never rises again: the object's Drop has started.
func boxUpgrade(obj unsafe.Pointer, sync bool) bool {
	if obj == nil {
		return false
	}
	c := countOf(obj, false)
	if sync && atomicCounts {
		for {
			n := load64(c)
			if n == 0 {
				return false
			}
			if cas64(c, n, n+1) {
				return true
			}
		}
	}
	if *c == 0 {
		return false
	}
	*c++
	return true
}

// BoxCounts returns the strong and weak counts of a shared or synced
// object, for tests and debugging.
func BoxCounts(obj unsafe.Pointer) (strong, weak int) {
	return int(*countOf(obj, false)), int(*countOf(obj, true))
}
