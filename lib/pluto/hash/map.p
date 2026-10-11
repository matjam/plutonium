package hash

import (
	"pluto/alloc"
	"pluto/iter"
	"pluto/rt"
)

// A Hasher is a key type that hashes itself. A Map whose key type
// implements Hasher (with a value receiver) uses its Hash method; keys that
// are equal must have equal hashes.
type Hasher interface {
	Hash(seed uint64) uint64
}

// DefaultSeed is the seed a Map uses unless given one.
const DefaultSeed = rt.DefaultMapSeed

// A Map is a hash map from K to V whose table comes from an allocator. It
// is the table of the built-in map[K]V (pluto/rt.MapTable) held directly as
// an owner: open addressing with linear probing, kept at most 3/4 full, that
// allocates a table twice as large from its allocator when it fills, moves
// the entries and frees the old table.
//
// The zero Map is empty and ready to use: the first Set initialises it as
// Init with the default allocator would. A Map must not be copied after
// first use; pass a *Map. It is not safe for concurrent use.
//
// Keys are hashed according to their type: integers, booleans, pointers,
// floats and strings, and structs, arrays and interfaces of comparable
// types, as map[K]V hashes them; a key type with a Hash(seed uint64) uint64
// method uses it; InitFunc takes a hash function.
//
// m[k] reads like a Go map element (the zero V for a missing key),
// m[k] = v stores, v, ok := m[k] reports whether k is present, m[k] += 1
// reads and stores, and len(m) is Len (language spec section 11.11). &m[k] is
// not allowed; Ptr and Upsert return pointers into the table.
//
// A Map owns its table, so it is an owner (language spec section 7.18): it
// moves instead of being copied, and Drop releases the table. Unlike
// map[K]V, it uses a fixed seed (DefaultSeed) unless given one with
// InitSeed, so its iteration order is the same in every run.
type Map[K comparable, V any] owner struct {
	t rt.MapTable[K, V]
}

// NewMap returns an empty Map that allocates from a (by default the default
// allocator), with room for capacity entries before it grows.
func NewMap[K comparable, V any](capacity int, a alloc.Allocator = alloc.Default()) Map[K, V] {
	var m Map[K, V]
	m.Init(capacity, a)
	return m
}

// Init prepares a zero Map to allocate from a (by default the default
// allocator), with room for capacity entries before it grows. It panics if
// m already holds a table.
func (m *Map[K, V]) Init(capacity int, a alloc.Allocator = alloc.Default()) {
	m.InitSeed(capacity, DefaultSeed, a)
}

// InitSeed is Init with a hash seed.
func (m *Map[K, V]) InitSeed(capacity int, seed uint64, a alloc.Allocator = alloc.Default()) {
	if a == nil {
		panic("hash: Init with a nil allocator")
	}
	var zero K
	if _, ok := any(zero).(Hasher); ok {
		m.t.InitSeeded(capacity, hasherHash[K], seed, a)
		return
	}
	m.t.Init(capacity, seed, a)
}

// hasherHash hashes a key that implements Hasher.
func hasherHash[K comparable](k K, seed uint64) uint64 { return any(k).(Hasher).Hash(seed) }

// InitFunc prepares a zero Map to allocate from a (by default the default
// allocator) and hash keys with hash, which must give equal keys equal
// hashes. The Map keeps hash, so it must not be a closure that outlives its
// frame.
func (m *Map[K, V]) InitFunc(capacity int, hash func(K) uint64, a alloc.Allocator = alloc.Default()) {
	if a == nil {
		panic("hash: Init with a nil allocator")
	}
	m.t.InitFunc(capacity, hash, a)
}

// Len returns the number of entries.
func (m *Map[K, V]) Len() int { return m.t.Len() }

// Get returns the value stored for k and true, or the zero V and false if k
// is not in the map.
func (m *Map[K, V]) Get(k K) (V, bool) { return m.t.Get(k) }

// At returns the value stored for k, or the zero V if k is not in the map:
// m[k] as a method call.
func (m *Map[K, V]) At(k K) V { return m.t.Index(k) }

// Index returns the value stored for k, or the zero V if k is not in the
// map; it is what m[k] calls (language spec section 6.7).
func (m *Map[K, V]) Index(k K) V { return m.t.Index(k) }

// IndexOk is Get; it is what v, ok := m[k] calls.
func (m *Map[K, V]) IndexOk(k K) (V, bool) { return m.t.Get(k) }

// SetIndex is Set; it is what m[k] = v calls.
func (m *Map[K, V]) SetIndex(k K, v V) { m.t.Set(k, v) }

// Has reports whether k is in the map.
func (m *Map[K, V]) Has(k K) bool { return m.t.Has(k) }

// FmtSlots and FmtSlot let pluto/fmt print m like a Go map, map[k:v], with
// its keys sorted (fmt.Mapper). FmtSlots returns the number of slots, and
// FmtSlot pointers to the key and value in slot i, if it is full.
func (m *Map[K, V]) FmtSlots() int { return m.t.Slots() }

// FmtSlot: see FmtSlots.
func (m *Map[K, V]) FmtSlot(i int) (k, v any, ok bool) {
	kp, vp, full := m.t.Slot(i)
	if !full {
		return nil, nil, false
	}
	return kp, vp, true
}

// Ptr returns a pointer to the value stored for k, or nil. The pointer is
// valid until the map is next modified.
func (m *Map[K, V]) Ptr(k K) *V { return m.t.Ptr(k) }

// Set stores v for k, replacing any value already stored.
func (m *Map[K, V]) Set(k K, v V) { m.t.Set(k, v) }

// Upsert returns a pointer to the value for k, adding k with the zero V if
// it is not present; inserted reports whether it was added. The pointer is
// valid until the map is next modified.
func (m *Map[K, V]) Upsert(k K) (p *V, inserted bool) { return m.t.Upsert(k) }

// Delete removes k from the map and reports whether it was present. No
// other entry moves, so deleting during a range over the map is safe (see
// All).
func (m *Map[K, V]) Delete(k K) bool { return m.t.Delete(k) }

// Clear removes all entries, keeping the table.
func (m *Map[K, V]) Clear() { m.t.Clear() }

// Free drops the entries (releasing the strings in their keys and values,
// language spec section 18.8), returns the table to the allocator and
// leaves the map empty. The map keeps its allocator and hash function, so
// it can be reused.
func (m *Map[K, V]) Free() { m.t.Free() }

// Drop returns the table to the allocator and leaves the map empty: the
// owner's release (Free under its spec name).
func (m *Map[K, V]) Drop() { m.t.Free() }

// Clone returns an independent copy of m, with the same hashing, whose
// table comes from a (by default the default allocator). Keys and values
// are copied as values.
func (m *Map[K, V]) Clone(a alloc.Allocator = alloc.Default()) Map[K, V] {
	return Map[K, V]{t: m.t.Clone(a)}
}

// An Iter walks the entries of a Map in the order All describes. The map
// must not be modified during the walk, except through Value on the current
// entry. Unlike All, an Iter can be advanced a step at a time from anywhere.
type Iter[K comparable, V any] struct {
	m *Map[K, V]
	i int
	v *V
}

// Iter returns an iterator over m's entries:
//
//	it := m.Iter()
//	for k, v, ok := it.Next(); ok; k, v, ok = it.Next() {
//		...
//	}
func (m *Map[K, V]) Iter() Iter[K, V] { return Iter[K, V]{m: m} }

// Next returns the next entry, or false when there are no more.
func (it *Iter[K, V]) Next() (K, V, bool) {
	t := &it.m.t
	for it.i < t.Slots() {
		i := it.i
		it.i++
		if k, v, ok := t.Slot(i); ok {
			it.v = v
			return *k, *v, true
		}
	}
	var k K
	var v V
	return k, v, false
}

// Value returns a pointer to the value of the entry Next last returned.
func (it *Iter[K, V]) Value() *V { return it.v }

// All returns an iterator over m's keys and values:
//
//	for k, v := range m.All() { ... }
//
// Entries come in the order of their slots in the table. That order depends
// on the keys' hashes, the table's size and the history of insertions and
// deletions, not on the order the keys were added; it is the same every
// time the map is walked without being changed in between, and the same in
// every run of a program that makes the same changes with the same seed.
// Iter, Keys and Values use the same order.
//
// Changes during the loop follow Go's rules for maps: an entry that is
// there when the loop starts and is not deleted before the loop reaches it
// is visited exactly once, a deleted one is not visited, and one added
// during the loop may or may not be.
func (m *Map[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		var it rt.MapIter[K, V]
		it.Start(&m.t)
		for {
			k, v, ok := it.Next()
			if !ok || !yield(k, v) {
				return
			}
		}
	}
}

// Keys returns an iterator over m's keys, in the order All uses.
func (m *Map[K, V]) Keys() iter.Seq[K] {
	return func(yield func(K) bool) {
		var it rt.MapIter[K, V]
		it.Start(&m.t)
		for {
			k, ok := it.NextKey()
			if !ok || !yield(k) {
				return
			}
		}
	}
}

// Values returns an iterator over m's values, in the order All uses.
func (m *Map[K, V]) Values() iter.Seq[V] {
	return func(yield func(V) bool) {
		var it rt.MapIter[K, V]
		it.Start(&m.t)
		for {
			_, v, ok := it.Next()
			if !ok || !yield(v) {
				return
			}
		}
	}
}

// Insert adds the pairs of seq to m, replacing the values of keys already
// present, as Go's maps.Insert.
func (m *Map[K, V]) Insert(seq iter.Seq2[K, V]) {
	for k, v := range seq {
		m.Set(k, v)
	}
}

// A Set is a set of K, a Map with no values. Like a Map, it is an owner.
type Set[K comparable] owner struct {
	m Map[K, struct{}]
}

// NewSet returns an empty Set that allocates from a (by default the default
// allocator), with room for capacity elements before it grows.
func NewSet[K comparable](capacity int, a alloc.Allocator = alloc.Default()) Set[K] {
	var s Set[K]
	s.m.Init(capacity, a)
	return s
}

// Init prepares a zero Set to allocate from a; see Map.Init.
func (s *Set[K]) Init(capacity int, a alloc.Allocator = alloc.Default()) { s.m.Init(capacity, a) }

// InitFunc prepares a zero Set with a hash function; see Map.InitFunc.
func (s *Set[K]) InitFunc(capacity int, hash func(K) uint64, a alloc.Allocator = alloc.Default()) {
	s.m.InitFunc(capacity, hash, a)
}

// Add adds k to the set and reports whether it was newly added.
func (s *Set[K]) Add(k K) bool {
	_, inserted := s.m.Upsert(k)
	return inserted
}

// Has reports whether k is in the set.
func (s *Set[K]) Has(k K) bool { return s.m.Has(k) }

// Delete removes k from the set and reports whether it was present.
func (s *Set[K]) Delete(k K) bool { return s.m.Delete(k) }

// Len returns the number of elements.
func (s *Set[K]) Len() int { return s.m.Len() }

// Clear removes all elements, keeping the table.
func (s *Set[K]) Clear() { s.m.Clear() }

// Free returns the table to the allocator and leaves the set empty.
func (s *Set[K]) Free() { s.m.Free() }

// Drop releases the set's table: the owner's release.
func (s *Set[K]) Drop() { s.m.Free() }

// Clone returns an independent copy of s whose table comes from a.
func (s *Set[K]) Clone(a alloc.Allocator = alloc.Default()) Set[K] {
	return Set[K]{m: s.m.Clone(a)}
}

// A SetIter walks the elements of a Set in an unspecified order.
type SetIter[K comparable] struct {
	it Iter[K, struct{}]
}

// Iter returns an iterator over the set's elements.
func (s *Set[K]) Iter() SetIter[K] { return SetIter[K]{s.m.Iter()} }

// All returns an iterator over the set's elements, in the order of
// Map.All.
func (s *Set[K]) All() iter.Seq[K] {
	return func(yield func(K) bool) {
		for k := range s.m.Keys() {
			if !yield(k) {
				return
			}
		}
	}
}

// Insert adds the values of seq to the set.
func (s *Set[K]) Insert(seq iter.Seq[K]) {
	for k := range seq {
		s.Add(k)
	}
}

// Next returns the next element, or false when there are no more.
func (it *SetIter[K]) Next() (K, bool) {
	k, _, ok := it.it.Next()
	return k, ok
}
