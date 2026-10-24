package sched

import (
	"fmt"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// Paths for parallel solves. The program's path interner (domain.Paths) is
// shared by every summary, and the IDs it gives depend on the order paths
// are first made. Solves running at once must not make paths in it: the
// IDs would depend on thread timing, and with them the order of every
// summary's keys. So during a round of parallel solves (parallel.go) the
// interner is read only, and each solve makes the paths it needs that are
// not there yet in an overlay of its own, with IDs past the interner's.
// When the round is over, the overlays are committed into the interner one
// after the other in a fixed order, and each result's keys are renamed to
// the paths' IDs there (remap). The interner then grows in the same order
// whatever the number of workers or their timing.

// pathIndex is the read-only index of the interner's paths by parent and
// step, the overlays' view of what is interned. It is kept in step with the
// interner between rounds (sync), never during one.
type pathIndex struct {
	base   domain.Paths
	child  map[uint64]domain.PathID
	parent []domain.PathID // by path
	n      int             // the interner's paths indexed so far
}

func childKey(p domain.PathID, s domain.Step) uint64 {
	return uint64(p)<<32 | uint64(uint32(s))
}

func newPathIndex(base domain.Paths) *pathIndex {
	ix := &pathIndex{base: base, child: map[uint64]domain.PathID{}, parent: []domain.PathID{domain.Root}, n: 1}
	ix.sync()
	return ix
}

// sync indexes the paths the interner made since the last sync. A path's
// parent is made before it, so it is found by walking from the root.
func (ix *pathIndex) sync() {
	for ; ix.n < ix.base.Len(); ix.n++ {
		id := domain.PathID(ix.n)
		steps := ix.base.Steps(id)
		p := domain.Root
		for _, s := range steps[:len(steps)-1] {
			q, ok := ix.child[childKey(p, s)]
			if !ok {
				panic(fmt.Sprintf("internal compiler error: escape2/sched: path %d's prefix is not interned", id))
			}
			p = q
		}
		ix.child[childKey(p, steps[len(steps)-1])] = id
		ix.parent = append(ix.parent, p)
	}
}

// overlay is the paths of one solve: the interner's, read only, and the
// solve's own past them.
type overlay struct {
	ix      *pathIndex
	baseLen domain.PathID
	parent  []domain.PathID // by own path, less baseLen
	step    []domain.Step
	steps   [][]domain.Step
	loads   []int
	own     map[uint64]domain.PathID
	cuts    []domain.PathID // Cut of each own path, once computed (0: not yet)
}

func newOverlay(ix *pathIndex) *overlay {
	return &overlay{ix: ix, baseLen: domain.PathID(ix.n), own: map[uint64]domain.PathID{}}
}

func (o *overlay) isOwn(p domain.PathID) bool { return p >= o.baseLen }

func (o *overlay) Len() int { return int(o.baseLen) + len(o.parent) }

// Filters is the interner's type filters: a solve makes them in the shared
// table (domain.Filters, renumbered when the analysis is over).
func (o *overlay) Filters() *domain.Filters { return o.ix.base.Filters() }

func (o *overlay) Steps(p domain.PathID) []domain.Step {
	if o.isOwn(p) {
		return o.steps[p-o.baseLen]
	}
	return o.ix.base.Steps(p)
}

func (o *overlay) Loads(p domain.PathID) int {
	if o.isOwn(p) {
		return o.loads[p-o.baseLen]
	}
	return o.ix.base.Loads(p)
}

func (o *overlay) parentOf(p domain.PathID) domain.PathID {
	if o.isOwn(p) {
		return o.parent[p-o.baseLen]
	}
	return o.ix.parent[p]
}

func (o *overlay) HasPrefix(p, prefix domain.PathID) bool {
	for o.isOwn(p) {
		if p == prefix {
			return true
		}
		p = o.parent[p-o.baseLen]
	}
	if o.isOwn(prefix) {
		return false
	}
	return o.ix.base.HasPrefix(p, prefix)
}

func (o *overlay) Cut(p domain.PathID) domain.PathID {
	if !o.isOwn(p) {
		return o.ix.base.Cut(p)
	}
	i := p - o.baseLen
	if c := o.cuts[i]; c != 0 {
		return c - 1
	}
	c := p
	if o.Loads(p) > 0 {
		c = o.throughLoad(p, 1)
	}
	o.cuts[i] = c + 1
	return c
}

// cut is the interner's cut: the prefix through the second load (or the
// last, or Root).
func (o *overlay) cut(p domain.PathID) domain.PathID {
	n := o.Loads(p)
	if n > 2 {
		n = 2
	}
	if n == 0 {
		return domain.Root
	}
	return o.throughLoad(p, n)
}

func (o *overlay) throughLoad(p domain.PathID, n int) domain.PathID {
	for {
		st := o.Steps(p)
		if o.Loads(p) == n && st[len(st)-1] == domain.StepLoad {
			return p
		}
		p = o.parentOf(p)
	}
}

// segment is the number of offset steps of p since its last load.
func (o *overlay) segment(p domain.PathID) int {
	st := o.Steps(p)
	n := 0
	for i := len(st) - 1; i >= 0 && st[i] != domain.StepLoad; i-- {
		n++
	}
	return n
}

// Extend is the interner's Extend (domain/paths.go), the same limits.
func (o *overlay) Extend(p domain.PathID, s domain.Step) (domain.PathID, bool) {
	if s < domain.StepLoad {
		panic("escape2/sched: Extend: invalid step")
	}
	depth := len(o.Steps(p))
	if s == domain.StepLoad {
		if o.Loads(p)+1 > domain.MaxLoads || depth+1 > domain.MaxSteps {
			return o.cut(p), true
		}
		return o.child(p, s), false
	}
	if o.segment(p) == domain.MaxFieldNest {
		return p, false
	}
	if depth+1 > domain.MaxSteps {
		return o.cut(p), true
	}
	return o.child(p, s), false
}

func (o *overlay) child(p domain.PathID, s domain.Step) domain.PathID {
	k := childKey(p, s)
	if !o.isOwn(p) {
		if c, ok := o.ix.child[k]; ok {
			return c
		}
	}
	if c, ok := o.own[k]; ok {
		return c
	}
	c := o.baseLen + domain.PathID(len(o.parent))
	st := append(append(make([]domain.Step, 0, len(o.Steps(p))+1), o.Steps(p)...), s)
	loads := o.Loads(p)
	if s == domain.StepLoad {
		loads++
	}
	o.parent = append(o.parent, p)
	o.step = append(o.step, s)
	o.steps = append(o.steps, st)
	o.loads = append(o.loads, loads)
	o.cuts = append(o.cuts, 0)
	o.own[k] = c
	return c
}

// commit makes the overlay's own paths in the interner, in the order they
// were made, and returns the renaming of their IDs (nil: none). Only one
// commit runs at a time, between rounds; the index is synced after it.
func (o *overlay) commit() func(domain.PathID) domain.PathID {
	if len(o.parent) == 0 {
		return nil
	}
	real := make([]domain.PathID, len(o.parent))
	rename := func(p domain.PathID) domain.PathID {
		if o.isOwn(p) {
			return real[p-o.baseLen]
		}
		return p
	}
	for i := range o.parent {
		q, cut := o.ix.base.Extend(rename(o.parent[i]), o.step[i])
		if cut {
			panic("internal compiler error: escape2/sched: a solve's path is past the limits")
		}
		real[i] = q
	}
	o.ix.sync()
	return rename
}

// remapKey renames the path of k.
func remapKey(k domain.Key, rename func(domain.PathID) domain.PathID) domain.Key {
	return k&^0xffffffff | domain.Key(rename(k.Path()))
}

// remapSummary renames the paths of s, in place, and makes its sets sorted
// antichains again (summary.Widen, which reorders the reasons with their
// entries and cuts nothing more: the counts are those of a widened
// summary).
func remapSummary(ps domain.Paths, s *summary.Summary, rename func(domain.PathID) domain.PathID) {
	if s == nil || rename == nil {
		return
	}
	for i := range s.Esc {
		s.Esc[i] = remapKey(s.Esc[i], rename)
	}
	for i := range s.Store {
		s.Store[i].Src = remapKey(s.Store[i].Src, rename)
		s.Store[i].Dst = remapKey(s.Store[i].Dst, rename)
	}
	for i := range s.HeapStore {
		s.HeapStore[i] = remapKey(s.HeapStore[i], rename)
	}
	for r := range s.Res {
		res := &s.Res[r]
		for i := range res.From {
			res.From[i].Key = remapKey(res.From[i].Key, rename)
			res.From[i].Field = rename(res.From[i].Field)
		}
		for i := range res.Heap {
			res.Heap[i] = rename(res.Heap[i])
		}
		for i := range res.CountedHeap {
			res.CountedHeap[i] = rename(res.CountedHeap[i])
		}
	}
	for i := range s.Deferred {
		d := &s.Deferred[i]
		d.Fn = remapKey(d.Fn, rename)
		for j := range d.Args {
			for k := range d.Args[j].Keys {
				d.Args[j].Keys[k] = remapKey(d.Args[j].Keys[k], rename)
			}
		}
	}
	summary.Widen(ps, s)
}
