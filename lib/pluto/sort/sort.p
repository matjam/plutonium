// Package sort sorts slices and searches sorted slices, without allocating.
//
// Sorting is generic: Sort and SortFunc use pattern-defeating quicksort
// (pdqsort), which falls back to heapsort after too many bad pivots, so the
// worst case is O(n log n) comparisons; recursion is on the smaller
// partition only, so the stack depth is O(log n). SortStable and
// SortStableFunc use insertion sort on blocks and in-place symmetric
// merging: O(n log n) comparisons and O(n log² n) swaps, with no extra
// memory.
//
// The functions follow Go's slices package (Sort, SortFunc, IsSorted,
// BinarySearch) and sort package (Search, Find, Ints, SearchInts, ...),
// except that the comparison functions here are less functions returning a
// bool, and there is no sort.Interface or reflection-based sort.Slice.
//
// Adapted from Go's sort and slices packages.
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.
package sort

import "pluto/bits"

// Ordered is a constraint that permits any ordered type: any type that
// supports the operators < <= >= >. It is Go's cmp.Ordered.
type Ordered interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64 |
		~string
}

// isNaN reports whether x is a NaN. It is always false if T is not a
// floating-point type.
func isNaN[T Ordered](x T) bool {
	return x != x
}

// cmpLess reports whether x is less than y. For floating-point types, a NaN
// is considered less than any non-NaN, and -0.0 is not less than (is equal
// to) 0.0. This is Go's cmp.Less.
func cmpLess[T Ordered](x, y T) bool {
	return (isNaN(x) && !isNaN(y)) || x < y
}

// Compare returns -1 if x is less than y, 0 if x equals y and +1 if x is
// greater than y, ordering NaNs before other values as cmpLess does. It is
// Go's cmp.Compare.
func Compare[T Ordered](x, y T) int {
	xNaN := isNaN(x)
	yNaN := isNaN(y)
	if xNaN {
		if yNaN {
			return 0
		}
		return -1
	}
	if yNaN {
		return +1
	}
	if x < y {
		return -1
	}
	if x > y {
		return +1
	}
	return 0
}

// Sort sorts a slice of any ordered type in ascending order. When sorting
// floating-point numbers, NaNs are ordered before other values.
func Sort[T Ordered](x []T) {
	n := len(x)
	pdqsortOrdered(x, 0, n, bits.Len(uint(n)))
}

// SortFunc sorts the slice x in ascending order as determined by less, which
// must be a strict weak ordering. The sort is not guaranteed to be stable.
func SortFunc[T any](x []T, less func(a, b T) bool) {
	n := len(x)
	pdqsortLessFunc(x, 0, n, bits.Len(uint(n)), less)
}

// SortStable sorts x in ascending order, keeping equal elements in their
// original order.
func SortStable[T Ordered](x []T) {
	stableOrdered(x, len(x))
}

// SortStableFunc sorts x as determined by less, keeping the original order
// of elements that compare equal (neither is less than the other).
func SortStableFunc[T any](x []T, less func(a, b T) bool) {
	stableLessFunc(x, len(x), less)
}

// IsSorted reports whether x is sorted in ascending order.
func IsSorted[T Ordered](x []T) bool {
	for i := len(x) - 1; i > 0; i-- {
		if cmpLess(x[i], x[i-1]) {
			return false
		}
	}
	return true
}

// IsSortedFunc reports whether x is sorted in ascending order, with less as
// the comparison function.
func IsSortedFunc[T any](x []T, less func(a, b T) bool) bool {
	for i := len(x) - 1; i > 0; i-- {
		if less(x[i], x[i-1]) {
			return false
		}
	}
	return true
}

// BinarySearch searches for target in a sorted slice and returns the
// earliest position where target is found, or the position where target
// would appear in the sort order; it also returns a bool saying whether the
// target is really found in the slice.
func BinarySearch[T Ordered](x []T, target T) (int, bool) {
	// Inlining is faster than calling BinarySearchFunc with a lambda.
	n := len(x)
	// Define x[-1] < target and x[n] >= target.
	// Invariant: x[i-1] < target, x[j] >= target.
	i, j := 0, n
	for i < j {
		h := int(uint(i+j) >> 1) // avoid overflow when computing h
		// i ≤ h < j
		if cmpLess(x[h], target) {
			i = h + 1 // preserves x[i-1] < target
		} else {
			j = h // preserves x[j] >= target
		}
	}
	// i == j, x[i-1] < target, and x[j] (= x[i]) >= target  =>  answer is i.
	return i, i < n && (x[i] == target || (isNaN(x[i]) && isNaN(target)))
}

// BinarySearchFunc works like BinarySearch, but uses a custom comparison
// function. The slice must be sorted in increasing order, where "increasing"
// is defined by cmp. cmp should return 0 if the slice element matches the
// target, a negative number if the slice element precedes the target, or a
// positive number if the slice element follows the target.
func BinarySearchFunc[T, U any](x []T, target U, cmp func(T, U) int) (int, bool) {
	n := len(x)
	i, j := 0, n
	for i < j {
		h := int(uint(i+j) >> 1)
		if cmp(x[h], target) < 0 {
			i = h + 1
		} else {
			j = h
		}
	}
	return i, i < n && cmp(x[i], target) == 0
}

// Search uses binary search to find and return the smallest index i in
// [0, n) at which f(i) is true, assuming that on the range [0, n), f(i) ==
// true implies f(i+1) == true. Search returns n if there is no such index.
func Search(n int, f func(int) bool) int {
	// Define f(-1) == false and f(n) == true.
	// Invariant: f(i-1) == false, f(j) == true.
	i, j := 0, n
	for i < j {
		h := int(uint(i+j) >> 1) // avoid overflow when computing h
		// i ≤ h < j
		if !f(h) {
			i = h + 1 // preserves f(i-1) == false
		} else {
			j = h // preserves f(j) == true
		}
	}
	// i == j, f(i-1) == false, and f(j) (= f(i)) == true  =>  answer is i.
	return i
}

// Find uses binary search to find and return the smallest index i in [0, n)
// at which cmp(i) <= 0. If there is no such index i, Find returns i = n.
// The found result is true if i < n and cmp(i) == 0. cmp(i) must be
// decreasing over [0, n): positive, then zero, then negative.
func Find(n int, cmp func(int) int) (i int, found bool) {
	// Define cmp(-1) > 0 and cmp(n) <= 0.
	// Invariant: cmp(i-1) > 0, cmp(j) <= 0.
	i, j := 0, n
	for i < j {
		h := int(uint(i+j) >> 1) // avoid overflow when computing h
		// i ≤ h < j
		if cmp(h) > 0 {
			i = h + 1 // preserves cmp(i-1) > 0
		} else {
			j = h // preserves cmp(j) <= 0
		}
	}
	// i == j, cmp(i-1) > 0 and cmp(j) <= 0
	return i, i < n && cmp(i) == 0
}

// Ints sorts a slice of ints in increasing order.
func Ints(x []int) { Sort(x) }

// Float64s sorts a slice of float64s in increasing order. Not-a-number
// (NaN) values are ordered before other values.
func Float64s(x []float64) { Sort(x) }

// Strings sorts a slice of strings in increasing order.
func Strings(x []string) { Sort(x) }

// IntsAreSorted reports whether the slice x is sorted in increasing order.
func IntsAreSorted(x []int) bool { return IsSorted(x) }

// Float64sAreSorted reports whether the slice x is sorted in increasing
// order, with not-a-number (NaN) values before any other values.
func Float64sAreSorted(x []float64) bool { return IsSorted(x) }

// StringsAreSorted reports whether the slice x is sorted in increasing
// order.
func StringsAreSorted(x []string) bool { return IsSorted(x) }

// SearchInts searches for x in a sorted slice of ints and returns the index
// as specified by Search. The return value is the index to insert x if x is
// not present (it could be len(a)).
func SearchInts(a []int, x int) int {
	i, _ := BinarySearch(a, x)
	return i
}

// SearchFloat64s searches for x in a sorted slice of float64s and returns
// the index as specified by Search.
func SearchFloat64s(a []float64, x float64) int {
	i, _ := BinarySearch(a, x)
	return i
}

// SearchStrings searches for x in a sorted slice of strings and returns the
// index as specified by Search.
func SearchStrings(a []string, x string) int {
	i, _ := BinarySearch(a, x)
	return i
}

// Reverse reverses the elements of the slice in place.
func Reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

type sortedHint int // hint for pdqsort when choosing the pivot

const (
	unknownHint sortedHint = iota
	increasingHint
	decreasingHint
)

// xorshift paper: https://www.jstatsoft.org/article/view/v008i14/xorshift.pdf
type xorshift uint64

func (r *xorshift) Next() uint64 {
	*r ^= *r << 13
	*r ^= *r >> 7
	*r ^= *r << 17
	return uint64(*r)
}

func nextPowerOfTwo(length int) uint {
	return 1 << bits.Len(uint(length))
}
