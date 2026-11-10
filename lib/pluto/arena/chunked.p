package arena

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/sync/atomic"
	"unsafe"
)

// A Chunked is a bump allocator that grows: when its current chunk is full
// it takes another from its parent allocator and carries on there. Like an
// Arena it implements alloc.Allocator (through its pointer), freeing into it
// does nothing (except for the most recent allocation), and memory is
// reclaimed all at once:
//
//   - Reset frees every allocation. The arena keeps its chunks of the
//     standard size for reuse and returns larger ones to the parent.
//   - Mark and Release free everything allocated after a mark, so one arena
//     can serve a long-lived phase and, between a Mark and its Release,
//     short-lived scratch work.
//   - Drop returns every chunk to the parent; Trim returns only the spare
//     ones Reset and Release kept.
//
// Every allocation made through Alloc must be freed before Reset, Release
// or Drop, which panic with "arena has live allocations" otherwise; the
// views of New and Slice are not counted. For a type with drop glue, New
// and Slice record a drop entry, and Reset, Release and Drop first drop
// those values in place, newest first. Memory a Reset, Release, Drop or
// latest Free takes back must not be used afterwards. Checked builds validate persistent allocation incarnations;
// unchecked builds omit that validation (language spec section 18.11).
//
// A compiler uses one per phase:
//
//	prog := arena.NewChunked(1 << 20)
//	defer prog.Drop()
//	restore := alloc.Use(&prog)
//	for _, fn := range funcs {
//		m := prog.Mark()
//		optimise(fn)   // scratch allocations
//		prog.Release(m)
//	}
//	restore.Restore()
//
// A Chunked owns its chunks, so it is an owner (language spec section 7.18):
// it moves instead of being copied, and it is released by Drop.
//
// A Chunked is not safe for concurrent use.
type Chunked owner struct {
	parent    alloc.Allocator
	chunkSize uintptr // data bytes in a standard chunk
	cur       *chunk  // the chunk being filled; older chunks follow prev
	off       uintptr // bytes used in cur
	before    uintptr // bytes used in the chunks older than cur
	spare     *chunk  // standard chunks kept for reuse, linked by prev
	held      uintptr // bytes taken from the parent, headers included
	poison    bool
	uncounted bool // made by NewChunkedUnsafe: no count of live allocations
	life      lifetime
	live      int64       // allocations made through Alloc and not freed yet
	entries   *entryBlock // the newest block of drop entries
	added     uint64      // drop entries ever recorded
}

// A chunk is a block from the parent allocator: this header, then size
// bytes of data at chunkHeader, or at epochTable in an epoch chunk
// (epoch.p).
type chunk struct {
	prev *chunk
	size uintptr
}

const chunkHeader = 16

// DefaultChunkSize is the chunk size of NewChunked when none is given.
const DefaultChunkSize = 64 << 10

// The Chunked satisfies alloc.Allocator through its pointer.
var _ alloc.Allocator = (*Chunked)(nil)

// NewChunked returns an empty Chunked that takes chunks of chunkSize bytes
// from parent (by default the default allocator) as it needs them; an
// allocation larger than a chunk gets a chunk of its own. It allocates
// nothing until the first allocation.
func NewChunked(chunkSize uintptr = DefaultChunkSize, parent alloc.Allocator = alloc.Default()) Chunked {
	if parent == nil {
		panic("arena: NewChunked with a nil parent allocator")
	}
	if chunkSize < 256 {
		chunkSize = 256
	}
	if chunkSize > ^uintptr(0)/4 {
		panic("arena: chunk size is too large")
	}
	return Chunked{parent: parent, chunkSize: (chunkSize + 15) &^ 15, life: newLifetime(parent)}
}

// NewChunkedUnsafe is NewChunked for a Chunked that keeps no count of live
// allocations and records no drop entries, so its Reset, Release and Drop
// take back memory that is still in use and drop nothing in it. As with NewUnsafe, the caller is responsible for every
// allocation it makes in the arena: nothing may use one, and no count
// obligation may stay in one, after the arena takes it back (language spec
// section 18.12). The compiler's own scratch memory, whose allocations are
// discarded in bulk, is the one user.
func NewChunkedUnsafe(chunkSize uintptr = DefaultChunkSize, parent alloc.Allocator = alloc.Default()) Chunked {
	a := NewChunked(chunkSize, parent)
	a.uncounted = true
	return a
}

func (c *chunk) data() unsafe.Pointer {
	if isEpoch(c) {
		return unsafe.Add(unsafe.Pointer(c), c.meta().dataOff)
	}
	return unsafe.Add(unsafe.Pointer(c), chunkHeader)
}

// Alloc returns size zeroed bytes aligned to align (a power of two), or nil
// if the parent allocator is out of memory. The allocation counts as live
// until it is freed (language spec section 18.9, Live allocations).
func (a *Chunked) Alloc(size, align uintptr) unsafe.Pointer {
	p := a.alloc(size, align)
	if p != nil && !a.uncounted {
		atomic.AddInt64(&a.live, 1)
	}
	return p
}

// alloc is Alloc without the count, for the plain views of New and Slice.
func (a *Chunked) alloc(size, align uintptr) unsafe.Pointer {
	if align == 0 {
		align = 1
	}
	if align&(align-1) != 0 {
		panic("arena: alignment is not a power of two")
	}
	if size == 0 {
		size = 1
	}
	if p := a.bump(size, align); p != nil {
		return p
	}
	if size > ^uintptr(0)/4 || align > ^uintptr(0)/4 || !a.grow(size+align) {
		return nil
	}
	return a.bump(size, align)
}

// bump allocates from the current chunk, or returns nil if it has no room.
func (a *Chunked) bump(size, align uintptr) unsafe.Pointer {
	c := a.cur
	if c == nil {
		return nil
	}
	if isEpoch(c) && align < epochGranule {
		align = epochGranule // a granule belongs to one allocation
	}
	start := uintptr(c.data()) + a.off
	pad := (align - start&(align-1)) & (align - 1)
	if pad > c.size-a.off || size > c.size-a.off-pad {
		return nil
	}
	p := unsafe.Add(c.data(), a.off+pad)
	p = a.life.allocate(p, size)
	if p == nil {
		if r := a.life.retired; r != 0 {
			// Granules whose address history is exhausted are retired:
			// allocate after them, in this chunk or the next.
			a.off = min(r-uintptr(c.data()), c.size)
			return a.bump(size, align)
		}
		return nil
	}
	a.off += pad + size
	if size > 0 {
		// The chunk may hold data from before a Reset or Release.
		clear(unsafe.Slice((*byte)(p), size))
	}
	return p
}

// allocEntry allocates n values of size bytes in all and records a drop
// entry for them, or returns nil when the parent has no memory for them or
// for a new block of entries.
func (a *Chunked) allocEntry(size, align, n uintptr, fn func(unsafe.Pointer, uintptr)) unsafe.Pointer {
	if a.uncounted {
		return a.alloc(size, align)
	}
	b := a.entries
	if b == nil || b.used == entriesPerBlock {
		raw := a.alloc(unsafe.Sizeof(entryBlock{}), unsafe.Alignof(entryBlock{}))
		if raw == nil {
			return nil
		}
		b = (*entryBlock)(raw)
		b.prev = a.entries
		a.entries = b
	}
	p := a.alloc(size, align)
	if p == nil {
		return nil
	}
	b.e[b.used] = dropEntry{p: p, n: n, fn: fn}
	b.used++
	a.added++
	return p
}

// runEntries runs the drop entries recorded after the first used entries
// of block stop (all of them for a nil stop), last first. An entry recorded
// while they run is not run, and makes the call panic.
func (a *Chunked) runEntries(stop *entryBlock, used int) {
	added := a.added
	for b := a.entries; b != nil; b = b.prev {
		low := 0
		if b == stop {
			low = used
		}
		for i := b.used - 1; i >= low; i-- {
			runEntry(&b.e[i])
		}
		if b == stop {
			break
		}
	}
	if a.added != added {
		panic("arena has live allocations")
	}
}

// grow makes a chunk with at least need bytes of data the current one: a
// spare standard chunk if one fits, otherwise a new one from the parent.
func (a *Chunked) grow(need uintptr) bool {
	if a.parent == nil {
		panic("arena: Chunked used before NewChunked")
	}
	var c *chunk
	if need <= a.chunkSize && a.spare != nil {
		c = a.spare
		a.spare = c.prev
	} else {
		size := a.chunkSize
		if need > size {
			size = (need + 15) &^ 15
		}
		if genCheck {
			c = a.newEpochChunk(size)
			if c == nil {
				return false
			}
		} else {
			p := a.parent.Alloc(chunkHeader+size, 16)
			if p == nil {
				return false
			}
			c = (*chunk)(p)
			c.size = size
			a.held += chunkHeader + size
		}
	}
	c.prev = a.cur
	if a.cur != nil {
		a.before += a.off
	}
	a.cur = c
	a.off = 0
	return true
}

// Free releases memory returned by Alloc. Memory is released all at once by
// Reset, Release and Drop, so Free does nothing, except that freeing the
// most recent allocation gives its space back (which makes a growing
// vec.Vec on an arena cheaper).
func (a *Chunked) Free(p unsafe.Pointer, size, align uintptr) {
	rt.GenCheckArenaFree(p, max(size, uintptr(1)))
	if p != nil && !a.uncounted {
		atomic.AddInt64(&a.live, -1)
	}
	r := a.life.head
	if c := a.cur; c != nil && r != nil && p != nil && uintptr(p) >= uintptr(r.p) && uintptr(p) < uintptr(r.p)+r.size && uintptr(p)+max(size, uintptr(1)) == uintptr(r.p)+r.size && uintptr(p)+max(size, uintptr(1)) == uintptr(c.data())+a.off {
		a.off = uintptr(p) - uintptr(c.data())
		if a.life.freeTail(p, max(size, uintptr(1)), rt.CallerPC()) {
			rt.ResumeAllocFreeExit()
		}
	}
}

// A ChunkMark records how much of a Chunked is in use; see Chunked.Mark.
type ChunkMark struct {
	cur      *chunk
	off      uintptr
	before   uintptr
	identity uint64
	reset    uint64
	sequence uint64
	end      uintptr
	entries  *entryBlock
	used     int // entries used in the block entries
}

// Mark returns the current fill level. Release(m) frees everything
// allocated after it, leaving earlier allocations intact.
func (a *Chunked) Mark() ChunkMark {
	m := ChunkMark{a.cur, a.off, a.before, a.life.identity, a.life.reset, a.life.sequence(), a.life.end(), a.entries, 0}
	if a.entries != nil {
		m.used = a.entries.used
	}
	return m
}

// Release frees every allocation made since m was taken: chunks taken since
// then go back to the spare list (or, if larger than standard, to the
// parent), and the chunk that was current when m was taken is cut back to
// its level then. m must not be stale (taken before a Reset, or before a
// Release that went further back); Release panics when it can tell.
func (a *Chunked) Release(m ChunkMark) {
	if a.release(m, rt.CallerPC()) {
		rt.ResumeAllocFreeExit()
	}
}

// release is Release for the call that returns to pc.
func (a *Chunked) release(m ChunkMark, pc uintptr) bool {
	if !a.life.valid(m.identity, m.reset, m.sequence, m.end) || genCheck && m.cur != nil && !rt.GenRegisteredLive(unsafe.Pointer(m.cur)) {
		panic("arena: Release of a stale mark")
	}
	a.runEntries(m.entries, m.used)
	a.checkLive()
	a.entries = m.entries
	if m.entries != nil {
		m.entries.used = m.used
	}
	exiting := a.life.reclaim(m.sequence, pc, false)
	for a.cur != m.cur {
		c := a.cur
		if c == nil {
			panic("arena: Release of a stale mark")
		}
		a.cur = c.prev
		if a.recycle(c, 0, a.off, pc) {
			exiting = true
		}
		// How full the older chunk was when it was left is not recorded;
		// treat it as full, so poisoning covers everything after the mark.
		if a.cur != nil {
			a.off = a.cur.size
		}
	}
	if a.off < m.off {
		panic("arena: Release of a stale mark")
	}
	if a.cur != nil && a.poison {
		fill(unsafe.Add(a.cur.data(), m.off), a.off-m.off)
	}
	a.off = m.off
	a.before = m.before
	return exiting
}

// recycle takes back a chunk that is no longer current, of which bytes
// [from, to) were in use, for the call that returns to pc: a standard chunk
// goes on the spare list, a larger one back to the parent.
func (a *Chunked) recycle(c *chunk, from, to, pc uintptr) bool {
	if a.poison {
		fill(unsafe.Add(c.data(), from), to-from)
	}
	if c.size == a.chunkSize {
		c.prev = a.spare
		a.spare = c
		return false
	}
	return a.freeChunk(c)
}

// freeChunk returns c to the parent allocator.
func (a *Chunked) freeChunk(c *chunk) bool {
	if isEpoch(c) {
		return a.freeEpochChunk(c)
	}
	a.held -= chunkHeader + c.size
	return rt.CatchAllocFree(a.parent, unsafe.Pointer(c), chunkHeader+c.size, 16)
}

// Reset frees every allocation. Standard chunks are kept for reuse.
func (a *Chunked) Reset() {
	if a.life.identity == 0 {
		return
	}
	a.runEntries(nil, 0)
	a.checkLive()
	a.life.resetMarks()
	if a.release(ChunkMark{identity: a.life.identity, reset: a.life.reset}, rt.CallerPC()) {
		rt.ResumeAllocFreeExit()
	}
}

// checkLive panics while an allocation made through Alloc is not freed
// (language spec section 18.9, Live allocations).
func (a *Chunked) checkLive() {
	if atomic.LoadInt64(&a.live) != 0 {
		panic("arena has live allocations")
	}
}

// Trim returns the spare chunks to the parent allocator.
func (a *Chunked) Trim() {
	if a.trim() {
		rt.ResumeAllocFreeExit()
	}
}

func (a *Chunked) trim() bool {
	exiting := false
	for a.spare != nil {
		c := a.spare
		a.spare = c.prev
		if a.freeChunk(c) {
			exiting = true
		}
	}
	return exiting
}

// Drop frees every allocation and returns all chunks to the parent
// allocator. The arena is empty afterwards and may be used again.
func (a *Chunked) Drop() {
	if a.life.identity == 0 {
		return
	}
	a.runEntries(nil, 0)
	a.checkLive()
	a.life.resetMarks()
	exiting := a.release(ChunkMark{identity: a.life.identity, reset: a.life.reset}, rt.CallerPC())
	if a.trim() {
		exiting = true
	}
	if exiting {
		rt.ResumeAllocFreeExit()
	}
}

// Used returns the number of bytes allocated, including alignment padding
// and the space left unused at the end of full chunks.
func (a *Chunked) Used() int {
	if a.cur == nil {
		return 0
	}
	return int(a.before + a.off)
}

// Held returns the number of bytes the arena has taken from its parent
// allocator and not returned, chunk headers included.
func (a *Chunked) Held() int { return int(a.held) }

// SetPoison turns poisoning on or off: with it on, memory freed by Reset
// and Release is overwritten with 0xDD before it is reused or returned, so
// a view that outlived its allocation reads poison instead of plausible
// data. Allocations are zeroed either way.
func (a *Chunked) SetPoison(on bool) { a.poison = on }

func fill(p unsafe.Pointer, n uintptr) {
	b := unsafe.Slice((*byte)(p), n)
	for i := range b {
		b[i] = 0xDD
	}
}
