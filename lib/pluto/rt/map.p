package rt

import "unsafe"

// ----------------------------------------------------------------------------
// Hash tables: the table behind map[K]V (language spec section 4.11) and
// pluto/hash.Map.
//
// A map value is a counted reference to a MapTable in a box with the header
// of a synced[T] (box.p): copies share the table, and the last reference
// drops it. The compiler makes the box and calls mapInit, and lowers m[k],
// m[k] = v, delete, len, clear and range to the functions at the end of
// this file. pluto/hash.Map holds a MapTable directly, as an owner.
//
// The table is open addressing with linear probing. Each slot has a
// metadata byte: empty, deleted, or full with the top seven bits of the
// key's hash. Deleting an entry never moves another (it leaves a deleted
// mark, or empties the slot when the next one is empty), so a walk over the
// slots visits each entry once however many deletions happen during it.
// When full and deleted slots reach three quarters of the table, it is
// rebuilt: twice as large if at least half of them are entries, otherwise
// the same size without the deleted marks.
//
// Iteration follows Go's rules (language spec section 4.11): an entry
// present when the walk starts and not deleted before it is reached is
// produced exactly once, a deleted one is not produced, and one added
// during the walk may or may not be. A walk follows the slots of the table
// it started on. If the table is rebuilt while walks are running, the old
// table is kept (with its own references to its keys and values) until the
// last walk ends, and a walk over it looks each key up in the live table:
// a key that is gone was deleted, and one that is there gives its current
// value.

// DefaultMapSeed is the hash seed of tables given none: pluto/hash.Map's,
// and every map[K]V's in builds without a source of random seeds.
const DefaultMapSeed = 0x2d358dccaa6c78a5

// How a table hashes its keys, chosen when it is initialised.
const (
	mapHashNone   = iota // not initialised
	mapHashWord          // integer, pointer, bool: ksize bytes as an integer
	mapHashFloat         // float32 or float64
	mapHashString        // string
	mapHashMem           // ksize bytes of plain memory (plainMemory)
	mapHashDesc          // anything else: walk the key's descriptor
	mapHashFunc          // fn, given by the user (pluto/hash.Map.InitFunc)
	mapHashSeeded        // sfn, given by the user (pluto/hash.Hasher)
)

// Slot metadata.
const (
	mapEmpty   = 0
	mapDeleted = 1
	mapFull    = 0x80 // | the top 7 bits of the hash
)

type mapSlot[K comparable, V any] struct {
	key K
	val V
}

// A MapTable is a hash table from K to V whose memory comes from an
// allocator. It owns that memory and the references its keys and values
// hold. The zero MapTable is empty and must be initialised (Init, InitFunc
// or InitSeeded) before anything is stored. It is not safe for concurrent
// use: a map that several tasks share is guarded by a lock (spec section
// 6.11).
type MapTable[K comparable, V any] owner struct {
	slots   []mapSlot[K, V] // a power of two of them, or none
	meta    []uint8         // one per slot, in the same allocation after the slots
	size    int             // len(slots)
	n       int             // entries
	used    int             // entries and deleted marks
	limit   int             // rebuild when used reaches limit
	a       Allocator
	seed    uint64
	how     uint8
	ksize   uintptr
	kt      *typeDesc // the key's descriptor (mapHashDesc)
	fn      func(K) uint64
	sfn     func(K, uint64) uint64
	iters   uint64  // walks running
	retired uintptr // *mapRetired: tables kept for the walks running
}

// A mapRetired is a table rebuilt while walks were running over it. The
// list is the table's own memory, from its allocator, and is kept as
// uintptrs, which the escape analysis takes for heap memory, as it is.
type mapRetired struct {
	next  uintptr
	slots unsafe.Pointer
	size  int
}

// mapSeedBase is mixed into every map's seed: random in hosted builds
// (rt_linux.p), DefaultMapSeed unless the kernel calls SetMapSeed in
// freestanding ones.
var (
	mapSeedBase  uint64 = DefaultMapSeed
	mapSeedCount uint64
)

// SetMapSeed sets the value that the seeds of map[K]V tables made from now
// on are derived from. Hosted programs start with random bytes from the
// operating system; a kernel with a source of entropy calls it early, and
// one without leaves the fixed default, which makes iteration order the
// same in every run but lets keys chosen by an attacker collide (spec
// section 4.11).
func SetMapSeed(seed uint64) { mapSeedBase = seed }

// nextMapSeed returns a new map's seed: the base mixed with a counter, so
// that each map orders its keys differently.
func nextMapSeed() uint64 {
	n := addMapCount(&mapSeedCount, 1)
	return mix(mapSeedBase^wyp0, uint64(n)^wyp1)
}

// addMapCount adds delta to a count shared by tasks once the program is
// threaded and returns the new value.
//
//pu:inline
func addMapCount(c *uint64, delta int64) int64 {
	if atomicCounts {
		return int64(xadd64(c, delta))
	}
	*c = uint64(int64(*c) + delta)
	return int64(*c)
}

// Init prepares a zero table to allocate from a (the current allocator if
// nil) with room for capacity entries, hashing keys by their type with seed.
// It panics if a table is already allocated.
func (t *MapTable[K, V]) Init(capacity int, seed uint64, a Allocator) {
	t.setup(a, seed)
	var zero K
	kt, _ := TypeOf(any(&zero))
	kt = kt.elem
	t.kt = kt
	switch kt.kind {
	case KindBool, KindInt, KindInt8, KindInt16, KindInt32, KindInt64,
		KindUint, KindUint8, KindUint16, KindUint32, KindUint64, KindUintptr,
		KindPointer, KindUnsafePointer, KindChan, KindOwned:
		t.how = mapHashWord
		t.ksize = kt.size
	case KindFloat32, KindFloat64:
		t.how = mapHashFloat
		t.ksize = kt.size
	case KindString:
		t.how = mapHashString
	default:
		if plainMemory(kt) {
			t.how = mapHashMem
			t.ksize = kt.size
		} else {
			t.how = mapHashDesc
		}
	}
	t.Reserve(capacity)
}

// InitFunc is Init with a hash function, which must give equal keys equal
// hashes; the table keeps it.
func (t *MapTable[K, V]) InitFunc(capacity int, fn func(K) uint64, a Allocator) {
	t.setup(a, DefaultMapSeed)
	t.how = mapHashFunc
	t.fn = fn
	t.Reserve(capacity)
}

// InitSeeded is Init with a hash function that takes the table's seed.
func (t *MapTable[K, V]) InitSeeded(capacity int, fn func(K, uint64) uint64, seed uint64, a Allocator) {
	t.setup(a, seed)
	t.how = mapHashSeeded
	t.sfn = fn
	t.Reserve(capacity)
}

func (t *MapTable[K, V]) setup(a Allocator, seed uint64) {
	if t.slots != nil {
		panicMessage("hash: Init on a map that holds a table", "", "", "", "")
	}
	if a == nil {
		a = DefaultAllocator()
		if a == nil {
			panicMessage("no allocator: call alloc.SetDefault before making a map", "", "", "", "")
		}
	}
	t.a = a
	t.seed = seed
}

// Initialized reports whether the table has been initialised.
func (t *MapTable[K, V]) Initialized() bool { return t.how != mapHashNone }

// Seed returns the table's hash seed.
func (t *MapTable[K, V]) Seed() uint64 { return t.seed }

// Reserve allocates a table with room for capacity entries, if the table
// is smaller.
func (t *MapTable[K, V]) Reserve(capacity int) {
	if capacity <= 0 {
		return
	}
	size := 8
	for size/4*3 < capacity {
		size <<= 1
	}
	if size > t.size {
		t.rebuild(size)
	}
}

// hash returns the hash of k, by the function the table was given if it
// was given one.
func (t *MapTable[K, V]) hash(k K) uint64 {
	switch t.how {
	case mapHashFunc:
		return t.fn(k)
	case mapHashSeeded:
		return t.sfn(k, t.seed)
	}
	return t.typeHash(k)
}

// typeHash returns the hash of k by its type: the hash of every map[K]V.
// It makes no call through a func value or an interface, so the compiler
// knows that looking a key up releases nothing, and a map read from memory
// a call could change is passed to it without counting (spec section
// 6.10.1). The key is read through its address in the form
// *(*T)(unsafe.Pointer(&k)), which the compiler knows does not change k,
// so a string key is borrowed without being counted.
func (t *MapTable[K, V]) typeHash(k K) uint64 {
	// The common key types, chosen when the function is compiled (the
	// switch is on a pointer type, so nothing is counted or boxed).
	var kp *K
	switch any(kp).(type) {
	case *string:
		return StringHash(*(*string)(unsafe.Pointer(&k)), t.seed)
	case *int, *int64, *uint, *uint64, *uintptr:
		return Uint64Hash(*(*uint64)(unsafe.Pointer(&k)), t.seed)
	case *int32, *uint32:
		return Uint64Hash(uint64(*(*uint32)(unsafe.Pointer(&k))), t.seed)
	default:
		// Only this clause is compiled for other key types.
		return t.kindHash(k)
	}
}

// kindHash is typeHash for the key types it does not pick when compiled:
// by the kind of key Init found.
func (t *MapTable[K, V]) kindHash(k K) uint64 {
	switch t.how {
	case mapHashWord:
		var x uint64
		switch t.ksize {
		case 1:
			x = uint64(*(*uint8)(unsafe.Pointer(&k)))
		case 2:
			x = uint64(*(*uint16)(unsafe.Pointer(&k)))
		case 4:
			x = uint64(*(*uint32)(unsafe.Pointer(&k)))
		default:
			x = *(*uint64)(unsafe.Pointer(&k))
		}
		return Uint64Hash(x, t.seed)
	case mapHashString:
		return StringHash(*(*string)(unsafe.Pointer(&k)), t.seed)
	case mapHashFloat:
		if t.ksize == 4 {
			return FloatHash(uint64(*(*uint32)(unsafe.Pointer(&k))), 4, t.seed)
		}
		return FloatHash(*(*uint64)(unsafe.Pointer(&k)), 8, t.seed)
	case mapHashMem:
		return t.memHash(k)
	case mapHashDesc:
		return t.descHash(k)
	}
	panicMessage("hash: map used before Init", "", "", "", "")
	return 0
}

// memHash hashes the bytes of k.
func (t *MapTable[K, V]) memHash(k K) uint64 {
	return MemHash(unsafe.Pointer(&k), int(t.ksize), t.seed)
}

// descHash hashes k by walking its descriptor.
func (t *MapTable[K, V]) descHash(k K) uint64 {
	return hashAt(t.kt, unsafe.Pointer(&k), t.seed)
}

// Len returns the number of entries.
//
//pu:inline
func (t *MapTable[K, V]) Len() int { return t.n }

// find returns the slot holding k, or -1.
func (t *MapTable[K, V]) find(k K) int {
	if t.n == 0 {
		return -1
	}
	return t.findHash(k, t.hash(k))
}

// findHash returns the slot holding k, whose hash is h, or -1.
func (t *MapTable[K, V]) findHash(k K, h uint64) int {
	if t.n == 0 {
		return -1
	}
	mask := t.size - 1
	tag := uint8(mapFull | h>>57)
	for i := int(h) & mask; ; i = (i + 1) & mask {
		m := t.meta[i]
		if m == mapEmpty {
			return -1
		}
		if m == tag && t.slots[i].key == k {
			return i
		}
	}
}

// Index returns the value stored for k, or the zero V.
func (t *MapTable[K, V]) Index(k K) V {
	if i := t.find(k); i >= 0 {
		return t.slots[i].val
	}
	var zero V
	return zero
}

// Get returns the value stored for k and true, or the zero V and false.
func (t *MapTable[K, V]) Get(k K) (V, bool) {
	if i := t.find(k); i >= 0 {
		return t.slots[i].val, true
	}
	var zero V
	return zero, false
}

// Has reports whether k is in the table.
func (t *MapTable[K, V]) Has(k K) bool { return t.find(k) >= 0 }

// Ptr returns a pointer to the value stored for k, or nil. It is valid
// until the table is next changed.
func (t *MapTable[K, V]) Ptr(k K) *V {
	if i := t.find(k); i >= 0 {
		return &t.slots[i].val
	}
	return nil
}

// Upsert returns a pointer to the value for k, adding k with the zero V if
// it is not there, and whether it added k. The pointer is valid until the
// table is next changed.
func (t *MapTable[K, V]) Upsert(k K) (*V, bool) {
	if t.used >= t.limit {
		t.grow()
	}
	return t.upsertHash(k, t.hash(k))
}

// upsertHash is Upsert for k, whose hash is h, in a table with room for
// one more entry.
func (t *MapTable[K, V]) upsertHash(k K, h uint64) (*V, bool) {
	mask := t.size - 1
	tag := uint8(mapFull | h>>57)
	del := -1
	for i := int(h) & mask; ; i = (i + 1) & mask {
		m := t.meta[i]
		if m == mapEmpty {
			if del >= 0 {
				i = del
			} else {
				t.used++
			}
			t.meta[i] = tag
			s := &t.slots[i]
			s.key = k
			t.n++
			return &s.val, true
		}
		if m == mapDeleted {
			if del < 0 {
				del = i
			}
		} else if m == tag && t.slots[i].key == k {
			return &t.slots[i].val, false
		}
	}
}

// Set stores v for k.
func (t *MapTable[K, V]) Set(k K, v V) {
	p, _ := t.Upsert(k)
	*p = v
}

// Delete removes k and reports whether it was there. No other entry moves.
func (t *MapTable[K, V]) Delete(k K) bool { return t.deleteAt(t.find(k)) }

// deleteAt removes the entry in slot i, if i is not -1.
func (t *MapTable[K, V]) deleteAt(i int) bool {
	if i < 0 {
		return false
	}
	var zero mapSlot[K, V]
	t.slots[i] = zero // releases the key and value
	if t.meta[(i+1)&(t.size-1)] == mapEmpty {
		// No probe passes through slot i to reach a full slot.
		t.meta[i] = mapEmpty
		t.used--
	} else {
		t.meta[i] = mapDeleted
	}
	t.n--
	return true
}

// Clear removes every entry, keeping the table.
func (t *MapTable[K, V]) Clear() {
	if t.size == 0 {
		return
	}
	if HoldsStrings[mapSlot[K, V]]() {
		// Empty and deleted slots are zero.
		ReleaseValues(&t.slots[0], t.size)
	}
	var p *mapSlot[K, V]
	memclr(unsafe.Pointer(unsafe.SliceData(t.slots)), t.size*int(unsafe.Sizeof(*p)))
	memclr(unsafe.Pointer(unsafe.SliceData(t.meta)), t.size)
	t.n = 0
	t.used = 0
}

// Free drops the entries, returns the table's memory to the allocator and
// leaves the table empty; it keeps its allocator and hashing, so it can be
// used again.
func (t *MapTable[K, V]) Free() {
	if t.slots != nil {
		if t.iters != 0 {
			t.retire()
		} else {
			freeMapTable[K, V](t.a, t.slots, true)
		}
	}
	t.slots = nil
	t.meta = nil
	t.size = 0
	t.n = 0
	t.used = 0
	t.limit = 0
	if t.iters == 0 {
		t.freeRetired()
	}
}

// Drop frees the table: the owner's release.
func (t *MapTable[K, V]) Drop() { t.Free() }

// Clone returns a copy of t with the same hashing, whose memory comes from
// a (the current allocator if nil). Keys and values are copied as values.
func (t *MapTable[K, V]) Clone(a Allocator) MapTable[K, V] {
	var out MapTable[K, V]
	out.setup(a, t.seed)
	out.how, out.ksize, out.kt, out.fn, out.sfn = t.how, t.ksize, t.kt, t.fn, t.sfn
	if t.n > 0 {
		out.alloc(t.size)
		var p *mapSlot[K, V]
		memmove(unsafe.Pointer(unsafe.SliceData(out.slots)), unsafe.Pointer(unsafe.SliceData(t.slots)), t.size*int(unsafe.Sizeof(*p)))
		memmove(unsafe.Pointer(unsafe.SliceData(out.meta)), unsafe.Pointer(unsafe.SliceData(t.meta)), t.size)
		RetainValues(&out.slots[0], t.size)
		out.n, out.used = t.n, t.used
	}
	return out
}

// tableBytes returns the size and alignment of the memory of a table of
// size slots: the slots followed by one metadata byte each.
func tableBytes[K comparable, V any](size int) (uintptr, uintptr) {
	var p *mapSlot[K, V]
	return uintptr(size) * (unsafe.Sizeof(*p) + 1), unsafe.Alignof(*p)
}

// alloc gives t a new zeroed table of size slots.
func (t *MapTable[K, V]) alloc(size int) {
	n, align := tableBytes[K, V](size)
	p := allocAt(t.a, n, align, CallerPC())
	var s *mapSlot[K, V]
	t.slots = unsafe.Slice((*mapSlot[K, V])(p), size)
	t.meta = unsafe.Slice((*uint8)(unsafe.Add(p, uintptr(size)*unsafe.Sizeof(*s))), size)
	t.size = size
	t.limit = size / 4 * 3
}

// freeMapTable returns the memory of the table slots to a, releasing its
// keys and values first if release is set.
func freeMapTable[K comparable, V any](a Allocator, slots []mapSlot[K, V], release bool) {
	if release && HoldsStrings[mapSlot[K, V]]() {
		ReleaseValues(&slots[0], len(slots))
	}
	n, align := tableBytes[K, V](len(slots))
	a.Free(unsafe.Pointer(unsafe.SliceData(slots)), n, align)
}

// grow makes room for an entry: a zero table that was never initialised
// (a pluto/hash.Map) is initialised with the current allocator first.
func (t *MapTable[K, V]) grow() {
	if t.how == mapHashNone {
		t.Init(0, DefaultMapSeed, nil)
	}
	size := t.size
	switch {
	case size == 0:
		size = 8
	case t.n >= t.limit/2:
		size *= 2
	}
	t.rebuild(size)
}

// rebuild moves the entries into a new table of size slots. If walks are
// running, the old table keeps its references and is retired until they
// end; otherwise the references move and the old table is freed.
func (t *MapTable[K, V]) rebuild(size int) {
	oldSlots, oldMeta, oldSize := t.slots, t.meta, t.size
	keep := t.iters != 0
	t.alloc(size)
	mask := size - 1
	for i := 0; i < oldSize; i++ {
		m := oldMeta[i]
		if m&mapFull == 0 {
			continue
		}
		src := &oldSlots[i]
		j := int(t.hash(src.key)) & mask
		for t.meta[j] != mapEmpty {
			j = (j + 1) & mask
		}
		dst := &t.slots[j]
		MoveValues(dst, src, 1)
		if keep {
			RetainValues(dst, 1)
		}
		t.meta[j] = m
	}
	t.used = t.n
	if oldSlots == nil {
		return
	}
	if keep {
		t.retireTable(oldSlots)
	} else {
		freeMapTable[K, V](t.a, oldSlots, false)
	}
}

// retire keeps the live table for the walks running, which own its
// references from now on.
func (t *MapTable[K, V]) retire() { t.retireTable(t.slots) }

func (t *MapTable[K, V]) retireTable(slots []mapSlot[K, V]) {
	r := (*mapRetired)(allocAt(t.a, unsafe.Sizeof(mapRetired{}), unsafe.Alignof(mapRetired{}), CallerPC()))
	// The table memory came from t.a; through uintptr so that the escape
	// analysis, which cannot tell, does not take it for the caller's.
	r.slots = unsafe.Pointer(uintptr(unsafe.Pointer(unsafe.SliceData(slots))))
	r.size = len(slots)
	r.next = t.retired
	t.retired = uintptr(unsafe.Pointer(r))
}

// freeRetired frees the tables kept for walks that have all ended.
func (t *MapTable[K, V]) freeRetired() {
	for p := t.retired; p != 0; {
		r := (*mapRetired)(unsafe.Pointer(p))
		next := r.next
		freeMapTable[K, V](t.a, unsafe.Slice((*mapSlot[K, V])(r.slots), r.size), true)
		t.a.Free(unsafe.Pointer(r), unsafe.Sizeof(mapRetired{}), unsafe.Alignof(mapRetired{}))
		p = next
	}
	t.retired = 0
}

// Slots returns the number of slots, and Slot the key and value in slot i
// if it is full: walking every slot visits the entries in table order.
func (t *MapTable[K, V]) Slots() int { return t.size }

// Slot: see Slots.
func (t *MapTable[K, V]) Slot(i int) (*K, *V, bool) {
	if t.meta[i]&mapFull == 0 {
		return nil, nil, false
	}
	s := &t.slots[i]
	return &s.key, &s.val, true
}

// ----------------------------------------------------------------------------
// Walks

// A MapIter walks the entries of a table in table order with Go's
// guarantees for changes made during the walk (see the top of the file).
// It is an owner: dropping it ends the walk, which the compiler does at
// the end of a range loop on every way out of it.
type MapIter[K comparable, V any] owner struct {
	t     *MapTable[K, V]
	slots []mapSlot[K, V] // the table the walk started on
	meta  []uint8
	size  int
	i     int
}

// Start begins a walk over t, which may be nil (a nil map).
func (it *MapIter[K, V]) Start(t *MapTable[K, V]) {
	it.t = t
	if t == nil {
		return
	}
	it.slots, it.meta, it.size = t.slots, t.meta, t.size
	addMapCount(&t.iters, 1)
}

// Drop ends the walk.
func (it *MapIter[K, V]) Drop() {
	t := it.t
	if t == nil {
		return
	}
	it.t = nil
	if addMapCount(&t.iters, -1) == 0 && t.retired != 0 {
		t.freeRetired()
	}
}

// next returns the slot of the next entry in the live table, or nil.
func (it *MapIter[K, V]) next() *mapSlot[K, V] {
	t := it.t
	if t == nil {
		return nil
	}
	for it.i < it.size {
		i := it.i
		it.i++
		if unsafe.SliceData(it.slots) == unsafe.SliceData(t.slots) {
			if t.meta[i]&mapFull != 0 {
				return &t.slots[i]
			}
			continue
		}
		// The table was rebuilt: the entry, if it is still there, is
		// wherever the live table has it.
		if it.meta[i]&mapFull != 0 {
			if j := t.find(it.slots[i].key); j >= 0 {
				return &t.slots[j]
			}
		}
	}
	return nil
}

// Next returns the next entry's key and value, or false when there are
// no more.
func (it *MapIter[K, V]) Next() (K, V, bool) {
	if s := it.next(); s != nil {
		return s.key, s.val, true
	}
	var k K
	var v V
	return k, v, false
}

// NextKey returns the next entry's key, or false when there are no more.
func (it *MapIter[K, V]) NextKey() (K, bool) {
	if s := it.next(); s != nil {
		return s.key, true
	}
	var k K
	return k, false
}

// ----------------------------------------------------------------------------
// map[K]V. The compiler calls these with the map's table: the object of
// its box, or nil for a nil map. The map is borrowed for the call.

// mapMake is make(map[K]V, hint) and a map literal: a new map from the
// current allocator with room for hint entries. The compiler takes the
// table's first reference.
func mapMake[K comparable, V any](hint int) *MapTable[K, V] {
	a := DefaultAllocator()
	if a == nil {
		panicMessage("no allocator: call alloc.SetDefault before making a map", "", "", "", "")
	}
	return mapMakeAt[K, V](a, hint, CallerPC())
}

// mapMakeWith is make(map[K]V, hint, a).
func mapMakeWith[K comparable, V any](a Allocator, hint int) *MapTable[K, V] {
	if a == nil {
		panicMessage("nil allocator passed to new or make", "", "", "", "")
	}
	return mapMakeAt[K, V](a, hint, CallerPC())
}

// mapMakeAt allocates a map's box from a, publishing pc, the make's or the
// literal's, as the allocation's site (AllocSite), and initialises its
// table.
func mapMakeAt[K comparable, V any](a Allocator, hint int, pc uintptr) *MapTable[K, V] {
	if hint < 0 {
		panicMessage("runtime error: makemap: size out of range", "", "", "", "")
	}
	var p *MapTable[K, V]
	off, align := boxOffset(countedHeader, unsafe.Alignof(*p))
	t := (*MapTable[K, V])(boxInit(allocAt(a, off+unsafe.Sizeof(*p), align, pc), a, countedHeader, off))
	t.Init(hint, nextMapSeed(), a)
	return t
}

// mapAccess is m[k].
func mapAccess[K comparable, V any](t *MapTable[K, V], k K) V {
	if t != nil {
		if i := t.findHash(k, t.typeHash(k)); i >= 0 {
			return t.slots[i].val
		}
	}
	var zero V
	return zero
}

// mapAccessOk is v, ok := m[k].
func mapAccessOk[K comparable, V any](t *MapTable[K, V], k K) (V, bool) {
	if t != nil {
		if i := t.findHash(k, t.typeHash(k)); i >= 0 {
			return t.slots[i].val, true
		}
	}
	var zero V
	return zero, false
}

// mapAssign is m[k] = v.
func mapAssign[K comparable, V any](t *MapTable[K, V], k K, v V) {
	*mapSlotFor(t, k) = v
}

// mapSlotFor returns the place of the value for k in the map's table,
// adding k if it is not there.
func mapSlotFor[K comparable, V any](t *MapTable[K, V], k K) *V {
	if t == nil {
		panicNilMap()
	}
	if t.used >= t.limit {
		t.grow()
	}
	p, _ := t.upsertHash(k, t.typeHash(k))
	return p
}

// mapAssignPtr returns a pointer to m[k], adding k with the zero V if it
// is not there, for m[k] op= v and m[k]++: the compiler evaluates v first,
// so nothing changes the map before the pointer is used.
func mapAssignPtr[K comparable, V any](t *MapTable[K, V], k K) *V {
	return mapSlotFor(t, k)
}

// mapDelete is delete(m, k).
func mapDelete[K comparable, V any](t *MapTable[K, V], k K) {
	if t != nil && t.n > 0 {
		t.deleteAt(t.findHash(k, t.typeHash(k)))
	}
}

// mapLen is len(m).
//
//pu:inline
func mapLen[K comparable, V any](t *MapTable[K, V]) int {
	if t == nil {
		return 0
	}
	return t.n
}

// mapClear is clear(m).
func mapClear[K comparable, V any](t *MapTable[K, V]) {
	if t != nil {
		t.Clear()
	}
}

// panicNilMap reports a store into a nil map.
//
//pu:noinline
func panicNilMap() {
	panicMessage("assignment to entry in nil map", "", "", "", "")
}

// ----------------------------------------------------------------------------
// Walking a map through its type descriptor (pluto/fmt). A map type's
// descriptor has the key and the value as the two fields of a table slot,
// and the slot's size as its length.

// mapHeader is the start of every MapTable[K, V], whatever K and V are.
type mapHeader struct {
	slots      unsafe.Pointer
	slen, scap int
	meta       *uint8
	mlen, mcap int
	size       int
}

// MapSlots returns the number of slots in the table of a map, given its
// value's word (nil for a nil map). Walking slots 0 to MapSlots-1 with
// MapSlot visits the entries in table order.
func MapSlots(m unsafe.Pointer) int {
	if m == nil {
		return 0
	}
	return (*mapHeader)(m).size
}

// MapSlot returns the addresses of the key and the value in slot i of the
// table of a map of type t, if the slot is full.
func MapSlot(t *Type, m unsafe.Pointer, i int) (k, v unsafe.Pointer, ok bool) {
	h := (*mapHeader)(m)
	if *(*uint8)(unsafe.Add(unsafe.Pointer(h.meta), i))&mapFull == 0 {
		return nil, nil, false
	}
	kf, vf := t.Field(0), t.Field(1)
	slot := unsafe.Add(h.slots, uintptr(i)*t.len) // a map descriptor's len is its slot size
	return unsafe.Add(slot, kf.Offset), unsafe.Add(slot, vf.Offset), true
}
