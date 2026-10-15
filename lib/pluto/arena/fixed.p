package arena

import (
	"pluto/alloc"
	"pluto/rt"
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

func (a *Fixed) Alloc(size, align uintptr) unsafe.Pointer {
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
	start := uintptr(a.base) + a.off
	pad := (align - start&(align-1)) & (align - 1)
	if pad > a.size-a.off || size > a.size-a.off-pad {
		return nil
	}
	p := unsafe.Add(a.base, a.off+pad)
	p = a.life.allocate(p, size)
	if p == nil {
		return nil
	}
	a.off += pad + size
	clear(unsafe.Slice((*byte)(p), size))
	return p
}

// Free reclaims only the latest allocation, invalidating it before reuse.
func (a *Fixed) Free(p unsafe.Pointer, size, align uintptr) {
	rt.GenCheckArenaFree(p, max(size, uintptr(1)))
	r := a.life.head
	if r == nil || p == nil || uintptr(p) < uintptr(r.p) || uintptr(p) >= uintptr(r.p)+r.size || max(size, uintptr(1)) != uintptr(r.p)+r.size-uintptr(p) {
		return
	}
	position := uintptr(p) - uintptr(a.base)
	a.off = position
	if a.life.freeTail(p, max(size, uintptr(1)), rt.CallerPC()) {
		rt.ExitTask()
	}
}

func (a *Fixed) Reset() {
	a.life.resetMarks()
	a.off = 0
	if a.life.reclaim(0, rt.CallerPC(), false) {
		rt.ExitTask()
	}
}

// FixedMark belongs to one Fixed and a still-live allocation position.
type FixedMark struct {
	identity uint64
	reset    uint64
	sequence uint64
	off      uintptr
	end      uintptr
}

func (a *Fixed) Mark() FixedMark {
	return FixedMark{a.life.identity, a.life.reset, a.life.sequence(), a.off, a.life.end()}
}

func (a *Fixed) Release(m FixedMark) {
	if !a.life.valid(m.identity, m.reset, m.sequence, m.end) || m.off > a.off {
		panic("arena: Release of a stale mark")
	}
	a.off = m.off
	if a.life.reclaim(m.sequence, rt.CallerPC(), false) {
		rt.ExitTask()
	}
}

func (a *Fixed) Drop() {
	a.life.resetMarks()
	base, size, parent := a.base, a.size, a.parent
	a.base, a.size, a.off = nil, 0, 0
	exiting := a.life.reclaim(0, rt.CallerPC(), false)
	if base != nil {
		if rt.CatchAllocFree(parent, base, size, 16) {
			exiting = true
		}
	}
	if exiting {
		rt.ExitTask()
	}
}

func (a *Fixed) Used() int      { return int(a.off) }
func (a *Fixed) Available() int { return int(a.size - a.off) }
func (a *Fixed) Cap() int       { return int(a.size) }
