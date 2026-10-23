package domain

// The path interner: a trie of steps, one node per interned path, held in
// flat slices so that it needs no garbage collector and its memory is a few
// words per path (plan section (e), program arena).
//
// What a path stands for. A path is a word over the steps load (L), element
// (E) and field i (Fi); E and Fi are the offset steps. A path names the
// pointers found by following its steps from an input. Between two loads (and
// before the first, and after the last) a path holds at most MaxFieldNest
// offset steps; a segment that holds MaxFieldNest of them stands for itself
// followed by any number of further offset steps, so a field of a field of a
// field keeps the path of its enclosing one (spec 18.3, "followed field by
// field, two levels deep"). With the GE bit of a key, a path stands for itself
// and every extension. PathMatches is this definition, executable; the
// property tests hold Extend, Cut and Covers to it.
//
// Extend cuts a path that would pass MaxLoads or MaxSteps back to its prefix
// through the second load, with GE. Such a path has at least two loads, so
// the prefix is a proper prefix of it, and GE makes the prefix cover it and
// everything after it (invariant I8). Keeping the fields between the two
// loads, rather than the first load alone, keeps a cut key from standing for
// the pointer the first load reads.

// Proposed change to api.go (piece C to piece A; api.go is shared): since the
// revert of e21eeef8, the comment of Paths.Extend describes the reverted
// behavior ("returns p itself"); Extend cuts to the prefix through the
// second load, with GE, which is not monotone under Covers (the SCC-level
// Join of the scheduler keeps termination).
//
// Note: this unit also builds FieldsGE ("GE within the object", bit 44 of a
// Key), asked for by the solver: Key.FieldsGE, Key.WithFieldsGE,
// KeyFlags.FieldsGE and the Covers cases in api.go; ExtendKey, AppendTail,
// KeyMatches and the antichain functions here.

// stepChunk is the number of steps in one chunk of the step store. Chunks are
// never moved, so the slices Steps returns stay valid.
const stepChunk = 4096

// pathInfo packs the facts of one path that Extend reads.
type pathInfo uint32

// The fields of pathInfo hold the limits; raising a limit past what its
// field holds fails to compile here.
const (
	_ uint = 15 - MaxSteps    // depth: 4 bits
	_ uint = 3 - MaxLoads     // loads: 2 bits
	_ uint = 3 - MaxFieldNest // segment: 2 bits
)

func (i pathInfo) depth() int   { return int(i & 0xf) }         // number of steps stored
func (i pathInfo) loads() int   { return int(i >> 4 & 0x3) }    // loads among them
func (i pathInfo) segment() int { return int(i >> 6 & 0x3) }    // offset steps after the last load
func (i pathInfo) chunk() int   { return int(i >> 8 & 0xffff) } // the chunk its steps are stored in
func makeInfo(depth, loads, seg, chunk int) pathInfo {
	return pathInfo(depth | loads<<4 | seg<<6 | chunk<<8)
}

// paths implements Paths.
type paths struct {
	parent []PathID
	step   []Step
	info   []pathInfo
	off    []uint16 // offset of the path's steps in its chunk
	chunks [][]Step
	// table is an open-addressing index of the children of each path:
	// 1 + the child's ID, keyed by (parent, step); 0 is a free slot.
	table []uint32
}

// NewPaths returns an empty path interner holding only Root.
func NewPaths() Paths {
	ps := &paths{table: make([]uint32, 64)}
	ps.parent = append(ps.parent, Root)
	ps.step = append(ps.step, 0)
	ps.info = append(ps.info, makeInfo(0, 0, 0, 0))
	ps.off = append(ps.off, 0)
	ps.chunks = append(ps.chunks, make([]Step, 0, stepChunk))
	return ps
}

func (ps *paths) Len() int { return len(ps.parent) }

func (ps *paths) Loads(p PathID) int { return ps.info[p].loads() }

func (ps *paths) Steps(p PathID) []Step {
	i := ps.info[p]
	o := int(ps.off[p])
	n := i.depth()
	return ps.chunks[i.chunk()][o : o+n : o+n]
}

func (ps *paths) HasPrefix(p, prefix PathID) bool {
	d := ps.info[prefix].depth()
	for ps.info[p].depth() > d {
		p = ps.parent[p]
	}
	return p == prefix
}

// Cut returns the prefix of p through its first load, or p itself when it
// has no load. With GE set the result covers p.
func (ps *paths) Cut(p PathID) PathID {
	if ps.info[p].loads() == 0 {
		return p
	}
	return ps.throughLoad(p, 1)
}

// cut returns the prefix of p through its second load, which with GE covers
// p and every extension. Under the limits a path that overflows them has two
// loads; should the limits be raised so that one has fewer, the cut is to its
// last load, or to Root, which still covers it.
func (ps *paths) cut(p PathID) PathID {
	n := ps.info[p].loads()
	if n > 2 {
		n = 2
	}
	if n == 0 {
		return Root
	}
	return ps.throughLoad(p, n)
}

// throughLoad returns the prefix of p that ends with its n-th load; p has at
// least n loads.
func (ps *paths) throughLoad(p PathID, n int) PathID {
	for {
		i := ps.info[p]
		if i.loads() == n && ps.step[p] == StepLoad {
			return p
		}
		p = ps.parent[p]
	}
}

func (ps *paths) Extend(p PathID, s Step) (PathID, bool) {
	if s < StepLoad {
		panic("escape2/domain: Extend: invalid step")
	}
	i := ps.info[p]
	steps := i.depth() // every stored step counts
	if s == StepLoad {
		if i.loads()+1 > MaxLoads || steps+1 > MaxSteps {
			return ps.cut(p), true
		}
		return ps.child(p, s), false
	}
	if i.segment() == MaxFieldNest {
		return p, false // the segment already stands for all its extensions by offsets
	}
	if steps+1 > MaxSteps {
		return ps.cut(p), true
	}
	return ps.child(p, s), false
}

// hashChild hashes a (parent, step) pair.
func hashChild(p PathID, s Step) uint32 {
	h := uint64(p)<<32 | uint64(uint32(s))
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	return uint32(h)
}

// child returns the interned path p followed by s, which must be within the
// limits.
func (ps *paths) child(p PathID, s Step) PathID {
	mask := uint32(len(ps.table) - 1)
	for h := hashChild(p, s) & mask; ; h = (h + 1) & mask {
		e := ps.table[h]
		if e == 0 {
			break
		}
		if c := PathID(e - 1); ps.parent[c] == p && ps.step[c] == s {
			return c
		}
	}
	// A new path.
	if len(ps.parent) == 1<<32-2 {
		panic("escape2/domain: too many paths")
	}
	i := ps.info[p]
	depth, loads, seg := i.depth()+1, i.loads(), i.segment()+1
	if s == StepLoad {
		loads, seg = loads+1, 0
	}
	ch := len(ps.chunks) - 1
	if len(ps.chunks[ch])+depth > stepChunk {
		ps.chunks = append(ps.chunks, make([]Step, 0, stepChunk))
		ch++
	}
	if ch > 0xffff {
		panic("escape2/domain: too many paths")
	}
	o := len(ps.chunks[ch])
	ps.chunks[ch] = append(ps.chunks[ch], ps.Steps(p)...)
	ps.chunks[ch] = append(ps.chunks[ch], s)
	c := PathID(len(ps.parent))
	ps.parent = append(ps.parent, p)
	ps.step = append(ps.step, s)
	ps.info = append(ps.info, makeInfo(depth, loads, seg, ch))
	ps.off = append(ps.off, uint16(o))
	if 2*len(ps.parent) > len(ps.table) {
		ps.rehash(2 * len(ps.table))
	} else {
		ps.insert(c)
	}
	return c
}

func (ps *paths) insert(c PathID) {
	mask := uint32(len(ps.table) - 1)
	h := hashChild(ps.parent[c], ps.step[c]) & mask
	for ps.table[h] != 0 {
		h = (h + 1) & mask
	}
	ps.table[h] = uint32(c) + 1
}

func (ps *paths) rehash(n int) {
	ps.table = make([]uint32, n)
	for c := 1; c < len(ps.parent); c++ {
		ps.insert(PathID(c))
	}
}

// Bytes returns the memory the interner holds.
func (ps *paths) bytes() int {
	n := cap(ps.parent)*4 + cap(ps.step)*4 + cap(ps.info)*4 + cap(ps.off)*2 + cap(ps.table)*4
	n += len(ps.chunks) * stepChunk * 4
	return n
}

// PathBytes returns the memory a Paths made by NewPaths holds, for the
// arena-size measurements; 0 for any other Paths.
func PathBytes(ps Paths) int {
	if p, ok := ps.(*paths); ok {
		return p.bytes()
	}
	return 0
}

// ExtendKey returns k with its path followed by s. A GE or Deep key is
// returned unchanged: it already stands for every extension, and extending
// its path would narrow it. A FieldsGE key is returned unchanged by an
// offset step, which it already stands for; a load through it becomes Deep
// on its path (its offsets, then the load), unless its last segment is
// full, when the key is exact and extended as one.
func ExtendKey(ps Paths, k Key, s Step) Key {
	if k.GE() || k.Deep() {
		return k
	}
	if k.FieldsGE() {
		if s != StepLoad {
			return k
		}
		if !segmentFull(ps, k.Path()) {
			return k.WithDeep()
		}
	}
	q, cut := ps.Extend(k.Path(), s)
	return k.WithPath(q, cut)
}

// segmentFull reports whether the last segment of p holds MaxFieldNest offset
// steps, so that p stands for its extensions by offsets.
func segmentFull(ps Paths, p PathID) bool {
	if x, ok := ps.(*paths); ok {
		return x.info[p].segment() == MaxFieldNest
	}
	st := ps.Steps(p)
	n := 0
	for i := len(st) - 1; i >= 0 && st[i] != StepLoad; i-- {
		n++
	}
	return n == MaxFieldNest
}

// AppendTail returns k with its path followed by tail's, and tail's GE,
// FieldsGE or Deep: the re-rooting of a callee's key tail on the caller's
// key k. tail's input and its other flags are not read.
func AppendTail(ps Paths, k Key, tail Key) Key {
	k = AppendKey(ps, k, tail.Path(), tail.GE())
	switch {
	case tail.FieldsGE():
		k = k.WithFieldsGE()
	case tail.Deep():
		k = k.WithDeep()
	}
	return k
}

// KeyMatches reports whether the concrete access path w is one of those key
// k stands for, by its path and its GE, FieldsGE or Deep bit (see
// PathMatches).
func KeyMatches(ps Paths, k Key, w []Step) bool {
	if k.Deep() {
		return deepMatches(ps, k.Path(), w)
	}
	return pathMatches(ps, k.Path(), k.GE(), k.FieldsGE(), w)
}

// deepMatches reports whether w is p followed by any offset steps, a load,
// and then anything: one of the words a Deep key on p stands for.
func deepMatches(ps Paths, p PathID, w []Step) bool {
	// w must start with a word p stands for (with its offsets after, as
	// FieldsGE) and go on with a load; the words p stands for followed by
	// offsets are those of FieldsGE p, so try each prefix of w ending
	// before a load.
	for j, s := range w {
		if s == StepLoad && pathMatches(ps, p, false, true, w[:j]) {
			return true
		}
	}
	return false
}

// AppendKey returns k with its path followed by the steps of p, and GE if k
// or the extension is GE (ge says whether p stands for its extensions too);
// a Deep k stays Deep, standing for every extension already. It is how a
// caller re-roots a callee's key on what the caller passed.
func AppendKey(ps Paths, k Key, p PathID, ge bool) Key {
	for _, s := range ps.Steps(p) {
		if k.GE() || k.Deep() {
			return k
		}
		k = ExtendKey(ps, k, s)
	}
	if ge && !k.Deep() {
		k = k.WithPath(k.Path(), true)
	}
	return k
}

// PathMatches reports whether the concrete access path w (loads, elements and
// field indices, of any length) is one of those path p stands for, with or
// without GE (see the comment at the top of this file). It is the
// concretization the property tests and the reference solver hold the
// domain to.
func PathMatches(ps Paths, p PathID, ge bool, w []Step) bool {
	return pathMatches(ps, p, ge, false, w)
}

// pathMatches is PathMatches, and with fieldsGE the path followed by any
// number of offset steps.
func pathMatches(ps Paths, p PathID, ge, fieldsGE bool, w []Step) bool {
	j := 0   // position in w
	seg := 0 // offset steps of p since its last load
	for _, s := range ps.Steps(p) {
		if s == StepLoad {
			j = skipOffsets(w, j, seg)
			seg = 0
		} else {
			seg++
		}
		if j == len(w) || w[j] != s {
			return false
		}
		j++
	}
	if fieldsGE {
		seg = MaxFieldNest
	}
	j = skipOffsets(w, j, seg)
	return j == len(w) || ge
}

// skipOffsets skips the offset steps of w from j when a segment of seg
// offset steps is full and so stands for any further ones.
func skipOffsets(w []Step, j, seg int) int {
	if seg == MaxFieldNest {
		for j < len(w) && w[j] != StepLoad {
			j++
		}
	}
	return j
}
