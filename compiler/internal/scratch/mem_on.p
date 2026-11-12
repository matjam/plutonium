//pu:build gencheck

package scratch

import (
	"pluto/alloc"
	"pluto/arena"
	"unsafe"
)

// With generation checks the scratch memory is pluto/arena's Chunked, which
// tags its allocations and invalidates them when it releases them. A bump
// would be an ordinary custom allocator there: each allocation registered
// on its own and never unregistered, as Release frees no allocation one by
// one, so the registrations of a chunk piled up and every lookup in it
// walked them. A Region and a home are Chunked too, for the same reason.
// They come from NewChunkedUnsafe, which keeps no count of live
// allocations: the code that allocates in scratch memory never frees, and
// its memory is taken back in bulk.
type mem = arena.Chunked

type memMark = arena.ChunkMark

func newMem(parent alloc.Allocator) mem { return arena.NewChunkedUnsafe(chunkSize, parent) }

// The data sizes of the chunks of a Region and of a home. With generation
// checks a chunk also holds 8K of metadata before its data, and is asked
// for aligned to 64K: these make it 64K and 32K.
const (
	regionChunk  = 56 << 10
	homeChunk    = 24 << 10
	chunkRequest = 4 << 10 // a smaller request is for Chunked's record of an allocation
)

// regionMem is a Region's memory: a Chunked whose chunks come from src,
// which records them for HeapFor. Its Alloc and Free are the Chunked's,
// which the compiler does not wrap, so its allocations are registered once,
// by the Chunked, and release ends each registration. It is the Region's
// first field, so its address is the Region's.
type regionMem owner struct {
	arena.Chunked
	src chunkSource
}

func (m *regionMem) init() {
	m.src = chunkSource{region: uintptr(unsafe.Pointer(m))}
	m.Chunked = arena.NewChunkedUnsafe(regionChunk, &m.src)
}

func (m *regionMem) bytes() uintptr { return m.src.used }

// release frees m's allocations and returns its chunks to the heap.
func (m *regionMem) release() {
	m.Chunked.Reset()
	m.Chunked.Trim()
}

// A home is memory that DoHome keeps a step's results in, freed together
// by drop: a Chunked whose chunks come from homeSource.
type home owner struct {
	arena.Chunked
	heap alloc.Allocator // for Heap
}

var homeSource chunkSource

func (h *home) init() { h.Chunked = arena.NewChunkedUnsafe(homeChunk, &homeSource) }

// empty reports whether nothing was allocated from h.
func (h *home) empty() bool { return h.Chunked.Held() == 0 }

// drop frees h's allocations and returns its chunks to the heap.
func (h *home) drop() {
	h.Chunked.Reset()
	h.Chunked.Trim()
}

// A chunkSource is the parent of a Region's or a home's Chunked. A chunk
// comes from the heap, or in check mode (PLUTO_SCRATCH_CHECK) is pages of
// its own, which Free makes inaccessible, so that a pointer into a freed
// chunk faults where it is used. A Region's chunks are recorded for
// HeapFor. The Chunked's records of its allocations come from the heap.
type chunkSource struct {
	region uintptr // the Region whose chunks these are, or 0
	used   uintptr // bytes of the chunks handed out
}

func (s *chunkSource) Alloc(size, align uintptr) unsafe.Pointer {
	if size < chunkRequest {
		return heap.Alloc(size, align)
	}
	var c unsafe.Pointer
	if check {
		c = mapChunk((size+pageSize-1)&^(pageSize-1), max(align, uintptr(pageSize)))
	} else {
		c = heap.Alloc(size, align)
	}
	if c == nil {
		return nil
	}
	s.used += size
	if s.region != 0 {
		regionUsed += size
		addSpan(regionSpan{uintptr(c), uintptr(c) + size, s.region})
	}
	return c
}

func (s *chunkSource) Free(p unsafe.Pointer, size, align uintptr) {
	if size < chunkRequest {
		heap.Free(p, size, align)
		return
	}
	s.used -= size
	if s.region != 0 {
		regionUsed -= size
		removeSpan(uintptr(p))
	}
	if check {
		sealChunk(p, (size+pageSize-1)&^(pageSize-1))
	} else {
		heap.Free(p, size, align)
	}
}
