package arena

import "unsafe"

// Typed allocation in an arena (language spec section 13.9). Memory that
// only an arena owns is handed out as views: they need no owner of their
// own, and stay valid until the arena is reset, released past them, or
// dropped.

// New allocates a zeroed T in the arena and returns a pointer to it. It
// panics if the arena is out of memory.
func (a *Arena) New[T any]() *T {
	var zero T
	return (*T)(must(a.Alloc(unsafe.Sizeof(zero), unsafe.Alignof(zero))))
}

// Slice allocates n zeroed elements of T in the arena. It panics if n is
// negative or the arena is out of memory.
func (a *Arena) Slice[T any](n int) []T {
	var zero T
	if n < 0 {
		panic("arena: Slice with a negative length")
	}
	return unsafe.Slice((*T)(must(a.Alloc(uintptr(n)*unsafe.Sizeof(zero), unsafe.Alignof(zero)))), n)
}

// New allocates a zeroed T in the arena and returns a pointer to it. It
// panics if the parent allocator is out of memory.
func (a *Chunked) New[T any]() *T {
	var zero T
	return (*T)(must(a.Alloc(unsafe.Sizeof(zero), unsafe.Alignof(zero))))
}

// Slice allocates n zeroed elements of T in the arena. It panics if n is
// negative or the parent allocator is out of memory.
func (a *Chunked) Slice[T any](n int) []T {
	var zero T
	if n < 0 {
		panic("arena: Slice with a negative length")
	}
	return unsafe.Slice((*T)(must(a.Alloc(uintptr(n)*unsafe.Sizeof(zero), unsafe.Alignof(zero)))), n)
}

func must(p unsafe.Pointer) unsafe.Pointer {
	if p == nil {
		panic("arena: out of memory")
	}
	return p
}
