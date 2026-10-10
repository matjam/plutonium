package hash

import (
	"pluto/alloc"
	"pluto/rt"
	"unsafe"
)

// A Hasher is a key type that hashes itself. A Map whose key type
// implements Hasher (with a value receiver) uses its Hash method; keys that
// are equal must have equal hashes.
type Hasher interface {
	Hash(seed uint64) uint64
}

// DefaultSeed is the seed maps use unless given one.
const DefaultSeed = 0x2d358dccaa6c78a5

// A hashMethod says how a Map hashes its keys, chosen once when the Map is
// initialised.
type hashMethod enum {
	none   // not initialised
	word   // integer, pointer, bool: hash the key's bytes as an integer
	float  // float32 or float64
	str    // string
	hasher // the key implements Hasher
	custom // a function given to InitFunc
}

// Slot metadata. A full slot stores 0x80 | the top 7 bits of its hash so
// most mismatches are rejected without comparing keys.
const (
	metaEmpty = 0
	metaFull  = 0x80
)

type slot[K comparable, V any] struct {
	key K
	val V
}

// A Map is a hash map from K to V whose table comes from an allocator. It
// is an open-addressing table with linear probing and backward-shift
// deletion, kept at most 3/4 full; when it fills, it allocates a table twice
// as large from its allocator, moves the entries and frees the old table.
//
// The zero Map is empty and can be read (Get, Len, Iter), but must be
// initialised with Init, InitFunc or NewMap before Set. A Map must not be
// copied after first use; pass a *Map. It is not safe for concurrent use.
//
// Keys are hashed according to their type: integers, booleans, pointers,
// floats and strings (and types defined on them) out of the box; a key type
// with a Hash(seed uint64) uint64 method uses it; any other key type needs
// a hash function given to InitFunc.
type Map[K comparable, V any] struct {
	slots []slot[K, V]
	meta  []uint8
	n     int // number of entries
	limit int // grow when n reaches limit
	a     alloc.Allocator
	seed  uint64
	how   hashMethod
	ksize uintptr // key size, for hashMethod.word and float
	fn    func(K) uint64
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
// the key type has no default hash (use InitFunc) or if m already holds a
// table.
func (m *Map[K, V]) Init(capacity int, a alloc.Allocator = alloc.Default()) {
	m.InitSeed(capacity, DefaultSeed, a)
}

// InitSeed is Init with a hash seed.
func (m *Map[K, V]) InitSeed(capacity int, seed uint64, a alloc.Allocator = alloc.Default()) {
	m.setup(a, seed)
	var zero K
	if _, ok := any(zero).(Hasher); ok {
		m.how = hashMethod.hasher
	} else {
		kind, size, _, _ := rt.TypeInfo(any(zero))
		switch kind {
		case rt.KindBool, rt.KindInt, rt.KindInt8, rt.KindInt16, rt.KindInt32, rt.KindInt64,
			rt.KindUint, rt.KindUint8, rt.KindUint16, rt.KindUint32, rt.KindUint64, rt.KindUintptr,
			rt.KindPointer, rt.KindUnsafePointer:
			m.how = hashMethod.word
			m.ksize = size
		case rt.KindFloat32, rt.KindFloat64:
			m.how = hashMethod.float
			m.ksize = size
		case rt.KindString:
			m.how = hashMethod.str
		default:
			panic("hash: key type has no default hash function; use Map.InitFunc")
		}
	}
	m.reserve(capacity)
}

// InitFunc prepares a zero Map to allocate from a (by default the default
// allocator) and hash keys with hash, which must give equal keys equal
// hashes. The Map keeps hash, so it must not be a closure that outlives its
// frame.
func (m *Map[K, V]) InitFunc(capacity int, hash func(K) uint64, a alloc.Allocator = alloc.Default()) {
	m.setup(a, DefaultSeed)
	m.how = hashMethod.custom
	m.fn = hash
	m.reserve(capacity)
}

func (m *Map[K, V]) setup(a alloc.Allocator, seed uint64) {
	if len(m.slots) != 0 {
		panic("hash: Init on a Map that holds a table")
	}
	if a == nil {
		panic("hash: Init with a nil allocator")
	}
	m.a = a
	m.seed = seed
}

// reserve allocates a table with room for capacity entries, if needed.
func (m *Map[K, V]) reserve(capacity int) {
	if capacity <= 0 {
		return
	}
	size := 8
	for size*3/4 < capacity {
		size <<= 1
	}
	if size > len(m.slots) {
		m.resize(size)
	}
}

// hash returns the hash of k. The switch lists every hashMethod, so adding
// one without handling it here is a compile error.
func (m *Map[K, V]) hash(k K) uint64 {
	switch m.how {
	// The key is read through its address in the form
	// *(*T)(unsafe.Pointer(&k)), which the compiler knows does not change
	// k, so a string key is borrowed without being counted.
	case word:
		var x uint64
		switch m.ksize {
		case 1:
			x = uint64(*(*uint8)(unsafe.Pointer(&k)))
		case 2:
			x = uint64(*(*uint16)(unsafe.Pointer(&k)))
		case 4:
			x = uint64(*(*uint32)(unsafe.Pointer(&k)))
		default:
			x = *(*uint64)(unsafe.Pointer(&k))
		}
		return Uint64(x, m.seed)
	case float:
		if m.ksize == 4 {
			return Float64(float64(*(*float32)(unsafe.Pointer(&k))), m.seed)
		}
		return Float64(*(*float64)(unsafe.Pointer(&k)), m.seed)
	case str:
		return String(*(*string)(unsafe.Pointer(&k)), m.seed)
	case hasher:
		return any(k).(Hasher).Hash(m.seed)
	case custom:
		return m.fn(k)
	case none:
	}
	panic("hash: Map used before Init")
}

// Len returns the number of entries.
func (m *Map[K, V]) Len() int { return m.n }

// find returns the slot index holding k, or -1.
func (m *Map[K, V]) find(k K) int {
	if m.n == 0 {
		return -1
	}
	h := m.hash(k)
	mask := len(m.slots) - 1
	tag := uint8(metaFull | h>>57)
	for i := int(h) & mask; ; i = (i + 1) & mask {
		t := m.meta[i]
		if t == metaEmpty {
			return -1
		}
		if t == tag && m.slots[i].key == k {
			return i
		}
	}
}

// Get returns the value stored for k and true, or the zero V and false if k
// is not in the map.
func (m *Map[K, V]) Get(k K) (V, bool) {
	if i := m.find(k); i >= 0 {
		return m.slots[i].val, true
	}
	var zero V
	return zero, false
}

// Has reports whether k is in the map.
func (m *Map[K, V]) Has(k K) bool { return m.find(k) >= 0 }

// Ptr returns a pointer to the value stored for k, or nil. The pointer is
// valid until the map is next modified.
func (m *Map[K, V]) Ptr(k K) *V {
	if i := m.find(k); i >= 0 {
		return &m.slots[i].val
	}
	return nil
}

// Set stores v for k, replacing any value already stored.
func (m *Map[K, V]) Set(k K, v V) {
	*m.slot(k) = v
}

// Upsert returns a pointer to the value for k, adding k with the zero V if
// it is not present; inserted reports whether it was added. The pointer is
// valid until the map is next modified.
func (m *Map[K, V]) Upsert(k K) (p *V, inserted bool) {
	n := m.n
	p = m.slot(k)
	return p, m.n != n
}

// slot returns the value slot for k, inserting k if needed.
func (m *Map[K, V]) slot(k K) *V {
	if m.n >= m.limit {
		if m.how == hashMethod.none {
			panic("hash: Set on a Map without an allocator (use NewMap or Init)")
		}
		size := 2 * len(m.slots)
		if size < 8 {
			size = 8
		}
		m.resize(size)
	}
	h := m.hash(k)
	mask := len(m.slots) - 1
	tag := uint8(metaFull | h>>57)
	for i := int(h) & mask; ; i = (i + 1) & mask {
		t := m.meta[i]
		if t == metaEmpty {
			m.meta[i] = tag
			m.slots[i].key = k
			m.n++
			return &m.slots[i].val
		}
		if t == tag && m.slots[i].key == k {
			return &m.slots[i].val
		}
	}
}

// Delete removes k from the map and reports whether it was present.
func (m *Map[K, V]) Delete(k K) bool {
	i := m.find(k)
	if i < 0 {
		return false
	}
	// Backward-shift deletion: move later entries of the same probe run
	// into the hole so lookups never need tombstones.
	mask := len(m.slots) - 1
	j := i
	for {
		j = (j + 1) & mask
		if m.meta[j] == metaEmpty {
			break
		}
		home := int(m.hash(m.slots[j].key)) & mask
		// The entry at j may move to i if its home is not in (i, j]
		// (cyclically).
		if (j > i && (home <= i || home > j)) || (j < i && home <= i && home > j) {
			m.slots[i] = m.slots[j]
			m.meta[i] = m.meta[j]
			i = j
		}
	}
	var zero slot[K, V]
	m.slots[i] = zero
	m.meta[i] = metaEmpty
	m.n--
	return true
}

// Clear removes all entries, keeping the table.
func (m *Map[K, V]) Clear() {
	clear(m.slots)
	clear(m.meta)
	m.n = 0
}

// Free drops the entries (releasing the strings in their keys and values,
// language spec section 6.10), returns the table to the allocator and
// leaves the map empty. The map keeps its allocator and hash function, so
// it can be reused.
func (m *Map[K, V]) Free() {
	if len(m.slots) > 0 && rt.HoldsStrings[slot[K, V]]() {
		// Empty slots are zero, so every slot can be released.
		rt.ReleaseValues(&m.slots[0], len(m.slots))
	}
	m.release()
	m.slots = nil
	m.meta = nil
	m.n = 0
	m.limit = 0
}

// tableSize returns the bytes and alignment of a table of size slots: the
// slots followed by one metadata byte per slot.
func tableSize[K comparable, V any](size int) (uintptr, uintptr) {
	var s slot[K, V]
	return uintptr(size) * (unsafe.Sizeof(s) + 1), unsafe.Alignof(s)
}

func (m *Map[K, V]) release() {
	if len(m.slots) == 0 {
		return
	}
	n, align := tableSize[K, V](len(m.slots))
	m.a.Free(unsafe.Pointer(unsafe.SliceData(m.slots)), n, align)
}

// resize moves the entries into a new table of size slots (a power of two).
func (m *Map[K, V]) resize(size int) {
	n, align := tableSize[K, V](size)
	p := m.a.Alloc(n, align)
	if p == nil {
		panic("hash: out of memory")
	}
	var s slot[K, V]
	slots := unsafe.Slice((*slot[K, V])(p), size)
	meta := unsafe.Slice((*uint8)(unsafe.Add(p, uintptr(size)*unsafe.Sizeof(s))), size)
	oldSlots, oldMeta := m.slots, m.meta
	mask := size - 1
	for i := range oldSlots {
		if oldMeta[i] == metaEmpty {
			continue
		}
		h := m.hash(oldSlots[i].key)
		j := int(h) & mask
		for meta[j] != metaEmpty {
			j = (j + 1) & mask
		}
		// The entry moves: its strings keep their counts, and the old
		// table is freed without releasing them.
		rt.MoveValues(&slots[j], &oldSlots[i], 1)
		meta[j] = oldMeta[i]
	}
	m.release()
	m.slots = slots
	m.meta = meta
	m.limit = size / 4 * 3
}

// An Iter walks the entries of a Map in an unspecified order. The map must
// not be modified during the walk, except by SetValue on the current entry.
type Iter[K comparable, V any] struct {
	m *Map[K, V]
	i int
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
	m := it.m
	for it.i < len(m.slots) {
		i := it.i
		it.i++
		if m.meta[i] != metaEmpty {
			return m.slots[i].key, m.slots[i].val, true
		}
	}
	var k K
	var v V
	return k, v, false
}

// Value returns a pointer to the value of the entry Next last returned.
func (it *Iter[K, V]) Value() *V { return &it.m.slots[it.i-1].val }

// A Set is a set of K, a Map with no values.
type Set[K comparable] struct {
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

// A SetIter walks the elements of a Set in an unspecified order.
type SetIter[K comparable] struct {
	it Iter[K, struct{}]
}

// Iter returns an iterator over the set's elements.
func (s *Set[K]) Iter() SetIter[K] { return SetIter[K]{s.m.Iter()} }

// Next returns the next element, or false when there are no more.
func (it *SetIter[K]) Next() (K, bool) {
	k, _, ok := it.it.Next()
	return k, ok
}
