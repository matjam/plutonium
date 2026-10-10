package arena

import (
	"pluto/alloc"
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
// A Chunked owns its chunks, so it is an owner (language spec section 6.4):
// it must not be copied after its first allocation, only moved, and it is
// released by Drop. Until the owner keyword reaches the compiler this is a
// convention; the declaration below then becomes `type Chunked owner struct`.
//
// A Chunked is not safe for concurrent use.
type Chunked struct {
	parent    alloc.Allocator
	chunkSize uintptr // data bytes in a standard chunk
	cur       *chunk  // the chunk being filled; older chunks follow prev
	off       uintptr // bytes used in cur
	before    uintptr // bytes used in the chunks older than cur
	spare     *chunk  // standard chunks kept for reuse, linked by prev
	held      uintptr // bytes taken from the parent, headers included
	poison    bool
}

// A chunk is a block from the parent allocator: this header, then size
// bytes of data at chunkHeader.
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
	return Chunked{parent: parent, chunkSize: (chunkSize + 15) &^ 15}
}

func (c *chunk) data() unsafe.Pointer { return unsafe.Add(unsafe.Pointer(c), chunkHeader) }

// Alloc returns size zeroed bytes aligned to align (a power of two), or nil
// if the parent allocator is out of memory.
func (a *Chunked) Alloc(size, align uintptr) unsafe.Pointer {
	if align == 0 {
		align = 1
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
	start := uintptr(c.data()) + a.off
	pad := (align - start&(align-1)) & (align - 1)
	if pad > c.size-a.off || size > c.size-a.off-pad {
		return nil
	}
	p := unsafe.Add(c.data(), a.off+pad)
	a.off += pad + size
	if size > 0 {
		// The chunk may hold data from before a Reset or Release.
		clear(unsafe.Slice((*byte)(p), size))
	}
	return p
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
		p := a.parent.Alloc(chunkHeader+size, 16)
		if p == nil {
			return false
		}
		c = (*chunk)(p)
		c.size = size
		a.held += chunkHeader + size
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
	if c := a.cur; c != nil && p != nil && uintptr(p)+size == uintptr(c.data())+a.off && uintptr(p) >= uintptr(c.data()) {
		a.off = uintptr(p) - uintptr(c.data())
	}
}

// A ChunkMark records how much of a Chunked is in use; see Chunked.Mark.
type ChunkMark struct {
	cur    *chunk
	off    uintptr
	before uintptr
}

// Mark returns the current fill level. Release(m) frees everything
// allocated after it, leaving earlier allocations intact.
func (a *Chunked) Mark() ChunkMark { return ChunkMark{a.cur, a.off, a.before} }

// Release frees every allocation made since m was taken: chunks taken since
// then go back to the spare list (or, if larger than standard, to the
// parent), and the chunk that was current when m was taken is cut back to
// its level then. m must not be stale (taken before a Reset, or before a
// Release that went further back); Release panics when it can tell.
func (a *Chunked) Release(m ChunkMark) {
	for a.cur != m.cur {
		c := a.cur
		if c == nil {
			panic("arena: Release of a stale mark")
		}
		a.cur = c.prev
		a.recycle(c, 0, a.off)
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
}

// recycle takes back a chunk that is no longer current, of which bytes
// [from, to) were in use: a standard chunk goes on the spare list, a larger
// one back to the parent.
func (a *Chunked) recycle(c *chunk, from, to uintptr) {
	if a.poison {
		fill(unsafe.Add(c.data(), from), to-from)
	}
	if c.size == a.chunkSize {
		c.prev = a.spare
		a.spare = c
		return
	}
	a.held -= chunkHeader + c.size
	a.parent.Free(unsafe.Pointer(c), chunkHeader+c.size, 16)
}

// Reset frees every allocation. Standard chunks are kept for reuse.
func (a *Chunked) Reset() { a.Release(ChunkMark{}) }

// Trim returns the spare chunks to the parent allocator.
func (a *Chunked) Trim() {
	for a.spare != nil {
		c := a.spare
		a.spare = c.prev
		a.held -= chunkHeader + c.size
		a.parent.Free(unsafe.Pointer(c), chunkHeader+c.size, 16)
	}
}

// Drop frees every allocation and returns all chunks to the parent
// allocator. The arena is empty afterwards and may be used again.
func (a *Chunked) Drop() {
	a.Reset()
	a.Trim()
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
