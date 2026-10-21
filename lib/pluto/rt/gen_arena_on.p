//pu:build gencheck

package rt

import "unsafe"

// Reclaim diagnoses are immutable after publication. Lookup includes the
// raw address and incarnation; different allocations can share a numeric tag.
type arenaEnded struct {
	lo, hi uintptr
	tag    uint16
	site   uintptr
	next   *arenaEnded
}

var arenaEndedHead uint64

func arenaRecordEnded(lo, hi uintptr, tag uint16, pc uintptr, freed bool) {
	e := (*arenaEnded)(genMetadata(unsafe.Sizeof(arenaEnded{})))
	e.lo, e.hi, e.tag, e.site = lo, hi, tag, pc
	if freed {
		e.site |= uintptr(1) << 63
	}
	e.next = (*arenaEnded)(unsafe.Pointer(uintptr(vload64(&arenaEndedHead))))
	store64(&arenaEndedHead, uint64(uintptr(unsafe.Pointer(e))))
}

// GenCheckArenaFree validates the supplied incarnation before Free compares
// physical addresses or changes a cursor. Stale cleanup must not free reuse.
func GenCheckArenaFree(p unsafe.Pointer, size uintptr) {
	if p != nil && !GenRegisteredRangeLive(p, size) {
		panicStale(p)
	}
}

// GenTagArena uses persistent raw-address history, with the arena namespace.
// It never issues a tag that can collide after reset or backing reuse.
func GenTagArena(token uintptr, p unsafe.Pointer, size uintptr) unsafe.Pointer {
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
	tag := next | uint16(0x8000)
	page = nil
	for b := a &^ 15; b < end; b += 16 {
		if page == nil || page.interval != b>>16 {
			page = genPage(b>>16, false)
		}
		page.history[(b&65535)>>4] = next
		vstore16(genEntry(r, b), tag)
	}
	genWriter.Unlock()
	return unsafe.Pointer(a | uintptr(tag)<<48)
}

// GenEndArena ends exactly one allocation before reuse or backing free.
func GenEndArena(token uintptr, pc uintptr, freed bool) {
	if token == 0 {
		return
	}
	genWriter.Lock()
	r := (*genRecord)(unsafe.Pointer(token))
	if r.active {
		tag := vload16(genEntry(r, r.lo))
		if tag >= 0x8000 {
			arenaRecordEnded(r.lo, r.hi, tag, pc, freed)
		}
		genUnregisterLocked(r)
	}
	genWriter.Unlock()
}

// GenTrimArena reclaims a suffix without changing the prefix's saved tag.
// Versioned bounds also reject accesses crossing within the final granule.
func GenTrimArena(token uintptr, from, pc uintptr) {
	genWriter.Lock()
	r := (*genRecord)(unsafe.Pointer(token))
	if !r.active || from <= r.lo || from >= r.hi {
		panic("invalid generation trim extent")
	}
	oldHi := r.hi
	arenaRecordEnded(from, oldHi, vload16(genEntry(r, r.lo)), pc, true)
	v := vload64(&r.version)
	store64(&r.version, v+1)
	store64((*uint64)(unsafe.Pointer(&r.hi)), uint64(from))
	store64(&r.version, v+2)
	// Unlink pages wholly in the suffix. The retained boundary page is
	// removed by the ordinary unregister of the remaining prefix.
	for i := (from + 65535) >> 16; i <= (oldHi-1)>>16; i++ {
		page := genPage(i, false)
		for l := uintptr(vload64(&page.head)); l != 0; {
			link := (*genLink)(unsafe.Pointer(l))
			if uintptr(vload64(&link.reg)) == token {
				store64(&link.reg, 0)
			}
			l = link.next
		}
	}
	genWriter.Unlock()
}

func arenaEndedSite(p unsafe.Pointer) uintptr {
	a := uintptr(p)
	tag := uint16(*(*uintptr)(unsafe.Pointer(&p)) >> 48)
	for e := (*arenaEnded)(unsafe.Pointer(uintptr(vload64(&arenaEndedHead)))); e != nil; e = e.next {
		if a >= e.lo && a < e.hi && tag == e.tag {
			return e.site
		}
	}
	return 0
}
