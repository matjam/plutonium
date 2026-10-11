// Package iter defines the iterator types that range-over-func loops walk,
// Seq and Seq2, as Go's iter package does, and adaptors that combine them
// without allocating.
//
// An iterator is a function that passes the elements of a sequence, one at
// a time, to a yield function, and stops early if yield returns false:
//
//	func Count(n int) iter.Seq[int] {
//		return func(yield func(int) bool) {
//			for i := 0; i < n; i++ {
//				if !yield(i) {
//					return
//				}
//			}
//		}
//	}
//
//	for i := range Count(3) { ... }
//
// A function like Count builds the closure it returns, and the variables
// the closure captures, in a region of memory its caller provides
// (language spec section 11.5), so the iterator is valid while the caller
// runs and costs no allocation. Every adaptor below works this way, and like
// any function can be called directly or through a func value.
//
// Go's Pull and Pull2, which turn an iterator into a pair of next and stop
// functions, need a coroutine for each iterator; Plutonium has none. Write
// an explicit iterator type with a Next method instead (strings.SplitIter
// and hash.Map.Iter are examples).
package iter

// Seq is an iterator over sequences of individual values. When called as
// seq(yield), seq calls yield(v) for each value v in the sequence, stopping
// early if yield returns false.
type Seq[V any] func(yield func(V) bool)

// Seq2 is an iterator over sequences of pairs of values, most commonly
// key-value pairs. When called as seq(yield), seq calls yield(k, v) for
// each pair (k, v) in the sequence, stopping early if yield returns false.
type Seq2[K, V any] func(yield func(K, V) bool)

// Filter returns an iterator over the values of seq for which keep returns
// true.
func Filter[V any](keep func(V) bool, seq Seq[V]) Seq[V] {
	return func(yield func(V) bool) {
		for v := range seq {
			if keep(v) && !yield(v) {
				return
			}
		}
	}
}

// Filter2 returns an iterator over the pairs of seq for which keep returns
// true.
func Filter2[K, V any](keep func(K, V) bool, seq Seq2[K, V]) Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for k, v := range seq {
			if keep(k, v) && !yield(k, v) {
				return
			}
		}
	}
}

// Map returns an iterator over f applied to the values of seq.
func Map[In, Out any](f func(In) Out, seq Seq[In]) Seq[Out] {
	return func(yield func(Out) bool) {
		for v := range seq {
			if !yield(f(v)) {
				return
			}
		}
	}
}

// Map2 returns an iterator over f applied to the pairs of seq.
func Map2[KIn, VIn, KOut, VOut any](f func(KIn, VIn) (KOut, VOut), seq Seq2[KIn, VIn]) Seq2[KOut, VOut] {
	return func(yield func(KOut, VOut) bool) {
		for k, v := range seq {
			if !yield(f(k, v)) {
				return
			}
		}
	}
}

// Limit returns an iterator over at most the first n values of seq.
func Limit[V any](seq Seq[V], n int) Seq[V] {
	return func(yield func(V) bool) {
		if n <= 0 {
			return
		}
		left := n
		for v := range seq {
			if !yield(v) {
				return
			}
			if left--; left <= 0 {
				return
			}
		}
	}
}

// Limit2 returns an iterator over at most the first n pairs of seq.
func Limit2[K, V any](seq Seq2[K, V], n int) Seq2[K, V] {
	return func(yield func(K, V) bool) {
		if n <= 0 {
			return
		}
		left := n
		for k, v := range seq {
			if !yield(k, v) {
				return
			}
			if left--; left <= 0 {
				return
			}
		}
	}
}

// Concat returns an iterator over the values of each of seqs in turn.
func Concat[V any](seqs ...Seq[V]) Seq[V] {
	return func(yield func(V) bool) {
		for _, seq := range seqs {
			for v := range seq {
				if !yield(v) {
					return
				}
			}
		}
	}
}

// Concat2 returns an iterator over the pairs of each of seqs in turn.
func Concat2[K, V any](seqs ...Seq2[K, V]) Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for _, seq := range seqs {
			for k, v := range seq {
				if !yield(k, v) {
					return
				}
			}
		}
	}
}

// Enumerate returns an iterator over the values of seq paired with their
// positions, counting from 0.
func Enumerate[V any](seq Seq[V]) Seq2[int, V] {
	return func(yield func(int, V) bool) {
		i := 0
		for v := range seq {
			if !yield(i, v) {
				return
			}
			i++
		}
	}
}

// Keys returns an iterator over the first elements of the pairs of seq.
func Keys[K, V any](seq Seq2[K, V]) Seq[K] {
	return func(yield func(K) bool) {
		for k := range seq {
			if !yield(k) {
				return
			}
		}
	}
}

// Values returns an iterator over the second elements of the pairs of seq.
func Values[K, V any](seq Seq2[K, V]) Seq[V] {
	return func(yield func(V) bool) {
		for _, v := range seq {
			if !yield(v) {
				return
			}
		}
	}
}

// Reduce combines the values of seq with f, starting from init, and returns
// the result: f(...f(f(init, v0), v1)..., vn).
func Reduce[Sum, V any](f func(Sum, V) Sum, init Sum, seq Seq[V]) Sum {
	sum := init
	for v := range seq {
		sum = f(sum, v)
	}
	return sum
}

// Count returns the number of values in seq.
func Count[V any](seq Seq[V]) int {
	n := 0
	for range seq {
		n++
	}
	return n
}

// Fill stores the values of seq in out, in order, until out is full, and
// returns how many it stored. It stops seq once out is full.
func Fill[V any](out []V, seq Seq[V]) int {
	n := 0
	if len(out) == 0 {
		return 0
	}
	for v := range seq {
		out[n] = v
		n++
		if n == len(out) {
			break
		}
	}
	return n
}
