package arena

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/sync/atomic"
	"unsafe"
)

// Fixed owns one fixed-capacity payload. It moves rather than copies and
// never grows or relocates its payload (language spec section 18.9).
type Fixed owner struct {
	parent alloc.Allocator
	base   unsafe.Pointer
	size   uintptr
	off    uintptr
	life   lifetime
	live   int64   // allocations made through Alloc and not freed yet
	nent   uintptr // drop entries, at the end of the payload, growing down
	added  uint64  // drop entries ever recorded
}

var _ alloc.Allocator = (*Fixed)(nil)

// NewFixed explicitly allocates capacity bytes from parent. Allocation
// bookkeeping is separately allocated from that parent as allocations occur.
// The parent must outlive the Fixed and its Drop. Zero capacity is empty.
func NewFixed(capacity uintptr, parent alloc.Allocator = alloc.Default()) Fixed {
	if parent == nil {
		panic("arena: NewFixed with a nil parent allocator")
	}
	if capacity > uintptr(^uint(0)>>1) {
		panic("arena: fixed capacity is too large")
	}
	if capacity == 0 {
		return Fixed{parent: parent, life: newLifetime(parent)}
	}
	p := parent.Alloc(capacity, 16)
	if p == nil {
		panic("arena: out of memory")
	}
	return Fixed{parent: parent, base: p, size: capacity, life: newLifetime(parent)}
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
// It leaves keep bytes free below the drop entries.
func (a *Fixed) alloc(size, align, keep uintptr) unsafe.Pointer {
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
	limit := a.limit()
	start := uintptr(a.base) + a.off
	pad := (align - start&(align-1)) & (align - 1)
	if pad > limit-a.off || size > limit-a.off-pad || keep > limit-a.off-pad-size {
		return nil
	}
	p := unsafe.Add(a.base, a.off+pad)
	p = a.life.allocate(p, size)
	if p == nil {
		if r := a.life.retired; r != 0 {
			// Granules whose address history is exhausted are retired:
			// allocate after them. A Fixed has no other storage, so once
			// every granule is retired its allocations fail.
			a.off = min(r-uintptr(a.base), limit)
			return a.alloc(size, align, keep)
		}
		return nil
	}
	a.off += pad + size
	clear(unsafe.Slice((*byte)(p), size))
	return p
}

// Free reclaims only the latest allocation, invalidating it before reuse.
func (a *Fixed) Free(p unsafe.Pointer, size, align uintptr) {
	rt.GenCheckArenaFree(p, max(size, uintptr(1)))
	if p != nil {
		atomic.AddInt64(&a.live, -1)
	}
	r := a.life.head
	if r == nil || p == nil || uintptr(p) < uintptr(r.p) || uintptr(p) >= uintptr(r.p)+r.size || max(size, uintptr(1)) != uintptr(r.p)+r.size-uintptr(p) {
		return
	}
	position := uintptr(p) - uintptr(a.base)
	a.off = position
	if a.life.freeTail(p, max(size, uintptr(1)), rt.CallerPC()) {
		rt.ResumeAllocFreeExit()
	}
}

// limit is the end of the payload room: the drop entries take the end of
// the backing.
func (a *Fixed) limit() uintptr {
	if a.nent == 0 {
		return a.size
	}
	return a.size&^(unsafe.Alignof(dropEntry{})-1) - a.nent*unsafe.Sizeof(dropEntry{})
}

// entry returns drop entry i.
func (a *Fixed) entry(i uintptr) *dropEntry {
	return (*dropEntry)(unsafe.Add(a.base, a.size&^(unsafe.Alignof(dropEntry{})-1)-(i+1)*unsafe.Sizeof(dropEntry{})))
}

// allocEntry allocates n values of size bytes in all and records a drop
// entry for them, or returns nil when the payload has no room for both.
func (a *Fixed) allocEntry(size, align, n uintptr, fn func(unsafe.Pointer, uintptr)) unsafe.Pointer {
	keep := unsafe.Sizeof(dropEntry{})
	if a.nent == 0 {
		keep += a.size & (unsafe.Alignof(dropEntry{}) - 1) // the first entry is aligned
	}
	p := a.alloc(size, align, keep)
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
	a.life.resetMarks()
	a.off = 0
	a.nent = 0
	if a.life.reclaim(0, rt.CallerPC(), false) {
		rt.ResumeAllocFreeExit()
	}
}

// FixedMark belongs to one Fixed and a still-live allocation position.
type FixedMark struct {
	identity uint64
	reset    uint64
	sequence uint64
	off      uintptr
	end      uintptr
	nent     uintptr
}

func (a *Fixed) Mark() FixedMark {
	return FixedMark{a.life.identity, a.life.reset, a.life.sequence(), a.off, a.life.end(), a.nent}
}

func (a *Fixed) Release(m FixedMark) {
	if !a.life.valid(m.identity, m.reset, m.sequence, m.end) || m.off > a.off || m.nent > a.nent {
		panic("arena: Release of a stale mark")
	}
	a.runEntries(m.nent)
	a.checkLive()
	a.off = m.off
	a.nent = m.nent
	if a.life.reclaim(m.sequence, rt.CallerPC(), false) {
		rt.ResumeAllocFreeExit()
	}
}

func (a *Fixed) Drop() {
	a.runEntries(0)
	a.checkLive()
	a.life.resetMarks()
	base, size, parent := a.base, a.size, a.parent
	a.base, a.size, a.off, a.nent = nil, 0, 0, 0
	exiting := a.life.reclaim(0, rt.CallerPC(), false)
	if base != nil {
		if rt.CatchAllocFree(parent, base, size, 16) {
			exiting = true
		}
	}
	if exiting {
		rt.ResumeAllocFreeExit()
	}
}

// checkLive panics while an allocation made through Alloc is not freed
// (language spec section 18.9, Live allocations).
func (a *Fixed) checkLive() {
	if atomic.LoadInt64(&a.live) != 0 {
		panic("arena has live allocations")
	}
}

func (a *Fixed) Used() int      { return int(a.off) }
func (a *Fixed) Available() int { return int(a.limit() - a.off) }
func (a *Fixed) Cap() int       { return int(a.size) }
