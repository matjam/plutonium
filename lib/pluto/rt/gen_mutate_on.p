//pu:build gencheck

package rt

import "unsafe"

// GenTagHeap takes the next generation in persistent raw-address history.
// An exhausted range is not published again, even after mapping/layout or
// allocator reuse. The caller quarantines the rejected allocation.
func GenTagHeap(token uintptr, p unsafe.Pointer, size uintptr) unsafe.Pointer {
	if token == 0 || size == 0 {
		return nil
	}
	genWriter.Lock()
	r := (*genRecord)(unsafe.Pointer(token))
	a, end := uintptr(p), uintptr(p)+size
	if !r.active || a < r.lo || end > r.hi || end < a {
		panic("invalid generation registration extent")
	}
	next := uint16(1)
	var page *genInterval
	for b := a &^ 15; b < end; b += 16 {
		if page == nil || page.interval != b>>16 {
			page = genPage(b>>16, true)
		}
		h := page.history[(b&65535)>>4]
		if h >= next {
			next = h + 1
		}
	}
	if next >= 0x8000 {
		genWriter.Unlock()
		return nil
	}
	page = nil
	for b := a &^ 15; b < end; b += 16 {
		if page == nil || page.interval != b>>16 {
			page = genPage(b>>16, false)
		}
		page.history[(b&65535)>>4] = next
		vstore16(genEntry(r, b), next)
	}
	genWriter.Unlock()
	return unsafe.Pointer(genWithTag(a, next))
}

// GenInvalidate clears reclaimed granules before their payload is reused.
// Rounding from upward preserves allocations preceding a partial mark.
func GenInvalidate(token uintptr, from, to uintptr) {
	r := (*genRecord)(unsafe.Pointer(token))
	if from < r.lo || to > r.hi || to < from {
		panic("invalid generation invalidation extent")
	}
	for b := (from + 15) &^ 15; b < to; b += 16 {
		vstore16(genEntry(r, b), 0)
	}
}

func genPointerRecord(p unsafe.Pointer) *genRecord {
	a := uintptr(p)
	bits := *(*uintptr)(unsafe.Pointer(&p))
	tag := genTagOf(bits)
	page := genPage(a>>16, false)
	if page == nil {
		return nil
	}
	var best *genRecord
	for l := uintptr(vload64(&page.head)); l != 0; {
		link := (*genLink)(unsafe.Pointer(l))
		if rp := uintptr(vload64(&link.reg)); rp != 0 {
			r := (*genRecord)(unsafe.Pointer(rp))
			if actual := genReadTag(r, a); actual != 0 && (best == nil || r.depth > best.depth) {
				if actual != 0 && (tag == 0 || actual == tag) && genLive(r, a, a+1, actual) {
					best = r
				}
			}
		}
		l = link.next
	}
	return best
}

// GenFreeHeap handles custom Alloc with a promoted builtin Free: remove
// its child layers first and restore the slab's original tag. Raw frees
// are unsafe but still invalidate the current allocation's metadata.
func GenFreeHeap(token uintptr, p unsafe.Pointer, size uintptr) bool {
	want := (*genRecord)(unsafe.Pointer(token))
	for {
		r := genPointerRecord(p)
		if r == nil {
			panic("alloc: Free of memory that was already freed")
		}
		if r == want {
			tag := vload16(genEntry(r, uintptr(p)))
			GenInvalidate(token, uintptr(p), uintptr(p)+size)
			return tag != 0x7fff
		}
		if r.parent == nil || r.key == 0 {
			panic("alloc: Free through a different allocator")
		}
		original := r.original
		GenUnregister(uintptr(unsafe.Pointer(r)))
		p = original
	}
}

// GenCustomAlloc and GenCustomFree are compiler-inferred allocator method
// wrappers. Custom byte-aligned siblings have distinct tables even when
// they share a history granule. The original return value is saved for a
// wrapper forwarding a Free to its parent.
func GenCustomAlloc(p unsafe.Pointer, size, key uintptr) unsafe.Pointer {
	if p == nil {
		return nil
	}
	if size == 0 {
		size = 1
	}
	token := GenRegister(unsafe.Pointer(uintptr(p)), size, p, key)
	result := GenTagHeap(token, p, size)
	if result == nil {
		// Keep the original allocation quarantined. Its persistent history
		// rejects this range if the custom allocator remaps it later too.
		GenQuarantine(token)
		return nil
	}
	return result
}

func GenCustomFree(p unsafe.Pointer, size, key uintptr) unsafe.Pointer {
	bits := *(*uintptr)(unsafe.Pointer(&p))
	if p == nil || genTagOf(bits) == 0 {
		return p
	}
	genWriter.Lock()
	defer genWriter.Unlock()
	return genCustomFreeLocked(p, size, key)
}

// Translation and invalidation use one writer snapshot; no allocator
// implementation is called while its lock is held.
func genCustomFreeLocked(p unsafe.Pointer, size, key uintptr) unsafe.Pointer {
	r := genPointerRecord(p)
	if r == nil || !GenRegisteredRangeLive(p, size) {
		panic("alloc: Free of memory that was already freed")
	}
	if r.key != 0 && uintptr(p) != r.lo {
		panic("alloc: Free of an interior pointer")
	}
	if key == 0 {
		// A promoted Alloc owns no layer of this Free receiver. Validate
		// before its body, but preserve the parent's registration until
		// forwarding has completed.
		return p
	}
	if r.key != key {
		found := false
		for parent := r.parent; parent != nil; parent = parent.parent {
			if parent.key == key {
				found = true
				break
			}
		}
		if !found {
			return p
		}
		// An Alloc wrapper can accompany a promoted custom Free. Restore
		// each saved pointer until that Free reaches its own allocation.
		for r.key != key {
			if r.key == 0 {
				panic("alloc: Free through a different allocator")
			}
			original := r.original
			genUnregisterLocked(r)
			p = original
			r = genPointerRecord(p)
			if r == nil {
				panic("alloc: Free of memory that was already freed")
			}
		}
	}
	if uintptr(p) != r.lo {
		panic("alloc: Free of an interior pointer")
	}
	original := r.original
	genUnregisterLocked(r)
	return original
}

// Forwarding must complete before invalidating a promoted Alloc's
// registration, so the parent's Free does not observe a double free.
func GenAfterCustomFree(p unsafe.Pointer, size uintptr) {
	if p == nil {
		return
	}
	if r := genPointerRecord(p); r != nil {
		if r.key != 0 && uintptr(p) != r.lo || max(size, uintptr(1)) > r.hi-uintptr(p) {
			panic("alloc: Free of an invalid allocation extent")
		}
		// This is a complete allocation, including a byte-aligned first
		// granule. Adjacent custom siblings have independent records.
		if r.key != 0 {
			GenUnregister(uintptr(unsafe.Pointer(r)))
		} else {
			for b := uintptr(p) &^ 15; b < uintptr(p)+max(size, uintptr(1)); b += 16 {
				vstore16(genEntry(r, b), 0)
			}
		}
	}
}
