// Package arena provides fixed-capacity and growing bump allocators.
// Fixed and Chunked own their private backing and check allocation lifetimes
// in checked builds. NewUnsafe instead borrows a caller-provided buffer;
// its Arena has manually managed lifetimes (language spec section 18.12).
// None of the allocators are safe for concurrent use.
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

// NewUnsafe borrows buf without allocating private payload. The caller must
// keep buf alive and exclusively available, avoid overlapping cursor copies,
// and end all views and count obligations before Free, Reset or Release.
func NewUnsafe(buf []byte) Arena {
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

// Free takes nothing back, not even the most recent allocation: an arena
// takes memory back in bulk only, through Reset and Release (language spec
// section 18.9, D46).
func (a *Arena) Free(p unsafe.Pointer, size, align uintptr) {}

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
