// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package heap provides heap operations for any type that implements
// heap.Interface, and Heap, a generic heap in a vec.Vec. A heap is a tree
// with the property that each node is the minimum-valued node in its
// subtree.
//
// The minimum element in the tree is the root, at index 0.
//
// A heap is a common way to implement a priority queue. To build a priority
// queue, implement the Heap interface with the (negative) priority as the
// ordering for the Less method, so Push adds items while Pop removes the
// highest-priority item from the queue.
//
// Interface is Go's container/heap.Interface, with Len, Less and Swap
// declared directly because there is no sort.Interface in Plutonium. Its
// Push and Pop pass elements as any, so an element larger than a word is
// copied into the frame of the caller of heap.Push, and can be kept only
// if the method stores a copy, as an append of a value does; for elements
// that are not words or pointers, Heap is simpler and does not box:
//
//	h := heap.New(func(a, b Task) bool { return a.priority < b.priority })
//	defer h.Free()
//	h.Push(Task{...})
//	next, _ := h.Pop()
package heap

import (
	"pluto/alloc"
	"pluto/vec"
)

// The Interface type describes the requirements
// for a type using the routines in this package.
// Any type that implements it may be used as a
// min-heap with the following invariants (established after
// [Init] has been called or if the data is empty or sorted):
//
//	!h.Less(j, i) for 0 <= i < h.Len() and 2*i+1 <= j <= 2*i+2 and j < h.Len()
//
// Note that [Push] and [Pop] in this interface are for package heap's
// implementation to call. To add and remove things from the heap,
// use [heap.Push] and [heap.Pop].
type Interface interface {
	Len() int
	Less(i, j int) bool
	Swap(i, j int)
	Push(x any) // add x as element Len()
	Pop() any   // remove and return element Len() - 1.
}

// Init establishes the heap invariants required by the other routines in this package.
// Init is idempotent with respect to the heap invariants
// and may be called whenever the heap invariants may have been invalidated.
// The complexity is O(n) where n = h.Len().
func Init(h Interface) {
	// heapify
	n := h.Len()
	for i := n/2 - 1; i >= 0; i-- {
		down(h, i, n)
	}
}

// Push pushes the element x onto the heap.
// The complexity is O(log n) where n = h.Len().
func Push(h Interface, x any) {
	h.Push(x)
	up(h, h.Len()-1)
}

// Pop removes and returns the minimum element (according to Less) from the heap.
// The complexity is O(log n) where n = h.Len().
// Pop is equivalent to [Remove](h, 0).
func Pop(h Interface) any {
	n := h.Len() - 1
	h.Swap(0, n)
	down(h, 0, n)
	return h.Pop()
}

// Remove removes and returns the element at index i from the heap.
// The complexity is O(log n) where n = h.Len().
func Remove(h Interface, i int) any {
	n := h.Len() - 1
	if n != i {
		h.Swap(i, n)
		if !down(h, i, n) {
			up(h, i)
		}
	}
	return h.Pop()
}

// Fix re-establishes the heap ordering after the element at index i has changed its value.
// Changing the value of the element at index i and then calling Fix is equivalent to,
// but less expensive than, calling [Remove](h, i) followed by a Push of the new value.
// The complexity is O(log n) where n = h.Len().
func Fix(h Interface, i int) {
	if !down(h, i, h.Len()) {
		up(h, i)
	}
}

func up(h Interface, j int) {
	for {
		i := (j - 1) / 2 // parent
		if i == j || !h.Less(j, i) {
			break
		}
		h.Swap(i, j)
		j = i
	}
}

func down(h Interface, i0, n int) bool {
	i := i0
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 { // j1 < 0 after int overflow
			break
		}
		j := j1 // left child
		if j2 := j1 + 1; j2 < n && h.Less(j2, j1) {
			j = j2 // = 2*i + 2  // right child
		}
		if !h.Less(j, i) {
			break
		}
		h.Swap(i, j)
		i = j
	}
	return i > i0
}

// A Heap is a min-heap of T held in a vec.Vec, ordered by a less function:
// the element for which less(x, y) is true for every other y comes out
// first. less must be a strict weak ordering and must not change the
// elements. The zero Heap has no less function; make one with New.
//
// A Heap owns its storage, so it is an owner (language spec section 7.18):
// Drop releases it. It must not be copied after first use; pass a *Heap.
type Heap[T any] owner struct {
	items vec.Vec[T]
	less  func(a, b T) bool
}

// New returns an empty Heap ordered by less, whose storage comes from a, by
// default the default allocator.
func New[T any](less func(a, b T) bool, a alloc.Allocator = alloc.Default()) Heap[T] {
	return Heap[T]{items: vec.New[T](a), less: less}
}

// Len returns the number of elements.
func (h *Heap[T]) Len() int { return h.items.Len() }

// Push adds x to the heap. The complexity is O(log n) where n = h.Len().
func (h *Heap[T]) Push(x T) {
	h.items.Push(x)
	h.up(h.items.Len() - 1)
}

// Peek returns the minimum element without removing it, and true; or the
// zero T and false if the heap is empty.
func (h *Heap[T]) Peek() (T, bool) {
	if h.items.Len() == 0 {
		var zero T
		return zero, false
	}
	return h.items.At(0), true
}

// Pop removes and returns the minimum element (according to less) and true;
// or the zero T and false if the heap is empty. The complexity is O(log n)
// where n = h.Len().
func (h *Heap[T]) Pop() (T, bool) {
	n := h.items.Len() - 1
	if n < 0 {
		var zero T
		return zero, false
	}
	h.swap(0, n)
	h.down(0, n)
	return h.items.Pop()
}

// Remove removes and returns the element at index i, where index 0 is the
// minimum and the rest are in heap order. It panics if i is out of range.
// The complexity is O(log n) where n = h.Len().
func (h *Heap[T]) Remove(i int) T {
	n := h.items.Len() - 1
	if i < 0 || i > n {
		panic("heap: Remove index out of range")
	}
	if n != i {
		h.swap(i, n)
		if !h.down(i, n) {
			h.up(i)
		}
	}
	x, _ := h.items.Pop()
	return x
}

// At returns the element at index i of the heap's array, where index 0 is
// the minimum. It panics if i is out of range.
func (h *Heap[T]) At(i int) T { return h.items.At(i) }

// Set replaces the element at index i with x and restores the heap order,
// as Fix does after a change. It panics if i is out of range.
func (h *Heap[T]) Set(i int, x T) {
	h.items.Set(i, x)
	h.Fix(i)
}

// Fix re-establishes the heap ordering after the element at index i has
// changed its value, for elements that hold their priority behind a pointer.
// The complexity is O(log n) where n = h.Len().
func (h *Heap[T]) Fix(i int) {
	if !h.down(i, h.items.Len()) {
		h.up(i)
	}
}

// Init re-establishes the heap invariants after the elements have been
// changed by other means, such as AppendUnordered. The complexity is O(n).
func (h *Heap[T]) Init() {
	n := h.items.Len()
	for i := n/2 - 1; i >= 0; i-- {
		h.down(i, n)
	}
}

// AppendUnordered adds the elements xs without restoring the heap order;
// call Init afterwards. Adding n elements this way and calling Init is O(n),
// where n calls to Push are O(n log n).
func (h *Heap[T]) AppendUnordered(xs ...T) { h.items.Append(xs...) }

// Slice returns the elements in heap order (the minimum first) as a slice,
// a view of the heap's storage valid until the heap grows or is freed.
func (h *Heap[T]) Slice() []T { return h.items.Slice() }

// Free releases the heap's storage and leaves it empty.
func (h *Heap[T]) Free() { h.items.Free() }

// Drop releases the heap's storage: the owner's release (Free under its
// spec name).
func (h *Heap[T]) Drop() { h.items.Free() }

func (h *Heap[T]) swap(i, j int) {
	s := h.items.Slice()
	s[i], s[j] = s[j], s[i]
}

func (h *Heap[T]) up(j int) {
	s := h.items.Slice()
	for {
		i := (j - 1) / 2 // parent
		if i == j || !h.less(s[j], s[i]) {
			break
		}
		s[i], s[j] = s[j], s[i]
		j = i
	}
}

func (h *Heap[T]) down(i0, n int) bool {
	s := h.items.Slice()
	i := i0
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 { // j1 < 0 after int overflow
			break
		}
		j := j1 // left child
		if j2 := j1 + 1; j2 < n && h.less(s[j2], s[j1]) {
			j = j2 // = 2*i + 2  // right child
		}
		if !h.less(s[j], s[i]) {
			break
		}
		s[i], s[j] = s[j], s[i]
		i = j
	}
	return i > i0
}
