// Package domain holds the abstract domain of the escape analysis (plan
// docs/plans/2026-10-20-escape-redesign.md, section (a)): access paths
// interned in one program-wide trie, keys packed into a uint64, and sets of
// tokens hash-consed in arenas that are reset when an SCC is done.
//
// Soundness rule for this package (plan invariant I8): every cut of a path and
// every cap on a set only widens. A cut path with GE set covers every path
// that extends it, and nothing here may ever drop a path or a token.
//
// This file is the interface other packages build against: change it only by
// agreement (internal/escape2 package comment).
package domain

// The limits of section 18.3 ("How precise the frame check is"): a path has
// at most MaxLoads loads and MaxSteps steps, and tells apart MaxFieldNest
// nested fields between two loads. A longer path is cut back to its first
// load plus one more load and marked GE. The specification allows raising
// these, never lowering them.
const (
	MaxLoads     = 3
	MaxSteps     = 8
	MaxFieldNest = 2
)

// Step is one step of an access path: a load through a pointer, an element of
// an array or slice (elements are not told apart), or a field index (>= 0).
type Step int32

const (
	StepLoad Step = -2
	StepElem Step = -1
)

// PathID names an interned access path. Root (0) is the empty path: the
// input's own pointers.
type PathID uint32

// Root is the empty path.
const Root PathID = 0

// Paths interns access paths. It lives as long as the program's analysis
// (the program arena); paths are bounded by the program's types and the
// limits, so it does not grow with the number of SCCs or iterations.
type Paths interface {
	// Extend returns p followed by s, cut to the limits. A path that would
	// pass MaxLoads or MaxSteps is cut back to its prefix through the
	// second load (the fields between the two loads are kept, so that the
	// cut covers the path), and cut reports that the result stands for p+s
	// and every extension (the key's GE bit must be set). An offset step
	// past MaxFieldNest in a segment is absorbed without a cut (the
	// segment already stands for its further offsets). Extend ignores GE:
	// extend a key with ExtendKey.
	Extend(p PathID, s Step) (q PathID, cut bool)
	// Steps returns the steps of p, in order. The slice is shared: do not
	// change it.
	Steps(p PathID) []Step
	// Loads returns the number of loads in p.
	Loads(p PathID) int
	// HasPrefix reports whether prefix is a prefix of p (p itself included).
	HasPrefix(p, prefix PathID) bool
	// Cut returns the prefix of p through its first load, or p itself when
	// it has no load: the widening a summary cap applies, with GE (plan
	// section (a), "Caps and widening").
	Cut(p PathID) PathID
	// Len is the number of interned paths.
	Len() int
}

// Key names pointers relative to an input of a function: those found by
// following Path from input Input, or any extension of it when GE is set.
// It packs into a uint64 (plan section (a), "Keys"):
//
//	bits  0-31 path
//	bits 32-39 input
//	bit  40    GE
//	bit  41    Borrowed (the selected value lends its box payload)
//	bit  42    Counts (conversion of the input to a counted error, 16.5-16.7)
//	bit  43    DirectCount (a definite strong-box conversion)
type Key uint64

const (
	keyGE          Key = 1 << 40
	keyBorrowed    Key = 1 << 41
	keyCounts      Key = 1 << 42
	keyDirectCount Key = 1 << 43
)

// MaxInputs is the number of inputs a key can name.
const MaxInputs = 256

// KeyFlags are the flag bits of a Key.
type KeyFlags struct {
	GE, Borrowed, Counts, DirectCount bool
}

// MakeKey packs a key. input must be in [0, MaxInputs): a larger one would
// name another input, so it is an internal compiler error (a function with
// more inputs is widened by its builder before keys are made for it).
func MakeKey(input int, path PathID, f KeyFlags) Key {
	if input < 0 || input >= MaxInputs {
		panic("internal compiler error: escape2/domain: key input out of range")
	}
	k := Key(path) | Key(uint8(input))<<32
	if f.GE {
		k |= keyGE
	}
	if f.Borrowed {
		k |= keyBorrowed
	}
	if f.Counts {
		k |= keyCounts
	}
	if f.DirectCount {
		k |= keyDirectCount
	}
	return k
}

// Input returns the input the key is relative to.
func (k Key) Input() int { return int(uint8(k >> 32)) }

// Path returns the key's path.
func (k Key) Path() PathID { return PathID(uint32(k)) }

// Flags returns the key's flags.
func (k Key) Flags() KeyFlags {
	return KeyFlags{GE: k&keyGE != 0, Borrowed: k&keyBorrowed != 0, Counts: k&keyCounts != 0, DirectCount: k&keyDirectCount != 0}
}

// GE reports whether the key stands for its path and every extension.
func (k Key) GE() bool { return k&keyGE != 0 }

// WithPath returns k with another path and GE bit, its input and other flags
// unchanged.
func (k Key) WithPath(p PathID, ge bool) Key {
	k = k&^(keyGE|0xffffffff) | Key(p)
	if ge {
		k |= keyGE
	}
	return k
}

// Covers reports whether a covers b: same input and flags other than GE, and
// either the same path, or a is GE and its path is a prefix of b's.
// Antichains under Covers are how summaries keep their sets small.
func Covers(ps Paths, a, b Key) bool {
	const flags = keyBorrowed | keyCounts | keyDirectCount
	if a.Input() != b.Input() || a&flags != b&flags {
		return false
	}
	if a.Path() == b.Path() {
		return a.GE() || !b.GE()
	}
	return a.GE() && ps.HasPrefix(b.Path(), a.Path())
}

// The helpers of keys and paths that the solver and the reference solver use
// are part of this interface too, defined beside their implementation:
// ExtendKey and AppendKey (paths.go) extend a key, keeping GE; PathMatches
// (paths.go) is the definition of what a path stands for, executable;
// PathBytes (paths.go) measures the interner; CoveredBy, SortKeys, Antichain,
// JoinKeys and CoversAll (antichain.go) keep sets of keys as antichains under
// Covers; NewCheckedArena (arena.go) poisons memory on Reset, for tests.

// SetID names a hash-consed set of uint64 elements (tokens or keys) in a
// Sets table. Empty (0) is the empty set.
type SetID uint32

// Empty is the empty set.
const Empty SetID = 0

// Sets holds hash-consed sorted sets in an arena: equal sets have equal IDs,
// so equality is ID comparison and unions are memoized. Reset frees every set
// at once (the SCC arena, plan section (e)); IDs from before a Reset must not
// be used after it.
type Sets interface {
	// Intern returns the ID of the set of elems (any order, duplicates
	// allowed). It may reorder elems.
	Intern(elems []uint64) SetID
	// Elems returns the elements of s in increasing order. The slice is
	// shared: do not change it.
	Elems(s SetID) []uint64
	// Union returns the ID of a ∪ b.
	Union(a, b SetID) SetID
	// Diff returns the ID of a \ b, for difference propagation.
	Diff(a, b SetID) SetID
	// Has reports whether x is in s.
	Has(s SetID, x uint64) bool
	// Len returns the size of s.
	Len(s SetID) int
	// Bytes is the memory the table holds, for the arena-size measurements.
	Bytes() int
	// Reset frees every set.
	Reset()
}

// Arena is a resettable allocator of uint64 slabs: what an SCC's working
// state (constraint graphs, worklists, sets) is carved from, released in one
// Reset when the SCC's summaries are final (plan section (e)).
type Arena interface {
	// Alloc returns a zeroed slice of n words, valid until Reset.
	Alloc(n int) []uint64
	// Bytes is the memory the arena holds now, and Peak the most it has
	// held since it was made.
	Bytes() int
	Peak() int
	// Reset frees everything allocated.
	Reset()
}
