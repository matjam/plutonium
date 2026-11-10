package arena

import (
	"pluto/rt"
	"unsafe"
)

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

// New allocates a zeroed T in the arena and returns a pointer to it. For a
// T with drop glue it records a drop entry (language spec section 18.9,
// Drop entries). It panics if the arena has no room for the value or for
// its entry.
func (a *Fixed) New[T owner any]() *T {
	var zero *T
	size, align := unsafe.Sizeof(*zero), unsafe.Alignof(*zero)
	if rt.NeedsDrop[T]() {
		return (*T)(must(a.allocEntry(size, align, 1, dropValues[T])))
	}
	return (*T)(must(a.alloc(size, align, 0)))
}

// Slice allocates n zeroed elements of T in the arena. For a T with drop
// glue and n above zero it records a drop entry. It panics if n is negative
// or the arena has no room.
func (a *Fixed) Slice[T owner any](n int) []T {
	var zero *T
	size, align := unsafe.Sizeof(*zero), unsafe.Alignof(*zero)
	if n < 0 || uintptr(n) > ^uintptr(0)/max(size, uintptr(1)) {
		panic("arena: Slice with an invalid length")
	}
	if n == 0 {
		return nil
	}
	if rt.NeedsDrop[T]() {
		return unsafe.Slice((*T)(must(a.allocEntry(uintptr(n)*size, align, uintptr(n), dropValues[T]))), n)
	}
	return unsafe.Slice((*T)(must(a.alloc(uintptr(n)*size, align, 0))), n)
}

// New allocates a zeroed T in the arena and returns a pointer to it. For a
// T with drop glue it records a drop entry (language spec section 18.9,
// Drop entries). It panics if the parent allocator is out of memory.
func (a *Chunked) New[T owner any]() *T {
	var zero *T
	size, align := unsafe.Sizeof(*zero), unsafe.Alignof(*zero)
	if rt.NeedsDrop[T]() {
		return (*T)(must(a.allocEntry(size, align, 1, dropValues[T])))
	}
	return (*T)(must(a.alloc(size, align)))
}

// Slice allocates n zeroed elements of T in the arena. For a T with drop
// glue and n above zero it records a drop entry. It panics if n is negative
// or the parent allocator is out of memory.
func (a *Chunked) Slice[T owner any](n int) []T {
	var zero *T
	size, align := unsafe.Sizeof(*zero), unsafe.Alignof(*zero)
	if n < 0 || uintptr(n) > ^uintptr(0)/max(size, uintptr(1)) {
		panic("arena: Slice with an invalid length")
	}
	if n == 0 {
		return nil
	}
	if rt.NeedsDrop[T]() {
		return unsafe.Slice((*T)(must(a.allocEntry(uintptr(n)*size, align, uintptr(n), dropValues[T]))), n)
	}
	return unsafe.Slice((*T)(must(a.alloc(uintptr(n)*size, align))), n)
}

func must(p unsafe.Pointer) unsafe.Pointer {
	if p == nil {
		panic("arena: out of memory")
	}
	return p
}
