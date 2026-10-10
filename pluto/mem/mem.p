package mem

import "unsafe"

// Arena is a simple bump-pointer allocator.
// It allocates memory from a fixed backing buffer; memory is never individually freed.
// To free all allocations, reset the arena.
type Arena struct {
	buf  []byte
	pos  uintptr
}

// NewArena creates an Arena backed by the given buffer.
// The buffer must be pre-allocated; Arena does not allocate.
func NewArena(buf []byte) Arena {
	return Arena{buf: buf, pos: 0}
}

// Alloc allocates n bytes from the arena, aligned to align bytes.
// Returns nil if the arena is full.
func (a *Arena) Alloc(n, align uintptr) unsafe.Pointer {
	// Align current position
	pos := (a.pos + align - 1) &^ (align - 1)
	if pos+n > uintptr(len(a.buf)) {
		return nil // out of memory
	}
	ptr := unsafe.Pointer(&a.buf[pos])
	a.pos = pos + n
	return ptr
}

// Reset resets the arena, making all previously allocated memory reusable.
func (a *Arena) Reset() {
	a.pos = 0
}

// Used returns the number of bytes currently allocated.
func (a *Arena) Used() uintptr {
	return a.pos
}

// Remaining returns the number of bytes still available.
func (a *Arena) Remaining() uintptr {
	return uintptr(len(a.buf)) - a.pos
}
