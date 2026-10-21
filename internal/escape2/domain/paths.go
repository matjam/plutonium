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
// When p followed by s would pass MaxLoads or MaxSteps, Extend returns p
// itself with GE: p with GE stands for p+s and every extension (invariant
// I8). It cuts no further back than p, for two reasons. Precision: the key
// keeps every step the limits allow. Monotonicity: if a GE key g covers a key
// k, g's path is a prefix of k's, so it is a prefix of k's path after a cut
// too, and g (which ExtendKey leaves as it is) still covers ExtendKey(k, s).
// A cut back to a shorter prefix, such as the one through the second load,
// can drop below g's path, so that a summary that grew could lose coverage
// and an SCC's iteration would not be monotone. The number of paths is not
// changed by this: the result is a path already interned.

// Proposed changes to api.go (piece C to piece A; api.go is shared):
//
//   - The comments of Extend and Cut: past the limits Extend returns p itself
//     with the cut flag (not a cut back to the first load plus one more load,
//     which is not monotone under Covers and, without the fields between the
//     loads, would not even cover the path), and it absorbs an offset step
//     past MaxFieldNest without a cut; Extend ignores GE, so a key is
//     extended with ExtendKey. Cut, the cap's widening, is the prefix through
//     the first load, or the path itself when it has no load.
//   - MakeKey truncates an input of MaxInputs or more to its low byte, which
//     would make a key name another input; it should panic instead.
//   - The helpers of this package that the solver and the reference solver
//     need (ExtendKey, AppendKey, PathMatches, CoveredBy, Antichain,
//     JoinKeys, CoversAll, NewCheckedArena) could be declared in api.go.

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
			return p, true
		}
		return ps.child(p, s), false
	}
	if i.segment() == MaxFieldNest {
		return p, false // the segment already stands for all its extensions by offsets
	}
	if steps+1 > MaxSteps {
		return p, true
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

// ExtendKey returns k with its path followed by s. A GE key is returned
// unchanged: it already stands for every extension, and extending its path
// would narrow it.
func ExtendKey(ps Paths, k Key, s Step) Key {
	if k.GE() {
		return k
	}
	q, cut := ps.Extend(k.Path(), s)
	return k.WithPath(q, cut)
}

// AppendKey returns k with its path followed by the steps of p, and GE if k
// or the extension is GE (ge says whether p stands for its extensions too).
// It is how a caller re-roots a callee's key on what the caller passed.
func AppendKey(ps Paths, k Key, p PathID, ge bool) Key {
	for _, s := range ps.Steps(p) {
		if k.GE() {
			return k
		}
		k = ExtendKey(ps, k, s)
	}
	if ge {
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
