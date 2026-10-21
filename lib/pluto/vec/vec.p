// Package vec provides Vec, a growable slice that records its allocator,
// and Fixed, a bounded vector over storage the caller provides.
//
// A Plutonium slice cannot grow, because it does not know where its memory
// came from (language spec section 7.5). A Vec does: when it runs out of
// room it allocates a buffer twice as large from its allocator, copies the
// elements and frees the old buffer.
//
//	v := vec.New[int]() // or vec.New[int](arena)
//	defer v.Free()
//	for i := 0; i < 100; i++ {
//		v.Push(i)
//	}
//	for _, x := range v.Slice() { ... }
//	for i, x := range v { ... } // v.All(), as over a slice
//
// v[i], v[i] = x and len(v) work as on a slice, through the methods Index,
// SetIndex and Len (language spec section 11.11).
//
// A Vec must not be copied after first use; pass a *Vec. Slices returned by
// Slice and pointers returned by Ptr are invalidated by any call that grows
// the Vec (Push, Append, Insert, Replace, Reserve) and by Free.
package vec

import (
	"pluto/alloc"
	"pluto/iter"
	"pluto/rt"
	"unsafe"
)

// A Vec is a growable sequence of T whose storage comes from an allocator.
// The zero Vec is empty and ready to use: unless Init gives it an allocator
// first, it takes the default allocator the first time it grows.
//
// A Vec owns its storage, so it is an owner (language spec section 7.18):
// it moves instead of being copied, and Drop releases it. Clone makes an
// independent copy.
//
// T may be an owner (language spec section 10.9): the Vec then owns its
// elements. Push, Set and SetIndex move an element in, dropping the one a
// Set replaces; Pop, Remove and SwapRemove move one out; Truncate, Clear,
// Free and Drop drop the elements in index order. Ptr lends an element, and
// the iterators yield pointers to the elements (rt.View). The methods that
// copy an element, At, Index, Last, Clone and Map, do not compile for an
// owner, since the copy would be a second owner.
type Vec[T owner any] owner struct {
	data []T
	a    alloc.Allocator
}

// New returns an empty Vec that allocates from a, by default the default
// allocator. It does not allocate until the first element is added.
func New[T owner any](a alloc.Allocator = alloc.Default()) Vec[T] {
	return Vec[T]{a: a}
}

// WithCapacity returns an empty Vec that allocates from a (by default the
// default allocator), with room for n elements already allocated.
func WithCapacity[T owner any](n int, a alloc.Allocator = alloc.Default()) Vec[T] {
	v := Vec[T]{a: a}
	v.Reserve(n)
	return v
}

// Make returns a Vec of n zero elements that allocates from a (by default
// the default allocator): the form of Go's make([]T, n). For Go's
// make([]T, 0, c), use WithCapacity.
func Make[T owner any](n int, a alloc.Allocator = alloc.Default()) Vec[T] {
	if n < 0 {
		panic("vec: negative Make length")
	}
	v := Vec[T]{a: a}
	if n > 0 {
		v.realloc(n)
		v.data = v.data[:n] // allocators return zeroed memory
	}
	return v
}

// Init sets the allocator of a zero Vec: a, by default the default
// allocator. It panics if v already holds storage from another allocator.
func (v *Vec[T]) Init(a alloc.Allocator = alloc.Default()) {
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
// until v grows or is freed. Its capacity is its length, so reslicing it
// cannot reach past the elements.
func (v *Vec[T]) Slice() []T { return v.data[:len(v.data):len(v.data)] }

// At returns element i. It panics if i is out of range.
func (v *Vec[T]) At(i int) T { return rt.CopyValue(&v.data[i]) }

// Set sets element i to x, dropping the element it replaces. It panics if
// i is out of range.
func (v *Vec[T]) Set(i int, x T) { v.data[i] = x }

// Index returns element i; it is what v[i] calls (language spec section
// 11.11). It panics if i is out of range.
func (v *Vec[T]) Index(i int) T { return rt.CopyValue(&v.data[i]) }

// SetIndex sets element i to x, dropping the element it replaces; it is what
// v[i] = x calls. It panics if i is out of range.
func (v *Vec[T]) SetIndex(i int, x T) { v.data[i] = x }

// FmtList returns a pointer to the first element and the number of
// elements, so that pluto/fmt prints v like a slice (fmt.Lister).
func (v *Vec[T]) FmtList() (any, int) {
	if len(v.data) == 0 {
		return nil, 0
	}
	return &v.data[0], len(v.data)
}

// Ptr returns a pointer to element i, valid until v grows or is freed.
// &v[i] is not allowed, since v[i] is a copy; Ptr is the way to change an
// element in place.
func (v *Vec[T]) Ptr(i int) *T { return &v.data[i] }

// Last returns the last element and true, or the zero T and false if v is
// empty.
func (v *Vec[T]) Last() (T, bool) {
	if len(v.data) == 0 {
		var zero T
		return zero, false
	}
	return rt.CopyValue(&v.data[len(v.data)-1]), true
}

// Push appends x.
//
//pu:inline
func (v *Vec[T]) Push(x T) {
	n := len(v.data)
	if n == cap(v.data) {
		v.grow(1)
	}
	v.data = v.data[:n+1]
	rt.InitValue(&v.data[n], x) // the place holds nothing
}

// Append appends the elements of xs. xs must not be a view of v itself.
//
//pu:inline
func (v *Vec[T]) Append(xs ...T) {
	n := len(v.data)
	if cap(v.data)-n < len(xs) {
		v.grow(len(xs))
	}
	v.data = v.data[:n+len(xs)]
	copyIn(v.data[n:], xs)
}

// Pop removes and returns the last element, moving it out of v. It returns
// the zero T and false if v is empty.
func (v *Vec[T]) Pop() (T, bool) {
	n := len(v.data)
	if n == 0 {
		var zero T
		return zero, false
	}
	x := take(&v.data[n-1])
	v.data = v.data[:n-1]
	return x, true
}

// Insert inserts the values xs at index i, shifting later elements up and
// growing as needed (Go's slices.Insert). It panics if i is not in
// [0, Len()]. xs must not be a view of v itself.
func (v *Vec[T]) Insert(i int, xs ...T) {
	n := len(v.data)
	if i < 0 || i > n {
		panic("vec: Insert index out of range")
	}
	m := len(xs)
	if m == 0 {
		return
	}
	if cap(v.data)-n < m {
		v.grow(m)
	}
	v.data = v.data[:n+m]
	if n > i {
		rt.MoveValues(&v.data[i+m], &v.data[i], n-i) // the elements move up
	}
	rt.MoveValues(&v.data[i], &xs[0], m)
	rt.RetainValues(&v.data[i], m)
}

// Replace replaces the elements v[i:j] with the values xs, shifting later
// elements and growing as needed (Go's slices.Replace). It panics if
// v[i:j] is not a valid range of v. xs must not be a view of v itself.
func (v *Vec[T]) Replace(i, j int, xs ...T) {
	n := len(v.data)
	if i < 0 || j < i || j > n {
		panic("vec: Replace range out of range")
	}
	m := n - (j - i) + len(xs)
	clear(v.data[i:j]) // drops the elements replaced
	if m > n {
		if cap(v.data) < m {
			v.grow(m - n)
		}
		v.data = v.data[:m]
	}
	if n > j {
		rt.MoveValues(&v.data[i+len(xs)], &v.data[j], n-j) // the tail moves
	}
	if len(xs) > 0 {
		rt.MoveValues(&v.data[i], &xs[0], len(xs))
		rt.RetainValues(&v.data[i], len(xs))
	}
	if m < n {
		rt.ZeroValues(&v.data[m], n-m) // the places the tail left
		v.data = v.data[:m]
	}
}

// Remove removes and returns element i, moving it out of v and shifting
// later elements down. It panics if i is out of range.
func (v *Vec[T]) Remove(i int) T {
	x := take(&v.data[i])
	n := len(v.data)
	if i < n-1 {
		rt.MoveValues(&v.data[i], &v.data[i+1], n-1-i)
		rt.ZeroValues(&v.data[n-1], 1)
	}
	v.data = v.data[:n-1]
	return x
}

// SwapRemove removes and returns element i, replacing it with the last
// element. It does not preserve order but takes constant time. It panics if
// i is out of range.
func (v *Vec[T]) SwapRemove(i int) T {
	x := take(&v.data[i])
	n := len(v.data)
	if i < n-1 {
		v.data[i] = take(&v.data[n-1])
	}
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

// Free drops v's elements in index order (releasing the strings in them and
// running the Drop methods of owners, language spec sections 6.10 and 18.6),
// returns its storage to its allocator and leaves v empty. The allocator is
// kept, so v can be reused.
func (v *Vec[T]) Free() {
	if len(v.data) > 0 && rt.NeedsDrop[T]() {
		clear(v.data)
	}
	v.release()
	v.data = nil
}

// Drop returns v's storage to its allocator and leaves v empty: the owner's
// release (Free under its spec name).
func (v *Vec[T]) Drop() { v.Free() }

// Clone returns an independent copy of v, allocating from a (by default
// the default allocator), with capacity for exactly its elements. Elements
// are copied as values, so a Vec of owners cannot be cloned.
func (v *Vec[T]) Clone(a alloc.Allocator = alloc.Default()) Vec[T] {
	out := WithCapacity[T](len(v.data), a)
	if n := len(v.data); n > 0 {
		out.data = out.data[:n]
		rt.CopyValues(&out.data[0], &v.data[0], n)
	}
	return out
}

// Map returns a new Vec, allocating from a (by default the default
// allocator), that holds f applied to each element of v in order. v is
// unchanged.
func (v *Vec[T]) Map[U owner any](f func(T) U, a alloc.Allocator = alloc.Default()) Vec[U] {
	out := WithCapacity[U](len(v.data), a)
	for i := 0; i < len(v.data); i++ {
		out.Push(f(rt.CopyValue(&v.data[i])))
	}
	return out
}

// All returns an iterator over the indexes and elements of v, in order:
//
//	for i, x := range v.All() { ... }
//
// As range over a slice does in Go, the loop reads v's length once, when it
// starts, so elements pushed during the loop are not visited. It reads each
// element when it reaches it, so it sees elements set during the loop and
// follows v's storage when v grows, and it stops early if v has shrunk
// below the next index. A loop that should visit elements it pushes (a
// worklist) is an index loop: for i := 0; i < v.Len(); i++.
//
// For a Vec of owners the loop yields pointers to the elements (rt.View,
// language spec section 12.10, Owners in iterators), which are valid until v
// is next changed; for other elements it yields copies.
func (v *Vec[T]) All() iter.Seq2[int, rt.View[T]] {
	return func(yield func(int, rt.View[T]) bool) {
		n := len(v.data)
		for i := 0; i < n && i < len(v.data); i++ {
			if !yield(i, rt.ViewOf(&v.data[i])) {
				return
			}
		}
	}
}

// Values returns an iterator over the elements of v, in order. Like All, it
// reads v's length once, when the loop starts.
func (v *Vec[T]) Values() iter.Seq[rt.View[T]] {
	return func(yield func(rt.View[T]) bool) {
		n := len(v.data)
		for i := 0; i < n && i < len(v.data); i++ {
			if !yield(rt.ViewOf(&v.data[i])) {
				return
			}
		}
	}
}

// Backward returns an iterator over the indexes and elements of v, last
// first, starting from v's length when the loop starts. It stops early if
// v has shrunk below the next index.
func (v *Vec[T]) Backward() iter.Seq2[int, rt.View[T]] {
	return func(yield func(int, rt.View[T]) bool) {
		for i := len(v.data) - 1; i >= 0 && i < len(v.data); i-- {
			if !yield(i, rt.ViewOf(&v.data[i])) {
				return
			}
		}
	}
}

// AppendSeq appends the values of seq, growing as needed. The parameter has
// the underlying type of an iter.Seq[T], which an iterator of owners cannot
// be (an iter.Seq does not carry owners); an iterator of owners passes each
// one to yield, which moves it into v.
func (v *Vec[T]) AppendSeq(seq func(yield func(T) bool)) {
	for x := range seq {
		v.Push(x)
	}
}

// Collect returns a new Vec, allocating from a (by default the default
// allocator), holding the values of seq in order.
func Collect[T owner any](seq func(yield func(T) bool), a alloc.Allocator = alloc.Default()) Vec[T] {
	v := New[T](a)
	v.AppendSeq(seq)
	return v
}

// copyIn copies src into dst, whose places hold nothing: the elements are
// copied as values, so each string gets a reference of its own. The callers
// are the methods whose variadic parameter cannot carry owners.
func copyIn[T owner any](dst, src []T) {
	n := min(len(dst), len(src))
	if n > 0 {
		rt.MoveValues(&dst[0], &src[0], n)
		rt.RetainValues(&dst[0], n)
	}
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
		v.a = alloc.Default()
		if v.a == nil {
			panic("vec: no allocator (call alloc.SetDefault, or give the Vec one with Init)")
		}
	}
	var tp *T // for the size and alignment: a T variable would be dropped
	p := v.a.Alloc(storageSize[T](c), unsafe.Alignof(*tp))
	if p == nil {
		panic("vec: out of memory")
	}
	old := v.data
	v.data = unsafe.Slice((*T)(p), c)[:len(old)]
	if len(old) > 0 {
		// The elements move within v, from its old storage to its new: their
		// strings keep their counts, and the old array is freed without
		// releasing them. (Moving into v.data, not into the new array before
		// it is v's, shows the escape analysis that nothing leaves v.)
		rt.MoveValues(&v.data[0], &old[0], len(old))
	}
	v.free(old)
}

// release frees v's storage, if any, without changing v.data.
func (v *Vec[T]) release() { v.free(v.data) }

// free returns storage of v to v's allocator, if it has any.
func (v *Vec[T]) free(data []T) {
	if cap(data) > 0 {
		var tp *T
		v.a.Free(unsafe.Pointer(unsafe.SliceData(data)), storageSize[T](cap(data)), unsafe.Alignof(*tp))
	}
}

// storageSize returns the bytes allocated for c elements of T (at least 1).
func storageSize[T owner any](c int) uintptr {
	var tp *T
	size := uintptr(c) * unsafe.Sizeof(*tp)
	if size == 0 {
		size = 1
	}
	return size
}
