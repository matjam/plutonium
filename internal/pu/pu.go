// Package pu mirrors, in Go, the Plutonium library types the compiler's
// data structures are made of, so that the compiler's Go sources can be
// written in the shape their Plutonium translation will have (self-hosting
// plan, section 11). Every name here has a Plutonium counterpart with the
// same methods, and tools/go2pu translates one into the other by name:
//
//	pu.Vec[T], pu.NewVec[T](), pu.VecWithCapacity[T](n)     vec.Vec[T], vec.New[T](), vec.WithCapacity[T](n)
//	pu.Map[K, V], pu.NewMap[K, V](n), pu.MapIter[K, V]      hash.Map[K, V], hash.NewMap[K, V](n), hash.Iter[K, V]
//	pu.Set[K], pu.NewSet[K](n), pu.SetIter[K]               hash.Set[K], hash.NewSet[K](n), hash.SetIter[K]
//	pu.Buf                                                  strings.Builder (strings.Buf once it exists)
//	pu.Sprintf(format, args...)                             pu.Sprintf in selfhost/pu (fmt.Sprintf once it exists)
//	pu.New(v)                                               pu.New(v) in selfhost/pu: v copied into the phase arena
//
// The types are owners in Plutonium (language spec section 6.4): a value
// holds its storage, moves on assignment, and is released by Drop. In Go
// the collector owns everything and Drop only empties the value; each type
// embeds a noCopy marker, so go vet's copylocks check reports a copy, which
// is Go's spelling of a use after move. Clone is the explicit copy in both
// languages.
//
// Pointer graphs (AST nodes, types, SSA values) stay *T, allocated with New:
// in Plutonium they are views of the phase arena (spec section 6.6, "an
// owner or an arena"), which the driver installs as the default allocator
// for each phase. Under the accepted proposal new(T) returns owned[T],
// which cannot be stored in a *T field, so arena allocation is written as
// New rather than &T{...} or new(T).
//
// Map iteration order differs between the two (Go randomises it, a
// Plutonium hash.Map iterates in table order), so code must not depend on
// it, as the compiler must not already.
package pu

import (
	"fmt"
	"unicode/utf8"
)

// noCopy makes go vet's copylocks check report copies of the value that
// embeds it (https://golang.org/issues/8005).
type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// New returns a pointer to a new variable holding v. In Plutonium the
// variable lives in the current phase arena.
func New[T any](v T) *T { return &v }

// ----------------------------------------------------------------------------
// Vec

// A Vec is a growable sequence of T: pluto/vec.Vec. The zero Vec is empty
// and ready to use.
type Vec[T any] struct {
	_ noCopy
	s []T
}

// NewVec returns an empty Vec (vec.New).
func NewVec[T any]() Vec[T] { return Vec[T]{} }

// VecWithCapacity returns an empty Vec with room for n elements
// (vec.WithCapacity).
func VecWithCapacity[T any](n int) Vec[T] { return Vec[T]{s: make([]T, 0, n)} }

// Len returns the number of elements.
func (v *Vec[T]) Len() int { return len(v.s) }

// Cap returns the number of elements v can hold without growing.
func (v *Vec[T]) Cap() int { return cap(v.s) }

// Slice returns the elements, a view valid until v grows or is dropped. Its
// capacity is its length.
func (v *Vec[T]) Slice() []T { return v.s[:len(v.s):len(v.s)] }

// At returns element i.
func (v *Vec[T]) At(i int) T { return v.s[i] }

// Set sets element i to x.
func (v *Vec[T]) Set(i int, x T) { v.s[i] = x }

// Ptr returns a pointer to element i, valid until v grows or is dropped.
func (v *Vec[T]) Ptr(i int) *T { return &v.s[i] }

// Last returns the last element and true, or the zero T and false.
func (v *Vec[T]) Last() (T, bool) {
	if len(v.s) == 0 {
		var zero T
		return zero, false
	}
	return v.s[len(v.s)-1], true
}

// Push appends x.
func (v *Vec[T]) Push(x T) { v.s = append(v.s, x) }

// Append appends xs, which must not be a view of v.
func (v *Vec[T]) Append(xs ...T) { v.s = append(v.s, xs...) }

// Pop removes and returns the last element, or the zero T and false.
func (v *Vec[T]) Pop() (T, bool) {
	var zero T
	n := len(v.s)
	if n == 0 {
		return zero, false
	}
	x := v.s[n-1]
	v.s[n-1] = zero
	v.s = v.s[:n-1]
	return x, true
}

// Insert inserts x at index i.
func (v *Vec[T]) Insert(i int, x T) {
	if i < 0 || i > len(v.s) {
		panic("vec: Insert index out of range")
	}
	var zero T
	v.s = append(v.s, zero)
	copy(v.s[i+1:], v.s[i:])
	v.s[i] = x
}

// Remove removes and returns element i, shifting later elements down.
func (v *Vec[T]) Remove(i int) T {
	x := v.s[i]
	n := len(v.s)
	copy(v.s[i:], v.s[i+1:])
	var zero T
	v.s[n-1] = zero
	v.s = v.s[:n-1]
	return x
}

// SwapRemove removes and returns element i, moving the last element into
// its place.
func (v *Vec[T]) SwapRemove(i int) T {
	x := v.s[i]
	n := len(v.s)
	v.s[i] = v.s[n-1]
	var zero T
	v.s[n-1] = zero
	v.s = v.s[:n-1]
	return x
}

// Reserve makes room for at least n more elements.
func (v *Vec[T]) Reserve(n int) {
	if n < 0 {
		panic("vec: negative Reserve")
	}
	if cap(v.s)-len(v.s) < n {
		s := make([]T, len(v.s), len(v.s)+n)
		copy(s, v.s)
		v.s = s
	}
}

// Truncate shortens v to n elements.
func (v *Vec[T]) Truncate(n int) {
	if n < 0 || n > len(v.s) {
		panic("vec: Truncate out of range")
	}
	clear(v.s[n:])
	v.s = v.s[:n]
}

// Clear removes all elements, keeping the storage.
func (v *Vec[T]) Clear() { v.Truncate(0) }

// Clone returns an independent copy of v.
func (v *Vec[T]) Clone() Vec[T] {
	s := make([]T, len(v.s))
	copy(s, v.s)
	return Vec[T]{s: s}
}

// Drop releases v's storage and leaves v empty.
func (v *Vec[T]) Drop() { v.s = nil }

// ----------------------------------------------------------------------------
// Map and Set

// A Map maps K to V: pluto/hash.Map. The zero Map is empty and ready to
// use. Values are stored behind pointers so that Ptr and Upsert can return
// them, as hash.Map's do.
type Map[K comparable, V any] struct {
	_ noCopy
	m map[K]*V
}

// NewMap returns an empty Map with room for capacity entries
// (hash.NewMap).
func NewMap[K comparable, V any](capacity int) Map[K, V] {
	return Map[K, V]{m: make(map[K]*V, capacity)}
}

// Len returns the number of entries.
func (m *Map[K, V]) Len() int { return len(m.m) }

// Get returns the value for k and true, or the zero V and false.
func (m *Map[K, V]) Get(k K) (V, bool) {
	if p, ok := m.m[k]; ok {
		return *p, true
	}
	var zero V
	return zero, false
}

// At returns the value for k, or the zero V (Go's m[k]).
func (m *Map[K, V]) At(k K) V {
	v, _ := m.Get(k)
	return v
}

// Has reports whether k is in the map.
func (m *Map[K, V]) Has(k K) bool {
	_, ok := m.m[k]
	return ok
}

// Ptr returns a pointer to the value for k, or nil. In Plutonium it is
// valid until the map is next modified.
func (m *Map[K, V]) Ptr(k K) *V { return m.m[k] }

// Set stores v for k.
func (m *Map[K, V]) Set(k K, v V) {
	if p, ok := m.m[k]; ok {
		*p = v
		return
	}
	if m.m == nil {
		m.m = map[K]*V{}
	}
	m.m[k] = &v
}

// Upsert returns a pointer to the value for k, adding k with the zero V if
// it is absent; inserted reports whether it was added.
func (m *Map[K, V]) Upsert(k K) (p *V, inserted bool) {
	if p, ok := m.m[k]; ok {
		return p, false
	}
	if m.m == nil {
		m.m = map[K]*V{}
	}
	p = new(V)
	m.m[k] = p
	return p, true
}

// Delete removes k and reports whether it was present.
func (m *Map[K, V]) Delete(k K) bool {
	_, ok := m.m[k]
	delete(m.m, k)
	return ok
}

// Clear removes all entries.
func (m *Map[K, V]) Clear() { clear(m.m) }

// Clone returns an independent copy of m.
func (m *Map[K, V]) Clone() Map[K, V] {
	c := make(map[K]*V, len(m.m))
	for k, p := range m.m {
		v := *p
		c[k] = &v
	}
	return Map[K, V]{m: c}
}

// Drop releases m's table and leaves m empty.
func (m *Map[K, V]) Drop() { m.m = nil }

// A MapIter walks a Map's entries in an unspecified order: hash.Iter.
type MapIter[K comparable, V any] struct {
	m    *Map[K, V]
	keys []K
	i    int
}

// Iter returns an iterator over m's entries. The map must not be modified
// during the walk, except through Value.
//
//	it := m.Iter()
//	for k, v, ok := it.Next(); ok; k, v, ok = it.Next() { ... }
func (m *Map[K, V]) Iter() MapIter[K, V] {
	keys := make([]K, 0, len(m.m))
	for k := range m.m {
		keys = append(keys, k)
	}
	return MapIter[K, V]{m: m, keys: keys}
}

// Next returns the next entry, or false when there are no more.
func (it *MapIter[K, V]) Next() (K, V, bool) {
	if it.i < len(it.keys) {
		k := it.keys[it.i]
		it.i++
		return k, *it.m.m[k], true
	}
	var k K
	var v V
	return k, v, false
}

// Value returns a pointer to the value of the entry Next last returned.
func (it *MapIter[K, V]) Value() *V { return it.m.m[it.keys[it.i-1]] }

// A Set is a set of K: pluto/hash.Set. The zero Set is empty and ready to
// use.
type Set[K comparable] struct {
	_ noCopy
	m map[K]struct{}
}

// NewSet returns an empty Set with room for capacity elements
// (hash.NewSet).
func NewSet[K comparable](capacity int) Set[K] {
	return Set[K]{m: make(map[K]struct{}, capacity)}
}

// Add adds k and reports whether it was newly added.
func (s *Set[K]) Add(k K) bool {
	if _, ok := s.m[k]; ok {
		return false
	}
	if s.m == nil {
		s.m = map[K]struct{}{}
	}
	s.m[k] = struct{}{}
	return true
}

// Has reports whether k is in the set.
func (s *Set[K]) Has(k K) bool {
	_, ok := s.m[k]
	return ok
}

// Delete removes k and reports whether it was present.
func (s *Set[K]) Delete(k K) bool {
	_, ok := s.m[k]
	delete(s.m, k)
	return ok
}

// Len returns the number of elements.
func (s *Set[K]) Len() int { return len(s.m) }

// Clear removes all elements.
func (s *Set[K]) Clear() { clear(s.m) }

// Clone returns an independent copy of s.
func (s *Set[K]) Clone() Set[K] {
	c := make(map[K]struct{}, len(s.m))
	for k := range s.m {
		c[k] = struct{}{}
	}
	return Set[K]{m: c}
}

// Drop releases s's table and leaves s empty.
func (s *Set[K]) Drop() { s.m = nil }

// A SetIter walks a Set's elements in an unspecified order: hash.SetIter.
type SetIter[K comparable] struct {
	keys []K
	i    int
}

// Iter returns an iterator over the set's elements.
func (s *Set[K]) Iter() SetIter[K] {
	keys := make([]K, 0, len(s.m))
	for k := range s.m {
		keys = append(keys, k)
	}
	return SetIter[K]{keys: keys}
}

// Next returns the next element, or false when there are no more.
func (it *SetIter[K]) Next() (K, bool) {
	if it.i < len(it.keys) {
		it.i++
		return it.keys[it.i-1], true
	}
	var k K
	return k, false
}

// ----------------------------------------------------------------------------
// Buf

// A Buf builds text: strings.Builder today, strings.Buf (the owning string
// buffer of the language spec) once it exists. The zero Buf is empty and
// ready to use. It is an io.Writer, so fmt.Fprintf can append to it.
type Buf struct {
	_ noCopy
	b []byte
}

// Sprintf formats into a new Buf (the spec's fmt.Sprintf).
func Sprintf(format string, args ...any) Buf { return Buf{b: fmt.Appendf(nil, format, args...)} }

// String returns the text, a view valid until b is modified or dropped.
func (b *Buf) String() string { return string(b.b) }

// Bytes returns the text's bytes, a view like String.
func (b *Buf) Bytes() []byte { return b.b }

// Len returns the length of the text.
func (b *Buf) Len() int { return len(b.b) }

// Cap returns the capacity of the buffer.
func (b *Buf) Cap() int { return cap(b.b) }

// Reset empties b, keeping its storage.
func (b *Buf) Reset() { b.b = b.b[:0] }

// Grow makes room for n more bytes.
func (b *Buf) Grow(n int) {
	if n < 0 {
		panic("strings.Builder.Grow: negative count")
	}
	if cap(b.b)-len(b.b) < n {
		nb := make([]byte, len(b.b), 2*cap(b.b)+n)
		copy(nb, b.b)
		b.b = nb
	}
}

// Write appends p.
func (b *Buf) Write(p []byte) (int, error) {
	b.b = append(b.b, p...)
	return len(p), nil
}

// WriteString appends s.
func (b *Buf) WriteString(s string) (int, error) {
	b.b = append(b.b, s...)
	return len(s), nil
}

// WriteByte appends c.
func (b *Buf) WriteByte(c byte) error {
	b.b = append(b.b, c)
	return nil
}

// WriteRune appends the UTF-8 encoding of r.
func (b *Buf) WriteRune(r rune) (int, error) {
	n := len(b.b)
	b.b = utf8.AppendRune(b.b, r)
	return len(b.b) - n, nil
}

// Drop releases b's storage and leaves b empty.
func (b *Buf) Drop() { b.b = nil }
