//pu:build gencheck

package rt

import "unsafe"

// Generation metadata has process lifetime. Payload may be unmapped, but
// readers never dereference it to find a table (ABI section 1.3).
// Interval/link topology is immutable after publication; only link.reg
// changes. Record reuse is published under a version, and each backing
// reference also saves the record's incarnation.
//
// An interval with many registrations (an arena chunk holds one for each
// of its allocations) also has hints: for each granule, the registration
// most recently published over it, which a lookup tries before walking
// the links. A hint is only a candidate. The lookup validates it exactly
// as a registration reached through the links (genLive), so a stale hint
// costs a walk and never makes memory live; it holds no liveness.
type genInterval struct {
	interval uintptr
	head     uint64
	history  *[4096]uint16
	hints    uint64 // 0, or a *[4096]uint64 of registrations by granule
	// Writer state (genWriter): the number of links, the links whose
	// registration was removed, and where genPrune continues.
	links uintptr
	free  *genLink
	sweep *genLink
}

type genLink struct {
	next     uintptr
	reg      uint64
	nextFree *genLink
}

// genHintLinks is the number of links past which an interval keeps hints.
const genHintLinks = 8

// The fields a lookup reads come first, within 64 bytes.
type genRecord struct {
	version     uint64
	active      bool
	lo, hi      uintptr
	origin      uintptr
	table       *uint16
	capacity    uintptr
	parent      *genRecord
	incarnation uint64
	parentID    uint64
	parentTag   uint16
	depth       uintptr
	original    unsafe.Pointer
	key         uintptr
	owner       uintptr
	ownerNext   *genRecord
	nextFree    uintptr
	nextClass   uintptr
	link0       *genLink // the link in the interval of lo, while published
}

// genTop is the directory's first level, indexed by bits 47..32 of an
// untagged address. Each entry is 0 or a leaf of genLeafSize interval
// pointers indexed by bits 31..16. Leaves and intervals are published
// once, fully initialized, and never freed or replaced, so a lookup is two
// dependent loads whatever the size of the heap.
var genTop [genLeafSize]uint64

const genLeafSize = 1 << 16

var genWriter Lock
var genPool, genPoolEnd uintptr
var genFreeRecords [32]uintptr
var genRecords [32]uintptr
var genSweep [32]uintptr

type genQuarantine struct {
	next        *genQuarantine
	original    unsafe.Pointer
	lo, hi, key uintptr
}

var genQuarantined *genQuarantine

// GenQuarantine retains the original rejected allocation, but releases
// its shadow table for reuse. Repeated custom returns of the same retired
// extent need only one retained bookkeeping record.
func GenQuarantine(token uintptr) {
	if token == 0 {
		panic("cannot register generation metadata")
	}
	genWriter.Lock()
	r := (*genRecord)(unsafe.Pointer(token))
	found := false
	for q := genQuarantined; q != nil; q = q.next {
		if q.original == r.original && q.key == r.key {
			q.lo, q.hi = min(q.lo, r.lo), max(q.hi, r.hi)
			found = true
			break
		}
	}
	if !found {
		q := (*genQuarantine)(genMetadata(unsafe.Sizeof(genQuarantine{})))
		q.original, q.lo, q.hi, q.key = r.original, r.lo, r.hi, r.key
		q.next, genQuarantined = genQuarantined, q
	}
	genWriter.Unlock()
	GenUnregister(token)
}

// genMetadata bypasses all program allocators, including their locks and
// registration hooks. Storage is pooled and is never unmapped.
func genMetadata(n uintptr) unsafe.Pointer {
	n = (n + 15) &^ 15
	if n > 65536 {
		p := Mmap(n)
		if p == nil {
			panic("out of memory for generation metadata")
		}
		return p
	}
	if genPoolEnd-genPool < n {
		p := Mmap(65536)
		if p == nil {
			panic("out of memory for generation metadata")
		}
		genPool = uintptr(p)
		genPoolEnd = genPool + 65536
	}
	p := genPool
	genPool += n
	return unsafe.Pointer(p)
}

// genBucket returns the leaf slot of interval i, or nil when no leaf
// covers it. Creating a leaf needs the writer lock.
func genBucket(i uintptr, create bool) *uint64 {
	if i>>16 >= genLeafSize {
		return nil
	}
	top := &genTop[i>>16]
	leaf := uintptr(vload64(top))
	if leaf == 0 {
		if !create {
			return nil
		}
		// Mmap memory is zero: every slot of a new leaf is empty before
		// the leaf is published.
		leaf = uintptr(genMetadata(genLeafSize * 8))
		store64(top, uint64(leaf))
	}
	return (*uint64)(unsafe.Pointer(leaf + (i&(genLeafSize-1))*8))
}

// genSlot is genBucket for readers, which never create a leaf. It neither
// allocates nor panics (no index check either), so the lookups that use it
// call nothing that comes back to a lookup. genBucket can: it allocates
// metadata, which may panic, and printing the panic checks its strings.
// That cycle kept genFirstLive, which the inliner does not inline within a
// cycle, out of line in GenRegisteredLive and GenRegisteredRangeLive, so
// every check made a second call.
//
//pu:inline
func genSlot(i uintptr) *uint64 {
	if i>>16 >= genLeafSize {
		return nil
	}
	leaf := uintptr(vload64((*uint64)(unsafe.Add(unsafe.Pointer(&genTop), i>>16*8))))
	if leaf == 0 {
		return nil
	}
	return (*uint64)(unsafe.Pointer(leaf + (i&(genLeafSize-1))*8))
}

// genPage returns the interval i's node. Creating one needs the writer
// lock; readers see a node only after its fields are initialized.
func genPage(i uintptr, create bool) *genInterval {
	slot := genBucket(i, create)
	if slot == nil {
		return nil
	}
	if p := uintptr(vload64(slot)); p != 0 || !create {
		return (*genInterval)(unsafe.Pointer(p))
	}
	r := (*genInterval)(genMetadata(unsafe.Sizeof(genInterval{})))
	r.interval = i
	r.history = (*[4096]uint16)(genMetadata(8192))
	store64(slot, uint64(uintptr(unsafe.Pointer(r))))
	return r
}

func genEntry(r *genRecord, a uintptr) *uint16 {
	return (*uint16)(unsafe.Add(unsafe.Pointer(r.table), (a-r.origin)>>4*2))
}

// genLive reports whether r is live at every byte of [a, end) under tag:
// one coherent snapshot of r's bounds, ancestry and table covers the whole
// extent, so each byte meets the single-address definition (ABI section
// 1.3) by the same registration. It validates the entire ancestor chain
// over the extent before consulting this record's stable table. Reuse can
// change its snapshot while we read, in which case we retry. Granule
// invalidation is independently atomic. The caller ensures a < end.
func genLive(r *genRecord, a, end uintptr, tag uint16) bool {
	for {
		v := vload64(&r.version)
		if v&1 != 0 {
			pause()
			continue
		}
		active, lo, hi := r.active, r.lo, uintptr(vload64((*uint64)(unsafe.Pointer(&r.hi))))
		parent, id, pt := r.parent, r.parentID, r.parentTag
		table, origin, capacity := r.table, r.origin, r.capacity
		if v != vload64(&r.version) {
			continue
		}
		ok := genCovers(active, lo, hi, origin, capacity, a, end)
		if ok && parent != nil {
			ok = vload64(&parent.incarnation) == id && genLive(parent, a, end, pt) && vload64(&parent.incarnation) == id
		}
		if ok {
			ok = genTableMatch(table, origin, a, end, tag)
		}
		if v == vload64(&r.version) {
			return ok
		}
	}
}

// genCovers reports whether a snapshot's registration covers [a, end):
// lo <= a < end <= hi, and origin <= lo, so the extent's granules all lie
// in the table when the last one does.
//
//pu:inline
func genCovers(active bool, lo, hi, origin, capacity, a, end uintptr) bool {
	return active && a >= lo && end <= hi && a >= origin && (end-1-origin)>>4 < capacity
}

// genTableMatch reports whether every table entry of the granules of
// [a, end) holds tag. Aligned runs of four entries are compared with one
// 8-byte load each; every entry is still read by a single aligned load, so
// each value read is one that a writer stored whole.
//
//pu:inline
func genTableMatch(table *uint16, origin, a, end uintptr, tag uint16) bool {
	e := uintptr(unsafe.Pointer(table)) + (a-origin)>>4*2
	last := uintptr(unsafe.Pointer(table)) + (end-1-origin)>>4*2
	for e&7 != 0 && e <= last {
		if vload16((*uint16)(unsafe.Pointer(e))) != tag {
			return false
		}
		e += 2
	}
	four := uint64(tag) * 0x0001000100010001
	for e+6 <= last {
		if vload64((*uint64)(unsafe.Pointer(e))) != four {
			return false
		}
		e += 8
	}
	for e <= last {
		if vload16((*uint16)(unsafe.Pointer(e))) != tag {
			return false
		}
		e += 2
	}
	return true
}

// genReadTag captures bounds and table coherently before computing an
// address. A final version check cannot repair an invalid dereference.
func genReadTag(r *genRecord, a uintptr) uint16 {
	for {
		v := vload64(&r.version)
		if v&1 != 0 {
			pause()
			continue
		}
		active, lo, hi := r.active, r.lo, uintptr(vload64((*uint64)(unsafe.Pointer(&r.hi))))
		table, origin, capacity := r.table, r.origin, r.capacity
		if v != vload64(&r.version) {
			continue
		}
		if !genCovers(active, lo, hi, origin, capacity, a, a+1) {
			return 0
		}
		tag := vload16((*uint16)(unsafe.Add(unsafe.Pointer(table), (a-origin)>>4*2)))
		if v == vload64(&r.version) {
			return tag
		}
	}
}

// GenRegisteredLive is the shared lookup for compiler checks and runtime
// count operations. Zero tags are the explicit unchecked addresses. It is
// called at every check, so it stays out of line: inlined, its lookup would
// grow every function that checks a pointer.
//
//pu:noinline
func GenRegisteredLive(p unsafe.Pointer) bool {
	bits := *(*uintptr)(unsafe.Pointer(&p))
	tag := uint16(bits >> 48)
	if tag == 0 {
		return true
	}
	a := uintptr(p)
	return genFirstLive(a, a+1, tag) || genRangeLive(p, 1)
}

// genReadLive reports whether one registration reached from the directory
// entry of a is live at every byte of [a, end) under tag. A registration
// covering the extent covers a, so the interval of a reaches it.
//
//pu:inline
func genReadLive(a, end uintptr, tag uint16) bool {
	slot := genSlot(a >> 16)
	if slot == nil {
		return false
	}
	page := uintptr(vload64(slot))
	if page == 0 {
		return false
	}
	if r := genHinted((*genInterval)(unsafe.Pointer(page)), a); r != nil && genLive(r, a, end, tag) {
		return true
	}
	for l := uintptr(vload64(&(*genInterval)(unsafe.Pointer(page)).head)); l != 0; {
		link := (*genLink)(unsafe.Pointer(l))
		if rp := uintptr(vload64(&link.reg)); rp != 0 {
			// genLive, with a registration that has no backing allocation
			// (the default heap's) validated here without a call.
			r := (*genRecord)(unsafe.Pointer(rp))
			for {
				v := vload64(&r.version)
				if v&1 != 0 {
					pause()
					continue
				}
				active, lo, hi := r.active, r.lo, uintptr(vload64((*uint64)(unsafe.Pointer(&r.hi))))
				parent, table, origin, capacity := r.parent, r.table, r.origin, r.capacity
				if v != vload64(&r.version) {
					continue
				}
				if parent != nil {
					if genLive(r, a, end, tag) {
						return true
					}
					break
				}
				ok := genCovers(active, lo, hi, origin, capacity, a, end) && genTableMatch(table, origin, a, end, tag)
				if v == vload64(&r.version) {
					if ok {
						return true
					}
					break
				}
			}
		}
		l = link.next
	}
	return false
}

// GenRegisteredRangeLive reports whether every byte of [p, p+n) is live
// under p's tag. One registration covering the extent in one snapshot is
// the common case. Otherwise each granule and the last byte are checked
// on their own, which is the definition (ABI section 1.3): the result is
// the same, and the fast path only avoids repeating the lookup. Out of line
// as GenRegisteredLive is.
//
//pu:noinline
func GenRegisteredRangeLive(p unsafe.Pointer, n uintptr) bool {
	bits := *(*uintptr)(unsafe.Pointer(&p))
	if n == 0 || bits>>48 == 0 {
		return true
	}
	a := uintptr(p)
	end := a + n
	if end >= a && end <= uintptr(1)<<48 && genFirstLive(a, end, uint16(bits>>48)) {
		return true
	}
	return genRangeLive(p, n)
}

// genFirstLive is the common case of genReadLive: one candidate, the
// granule's hint or else the interval's newest registration, covers
// [a, end) live under tag. A candidate without a backing allocation (the
// default heap's) is validated here in one stable snapshot, without a call
// or a retry; one with a backing allocation (an arena's) by genLive. False
// means only that the complete lookup must decide.
//
//pu:inline
func genFirstLive(a, end uintptr, tag uint16) bool {
	slot := genSlot(a >> 16)
	if slot == nil {
		return false
	}
	page := (*genInterval)(unsafe.Pointer(uintptr(vload64(slot))))
	if page == nil {
		return false
	}
	r := genHinted(page, a)
	if r == nil {
		l := uintptr(vload64(&page.head))
		if l == 0 {
			return false
		}
		r = (*genRecord)(unsafe.Pointer(uintptr(vload64(&(*genLink)(unsafe.Pointer(l)).reg))))
		if r == nil {
			return false
		}
	}
	v := vload64(&r.version)
	if r.parent != nil {
		return genLive(r, a, end, tag)
	}
	// genCovers, with each field compared as soon as it is read, so that
	// few values are live at once and the lookup saves fewer registers. The
	// fields read between two equal even versions are one snapshot, in
	// whatever order they are read; the table is dereferenced only after the
	// version is confirmed.
	ok := v&1 == 0 && r.active && a >= r.lo && end <= uintptr(vload64((*uint64)(unsafe.Pointer(&r.hi))))
	origin, table := r.origin, r.table
	if !ok || a < origin || (end-1-origin)>>4 >= r.capacity || v != vload64(&r.version) {
		return false
	}
	return genTableMatch(table, origin, a, end, tag) && v == vload64(&r.version)
}

// genRangeLive is GenRegisteredRangeLive's complete lookup: every
// registration of the interval, then the definition.
//
//pu:noinline
func genRangeLive(p unsafe.Pointer, n uintptr) bool {
	bits := *(*uintptr)(unsafe.Pointer(&p))
	a := uintptr(p)
	end := a + n
	if end < a || end > uintptr(1)<<48 {
		return false
	}
	tag := uint16(bits >> 48)
	if genReadLive(a, end, tag) {
		return true
	}
	for b := a; b < end; b = min(end, (b&^15)+16) {
		if !genReadLive(b, b+1, tag) {
			return false
		}
	}
	return genReadLive(end-1, end, tag)
}

type genSlabOwner struct {
	poisoned uint32
	chunks   *genRecord
}

// Each constructed allocator gets a stable identity, including when a
// value copy moves it or a new allocator reuses the same stack address.
func GenNewSlabOwner() uintptr {
	genWriter.Lock()
	p := genMetadata(unsafe.Sizeof(genSlabOwner{}))
	genWriter.Unlock()
	return uintptr(p)
}

func GenSlabOwner(token, owner uintptr) {
	genWriter.Lock()
	r := (*genRecord)(unsafe.Pointer(token))
	o := (*genSlabOwner)(unsafe.Pointer(owner))
	v := vload64(&r.version)
	store64(&r.version, v+1)
	r.owner, r.ownerNext = owner, o.chunks
	o.chunks = r
	store64(&r.version, v+2)
	genWriter.Unlock()
}

// A failed cache operation poisons the allocator identity. Only fully
// reclaimed chunks return their shadows to the pools; earlier live
// allocations survive a partial parent Release.
func genPoisonSlab(owner uintptr) {
	genWriter.Lock()
	genPoisonSlabLocked(owner)
	genWriter.Unlock()
}

func genPoisonSlabLocked(owner uintptr) {
	o := (*genSlabOwner)(unsafe.Pointer(owner))
	store32(&o.poisoned, 1)
	for r := o.chunks; r != nil; {
		next := r.ownerNext
		if genRecordDead(r) {
			genUnregisterLocked(r)
		}
		r = next
	}
}

// Called under the writer lock, before recycling an obsolete record.
// Checking every backing granule preserves partially reclaimed records.
func genRecordDead(r *genRecord) bool {
	if r.parent == nil {
		return false
	}
	if vload64(&r.parent.incarnation) != r.parentID {
		return true
	}
	for b := r.lo; b < r.hi; b = min(r.hi, (b&^15)+16) {
		if genLive(r.parent, b, b+1, r.parentTag) {
			return false
		}
	}
	return true
}

// Pruning on reuse bounds lookup and shadow storage by active overlaps,
// even when a discarded allocator's old heads are never touched again.
// Each registration examines a few links of each interval it covers,
// continuing round the chain where the last one stopped, so an interval of
// n links costs O(1) per registration and is swept every n/genPruneLinks
// registrations. A dead registration fails every lookup through its
// backing (genLive), so pruning reclaims storage and is never needed for
// a check to fail.
func genPrune(i uintptr) {
	page := genPage(i, false)
	if page == nil {
		return
	}
	l := page.sweep
	for n := 0; n < genPruneLinks; n++ {
		if l == nil {
			l = (*genLink)(unsafe.Pointer(uintptr(vload64(&page.head))))
			if l == nil {
				break
			}
		}
		if rp := uintptr(vload64(&l.reg)); rp != 0 {
			r := (*genRecord)(unsafe.Pointer(rp))
			if r.active && genRecordDead(r) {
				if r.owner != 0 {
					genPoisonSlabLocked(r.owner)
				} else {
					genUnregisterLocked(r)
				}
			}
		}
		l = (*genLink)(unsafe.Pointer(l.next))
	}
	page.sweep = l
}

// genPruneLinks is the number of links genPrune examines in an interval.
const genPruneLinks = 2

// genHinted returns the registration hinted for the granule of a, or nil.
//
//pu:inline
func genHinted(page *genInterval, a uintptr) *genRecord {
	h := uintptr(vload64(&page.hints))
	if h == 0 {
		return nil
	}
	return (*genRecord)(unsafe.Pointer(uintptr(vload64((*uint64)(unsafe.Pointer(h + (a&65535)>>4*8))))))
}

// genHint sets the hints of r's granules in page's interval to to, or,
// when to is nil, clears those that name r. The writer lock is held.
func genHint(page *genInterval, r, to *genRecord) {
	h := uintptr(page.hints)
	if h == 0 {
		return
	}
	base := page.interval << 16
	lo, hi := max(r.lo, base), min(r.hi, base+65536)
	if lo >= hi {
		return // a trimmed registration that no longer reaches here
	}
	for g := (lo - base) >> 4; g <= (hi-1-base)>>4; g++ {
		e := (*uint64)(unsafe.Pointer(h + g*8))
		if to != nil {
			vstore64(e, uint64(uintptr(unsafe.Pointer(to))))
		} else if uintptr(*e) == uintptr(unsafe.Pointer(r)) {
			vstore64(e, 0)
		}
	}
}

// genHintsCreate gives page hints, each granule naming the newest of the
// registrations linked over it. The writer lock is held.
func genHintsCreate(page *genInterval) {
	h := uintptr(genMetadata(4096 * 8)) // zero: metadata is never reused
	for l := (*genLink)(unsafe.Pointer(uintptr(page.head))); l != nil; l = (*genLink)(unsafe.Pointer(l.next)) {
		rp := uintptr(l.reg)
		if rp == 0 {
			continue
		}
		r := (*genRecord)(unsafe.Pointer(rp))
		base := page.interval << 16
		lo, hi := max(r.lo, base), min(r.hi, base+65536)
		for g := (lo - base) >> 4; lo < hi && g <= (hi-1-base)>>4; g++ {
			if e := (*uint64)(unsafe.Pointer(h + g*8)); *e == 0 {
				*e = uint64(rp)
			}
		}
	}
	store64(&page.hints, uint64(h))
}

// genUnlink removes r's link from page's interval, if r has one there,
// and keeps the link for reuse. The writer lock is held.
func genUnlink(page *genInterval, r *genRecord) {
	link := r.link0
	if link == nil || page.interval != r.lo>>16 || uintptr(link.reg) != uintptr(unsafe.Pointer(r)) {
		link = nil
		for l := (*genLink)(unsafe.Pointer(uintptr(page.head))); l != nil; l = (*genLink)(unsafe.Pointer(l.next)) {
			if uintptr(l.reg) == uintptr(unsafe.Pointer(r)) {
				link = l
				break
			}
		}
	}
	if link == nil {
		return
	}
	store64(&link.reg, 0)
	link.nextFree, page.free = page.free, link
	genHint(page, r, nil)
}

func GenSlabBackingLive(p unsafe.Pointer, owner uintptr) bool {
	if owner == 0 || load32(&(*genSlabOwner)(unsafe.Pointer(owner)).poisoned) != 0 {
		return false
	}
	a := uintptr(p)
	page := genPage(a>>16, false)
	if page == nil {
		return false
	}
	found := false
	for l := uintptr(vload64(&page.head)); l != 0; {
		link := (*genLink)(unsafe.Pointer(l))
		if rp := uintptr(vload64(&link.reg)); rp != 0 {
			r := (*genRecord)(unsafe.Pointer(rp))
			for {
				v := vload64(&r.version)
				if v&1 != 0 {
					pause()
					continue
				}
				active, lo, hi, ro := r.active, r.lo, uintptr(vload64((*uint64)(unsafe.Pointer(&r.hi)))), r.owner
				parent, id, tag := r.parent, r.parentID, r.parentTag
				if v != vload64(&r.version) {
					continue
				}
				match := active && ro == owner && a >= lo && a < hi
				live := true
				if match && parent != nil {
					live = vload64(&parent.incarnation) == id && genLive(parent, a, a+1, tag) && vload64(&parent.incarnation) == id
				}
				if v != vload64(&r.version) {
					continue
				}
				if match {
					if !live {
						genPoisonSlab(owner)
						return false
					}
					found = true
				}
				break
			}
		}
		l = link.next
	}
	return found && load32(&(*genSlabOwner)(unsafe.Pointer(owner)).poisoned) == 0
}

// genBacking chooses the deepest live allocation covering the complete
// extent. Raw custom returns still preserve the containing allocation's
// identity, even if their implementation erased the parent pointer tag.
// The writer lock is held, so registration snapshots do not change here.
func genBacking(p unsafe.Pointer, size uintptr) *genRecord {
	a := uintptr(p)
	bits := *(*uintptr)(unsafe.Pointer(&p))
	want := uint16(bits >> 48)
	page := genPage(a>>16, false)
	if page == nil {
		if want != 0 {
			panicStale(p)
		}
		return nil
	}
	// Tags at a granule are never reused (address history), so at most one
	// registration's table holds want at a: when the hint, or a backing
	// allocation of it, is that one, it is the registration the walk below
	// would choose. (A new arena allocation's backing is the heap block
	// under the arena's chunk, which the previous allocation's hint
	// reaches as its parent.)
	for r, n := genHinted(page, a), 0; want != 0 && r != nil && n < 4; r, n = r.parent, n+1 {
		if r.active && a >= r.lo && a < r.hi && size <= r.hi-a &&
			vload16(genEntry(r, a)) == want && (size == 0 || genLive(r, a, a+size, want)) {
			return r
		}
	}
	var best *genRecord
	for l := uintptr(vload64(&page.head)); l != 0; {
		link := (*genLink)(unsafe.Pointer(l))
		if rp := uintptr(vload64(&link.reg)); rp != 0 {
			r := (*genRecord)(unsafe.Pointer(rp))
			if r.active && a >= r.lo && a < r.hi && size <= r.hi-a {
				tag := vload16(genEntry(r, a))
				if tag != 0 && (want == 0 || want == tag) && (best == nil || r.depth > best.depth) {
					if size == 0 || genLive(r, a, a+size, tag) {
						best = r
					}
				}
			}
		}
		l = link.next
	}
	if best == nil && want != 0 {
		panicStale(p)
	}
	return best
}

// genPublish links r into, or unlinks it from, every interval it covers.
// The writer lock is held.
func genPublish(r *genRecord, add bool) {
	for i := r.lo >> 16; i <= (r.hi-1)>>16; i++ {
		page := genPage(i, add)
		if page == nil {
			continue
		}
		if !add {
			genUnlink(page, r)
			continue
		}
		link := page.free
		if link != nil {
			page.free = link.nextFree
		} else {
			link = (*genLink)(genMetadata(unsafe.Sizeof(genLink{})))
			link.next = uintptr(page.head)
			store64(&page.head, uint64(uintptr(unsafe.Pointer(link))))
			page.links++
		}
		store64(&link.reg, uint64(uintptr(unsafe.Pointer(r))))
		if i == r.lo>>16 {
			r.link0 = link
		}
		if page.hints != 0 {
			genHint(page, r, r)
		} else if page.links > genHintLinks {
			genHintsCreate(page)
		}
	}
}

// GenRegister registers contiguous payload, with a stable shadow table.
// The physical allocator prefix can then be reclaimed safely. backing
// retains the parent's original pointer; key identifies a custom method.
func GenRegister(p unsafe.Pointer, size uintptr, backing unsafe.Pointer, key uintptr) uintptr {
	if p == nil || size == 0 || size > ^uintptr(0)-uintptr(p)-16 {
		return 0
	}
	genWriter.Lock()
	for i := uintptr(p) >> 16; i <= (uintptr(p)+size-1)>>16; i++ {
		genPrune(i)
	}
	var parent *genRecord
	if backing != nil {
		// The payload may begin after the parent's returned pointer.
		bits := *(*uintptr)(unsafe.Pointer(&backing))
		parentPointer := unsafe.Pointer(uintptr(p) | bits>>48<<48)
		parent = genBacking(parentPointer, size)
	}
	origin := uintptr(p) &^ 15
	entries := (uintptr(p) + size - origin + 15) >> 4
	capacity, class := uintptr(1), uintptr(0)
	for capacity < entries {
		capacity <<= 1
		class++
	}
	if class >= 32 {
		genWriter.Unlock()
		return 0
	}
	// A discarded child allocator may never touch its poisoned heads.
	// Reclaim dead records of this size before growing its shadow pool,
	// including when the new payload uses a different raw address.
	if genFreeRecords[class] == 0 {
		rp := genSweep[class]
		if rp == 0 {
			rp = genRecords[class]
		}
		// A fixed budget makes simultaneous live growth linear rather
		// than rescanning every live registration on each allocation.
		for budget := 0; rp != 0 && budget < 8; budget++ {
			r := (*genRecord)(unsafe.Pointer(rp))
			rp = r.nextClass
			if r.active && genRecordDead(r) {
				if r.owner != 0 {
					genPoisonSlabLocked(r.owner)
				} else {
					genUnregisterLocked(r)
				}
				if genFreeRecords[class] != 0 {
					break
				}
			}
		}
		genSweep[class] = rp
	}
	var r *genRecord
	if genFreeRecords[class] != 0 {
		r = (*genRecord)(unsafe.Pointer(genFreeRecords[class]))
		genFreeRecords[class] = r.nextFree
	} else {
		r = (*genRecord)(genMetadata(unsafe.Sizeof(genRecord{})))
		r.capacity = capacity
		r.table = (*uint16)(genMetadata(capacity * 2))
		r.nextClass, genRecords[class] = genRecords[class], uintptr(unsafe.Pointer(r))
	}
	v := vload64(&r.version)
	store64(&r.version, v+1)
	store64(&r.incarnation, vload64(&r.incarnation)+1)
	r.lo, r.origin = uintptr(p), origin
	store64((*uint64)(unsafe.Pointer(&r.hi)), uint64(uintptr(p)+size))
	r.parent, r.parentID, r.parentTag, r.depth = parent, 0, 0, 0
	if parent != nil {
		r.parentID = vload64(&parent.incarnation)
		r.parentTag = vload16(genEntry(parent, uintptr(p)))
		r.depth = parent.depth + 1
	}
	r.original, r.key, r.owner, r.active = backing, key, 0, true
	// Tables are 16-byte aligned and a capacity of 4 or more fills whole
	// 8-byte words, each store clearing four entries at once.
	if r.capacity >= 4 {
		for i := uintptr(0); i < r.capacity; i += 4 {
			vstore64((*uint64)(unsafe.Add(unsafe.Pointer(r.table), i*2)), 0)
		}
	} else {
		for i := uintptr(0); i < r.capacity; i++ {
			vstore16((*uint16)(unsafe.Add(unsafe.Pointer(r.table), i*2)), 0)
		}
	}
	store64(&r.version, v+2)
	genPublish(r, true)
	genWriter.Unlock()
	return uintptr(unsafe.Pointer(r))
}

// GenUnregister removes a payload registration before its pages are freed.
func GenUnregister(token uintptr) {
	if token == 0 {
		return
	}
	genWriter.Lock()
	genUnregisterLocked((*genRecord)(unsafe.Pointer(token)))
	genWriter.Unlock()
}

func genUnregisterLocked(r *genRecord) {
	if !r.active {
		return
	}
	v := vload64(&r.version)
	store64(&r.version, v+1)
	if r.owner != 0 {
		o := (*genSlabOwner)(unsafe.Pointer(r.owner))
		var prev *genRecord
		for q := o.chunks; q != nil; q = q.ownerNext {
			if q == r {
				if prev == nil {
					o.chunks = r.ownerNext
				} else {
					prev.ownerNext = r.ownerNext
				}
				break
			}
			prev = q
		}
		r.owner, r.ownerNext = 0, nil
	}
	r.active = false
	store64(&r.version, v+2)
	genPublish(r, false)
	class := uintptr(0)
	for n := r.capacity; n > 1; n >>= 1 {
		class++
	}
	r.nextFree = genFreeRecords[class]
	genFreeRecords[class] = uintptr(unsafe.Pointer(r))
}
