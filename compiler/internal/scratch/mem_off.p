//pu:build !gencheck

package scratch

import (
	"pluto/alloc"
	"unsafe"
)

// The scratch memory of a level or a Worker is a bump (scratch.p): raw
// memory with no record of each allocation.
type mem = bump

type memMark = bumpMark

func newMem(parent alloc.Allocator) mem { return bump{parent: parent} }

const (
	regionFirst = 4 << 10
	regionMax   = 1 << 20
)

// regionMem is a Region's memory: chunks taken from the heap and filled in
// order, each beginning with a link to the one taken before it and its own
// size. It is the Region's first field, so its address is the Region's.
type regionMem struct {
	chunks unsafe.Pointer // the chunk being filled, linking to the older ones
	off    uintptr        // bytes of it in use
	size   uintptr        // its size
	used   uintptr        // bytes of all its chunks
}

func (m *regionMem) init() {}

func (m *regionMem) Alloc(size, align uintptr) unsafe.Pointer {
	if size == 0 {
		size = 1
	}
	if align < 8 {
		align = 8
	}
	base := uintptr(m.chunks)
	off := (base+m.off+align-1)&^(align-1) - base
	if m.chunks == nil || off+size > m.size {
		if !m.grow(size + align) {
			return nil
		}
		base = uintptr(m.chunks)
		off = (base+m.off+align-1)&^(align-1) - base
	}
	m.off = off + size
	return unsafe.Add(m.chunks, off)
}

// grow makes a chunk with room for need bytes the current one: twice the
// size of the last, up to regionMax, or larger if need is.
func (m *regionMem) grow(need uintptr) bool {
	n := uintptr(regionFirst)
	if m.size > 0 {
		n = m.size * 2
		if n > regionMax {
			n = regionMax
		}
	}
	if need+bumpHeader > n {
		n = need + bumpHeader
	}
	n = (n + pageSize - 1) &^ (pageSize - 1)
	var c unsafe.Pointer
	if check {
		c = mapChunk(n, pageSize)
	} else {
		c = heap.Alloc(n, 16)
	}
	if c == nil {
		return false
	}
	*(*unsafe.Pointer)(c) = m.chunks
	*(*uintptr)(unsafe.Add(c, 8)) = n
	m.chunks, m.off, m.size = c, bumpHeader, n
	m.used += n
	regionUsed += n
	addSpan(regionSpan{uintptr(c), uintptr(c) + n, uintptr(unsafe.Pointer(m))})
	return true
}

// Free does nothing: a region's memory is freed all at once by Release.
func (m *regionMem) Free(p unsafe.Pointer, size, align uintptr) {}

func (m *regionMem) bytes() uintptr { return m.used }

// release frees m's chunks.
func (m *regionMem) release() {
	c := m.chunks
	for c != nil {
		next := *(*unsafe.Pointer)(c)
		n := *(*uintptr)(unsafe.Add(c, 8))
		removeSpan(uintptr(c))
		if check {
			sealChunk(c, n)
		} else {
			heap.Free(c, n, 16)
		}
		c = next
	}
	regionUsed -= m.used
	m.chunks, m.off, m.size, m.used = nil, 0, 0, 0
}

// homeChunk is the size of the chunks a home takes from the heap: most
// functions' code fits in a few.
const homeChunk = 32 << 10

// A home is memory that DoHome keeps a step's results in: chunks taken
// from the heap and filled in order, freed together by drop. Each chunk
// begins with a link to the one taken before it and its own size. In
// check mode (PLUTO_SCRATCH_CHECK) the chunks are pages of their own,
// which drop makes inaccessible, so that a pointer to an older version
// faults where it is used.
type home struct {
	chunks unsafe.Pointer // the chunk being filled, linking to the older ones
	off    uintptr        // bytes of it in use
	size   uintptr        // its size
}

const homeHeader = 16

func (h *home) init() {}

func (h *home) Alloc(size, align uintptr) unsafe.Pointer {
	if align < 8 {
		align = 8
	}
	off := (h.off + align - 1) &^ (align - 1)
	if h.chunks == nil || off+size > h.size {
		n := uintptr(homeChunk)
		if need := homeHeader + size + align; need > n {
			n = need
		}
		var c unsafe.Pointer
		if check {
			n = (n + pageSize - 1) &^ (pageSize - 1)
			c = mapChunk(n, pageSize)
		} else {
			c = heap.Alloc(n, 16)
		}
		if c == nil {
			return nil
		}
		*(*unsafe.Pointer)(c) = h.chunks
		*(*uintptr)(unsafe.Add(c, 8)) = n
		h.chunks, h.size = c, n
		off = (homeHeader + align - 1) &^ (align - 1)
	}
	h.off = off + size
	return unsafe.Add(h.chunks, off)
}

// Free does nothing: a home's memory is freed all at once by drop.
func (h *home) Free(p unsafe.Pointer, size, align uintptr) {}

// empty reports whether nothing was allocated from h.
func (h *home) empty() bool { return h.chunks == nil }

// drop frees h's chunks.
func (h *home) drop() {
	c := h.chunks
	for c != nil {
		next := *(*unsafe.Pointer)(c)
		n := *(*uintptr)(unsafe.Add(c, 8))
		if check {
			sealChunk(c, n)
		} else {
			heap.Free(c, n, 16)
		}
		c = next
	}
	h.chunks, h.off, h.size = nil, 0, 0
}
