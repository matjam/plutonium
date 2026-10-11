// Package arena provides bump allocators: Arena, over a caller-provided
// buffer, and Chunked, which grows by taking chunks from a parent allocator
// and supports Mark/Release (chunked.p).
//
// An Arena hands out memory from the front of its buffer and never frees
// individual allocations (except the most recent one, see Free); Reset
// frees everything at once. It implements alloc.Allocator, so it can back
// new, make, vec.Vec, hash.Map or any other allocating code:
//
//	var mem [64 << 10]byte // package-level
//	var a arena.Arena
//
//	a = arena.New(mem[:])
//	defer alloc.Use(&a).Restore()
//
// An arena in a function's own frame, over a buffer in that frame, can back
// code that function calls (vec.New[int](&a)), but cannot become the default
// allocator: the default is a package variable, and a pointer into a frame
// must not be stored there (language spec section 18.3).
//
// An Arena is not safe for concurrent use.
package arena

import (
	"pluto/alloc"
	"unsafe"
)

// An Arena is a bump allocator over a fixed buffer.
type Arena struct {
	base unsafe.Pointer // start of the buffer
	size uintptr        // length of the buffer
	off  uintptr        // bytes in use
}

// The Arena satisfies alloc.Allocator through its pointer.
var _ alloc.Allocator = (*Arena)(nil)

// New returns an Arena that allocates from buf. The caller must not use buf
// while the Arena is in use.
func New(buf []byte) Arena {
	if len(buf) == 0 {
		return Arena{}
	}
	return Arena{base: unsafe.Pointer(unsafe.SliceData(buf)), size: uintptr(len(buf))}
}

// Alloc returns size zeroed bytes aligned to align (a power of two), or nil
// if the arena does not have room.
func (a *Arena) Alloc(size, align uintptr) unsafe.Pointer {
	if align == 0 {
		align = 1
	}
	start := uintptr(a.base) + a.off
	pad := (align - start&(align-1)) & (align - 1)
	if pad > a.size-a.off || size > a.size-a.off-pad {
		return nil
	}
	p := unsafe.Add(a.base, a.off+pad)
	a.off += pad + size
	if size > 0 {
		// The buffer may hold data from before the last Reset.
		clear(unsafe.Slice((*byte)(p), size))
	}
	return p
}

// Free releases memory returned by Alloc. Arena memory is released all at
// once by Reset, so Free does nothing, except that freeing the most recent
// allocation gives its space back (which makes a growing vec.Vec or
// strings.Builder on an arena cheaper).
func (a *Arena) Free(p unsafe.Pointer, size, align uintptr) {
	if p != nil && uintptr(p)+size == uintptr(a.base)+a.off {
		a.off = uintptr(p) - uintptr(a.base)
	}
}

// Reset frees every allocation, making the whole buffer available again.
func (a *Arena) Reset() { a.off = 0 }

// A Mark records how much of an arena is in use; see Arena.Mark.
type Mark struct{ off uintptr }

// Mark returns the current fill level. Release(m) frees everything allocated
// after it, leaving earlier allocations intact.
func (a *Arena) Mark() Mark { return Mark{a.off} }

// Release frees every allocation made since m was taken. It panics if m is
// past the current fill level (it was taken before a Reset or Release that
// went further back).
func (a *Arena) Release(m Mark) {
	if m.off > a.off {
		panic("arena: Release of a stale mark")
	}
	a.off = m.off
}

// Used returns the number of bytes allocated, including alignment padding.
func (a *Arena) Used() int { return int(a.off) }

// Available returns the number of bytes not yet allocated.
func (a *Arena) Available() int { return int(a.size - a.off) }

// Cap returns the size of the arena's buffer.
func (a *Arena) Cap() int { return int(a.size) }
