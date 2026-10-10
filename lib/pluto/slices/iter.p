// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package slices

import (
	"pluto/alloc"
	"pluto/cmp"
	"pluto/iter"
	"pluto/vec"
)

// All returns an iterator over index-value pairs in the slice
// in the usual order.
func All[Slice ~[]E, E any](s Slice) iter.Seq2[int, E] {
	return func(yield func(int, E) bool) {
		for i, v := range s {
			if !yield(i, v) {
				return
			}
		}
	}
}

// Backward returns an iterator over index-value pairs in the slice,
// traversing it backward with descending indices.
func Backward[Slice ~[]E, E any](s Slice) iter.Seq2[int, E] {
	return func(yield func(int, E) bool) {
		for i := len(s) - 1; i >= 0; i-- {
			if !yield(i, s[i]) {
				return
			}
		}
	}
}

// Values returns an iterator that yields the slice elements in order.
func Values[Slice ~[]E, E any](s Slice) iter.Seq[E] {
	return func(yield func(E) bool) {
		for _, v := range s {
			if !yield(v) {
				return
			}
		}
	}
}

// Collect collects values from seq into a new Vec, allocated from the
// default allocator, and returns it. If seq is empty, the Vec is empty.
func Collect[E any](seq iter.Seq[E]) vec.Vec[E] {
	return vec.Collect(seq)
}

// CollectIn is Collect with the new Vec's storage allocated from a.
func CollectIn[E any](a alloc.Allocator, seq iter.Seq[E]) vec.Vec[E] {
	return vec.Collect(seq, a)
}

// Sorted collects values from seq into a new Vec, allocated from the
// default allocator, sorts it, and returns it.
func Sorted[E cmp.Ordered](seq iter.Seq[E]) vec.Vec[E] {
	return SortedIn(alloc.Default(), seq)
}

// SortedIn is Sorted with the new Vec's storage allocated from a.
func SortedIn[E cmp.Ordered](a alloc.Allocator, seq iter.Seq[E]) vec.Vec[E] {
	v := vec.Collect(seq, a)
	Sort(v.Slice())
	return v
}

// SortedFunc collects values from seq into a new Vec, allocated from the
// default allocator, sorts it using the comparison function, and returns it.
func SortedFunc[E any](seq iter.Seq[E], cmp func(E, E) int) vec.Vec[E] {
	return SortedFuncIn(alloc.Default(), seq, cmp)
}

// SortedFuncIn is SortedFunc with the new Vec's storage allocated from a.
func SortedFuncIn[E any](a alloc.Allocator, seq iter.Seq[E], cmp func(E, E) int) vec.Vec[E] {
	v := vec.Collect(seq, a)
	SortFunc(v.Slice(), cmp)
	return v
}

// SortedStableFunc collects values from seq into a new Vec, allocated from
// the default allocator. It then sorts the Vec while keeping the original
// order of equal elements, using the comparison function to compare
// elements. It returns the new Vec.
func SortedStableFunc[E any](seq iter.Seq[E], cmp func(E, E) int) vec.Vec[E] {
	return SortedStableFuncIn(alloc.Default(), seq, cmp)
}

// SortedStableFuncIn is SortedStableFunc with the new Vec's storage
// allocated from a.
func SortedStableFuncIn[E any](a alloc.Allocator, seq iter.Seq[E], cmp func(E, E) int) vec.Vec[E] {
	v := vec.Collect(seq, a)
	SortStableFunc(v.Slice(), cmp)
	return v
}

// Chunk returns an iterator over consecutive sub-slices of up to n elements of s.
// All but the last sub-slice will have size n.
// All sub-slices are clipped to have no capacity beyond the length.
// If s is empty, the sequence is empty: there is no empty slice in the sequence.
// Chunk panics if n is less than 1.
func Chunk[Slice ~[]E, E any](s Slice, n int) iter.Seq[Slice] {
	if n < 1 {
		panic("cannot be less than 1")
	}

	return func(yield func(Slice) bool) {
		for i := 0; i < len(s); i += n {
			// Clamp the last chunk to the slice bound as necessary.
			end := min(n, len(s[i:]))

			// Set the capacity of each chunk so that appending to a chunk does
			// not modify the original slice.
			if !yield(s[i : i+end : i+end]) {
				return
			}
		}
	}
}
