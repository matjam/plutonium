// Translated by go2pu from internal/escape2/domain/sets.go at the go-cutover tag; edited by hand since.

package domain

import (
	"pluto/slices"
	"compiler/compat"
	"compiler/pu"
)

// Hash-consed sets. Each distinct set is stored once, as a sorted array of
// words in blocks carved from the arena; a SetID is its index in the table,
// with the table's generation in the top bits so that an ID kept past a Reset
// is caught rather than read as another set. Union and Diff are memoized in a
// direct-mapped cache of bounded size, which can only save work: a miss
// recomputes the same result. Nothing depends on a map's order or an
// address, so the IDs a run makes depend only on the order of its calls.

const (
	setIndexBits = 24
	setIndexMask = 1<<setIndexBits - 1
	setGenMask   = 1<<(32-setIndexBits) - 1

	// blockWords is the size of the blocks sets are stored in; a set of
	// more than a quarter of it gets a block of its own.
	blockWords = chunkWords / 4

	memoMin = 256
	memoMax = 1 << 20
)

// setHdr is where one set is stored.
type setHdr struct {
	blk  uint32 // index in blocks
	off  uint32 // offset in the block
	n    uint32 // number of elements
	hash uint32
}

const (
	opUnion = 1
	opDiff  = 2
)

// memoEnt caches op(a, b) = r; op 0 is a free entry.
type memoEnt struct {
	a, b, r SetID
	op      uint32
}

// sets implements Sets.
type sets struct {
	a          Arena
	ar         *arena // a, when it is one of this package's arenas
	arenaEpoch uint32 // ar.epoch the stored sets were made in
	gen        uint32 // generation of the IDs, in their top bits

	hdr    []setHdr // hdr[0] is the empty set
	blocks [][]uint64
	bigs   [][]uint64 // sets of more than blockWords/4 elements, one each
	nblk   int        // blocks[:nblk] are in use; blocks[nblk-1] is the current one
	pos    int        // words used of the current block
	words  int        // words of elements stored
	table  []uint32   // open-addressing index: 1 + set index, 0 free
	memo   []memoEnt
	buf    []uint64 // scratch for merges
}

// NewSets returns an empty set table whose sets live in a.
func NewSets(a Arena) Sets {
	s := pu.New(sets{a: a, gen: 1})
	s.ar, _ = a.(*arena)
	if s.ar != nil {
		s.arenaEpoch = s.ar.epoch
	}
	s.hdr = compat.Append(s.hdr, setHdr{})
	s.table = compat.Make[uint32](64)
	s.memo = compat.Make[memoEnt](memoMin)
	return s
}

// sync forgets every set when the arena was reset under the table.
func (s *sets) sync() {
	if s.ar != nil && s.ar.epoch != s.arenaEpoch {
		s.blocks, s.nblk, s.pos = nil, 0, 0
		s.bigs = nil
		s.clear()
		s.arenaEpoch = s.ar.epoch
	}
}

// Reset frees every set. The blocks stay with the table for its next sets
// (a checked arena's are poisoned and dropped instead), and go back to the
// arena at its Reset.
func (s *sets) Reset() {
	s.sync()
	if s.ar != nil && s.ar.checked {
		for _, b := range s.blocks[:s.nblk] {
			poison(b)
		}
		for _, b := range s.bigs {
			poison(b)
		}
		s.blocks = nil
	}
	s.bigs = nil
	s.nblk, s.pos = 0, 0
	s.clear()
}

func (s *sets) clear() {
	s.gen = (s.gen + 1) & setGenMask
	if s.gen == 0 {
		s.gen = 1
	}
	s.hdr = s.hdr[:1]
	s.words = 0
	clear(s.table)
	clear(s.memo)
}

// index checks that id belongs to this generation and returns its index.
func (s *sets) index(id SetID) int {
	if id == Empty {
		return 0
	}
	i := int(id & setIndexMask)
	if uint32(id)>>setIndexBits != s.gen || i >= len(s.hdr) {
		panic("escape2/domain: a set is used after the Reset of its table or arena")
	}
	return i
}

func (s *sets) elems(i int) []uint64 {
	h := s.hdr[i]
	if h.n == 0 {
		return nil
	}
	var b []uint64
	if h.blk&bigBlock != 0 {
		b = s.bigs[h.blk&^bigBlock]
	} else {
		b = s.blocks[h.blk]
	}
	return b[h.off : h.off+h.n : h.off+h.n]
}

func (s *sets) Elems(id SetID) []uint64 {
	s.sync()
	return s.elems(s.index(id))
}

func (s *sets) Len(id SetID) int {
	s.sync()
	return int(s.hdr[s.index(id)].n)
}

func (s *sets) Has(id SetID, x uint64) bool {
	s.sync()
	e := s.elems(s.index(id))
	lo, hi := 0, len(e)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if e[m] < x {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo < len(e) && e[lo] == x
}

func (s *sets) Intern(elems []uint64) SetID {
	s.sync()
	if len(elems) <= 16 {
		for i := 1; i < len(elems); i++ {
			for j := i; j > 0 && elems[j] < elems[j-1]; j-- {
				elems[j], elems[j-1] = elems[j-1], elems[j]
			}
		}
	} else {
		slices.Sort(elems)
	}
	n := 0
	for i, x := range elems {
		if i == 0 || x != elems[n-1] {
			elems[n] = x
			n++
		}
	}
	return s.intern(elems[:n])
}

func (s *sets) Union(a, b SetID) SetID {
	s.sync()
	ia, ib := s.index(a), s.index(b)
	switch {
	case a == b || ib == 0:
		return a
	case ia == 0:
		return b
	}
	if a > b {
		a, b = b, a
		ia, ib = ib, ia
	}
	if r, ok := s.lookupMemo(opUnion, a, b); ok {
		return r
	}
	ea, eb := s.elems(ia), s.elems(ib)
	out := s.buf[:0]
	i, j := 0, 0
	for i < len(ea) && j < len(eb) {
		switch {
		case ea[i] < eb[j]:
			out = compat.AppendPlain(out, ea[i])
			i++
		case ea[i] > eb[j]:
			out = compat.AppendPlain(out, eb[j])
			j++
		default:
			out = compat.AppendPlain(out, ea[i])
			i++
			j++
		}
	}
	out = compat.AppendPlain(out, ea[i:]...)
	out = compat.AppendPlain(out, eb[j:]...)
	s.buf = out
	var r SetID
	switch len(out) {
	case len(ea):
		r = a
	case len(eb):
		r = b
	default:
		r = s.intern(out)
	}
	s.storeMemo(opUnion, a, b, r)
	return r
}

func (s *sets) Diff(a, b SetID) SetID {
	s.sync()
	ia, ib := s.index(a), s.index(b)
	switch {
	case ia == 0 || a == b:
		return Empty
	case ib == 0:
		return a
	}
	if r, ok := s.lookupMemo(opDiff, a, b); ok {
		return r
	}
	ea, eb := s.elems(ia), s.elems(ib)
	out := s.buf[:0]
	j := 0
	for _, x := range ea {
		for j < len(eb) && eb[j] < x {
			j++
		}
		if j == len(eb) || eb[j] != x {
			out = compat.AppendPlain(out, x)
		}
	}
	s.buf = out
	var r SetID
	if len(out) == len(ea) {
		r = a
	} else {
		r = s.intern(out)
	}
	s.storeMemo(opDiff, a, b, r)
	return r
}

func (s *sets) Bytes() int {
	n := s.nblkWords() * 8
	n += cap(s.hdr)*16 + cap(s.table)*4 + cap(s.memo)*16 + cap(s.buf)*8
	return n
}

func (s *sets) nblkWords() int {
	n := 0
	for _, b := range s.blocks {
		n += len(b)
	}
	for _, b := range s.bigs {
		n += len(b)
	}
	return n
}

// hashWords hashes a sorted set.
func hashWords(xs []uint64) uint32 {
	h := uint64(len(xs)) * 0x9e3779b97f4a7c15
	for _, x := range xs {
		h ^= x
		h *= 0xbf58476d1ce4e5b9
		h ^= h >> 31
	}
	return uint32(h ^ h>>32)
}

// intern returns the ID of the sorted, duplicate-free set xs, storing a copy
// of it if it is new.
func (s *sets) intern(xs []uint64) SetID {
	if len(xs) == 0 {
		return Empty
	}
	h := hashWords(xs)
	mask := uint32(len(s.table) - 1)
	slot := h & mask
	for ; s.table[slot] != 0; slot = (slot + 1) & mask {
		i := int(s.table[slot] - 1)
		if s.hdr[i].hash == h && slices.Equal(s.elems(i), xs) {
			return s.id(i)
		}
	}
	i := len(s.hdr)
	if i > setIndexMask {
		panic("escape2/domain: too many sets in one arena (the limit is 2^24)")
	}
	blk, off := s.store(xs)
	s.hdr = compat.Append(s.hdr, setHdr{blk: blk, off: off, n: uint32(len(xs)), hash: h})
	s.words += len(xs)
	if 2*len(s.hdr) > len(s.table) {
		s.grow()
	} else {
		s.table[slot] = uint32(i) + 1
	}
	return s.id(i)
}

func (s *sets) id(i int) SetID { return SetID(s.gen<<setIndexBits | uint32(i)) }

// bigBlock marks a block index as one of bigs.
const bigBlock = 1 << 31

// store copies xs into a block and says where.
func (s *sets) store(xs []uint64) (uint32, uint32) {
	n := len(xs)
	if n > blockWords/4 {
		b := s.a.Alloc(n)
		copy(b, xs)
		s.bigs = compat.Append(s.bigs, b)
		return uint32(len(s.bigs)-1) | bigBlock, 0
	}
	if s.nblk == 0 || s.pos+n > len(s.blocks[s.nblk-1]) {
		if s.nblk == len(s.blocks) {
			s.blocks = compat.Append(s.blocks, s.a.Alloc(blockWords))
		} else {
			clear(s.blocks[s.nblk])
		}
		s.nblk++
		s.pos = 0
	}
	b := s.blocks[s.nblk-1]
	off := s.pos
	copy(b[off:off+n], xs)
	s.pos += n
	return uint32(s.nblk - 1), uint32(off)
}

func (s *sets) grow() {
	s.table = compat.Make[uint32](2 * len(s.table))
	mask := uint32(len(s.table) - 1)
	for i := 1; i < len(s.hdr); i++ {
		slot := s.hdr[i].hash & mask
		for s.table[slot] != 0 {
			slot = (slot + 1) & mask
		}
		s.table[slot] = uint32(i) + 1
	}
	if len(s.memo) < memoMax && len(s.memo) < len(s.hdr) {
		s.memo = compat.Make[memoEnt](2 * len(s.memo))
	}
}

func memoSlot(op uint32, a, b SetID, n int) int {
	h := uint64(a)<<32 | uint64(b)
	h ^= uint64(op) << 62
	h *= 0x9e3779b97f4a7c15
	return int(h>>32) & (n - 1)
}

func (s *sets) lookupMemo(op uint32, a, b SetID) (SetID, bool) {
	e := &s.memo[memoSlot(op, a, b, len(s.memo))]
	if e.op == op && e.a == a && e.b == b {
		return e.r, true
	}
	return 0, false
}

func (s *sets) storeMemo(op uint32, a, b, r SetID) {
	s.memo[memoSlot(op, a, b, len(s.memo))] = memoEnt{a: a, b: b, r: r, op: op}
}
