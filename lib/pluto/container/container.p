// Package container declares the constraints of the indexing interface
// (language spec section 6.7), for generic code over library containers.
//
// A container is indexed through its methods: x[k] calls x.Index(k),
// x[k] = v calls x.SetIndex(k, v), v, ok := x[k] calls x.IndexOk(k), and
// len(x) calls x.Len(). A type parameter constrained by one of these
// interfaces allows the same syntax:
//
//	func Count[M container.Indexable[K, int], K any](m M, keys []K) {
//		for _, k := range keys {
//			m[k] += 1
//		}
//	}
//
// A range over a container ranges over the iterator its All method
// returns; Iterable allows it on a type parameter:
//
//	func Sum[C container.Iterable[K, int], K any](c C) int {
//		n := 0
//		for _, v := range c {
//			n += v
//		}
//		return n
//	}
//
// vec.Vec[T] and hash.Map[K, V] declare these methods on pointer
// receivers, so their pointers satisfy the constraints:
// Count(&counts, words).
package container

import "pluto/iter"

// Indexable is a container whose elements can be read and assigned with
// x[k] and x[k] = v.
type Indexable[K, V any] interface {
	Index(k K) V
	SetIndex(k K, v V)
}

// Reader is a container whose elements can be read with x[k].
type Reader[K, V any] interface {
	Index(k K) V
}

// Lookup is a container that reports whether it holds a key, with
// v, ok := x[k].
type Lookup[K, V any] interface {
	IndexOk(k K) (V, bool)
}

// Sized is a container with a length, len(x).
type Sized interface {
	Len() int
}

// Iterable is a container that can be ranged over, for k, v := range x,
// which ranges over x.All().
type Iterable[K, V any] interface {
	All() iter.Seq2[K, V]
}
