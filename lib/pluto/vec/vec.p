// Package vec provides Vec, a growable slice that records its allocator.
//
// Plutonium's append works only within a slice's capacity, because a slice
// does not know where its memory came from. A Vec does: when it runs out of
// room it allocates a buffer twice as large from its allocator, copies the
// elements and frees the old buffer.
//
//	v := vec.New[int](alloc.Default())
//	defer v.Free()
//	for i := 0; i < 100; i++ {
//		v.Push(i)
//	}
//	for _, x := range v.Slice() { ... }
//
// A Vec must not be copied after first use; pass a *Vec. Slices returned by
// Slice and pointers returned by Ptr are invalidated by any call that grows
// the Vec (Push, Append, Insert, Reserve) and by Free.
package vec

import (
	"pluto/alloc"
	"unsafe"
)

// A Vec is a growable sequence of T whose storage comes from an allocator.
// The zero Vec is empty and has no allocator: it can be read but must be
// given one with Init before it can grow.
type Vec[T any] struct {
	data []T
	a    alloc.Allocator
}

// New returns an empty Vec that allocates from a. It does not allocate
// until the first element is added.
func New[T any](a alloc.Allocator) Vec[T] {
	return Vec[T]{a: a}
}

// WithCapacity returns an empty Vec that allocates from a, with room for n
// elements already allocated.
func WithCapacity[T any](a alloc.Allocator, n int) Vec[T] {
	v := Vec[T]{a: a}
	v.Reserve(n)
	return v
}

// Init sets the allocator of a zero Vec. It panics if v already holds
// storage from another allocator.
func (v *Vec[T]) Init(a alloc.Allocator) {
	if cap(v.data) > 0 && v.a != a {
		panic("vec: Init on a Vec that holds storage")
	}
	v.a = a
}

// Allocator returns the allocator v grows from.
func (v *Vec[T]) Allocator() alloc.Allocator { return v.a }

// Len returns the number of elements.
func (v *Vec[T]) Len() int { return len(v.data) }

// Cap returns the number of elements v can hold without growing.
func (v *Vec[T]) Cap() int { return cap(v.data) }

// Slice returns the elements as a slice. It is a view of v's storage, valid
// until v grows or is freed. Its capacity is its length, so append on it
// cannot write past the elements.
func (v *Vec[T]) Slice() []T { return v.data[:len(v.data):len(v.data)] }

// At returns element i. It panics if i is out of range.
func (v *Vec[T]) At(i int) T { return v.data[i] }

// Set sets element i to x. It panics if i is out of range.
func (v *Vec[T]) Set(i int, x T) { v.data[i] = x }

// Ptr returns a pointer to element i, valid until v grows or is freed.
func (v *Vec[T]) Ptr(i int) *T { return &v.data[i] }

// Last returns the last element and true, or the zero T and false if v is
// empty.
func (v *Vec[T]) Last() (T, bool) {
	if len(v.data) == 0 {
		var zero T
		return zero, false
	}
	return v.data[len(v.data)-1], true
}

// Push appends x.
func (v *Vec[T]) Push(x T) {
	if len(v.data) == cap(v.data) {
		v.grow(1)
	}
	v.data = append(v.data, x)
}

// Append appends the elements of xs. xs must not be a view of v itself.
func (v *Vec[T]) Append(xs ...T) {
	if cap(v.data)-len(v.data) < len(xs) {
		v.grow(len(xs))
	}
	v.data = append(v.data, xs...)
}

// Pop removes and returns the last element. It returns the zero T and false
// if v is empty.
func (v *Vec[T]) Pop() (T, bool) {
	var zero T
	n := len(v.data)
	if n == 0 {
		return zero, false
	}
	x := v.data[n-1]
	v.data[n-1] = zero
	v.data = v.data[:n-1]
	return x, true
}

// Insert inserts x at index i, shifting later elements up. It panics if i
// is not in [0, Len()].
func (v *Vec[T]) Insert(i int, x T) {
	n := len(v.data)
	if i < 0 || i > n {
		panic("vec: Insert index out of range")
	}
	if n == cap(v.data) {
		v.grow(1)
	}
	v.data = v.data[:n+1]
	copy(v.data[i+1:], v.data[i:n])
	v.data[i] = x
}

// Remove removes and returns element i, shifting later elements down. It
// panics if i is out of range.
func (v *Vec[T]) Remove(i int) T {
	x := v.data[i]
	n := len(v.data)
	copy(v.data[i:], v.data[i+1:])
	var zero T
	v.data[n-1] = zero
	v.data = v.data[:n-1]
	return x
}

// SwapRemove removes and returns element i, replacing it with the last
// element. It does not preserve order but takes constant time. It panics if
// i is out of range.
func (v *Vec[T]) SwapRemove(i int) T {
	x := v.data[i]
	n := len(v.data)
	v.data[i] = v.data[n-1]
	var zero T
	v.data[n-1] = zero
	v.data = v.data[:n-1]
	return x
}

// Reserve makes room for at least n more elements without further
// allocation.
func (v *Vec[T]) Reserve(n int) {
	if n < 0 {
		panic("vec: negative Reserve")
	}
	if cap(v.data)-len(v.data) < n {
		v.realloc(len(v.data) + n)
	}
}

// Truncate shortens v to n elements. It panics if n is negative or greater
// than Len.
func (v *Vec[T]) Truncate(n int) {
	if n < 0 || n > len(v.data) {
		panic("vec: Truncate out of range")
	}
	clear(v.data[n:])
	v.data = v.data[:n]
}

// Clear removes all elements, keeping the storage.
func (v *Vec[T]) Clear() { v.Truncate(0) }

// Free returns v's storage to its allocator and leaves v empty. The
// allocator is kept, so v can be reused.
func (v *Vec[T]) Free() {
	v.release()
	v.data = nil
}

// grow makes room for at least n more elements, at least doubling the
// capacity.
func (v *Vec[T]) grow(n int) {
	c := 2 * cap(v.data)
	if c < len(v.data)+n {
		c = len(v.data) + n
	}
	if c < 4 {
		c = 4
	}
	v.realloc(c)
}

// realloc moves the elements into new storage of capacity c.
func (v *Vec[T]) realloc(c int) {
	if v.a == nil {
		panic("vec: Vec has no allocator (create it with vec.New or call Init)")
	}
	var zero T
	p := v.a.Alloc(storageSize[T](c), unsafe.Alignof(zero))
	if p == nil {
		panic("vec: out of memory")
	}
	nd := unsafe.Slice((*T)(p), c)[:len(v.data)]
	copy(nd, v.data)
	v.release()
	v.data = nd
}

// release frees v's storage, if any, without changing v.data.
func (v *Vec[T]) release() {
	if cap(v.data) > 0 {
		var zero T
		v.a.Free(unsafe.Pointer(unsafe.SliceData(v.data)), storageSize[T](cap(v.data)), unsafe.Alignof(zero))
	}
}

// storageSize returns the bytes allocated for c elements of T (at least 1).
func storageSize[T any](c int) uintptr {
	var zero T
	size := uintptr(c) * unsafe.Sizeof(zero)
	if size == 0 {
		size = 1
	}
	return size
}
