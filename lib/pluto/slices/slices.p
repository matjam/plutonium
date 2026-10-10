// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package slices defines various functions useful with slices of any type.
//
// It is Go's slices package, with these differences, all of which follow
// from Plutonium's rules that a slice is a view that cannot grow and that
// nothing allocates behind the program's back:
//
//   - Insert and Replace work within the slice's capacity, as append does,
//     and panic when the result would not fit. To grow a sequence, keep it in
//     a vec.Vec.
//   - Clone, Concat, Repeat, Collect, Sorted, SortedFunc and SortedStableFunc
//     return a vec.Vec, which owns the new storage, allocated from an
//     allocator: by default the default allocator, or the one passed to
//     CloneIn, ConcatIn, RepeatIn, CollectIn, SortedIn, SortedFuncIn and
//     SortedStableFuncIn. Use v.Slice() for the elements as a slice, and
//     v.Free() when done.
//   - AppendSeq, like append, must fit in the slice's capacity; Collect is
//     the way to gather values of unknown number.
//   - Grow is vec.Vec's Reserve.
//   - The iterators All, Values, Backward and Chunk are closure constructors
//     (pluto/iter): they are called directly, in a range loop or as an
//     argument, and build no allocation.
//
// Everything else, including Sort and SortFunc (pattern-defeating quicksort)
// and SortStableFunc (insertion sort and symmetric merges, without extra
// memory), is Go's code and does not allocate.
package slices

import (
	"pluto/alloc"
	"pluto/bits"
	"pluto/cmp"
	"pluto/vec"
	"unsafe"
)

// Equal reports whether two slices are equal: the same length and all
// elements equal. If the lengths are different, Equal returns false.
// Otherwise, the elements are compared in increasing index order, and the
// comparison stops at the first unequal pair.
// Empty and nil slices are considered equal.
// Floating point NaNs are not considered equal.
func Equal[S ~[]E, E comparable](s1, s2 S) bool {
	if len(s1) != len(s2) {
		return false
	}
	for i := range s1 {
		if s1[i] != s2[i] {
			return false
		}
	}
	return true
}

// EqualFunc reports whether two slices are equal using an equality
// function on each pair of elements. If the lengths are different,
// EqualFunc returns false. Otherwise, the elements are compared in
// increasing index order, and the comparison stops at the first index
// for which eq returns false.
func EqualFunc[S1 ~[]E1, S2 ~[]E2, E1, E2 any](s1 S1, s2 S2, eq func(E1, E2) bool) bool {
	if len(s1) != len(s2) {
		return false
	}
	for i, v1 := range s1 {
		v2 := s2[i]
		if !eq(v1, v2) {
			return false
		}
	}
	return true
}

// Compare compares the elements of s1 and s2, using [cmp.Compare] on each pair
// of elements. The elements are compared sequentially, starting at index 0,
// until one element is not equal to the other.
// The result of comparing the first non-matching elements is returned.
// If both slices are equal until one of them ends, the shorter slice is
// considered less than the longer one.
// The result is 0 if s1 == s2, -1 if s1 < s2, and +1 if s1 > s2.
func Compare[S ~[]E, E cmp.Ordered](s1, s2 S) int {
	for i, v1 := range s1 {
		if i >= len(s2) {
			return +1
		}
		v2 := s2[i]
		if c := cmp.Compare(v1, v2); c != 0 {
			return c
		}
	}
	if len(s1) < len(s2) {
		return -1
	}
	return 0
}

// CompareFunc is like [Compare] but uses a custom comparison function on each
// pair of elements.
// The result is the first non-zero result of cmp; if cmp always
// returns 0 the result is 0 if len(s1) == len(s2), -1 if len(s1) < len(s2),
// and +1 if len(s1) > len(s2).
func CompareFunc[S1 ~[]E1, S2 ~[]E2, E1, E2 any](s1 S1, s2 S2, cmp func(E1, E2) int) int {
	for i, v1 := range s1 {
		if i >= len(s2) {
			return +1
		}
		v2 := s2[i]
		if c := cmp(v1, v2); c != 0 {
			return c
		}
	}
	if len(s1) < len(s2) {
		return -1
	}
	return 0
}

// Index returns the index of the first occurrence of v in s,
// or -1 if not present.
func Index[S ~[]E, E comparable](s S, v E) int {
	for i := range s {
		if v == s[i] {
			return i
		}
	}
	return -1
}

// IndexFunc returns the first index i satisfying f(s[i]),
// or -1 if none do.
func IndexFunc[S ~[]E, E any](s S, f func(E) bool) int {
	for i := range s {
		if f(s[i]) {
			return i
		}
	}
	return -1
}

// Contains reports whether v is present in s.
func Contains[S ~[]E, E comparable](s S, v E) bool {
	return Index(s, v) >= 0
}

// ContainsFunc reports whether at least one
// element e of s satisfies f(e).
// It stops as soon as a call to f returns true.
func ContainsFunc[S ~[]E, E any](s S, f func(E) bool) bool {
	return IndexFunc(s, f) >= 0
}

// Insert modifies s in place by inserting the values v... at index i,
// and returns the modified slice.
// The elements at s[i:] are shifted up to make room.
// In the returned slice r, r[i] == v[0],
// and, if i < len(s), r[i+len(v)] == value originally at s[i].
// Insert panics if i > len(s), or if len(s)+len(v) > cap(s): as with
// append, the result must fit in the slice's capacity.
// This function is O(len(s) + len(v)).
func Insert[S ~[]E, E any](s S, i int, v ...E) S {
	_ = s[i:] // bounds check

	m := len(v)
	if m == 0 {
		return s
	}
	n := len(s)
	if n+m > cap(s) {
		panic("slices.Insert: the result does not fit in the capacity")
	}
	if i == n {
		return append(s, v...)
	}
	s = s[:n+m]

	// before:
	// s: aaaaaaaabbbbccccccccdddd
	//            ^   ^       ^   ^
	//            i  i+m      n  n+m
	// after:
	// s: aaaaaaaavvvvbbbbcccccccc
	//            ^   ^       ^   ^
	//            i  i+m      n  n+m
	//
	// a are the values that don't move in s.
	// v are the values copied in from v.
	// b and c are the values from s that are shifted up in index.
	// d are the values that get overwritten, never to be seen again.

	if !overlaps(v, s[i+m:]) {
		// Easy case - v does not overlap either the c or d regions.
		// (It might be in some of a or b, or elsewhere entirely.)
		// The data we copy up doesn't write to v at all, so just do it.

		copy(s[i+m:], s[i:])

		// Now we have
		// s: aaaaaaaabbbbbbbbcccccccc
		//            ^   ^       ^   ^
		//            i  i+m      n  n+m
		// Note the b values are duplicated.

		copy(s[i:], v)

		// Now we have
		// s: aaaaaaaavvvvbbbbcccccccc
		//            ^   ^       ^   ^
		//            i  i+m      n  n+m
		// That's the result we want.
		return s
	}

	// The hard case - v overlaps c or d. We can't just shift up
	// the data because we'd move or clobber the values we're trying
	// to insert.
	// So instead, write v on top of d, then rotate.
	copy(s[n:], v)

	// Now we have
	// s: aaaaaaaabbbbccccccccvvvv
	//            ^   ^       ^   ^
	//            i  i+m      n  n+m

	rotateRight(s[i:], m)

	// Now we have
	// s: aaaaaaaavvvvbbbbcccccccc
	//            ^   ^       ^   ^
	//            i  i+m      n  n+m
	// That's the result we want.
	return s
}

// Delete modifies s in place by removing the elements s[i:j],
// and returns the modified slice.
// Delete panics if j > len(s) or s[i:j] is not a valid slice of s.
// Delete is O(len(s)-i), so if many items must be deleted, it is better to
// make a single call deleting them all together than to delete one at a time.
// Delete zeroes the elements s[len(s)-(j-i):len(s)].
func Delete[S ~[]E, E any](s S, i, j int) S {
	_ = s[i:j:len(s)] // bounds check

	if i == j {
		return s
	}

	oldlen := len(s)
	s = append(s[:i], s[j:]...)
	clear(s[len(s):oldlen]) // zero the obsolete elements
	return s
}

// DeleteFunc modifies s in place by removing any elements for which del returns true,
// and returns the modified slice.
// DeleteFunc zeroes the elements between the new length and the original length.
func DeleteFunc[S ~[]E, E any](s S, del func(E) bool) S {
	i := IndexFunc(s, del)
	if i == -1 {
		return s
	}
	// Don't start copying elements until we find one to delete.
	for j := i + 1; j < len(s); j++ {
		if v := s[j]; !del(v) {
			s[i] = v
			i++
		}
	}
	clear(s[i:]) // zero the obsolete elements
	return s[:i]
}

// Replace modifies s in place by replacing the elements s[i:j] with the given v,
// and returns the modified slice.
// Replace panics if j > len(s) or s[i:j] is not a valid slice of s, or if
// the result does not fit in the capacity of s.
// When len(v) < (j-i), Replace zeroes the elements between the new length and the original length.
func Replace[S ~[]E, E any](s S, i, j int, v ...E) S {
	_ = s[i:j] // bounds check

	if i == j {
		return Insert(s, i, v...)
	}
	if j == len(s) {
		if i+len(v) > cap(s) {
			panic("slices.Replace: the result does not fit in the capacity")
		}
		s2 := append(s[:i], v...)
		if len(s2) < len(s) {
			clear(s[len(s2):]) // zero the obsolete elements
		}
		return s2
	}

	tot := len(s[:i]) + len(v) + len(s[j:])
	if tot > cap(s) {
		panic("slices.Replace: the result does not fit in the capacity")
	}

	r := s[:tot]

	if i+len(v) <= j {
		// Easy, as v fits in the deleted portion.
		copy(r[i:], v)
		copy(r[i+len(v):], s[j:])
		clear(s[tot:]) // zero the obsolete elements
		return r
	}

	// We are expanding (v is bigger than j-i).
	// The situation is something like this:
	// (example has i=4,j=8,len(s)=16,len(v)=6)
	// s: aaaaxxxxbbbbbbbbyy
	//        ^   ^       ^ ^
	//        i   j  len(s) tot
	// a: prefix of s
	// x: deleted range
	// b: more of s
	// y: area to expand into

	if !overlaps(r[i+len(v):], v) {
		// Easy, as v is not clobbered by the first copy.
		copy(r[i+len(v):], s[j:])
		copy(r[i:], v)
		return r
	}

	// This is a situation where we don't have a single place to which
	// we can copy v. Parts of it need to go to two different places.
	// We want to copy the prefix of v into y and the suffix into x, then
	// rotate |y| spots to the right.
	//
	//        v[2:]      v[:2]
	//         |           |
	// s: aaaavvvvbbbbbbbbvv
	//        ^   ^       ^ ^
	//        i   j  len(s) tot
	//
	// If either of those two destinations don't alias v, then we're good.
	y := len(v) - (j - i) // length of y portion

	if !overlaps(r[i:j], v) {
		copy(r[i:j], v[y:])
		copy(r[len(s):], v[:y])
		rotateRight(r[i:], y)
		return r
	}
	if !overlaps(r[len(s):], v) {
		copy(r[len(s):], v[:y])
		copy(r[i:j], v[y:])
		rotateRight(r[i:], y)
		return r
	}

	// Now we know that v overlaps both x and y.
	// That means that the entirety of b is *inside* v.
	// So we don't need to preserve b at all; instead we
	// can copy v first, then copy the b part of v out of
	// v to the right destination.
	k := startIdx(v, s[j:])
	copy(r[i:], v)
	copy(r[i+len(v):], r[i+k:])
	return r
}

// Clone returns a copy of the elements of s in a new Vec, allocated from the
// default allocator. The elements are copied using assignment, so this is a
// shallow clone. The Vec has capacity for exactly len(s) elements.
func Clone[S ~[]E, E any](s S) vec.Vec[E] {
	return CloneIn(alloc.Default(), s)
}

// CloneIn is Clone with the new Vec's storage allocated from a.
func CloneIn[S ~[]E, E any](a alloc.Allocator, s S) vec.Vec[E] {
	v := vec.WithCapacity[E](len(s), a)
	v.Append(s...)
	return v
}

// Compact replaces consecutive runs of equal elements with a single copy.
// This is like the uniq command found on Unix.
// Compact modifies the contents of the slice s and returns the modified slice,
// which may have a smaller length.
// Compact zeroes the elements between the new length and the original length.
func Compact[S ~[]E, E comparable](s S) S {
	if len(s) < 2 {
		return s
	}
	for k := 1; k < len(s); k++ {
		if s[k] == s[k-1] {
			s2 := s[k:]
			for k2 := 1; k2 < len(s2); k2++ {
				if s2[k2] != s2[k2-1] {
					s[k] = s2[k2]
					k++
				}
			}

			clear(s[k:]) // zero the obsolete elements
			return s[:k]
		}
	}
	return s
}

// CompactFunc is like [Compact] but uses an equality function to compare elements.
// For runs of elements that compare equal, CompactFunc keeps the first one.
// CompactFunc zeroes the elements between the new length and the original length.
func CompactFunc[S ~[]E, E any](s S, eq func(E, E) bool) S {
	if len(s) < 2 {
		return s
	}
	for k := 1; k < len(s); k++ {
		if eq(s[k], s[k-1]) {
			s2 := s[k:]
			for k2 := 1; k2 < len(s2); k2++ {
				if !eq(s2[k2], s2[k2-1]) {
					s[k] = s2[k2]
					k++
				}
			}

			clear(s[k:]) // zero the obsolete elements
			return s[:k]
		}
	}
	return s
}

// Clip removes unused capacity from the slice, returning s[:len(s):len(s)].
func Clip[S ~[]E, E any](s S) S {
	return s[:len(s):len(s)]
}

// TODO: There are other rotate algorithms.
// This algorithm has the desirable property that it moves each element at most twice.
// The follow-cycles algorithm can be 1-write but it is not very cache friendly.

// rotateLeft rotates s left by r spaces.
// s_final[i] = s_orig[i+r], wrapping around.
func rotateLeft[E any](s []E, r int) {
	Reverse(s[:r])
	Reverse(s[r:])
	Reverse(s)
}
func rotateRight[E any](s []E, r int) {
	rotateLeft(s, len(s)-r)
}

// overlaps reports whether the memory ranges a[:len(a)] and b[:len(b)] overlap.
func overlaps[E any](a, b []E) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	elemSize := unsafe.Sizeof(a[0])
	if elemSize == 0 {
		return false
	}
	return uintptr(unsafe.Pointer(&a[0])) <= uintptr(unsafe.Pointer(&b[len(b)-1]))+(elemSize-1) &&
		uintptr(unsafe.Pointer(&b[0])) <= uintptr(unsafe.Pointer(&a[len(a)-1]))+(elemSize-1)
}

// startIdx returns the index in haystack where the needle starts.
// prerequisite: the needle must be aliased entirely inside the haystack.
func startIdx[E any](haystack, needle []E) int {
	p := &needle[0]
	for i := range haystack {
		if p == &haystack[i] {
			return i
		}
	}
	panic("needle not found")
}

// Reverse reverses the elements of the slice in place.
func Reverse[S ~[]E, E any](s S) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// Concat returns a new Vec, allocated from the default allocator, holding
// the elements of the passed in slices one after the other. The Vec has
// capacity for exactly their total length.
func Concat[S ~[]E, E any](slices ...S) vec.Vec[E] {
	return ConcatIn(alloc.Default(), slices...)
}

// ConcatIn is Concat with the new Vec's storage allocated from a.
func ConcatIn[S ~[]E, E any](a alloc.Allocator, slices ...S) vec.Vec[E] {
	size := 0
	for _, s := range slices {
		size += len(s)
		if size < 0 {
			panic("len out of range")
		}
	}
	v := vec.WithCapacity[E](size, a)
	for _, s := range slices {
		v.Append(s...)
	}
	return v
}

// Repeat returns a new Vec, allocated from the default allocator, that
// repeats the provided slice the given number of times. The Vec has length
// and capacity len(x) * count.
// Repeat panics if count is negative or if the result of (len(x) * count)
// overflows.
func Repeat[S ~[]E, E any](x S, count int) vec.Vec[E] {
	return RepeatIn(alloc.Default(), x, count)
}

// RepeatIn is Repeat with the new Vec's storage allocated from a.
func RepeatIn[S ~[]E, E any](a alloc.Allocator, x S, count int) vec.Vec[E] {
	if count < 0 {
		panic("cannot be negative")
	}

	const maxInt = ^uint64(0) >> 1
	hi, lo := bits.Mul64(uint64(len(x)), uint64(count))
	if hi > 0 || lo > maxInt {
		panic("the result of (len(x) * count) overflows")
	}

	v := vec.WithCapacity[E](int(lo), a) // lo = len(x) * count
	for i := 0; i < count; i++ {
		v.Append(x...)
	}
	return v
}
