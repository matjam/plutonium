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

// arenaRecordEnded runs under genWriter. Reclaim ends allocations newest
// first, so a run of ended allocations usually arrives as extents that each
// end where the newest record begins. When the newest record has the same
// tag and site and the new extent reaches its first granule, the record is
// extended down instead of adding one: the bytes in between lie in granules
// that carry this tag from this incarnation, so the lookup stays exact.
// Without this a long-running program keeps one record per ended
// allocation forever.
func arenaRecordEnded(lo, hi uintptr, tag uint16, pc uintptr) {
	site := pc
	if h := (*arenaEnded)(unsafe.Pointer(uintptr(vload64(&arenaEndedHead)))); h != nil &&
		h.tag == tag && h.site == site && lo < h.lo && hi <= h.lo && (hi+15)&^15 >= h.lo&^15 {
		store64((*uint64)(unsafe.Pointer(&h.lo)), uint64(lo))
		return
	}
	e := (*arenaEnded)(genMetadata(unsafe.Sizeof(arenaEnded{})))
	e.lo, e.hi, e.tag, e.site = lo, hi, tag, site
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
	return unsafe.Pointer(genWithTag(a, tag))
}

// GenArenaExhausted returns, for an extent whose GenTagArena failed, the
// address just past the last of its granules whose address history is
// exhausted (no further tag is available there), or 0 if none is. An
// arena retires that storage by never allocating it again: it allocates
// after it, or in other storage (abi.md section 1.3, Arena epochs).
func GenArenaExhausted(p unsafe.Pointer, size uintptr) uintptr {
	a, end := uintptr(p), uintptr(p)+max(size, uintptr(1))
	retired := uintptr(0)
	genWriter.Lock()
	var page *genInterval
	for b := a &^ 15; b < end; b += 16 {
		if page == nil || page.interval != b>>16 {
			page = genPage(b>>16, false)
		}
		if page != nil && page.history[(b&65535)>>4] >= 0x7fff {
			retired = b + 16
		}
	}
	genWriter.Unlock()
	return retired
}

// GenEndArena ends exactly one allocation before reuse or backing free.
func GenEndArena(token uintptr, pc uintptr) {
	if token == 0 {
		return
	}
	genWriter.Lock()
	r := (*genRecord)(unsafe.Pointer(token))
	if r.active {
		tag := vload16(genEntry(r, r.lo))
		if tag >= 0x8000 {
			arenaRecordEnded(r.lo, r.hi, tag, pc)
		}
		genUnregisterLocked(r)
	}
	genWriter.Unlock()
}

func arenaEndedSite(p unsafe.Pointer) uintptr {
	a := uintptr(p)
	tag := genTagOf(*(*uintptr)(unsafe.Pointer(&p)))
	for e := (*arenaEnded)(unsafe.Pointer(uintptr(vload64(&arenaEndedHead)))); e != nil; e = e.next {
		if a >= e.lo && a < e.hi && tag == e.tag {
			return e.site
		}
	}
	return 0
}
