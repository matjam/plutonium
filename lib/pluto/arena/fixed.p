package arena

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/sync/atomic"
	"unsafe"
)

// Fixed owns one fixed-capacity payload. It moves rather than copies and
// never grows or relocates its payload (language spec section 18.9).
//
// Its bookkeeping lives in its own backing (abi.md section 1.3, D54): a
// table after the payload, sized when the arena is made for one record and
// one drop entry per 16-byte granule of payload. Records of allocations grow
// up from the start of the table and drop entries grow down from its end. An allocation that finds
// no room in the table fails, and the parent is never asked again.
type Fixed owner struct {
	parent   alloc.Allocator
	base     unsafe.Pointer
	size     uintptr // payload capacity
	held     uintptr // bytes of the backing: payload and table
	table    uintptr // offset of the table in the backing
	off      uintptr
	nrec     uintptr // allocation records, from the start of the table
	nent     uintptr // drop entries, from the end of the table
	added    uint64  // drop entries ever recorded
	live     int64   // allocations made through Alloc and not freed yet
	identity uint64
	reset    uint64
	next     uint64 // the identity of the latest allocation
	// retired is set by an allocation whose granules' address history is
	// exhausted: the address just past the last such granule (abi.md
	// section 1.3, Arena epochs).
	retired uintptr
}

var _ alloc.Allocator = (*Fixed)(nil)

// NewFixed explicitly allocates capacity bytes from parent, with the
// arena's bookkeeping table after them, in one request. The table has room
// for one allocation and its drop entry per 16-byte granule of the
// payload; the arena asks the parent for nothing more until its Drop gives
// the backing back. The
// parent must outlive the Fixed and its Drop. Zero capacity is empty.
func NewFixed(capacity uintptr, parent alloc.Allocator = alloc.Default()) Fixed {
	if parent == nil {
		panic("arena: NewFixed with a nil parent allocator")
	}
	if capacity > uintptr(^uint(0)>>1)/4 {
		panic("arena: fixed capacity is too large")
	}
	if capacity == 0 {
		return Fixed{parent: parent, identity: rt.ArenaIdentity()}
	}
	payload := (capacity + 15) &^ 15
	held := payload + payload/16*(unsafe.Sizeof(fixedRecord{})+unsafe.Sizeof(dropEntry{}))
	p := parent.Alloc(held, 16)
	if p == nil {
		panic("arena: out of memory")
	}
	return Fixed{parent: parent, base: p, size: capacity, held: held, table: payload, identity: rt.ArenaIdentity()}
}

// Alloc returns size zeroed bytes aligned to align, or nil when the arena
// has no room. The allocation counts as live until it is freed (language
// spec section 18.9, Live allocations).
func (a *Fixed) Alloc(size, align uintptr) unsafe.Pointer {
	p := a.alloc(size, align, 0)
	if p != nil {
		atomic.AddInt64(&a.live, 1)
	}
	return p
}

// alloc is Alloc without the count, for the plain views of New and Slice.
// It leaves room in the table for ents more drop entries.
func (a *Fixed) alloc(size, align, ents uintptr) unsafe.Pointer {
	if align == 0 {
		align = 1
	}
	if align&(align-1) != 0 {
		panic("arena: alignment is not a power of two")
	}
	if size == 0 {
		size = 1
	}
	if a.base == nil {
		return nil
	}
	if (a.nrec+1)*unsafe.Sizeof(fixedRecord{})+(a.nent+ents)*unsafe.Sizeof(dropEntry{}) > a.held-a.table {
		return nil // the table is full
	}
	start := uintptr(a.base) + a.off
	pad := (align - start&(align-1)) & (align - 1)
	if pad > a.size-a.off || size > a.size-a.off-pad {
		return nil
	}
	p := a.allocate(unsafe.Add(a.base, a.off+pad), size)
	if p == nil {
		if r := a.retired; r != 0 {
			// Granules whose address history is exhausted are retired:
			// allocate after them. A Fixed has no other storage, so once
			// every granule is retired its allocations fail.
			a.off = min(r-uintptr(a.base), a.size)
			return a.alloc(size, align, ents)
		}
		return nil
	}
	a.off += pad + size
	clear(unsafe.Slice((*byte)(p), size))
	return p
}

// allocate records an allocation of size bytes at p in the table and
// returns its pointer, tagged with a new epoch in a checked build, or nil
// when the address history of its granules is exhausted.
func (a *Fixed) allocate(p unsafe.Pointer, size uintptr) unsafe.Pointer {
	if a.next == ^uint64(0) {
		panic("arena: allocation identity exhausted")
	}
	a.retired = 0
	token := uintptr(0)
	result := p
	if genCheck {
		token = rt.GenRegister(unsafe.Pointer(uintptr(p)), size, p, 0)
		result = rt.GenTagArena(token, p, size)
		if result == nil {
			if token != 0 {
				a.retired = rt.GenArenaExhausted(p, size)
			}
			rt.GenUnregister(token)
			return nil
		}
	}
	a.next++
	r := a.record(a.nrec)
	*r = fixedRecord{id: a.next}
	r.setToken(token)
	a.nrec++
	return result
}

// record returns allocation record i.
func (a *Fixed) record(i uintptr) *fixedRecord {
	return (*fixedRecord)(unsafe.Add(a.base, a.table+i*unsafe.Sizeof(fixedRecord{})))
}

// reclaim ends the allocations after the first n, latest first.
func (a *Fixed) reclaim(n uintptr, pc uintptr) {
	for ; a.nrec > n; a.nrec-- {
		if genCheck {
			rt.GenEndArena(a.record(a.nrec-1).token(), pc)
		}
	}
}

// Free takes back nothing, not even the latest allocation: an arena frees in
// bulk only, by Reset, Release and Drop (language spec section 18.9, D46).
// It ends the allocation's count, so Reset, Release and Drop no longer
// find it live.
func (a *Fixed) Free(p unsafe.Pointer, size, align uintptr) {
	rt.GenCheckArenaFree(p, max(size, uintptr(1)))
	if p != nil {
		atomic.AddInt64(&a.live, -1)
	}
}

// entry returns drop entry i.
func (a *Fixed) entry(i uintptr) *dropEntry {
	return (*dropEntry)(unsafe.Add(a.base, a.held-(i+1)*unsafe.Sizeof(dropEntry{})))
}

// allocEntry allocates n values of size bytes in all and records a drop
// entry for them, or returns nil when the payload or the table has no room
// for both.
func (a *Fixed) allocEntry(size, align, n uintptr, fn func(unsafe.Pointer, uintptr)) unsafe.Pointer {
	p := a.alloc(size, align, 1)
	if p == nil {
		return nil
	}
	*a.entry(a.nent) = dropEntry{p: p, n: n, fn: fn}
	a.nent++
	a.added++
	return p
}

// runEntries runs the drop entries above stop, last first. An entry
// recorded while they run is not run, and makes the call panic.
func (a *Fixed) runEntries(stop uintptr) {
	added := a.added
	for i := a.nent; i > stop; i-- {
		runEntry(a.entry(i - 1))
	}
	if a.added != added {
		panic("arena has live allocations")
	}
}

func (a *Fixed) Reset() {
	a.runEntries(0)
	a.checkLive()
	a.resetMarks()
	a.off = 0
	a.nent = 0
	a.reclaim(0, rt.CallerPC())
}

// FixedMark belongs to one Fixed and a still-live allocation position.
type FixedMark struct {
	identity uint64
	reset    uint64
	id       uint64 // the identity of the latest allocation, or 0
	nrec     uintptr
	off      uintptr
	nent     uintptr
}

func (a *Fixed) Mark() FixedMark {
	id := uint64(0)
	if a.nrec > 0 {
		id = a.record(a.nrec - 1).id
	}
	return FixedMark{a.identity, a.reset, id, a.nrec, a.off, a.nent}
}

// valid reports whether m is a mark of a's present allocations: none of
// the allocations before it has been reclaimed since it was taken.
func (a *Fixed) valid(m FixedMark) bool {
	if m.identity != a.identity || m.reset != a.reset || m.identity == 0 {
		return false
	}
	if m.nrec > a.nrec || m.off > a.off || m.nent > a.nent {
		return false
	}
	if m.nrec == 0 {
		return m.id == 0
	}
	return a.record(m.nrec-1).id == m.id
}

func (a *Fixed) Release(m FixedMark) {
	if !a.valid(m) {
		panic("arena: Release of a stale mark")
	}
	a.runEntries(m.nent)
	a.checkLive()
	a.off = m.off
	a.nent = m.nent
	a.reclaim(m.nrec, rt.CallerPC())
}

func (a *Fixed) Drop() {
	a.runEntries(0)
	a.checkLive()
	a.resetMarks()
	a.reclaim(0, rt.CallerPC())
	base, held, parent := a.base, a.held, a.parent
	a.base, a.size, a.held, a.table, a.off, a.nent = nil, 0, 0, 0, 0, 0
	if base != nil {
		if rt.CatchAllocFree(parent, base, held, 16) {
			rt.ResumeAllocFreeExit()
		}
	}
}

func (a *Fixed) resetMarks() {
	if a.reset == ^uint64(0) {
		panic("arena: mark identity exhausted")
	}
	a.reset++
}

// checkLive panics while an allocation made through Alloc is not freed
// (language spec section 18.9, Live allocations).
func (a *Fixed) checkLive() {
	if atomic.LoadInt64(&a.live) != 0 {
		panic("arena has live allocations")
	}
}

func (a *Fixed) Used() int      { return int(a.off) }
func (a *Fixed) Available() int { return int(a.size - a.off) }
func (a *Fixed) Cap() int       { return int(a.size) }
