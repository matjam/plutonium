//pu:build gencheck

package rt

import "unsafe"

// Generation metadata has process lifetime. Payload may be unmapped, but
// readers never dereference it to find a table (ABI section 1.3).
// Interval/link topology is immutable after publication; only link.reg
// changes. Record reuse is published under a version, and each backing
// reference also saves the record's incarnation.
type genInterval struct {
	interval uintptr
	next     uintptr
	head     uint64
	history  *[4096]uint16
}

type genLink struct {
	next uintptr
	reg  uint64
}

type genRecord struct {
	version     uint64
	incarnation uint64
	lo, hi      uintptr
	origin      uintptr
	table       *uint16
	capacity    uintptr
	parent      *genRecord
	parentID    uint64
	parentTag   uint16
	depth       uintptr
	original    unsafe.Pointer
	key         uintptr
	owner       uintptr
	ownerNext   *genRecord
	active      bool
	nextFree    uintptr
	nextClass   uintptr
}

var genIndex [4096]uint64
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

func genBucket(i uintptr) uintptr { return (i ^ i>>12) & 4095 }

func genPage(i uintptr, create bool) *genInterval {
	b := &genIndex[genBucket(i)]
	for p := uintptr(load64(b)); p != 0; {
		r := (*genInterval)(unsafe.Pointer(p))
		if r.interval == i {
			return r
		}
		p = r.next
	}
	if !create {
		return nil
	}
	r := (*genInterval)(genMetadata(unsafe.Sizeof(genInterval{})))
	r.interval = i
	r.history = (*[4096]uint16)(genMetadata(8192))
	r.next = uintptr(load64(b))
	store64(b, uint64(uintptr(unsafe.Pointer(r))))
	return r
}

func genEntry(r *genRecord, a uintptr) *uint16 {
	return (*uint16)(unsafe.Add(unsafe.Pointer(r.table), (a-r.origin)>>4*2))
}

// genLive validates the entire ancestor chain before consulting this
// record's stable table. Reuse can change its snapshot while we read, in
// which case we retry. Granule invalidation is independently atomic.
func genLive(r *genRecord, a uintptr, tag uint16) bool {
	for {
		v := load64(&r.version)
		if v&1 != 0 {
			pause()
			continue
		}
		active, lo, hi := r.active, r.lo, r.hi
		parent, id, pt := r.parent, r.parentID, r.parentTag
		table, origin, capacity := r.table, r.origin, r.capacity
		if v != load64(&r.version) {
			continue
		}
		ok := active && a >= lo && a < hi
		if ok && parent != nil {
			ok = load64(&parent.incarnation) == id && genLive(parent, a, pt) && load64(&parent.incarnation) == id
		}
		if ok && a >= origin && (a-origin)>>4 < capacity {
			ok = load16((*uint16)(unsafe.Add(unsafe.Pointer(table), (a-origin)>>4*2))) == tag
		} else {
			ok = false
		}
		if v == load64(&r.version) {
			return ok
		}
	}
}

// genReadTag captures bounds and table coherently before computing an
// address. A final version check cannot repair an invalid dereference.
func genReadTag(r *genRecord, a uintptr) uint16 {
	for {
		v := load64(&r.version)
		if v&1 != 0 {
			pause()
			continue
		}
		active, lo, hi := r.active, r.lo, r.hi
		table, origin, capacity := r.table, r.origin, r.capacity
		if v != load64(&r.version) {
			continue
		}
		if !active || a < lo || a >= hi || a < origin || (a-origin)>>4 >= capacity {
			return 0
		}
		tag := load16((*uint16)(unsafe.Add(unsafe.Pointer(table), (a-origin)>>4*2)))
		if v == load64(&r.version) {
			return tag
		}
	}
}

// GenRegisteredLive is the shared lookup for compiler checks and runtime
// count operations. Zero tags are the explicit unchecked addresses.
func GenRegisteredLive(p unsafe.Pointer) bool {
	bits := *(*uintptr)(unsafe.Pointer(&p))
	tag := uint16(bits >> 48)
	if tag == 0 {
		return true
	}
	a := uintptr(p)
	page := genPage(a>>16, false)
	if page == nil {
		return false
	}
	for l := uintptr(load64(&page.head)); l != 0; {
		link := (*genLink)(unsafe.Pointer(l))
		if rp := uintptr(load64(&link.reg)); rp != 0 && genLive((*genRecord)(unsafe.Pointer(rp)), a, tag) {
			return true
		}
		l = link.next
	}
	return false
}

func GenRegisteredRangeLive(p unsafe.Pointer, n uintptr) bool {
	bits := *(*uintptr)(unsafe.Pointer(&p))
	if n == 0 || bits>>48 == 0 {
		return true
	}
	a := uintptr(p)
	end := a + n
	if end < a || end > uintptr(1)<<48 {
		return false
	}
	for b := a; b < end; b = min(end, (b&^15)+16) {
		if !GenRegisteredLive(unsafe.Pointer(b | bits>>48<<48)) {
			return false
		}
	}
	return GenRegisteredLive(unsafe.Pointer((end - 1) | bits>>48<<48))
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
	v := load64(&r.version)
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
	if load64(&r.parent.incarnation) != r.parentID {
		return true
	}
	for b := r.lo; b < r.hi; b = min(r.hi, (b&^15)+16) {
		if genLive(r.parent, b, r.parentTag) {
			return false
		}
	}
	return true
}

// Pruning on reuse bounds lookup and shadow storage by active overlaps,
// even when a discarded allocator's old heads are never touched again.
func genPrune(i uintptr) {
	page := genPage(i, false)
	if page == nil {
		return
	}
	for l := uintptr(load64(&page.head)); l != 0; {
		link := (*genLink)(unsafe.Pointer(l))
		if rp := uintptr(load64(&link.reg)); rp != 0 {
			r := (*genRecord)(unsafe.Pointer(rp))
			if r.active && genRecordDead(r) {
				if r.owner != 0 {
					genPoisonSlabLocked(r.owner)
				} else {
					genUnregisterLocked(r)
				}
			}
		}
		l = link.next
	}
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
	for l := uintptr(load64(&page.head)); l != 0; {
		link := (*genLink)(unsafe.Pointer(l))
		if rp := uintptr(load64(&link.reg)); rp != 0 {
			r := (*genRecord)(unsafe.Pointer(rp))
			for {
				v := load64(&r.version)
				if v&1 != 0 {
					pause()
					continue
				}
				active, lo, hi, ro := r.active, r.lo, r.hi, r.owner
				parent, id, tag := r.parent, r.parentID, r.parentTag
				if v != load64(&r.version) {
					continue
				}
				match := active && ro == owner && a >= lo && a < hi
				live := true
				if match && parent != nil {
					live = load64(&parent.incarnation) == id && genLive(parent, a, tag) && load64(&parent.incarnation) == id
				}
				if v != load64(&r.version) {
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
	var best *genRecord
	for l := uintptr(load64(&page.head)); l != 0; {
		link := (*genLink)(unsafe.Pointer(l))
		if rp := uintptr(load64(&link.reg)); rp != 0 {
			r := (*genRecord)(unsafe.Pointer(rp))
			if r.active && a >= r.lo && a < r.hi && size <= r.hi-a {
				tag := load16(genEntry(r, a))
				if tag != 0 && (want == 0 || want == tag) && (best == nil || r.depth > best.depth) {
					ok := true
					for b := a; b < a+size; {
						if !genLive(r, b, tag) {
							ok = false
							break
						}
						b = min(a+size, (b&^15)+16)
					}
					if ok {
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

func genPublish(r *genRecord, add bool) {
	for i := r.lo >> 16; i <= (r.hi-1)>>16; i++ {
		page := genPage(i, add)
		var empty *genLink
		for l := uintptr(load64(&page.head)); l != 0; {
			link := (*genLink)(unsafe.Pointer(l))
			p := uintptr(load64(&link.reg))
			if !add && p == uintptr(unsafe.Pointer(r)) {
				store64(&link.reg, 0)
			}
			if add && p == 0 && empty == nil {
				empty = link
			}
			l = link.next
		}
		if !add {
			continue
		}
		if empty == nil {
			empty = (*genLink)(genMetadata(unsafe.Sizeof(genLink{})))
			empty.next = uintptr(load64(&page.head))
			store64(&page.head, uint64(uintptr(unsafe.Pointer(empty))))
		}
		store64(&empty.reg, uint64(uintptr(unsafe.Pointer(r))))
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
	v := load64(&r.version)
	store64(&r.version, v+1)
	store64(&r.incarnation, load64(&r.incarnation)+1)
	r.lo, r.hi, r.origin = uintptr(p), uintptr(p)+size, origin
	r.parent, r.parentID, r.parentTag, r.depth = parent, 0, 0, 0
	if parent != nil {
		r.parentID = load64(&parent.incarnation)
		r.parentTag = load16(genEntry(parent, uintptr(p)))
		r.depth = parent.depth + 1
	}
	r.original, r.key, r.owner, r.active = backing, key, 0, true
	for i := uintptr(0); i < r.capacity; i++ {
		store16((*uint16)(unsafe.Add(unsafe.Pointer(r.table), i*2)), 0)
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
	v := load64(&r.version)
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

// Explicit 16-bit atomic accesses keep tag stores and reads indivisible,
// and prevent the optimiser from caching a mutable metadata entry.
//
//pu:noescape
func load16(p *uint16) uint16

//pu:noescape
func store16(p *uint16, value uint16)
