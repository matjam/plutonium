// Package compat gives code translated from Go by tools/go2pu the Go
// semantics it relies on, on top of Plutonium's allocators: maps that are
// references, append that grows, string concatenation and conversions that
// copy, and fmt.Sprintf and fmt.Errorf that return new strings and errors.
//
// Everything here allocates from alloc.Default() and never frees. That is
// Go's garbage-collected model with the collector replaced by an arena: the
// compiler installs an arena with alloc.Use for each phase and releases it
// when the phase's results are no longer needed (see
// docs/plans/2026-10-09-self-hosting-plan.md). Hand-written Plutonium should
// use pluto/hash, pluto/vec and pluto/strings directly instead.
package compat

import (
	"pluto/alloc"
	"pluto/hash"
	"pluto/rt"
)

// NewMap returns a new, empty map with room for n entries, like Go's
// make(map[K]V, n). Go maps are references, so a translated map is a
// *hash.Map. Interface-typed keys, which hash.Map cannot hash by itself,
// are hashed by their dynamic value (see ifaceHash).
func NewMap[K comparable, V any](n int) *hash.Map[K, V] {
	m := alloc.New[hash.Map[K, V]]()
	var zero K
	if any(zero) == nil {
		// Only an interface type has a nil zero value that converts to a
		// nil any.
		m.InitFunc(alloc.Default(), n, func(k K) uint64 { return ifaceHash(any(k)) })
	} else {
		m.Init(alloc.Default(), n)
	}
	return m
}

// NewMapFunc is NewMap for keys hash.Map cannot hash by itself (structs and
// arrays), with a hash function go2pu generates from the key type.
func NewMapFunc[K comparable, V any](n int, h func(K) uint64) *hash.Map[K, V] {
	m := alloc.New[hash.Map[K, V]]()
	m.InitFunc(alloc.Default(), n, h)
	return m
}

// MapOfFunc is MapOf with a hash function, as for NewMapFunc.
func MapOfFunc[K comparable, V any](h func(K) uint64, entries ...Pair[K, V]) *hash.Map[K, V] {
	m := NewMapFunc[K, V](len(entries), h)
	for _, e := range entries {
		m.Set(e.Key, e.Val)
	}
	return m
}

// HashAny hashes a value of a type hash.Map hashes by itself (integers,
// booleans, pointers, floats and strings), for generated key hashes.
func HashAny[T comparable](x T) uint64 { return ifaceHash(any(x)) }

// HashBytes hashes a byte slice by its contents.
func HashBytes(b []byte) uint64 { return hash.Bytes(b, hash.DefaultSeed) }

// Mix combines a running hash with the hash of the next component.
func Mix(h, x uint64) uint64 { return hash.Uint64(h^x, hash.DefaultSeed) }

// ifaceHash hashes an interface value by its dynamic value: the data word
// for values that fit in it (pointers, integers), the contents for strings.
// Other dynamic types panic: their equality compares contents the hash
// cannot see.
func ifaceHash(k any) uint64 {
	kind, _, _, data := rt.TypeInfo(k)
	switch kind {
	case 0:
		return 0
	case rt.KindString:
		return hash.String(*(*string)(data), hash.DefaultSeed) ^ uint64(kind)
	case rt.KindStruct, rt.KindArray, rt.KindSlice, rt.KindInterface:
		panic("compat: map key holds a value with no hash: give the key type a Hash method")
	}
	return hash.Uint64(uint64(uintptr(data))^uint64(kind)<<56, hash.DefaultSeed)
}

// MapAt returns m[k], or the zero V if k is absent or m is nil.
func MapAt[K comparable, V any](m *hash.Map[K, V], k K) V {
	if m == nil {
		var zero V
		return zero
	}
	v, _ := m.Get(k)
	return v
}

// MapGet returns m[k] and whether it was present; m may be nil.
func MapGet[K comparable, V any](m *hash.Map[K, V], k K) (V, bool) {
	if m == nil {
		var zero V
		return zero, false
	}
	return m.Get(k)
}

// MapSlot returns a pointer to m[k], adding k with the zero V if absent,
// for m[k] op= v and m[k]++.
func MapSlot[K comparable, V any](m *hash.Map[K, V], k K) *V {
	p, _ := m.Upsert(k)
	return p
}

// MapDelete removes k from m; m may be nil.
func MapDelete[K comparable, V any](m *hash.Map[K, V], k K) {
	if m != nil {
		m.Delete(k)
	}
}

// MapLen returns the number of entries in m; m may be nil.
func MapLen[K comparable, V any](m *hash.Map[K, V]) int {
	if m == nil {
		return 0
	}
	return m.Len()
}

// MapClear removes every entry of m; m may be nil.
func MapClear[K comparable, V any](m *hash.Map[K, V]) {
	if m != nil {
		m.Clear()
	}
}

// MapIter returns an iterator over m; m may be nil. The order is fixed for
// a given sequence of insertions, unlike Go's randomised order.
func MapIter[K comparable, V any](m *hash.Map[K, V]) hash.Iter[K, V] {
	if m == nil {
		m = alloc.New[hash.Map[K, V]]()
	}
	return m.Iter()
}

// A Pair is one entry of a map literal.
type Pair[K comparable, V any] struct {
	Key K
	Val V
}

// MapOf returns a new map holding the given entries, for Go map literals.
func MapOf[K comparable, V any](entries ...Pair[K, V]) *hash.Map[K, V] {
	m := NewMap[K, V](len(entries))
	for _, e := range entries {
		m.Set(e.Key, e.Val)
	}
	return m
}
