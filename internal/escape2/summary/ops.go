package summary

import (
	"slices"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
)

// Leq reports whether s is no wider than t: every entry of s is covered by
// one of t. It is the post-fixed-point check of D44 (plan section (f)): a
// loaded summary t is accepted when the summary computed against it is Leq t.
//
// Coverage is entry by entry: an Esc or HeapStore key by a key that covers it
// (domain.Covers), a Store pair by a pair whose two keys cover its two keys,
// a result key at a field by a key at the same field that covers it, or by a
// GE key at a prefix of the field (a caller places what a GE key leads to
// anywhere under that field), and heap results by heap results of the same
// kind (a caller does not tell heap pointers apart by field). Region facts
// are compared as sets. Reasons are explanations and are not compared.
func Leq(ps domain.Paths, s, t *Summary) bool {
	if s == nil {
		return true
	}
	if t == nil {
		return isBottom(s)
	}
	for _, k := range s.Esc {
		if !coveredKey(ps, k, t.Esc) {
			return false
		}
	}
	for _, k := range s.HeapStore {
		if !coveredKey(ps, k, t.HeapStore) {
			return false
		}
	}
	for _, p := range s.Store {
		if !coveredPair(ps, p, t.Store) {
			return false
		}
	}
	for r, res := range s.Res {
		var tr Result
		if r < len(t.Res) {
			tr = t.Res[r]
		}
		for _, rk := range res.From {
			if !coveredResult(ps, rk, tr.From) {
				return false
			}
		}
		if len(res.Heap) > 0 && len(tr.Heap) == 0 {
			return false
		}
		if len(res.CountedHeap) > 0 && len(tr.CountedHeap) == 0 {
			return false
		}
	}
	for _, d := range s.Deferred {
		if !slices.ContainsFunc(t.Deferred, func(e Deferred) bool { return DeferredCovers(ps, e, d) }) {
			return false
		}
	}
	if s.Region.Self && !t.Region.Self {
		return false
	}
	if s.Region.Builds && !t.Region.Builds {
		return false
	}
	for _, r := range s.Region.OuterResults {
		if !slices.Contains(t.Region.OuterResults, r) {
			return false
		}
	}
	if !slices.Contains(t.Region.Lits, eir.NoFunc) {
		for _, l := range s.Region.Lits {
			if !slices.Contains(t.Region.Lits, l) {
				return false
			}
		}
	}
	return true
}

// isBottom reports whether s has no effects at all.
func isBottom(s *Summary) bool {
	if len(s.Esc) > 0 || len(s.Store) > 0 || len(s.HeapStore) > 0 || len(s.Deferred) > 0 || s.Region.Builds || s.Region.Self || len(s.Region.OuterResults) > 0 || len(s.Region.Lits) > 0 {
		return false
	}
	for _, r := range s.Res {
		if len(r.From) > 0 || len(r.Heap) > 0 || len(r.CountedHeap) > 0 {
			return false
		}
	}
	return true
}

func coveredKey(ps domain.Paths, k domain.Key, by []domain.Key) bool {
	for _, b := range by {
		if domain.Covers(ps, b, k) {
			return true
		}
	}
	return false
}

func coveredPair(ps domain.Paths, p StorePair, by []StorePair) bool {
	for _, b := range by {
		if domain.Covers(ps, b.Src, p.Src) && domain.Covers(ps, b.Dst, p.Dst) {
			return true
		}
	}
	return false
}

// resultCovers reports whether result key a covers b: a's key covers b's,
// and a's field holds b's (the same field, a FieldGE b does not exceed, or a
// prefix of it with FieldGE). A GE key stands anywhere under its field too:
// what it leads to is placed with GE tags (rules.go, reach).
func resultCovers(ps domain.Paths, a, b ResultKey) bool {
	if !domain.Covers(ps, a.Key, b.Key) {
		return false
	}
	anywhere := a.FieldGE || a.Key.GE() || a.Key.Deep()
	if a.Field == b.Field {
		return anywhere || !b.FieldGE
	}
	return anywhere && ps.HasPrefix(b.Field, a.Field)
}

func coveredResult(ps domain.Paths, rk ResultKey, by []ResultKey) bool {
	for _, b := range by {
		if resultCovers(ps, b, rk) {
			return true
		}
	}
	return false
}

// Widen applies the caps: past MaxEscPerInput or MaxStorePerInput entries of
// one input, that input's keys are cut and marked GE, and the sets are made
// antichains again. The result covers s.
//
// The cap on Esc applies to HeapStore and to each result's keys as well. If
// the cut keys of an input are still past the cap, they are replaced by the
// input's root with GE, which covers every key of the input, or, for a key
// that stands for no pointer of the input's own object (a Deep key, or one
// whose path has a load), by Deep on the root (rootKey). Reasons, when
// present, follow their entries; an entry that absorbs others keeps its own.
func Widen(ps domain.Paths, s *Summary) {
	if s == nil {
		return
	}
	dropFilters(ps, s)
	var escWhy, storeWhy []Reason
	if len(s.Reasons) == len(s.Esc)+len(s.Store) && len(s.Reasons) > 0 {
		escWhy = s.Reasons[:len(s.Esc)]
		storeWhy = s.Reasons[len(s.Esc):]
	}
	s.Esc, escWhy = widenKeys(ps, s.Esc, escWhy, MaxEscPerInput)
	s.HeapStore, _ = widenKeys(ps, s.HeapStore, nil, MaxEscPerInput)
	s.Store, storeWhy = widenPairs(ps, s.Store, storeWhy)
	for r := range s.Res {
		s.Res[r].From = widenResults(ps, s.Res[r].From)
		s.Res[r].Heap = sortedPaths(s.Res[r].Heap)
		s.Res[r].CountedHeap = sortedPaths(s.Res[r].CountedHeap)
	}
	s.Region.OuterResults = sortedInts(s.Region.OuterResults)
	s.Region.Lits = SortedLits(s.Region.Lits)
	s.Deferred = widenDeferred(ps, s.Deferred)
	if escWhy != nil || storeWhy != nil {
		s.Reasons = append(append([]Reason(nil), escWhy...), storeWhy...)
	} else {
		s.Reasons = nil
	}
}

// dropFilters makes one entry of the entries that differ only in their
// type filter (D51): the key with the union of their filters, which stands
// for what they stand for together; with no filter when the union is too
// large (domain.MaxAtoms), which covers them. A join of many candidates'
// entries of one receiver, one filter each, is then as small as without
// filters.
func dropFilters(ps domain.Paths, s *Summary) {
	type fk struct {
		k   domain.Key // without filter
		pos int
		x   domain.Key // the store's destination, or the result's field and index
		y   uint64
	}
	union := func(ids map[fk]domain.FilterID, g fk, id domain.FilterID) {
		if old, ok := ids[g]; ok {
			if old != 0 {
				id = ps.Filters().Union(old, id)
			} else {
				id = 0
			}
		}
		ids[g] = id
	}
	apply := func(ids map[fk]domain.FilterID, g fk, k domain.Key, ps0 int) domain.Key {
		id := ids[g]
		if id == 0 {
			return k.NoFilter()
		}
		return k.NoFilter().WithFilterRaw(id, ps0)
	}
	ids := map[fk]domain.FilterID{}
	for _, k := range s.Esc {
		if id, pos := k.Filter(); id != 0 {
			union(ids, fk{k: k.NoFilter(), pos: pos}, id)
		}
	}
	for i, k := range s.Esc {
		if id, pos := k.Filter(); id != 0 {
			s.Esc[i] = apply(ids, fk{k: k.NoFilter(), pos: pos}, k, pos)
		}
	}
	clear(ids)
	for _, p := range s.Store {
		if id, pos := p.Src.Filter(); id != 0 {
			union(ids, fk{k: p.Src.NoFilter(), pos: pos, x: p.Dst}, id)
		}
	}
	for i, p := range s.Store {
		if id, pos := p.Src.Filter(); id != 0 {
			s.Store[i].Src = apply(ids, fk{k: p.Src.NoFilter(), pos: pos, x: p.Dst}, p.Src, pos)
		}
	}
	for r := range s.Res {
		clear(ids)
		for _, rk := range s.Res[r].From {
			if id, pos := rk.Key.Filter(); id != 0 {
				union(ids, fk{k: rk.Key.NoFilter(), pos: pos, x: domain.Key(rk.Field), y: b2u(rk.FieldGE)}, id)
			}
		}
		for i, rk := range s.Res[r].From {
			if id, pos := rk.Key.Filter(); id != 0 {
				s.Res[r].From[i].Key = apply(ids, fk{k: rk.Key.NoFilter(), pos: pos, x: domain.Key(rk.Field), y: b2u(rk.FieldGE)}, rk.Key, pos)
			}
		}
	}
}

func b2u(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// MaxRawStorePerInput bounds the distinct store pairs of one input (by its
// source) a solver collects before Widen: past it, the input's pairs are
// cut (CutKey on both keys) as they are collected. A store of a value of n
// keys through m pointers makes n*m pairs, which Widen would cut anyway;
// cutting them as they come keeps a solve's memory bounded (rules.go, "The
// summary"). It is a memory guard, set far above the caps: whether it fires
// depends on which covered tokens a solver happens to hold (GE subsumption
// drops them in an order of its own), so two solvers may cut at different
// times and give results that differ by a widening, both sound; at 8x the
// cap the differential test met such a case (seed 3719), at 64x none of
// 30,000.
const MaxRawStorePerInput = 64 * MaxStorePerInput

// CutKey is the widening of one key: its path cut, GE set; a Deep key whose
// path has no load stays Deep, on Root (what the input points at, anywhere
// in its object, transitively, covers it and never the input's own
// pointers); an exact or FieldsGE key whose path has no load, a place in
// the input's own object, becomes FieldsGE on its path (that place and every
// offset under it: GE would add what the object points to, and a store
// pair cut for its source's count would make its destination every
// pointer the caller's object leads to).
func CutKey(ps domain.Paths, k domain.Key) domain.Key {
	// A type filter (D51) at a position the cut path still reaches stays:
	// the cut key stands for what passes it there and more beyond.
	id, pos := k.Filter()
	c := cutKeyPlain(ps, k.NoFilter())
	return c.WithFilter(ps, id, pos)
}

func cutKeyPlain(ps domain.Paths, k domain.Key) domain.Key {
	if k.Deep() && ps.Loads(k.Path()) == 0 {
		return k.WithPath(domain.Root, false).WithDeep()
	}
	if !k.GE() && !k.Deep() && ps.Loads(k.Path()) == 0 {
		return k.WithPath(k.Path(), false).WithFieldsGE()
	}
	if ps.Loads(k.Path()) >= 2 {
		// The path goes on past its first load with offsets, a load and
		// more: Deep on the cut covers it, and every extension, and leaves
		// out the pointers of the cut path itself (a closure record's
		// pointer to a captured variable, not the variable's contents).
		return k.WithPath(ps.Cut(k.Path()), false).WithDeep()
	}
	return k.WithPath(ps.Cut(k.Path()), true)
}

func cutKey(ps domain.Paths, k domain.Key) domain.Key { return CutKey(ps, k) }

// cutTwo covers k with a key on the prefix of its path through the second
// load, Deep, when the path goes on past that load; any other key is kept.
// Deep on the prefix covers the path and every extension, as in cutKeyPlain.
func cutTwo(ps domain.Paths, k domain.Key) domain.Key {
	if ps.Loads(k.Path()) < 3 {
		return k
	}
	id, pos := k.Filter()
	q, n := domain.Root, 0
	for _, s := range ps.Steps(k.Path()) {
		q, _ = ps.Extend(q, s)
		if s == domain.StepLoad {
			if n++; n == 2 {
				break
			}
		}
	}
	return k.NoFilter().WithPath(q, false).WithDeep().WithFilter(ps, id, pos)
}

// rootKey covers k with a key on its input's root, with k's flags: Deep on
// Root when k stands for no pointer of the input's object itself (k is Deep,
// or its path has a load), else GE on Root, which covers every key of the
// input.
func rootKey(ps domain.Paths, k domain.Key) domain.Key {
	id, pos := k.Filter()
	return rootKeyPlain(ps, k.NoFilter()).WithFilter(ps, id, pos)
}

func rootKeyPlain(ps domain.Paths, k domain.Key) domain.Key {
	if k.Deep() || ps.Loads(k.Path()) > 0 {
		return k.WithPath(domain.Root, false).WithDeep()
	}
	return k.WithPath(domain.Root, true)
}

// perInput counts the keys of each input.
func perInput(keys []domain.Key) map[int]int {
	n := map[int]int{}
	for _, k := range keys {
		n[k.Input()]++
	}
	return n
}

// widenKeys caps keys per input and returns an antichain, sorted, with the
// reasons (if any) of the surviving keys.
func widenKeys(ps domain.Paths, keys []domain.Key, why []Reason, limit int) ([]domain.Key, []Reason) {
	keys, why = antichainKeys(ps, keys, why)
	// Cuts keep the type filters (D51) where the cut path still reaches
	// them, and the filtered forms of a key merge into one, with the union
	// of their filters (mergeKeyFilters): an input of a join of many
	// candidates stays filtered by its members' receivers. Only a third
	// pass drops them.
	for pass := 0; pass < 3; pass++ {
		over := false
		for in, n := range perInput(keys) {
			if n <= limit {
				continue
			}
			over = true
			for i, k := range keys {
				if k.Input() != in {
					continue
				}
				switch pass {
				case 2:
					keys[i] = k.NoFilter()
				case 0:
					keys[i] = cutKey(ps, k)
				default:
					keys[i] = rootKey(ps, k)
				}
			}
		}
		if !over {
			break
		}
		mergeKeyFilters(ps, keys)
		keys, why = antichainKeys(ps, keys, why)
	}
	return keys, why
}

// mergeKeyFilters gives the keys that differ only in their type filter
// (D51) the union of their filters (dropFilters), in place.
func mergeKeyFilters(ps domain.Paths, keys []domain.Key) {
	type g struct {
		k   domain.Key
		pos int
	}
	ids := map[g]domain.FilterID{}
	for _, k := range keys {
		if id, pos := k.Filter(); id != 0 {
			x := g{k.NoFilter(), pos}
			if old, ok := ids[x]; ok {
				if old != 0 {
					id = ps.Filters().Union(old, id)
				} else {
					id = 0
				}
			}
			ids[x] = id
		}
	}
	for i, k := range keys {
		if id, pos := k.Filter(); id != 0 {
			keys[i] = k.NoFilter().WithFilterRaw(ids[g{k.NoFilter(), pos}], pos)
		}
	}
}

// mergePairFilters is mergeKeyFilters for the sources of store pairs of
// one destination.
func mergePairFilters(ps domain.Paths, pairs []StorePair) {
	type g struct {
		k, d domain.Key
		pos  int
	}
	ids := map[g]domain.FilterID{}
	for _, p := range pairs {
		if id, pos := p.Src.Filter(); id != 0 {
			x := g{p.Src.NoFilter(), p.Dst, pos}
			if old, ok := ids[x]; ok {
				if old != 0 {
					id = ps.Filters().Union(old, id)
				} else {
					id = 0
				}
			}
			ids[x] = id
		}
	}
	for i, p := range pairs {
		if id, pos := p.Src.Filter(); id != 0 {
			pairs[i].Src = p.Src.NoFilter().WithFilterRaw(ids[g{p.Src.NoFilter(), p.Dst, pos}], pos)
		}
	}
}

// antichainKeys sorts keys, drops duplicates and keys another key covers.
func antichainKeys(ps domain.Paths, keys []domain.Key, why []Reason) ([]domain.Key, []Reason) {
	idx := make([]int, len(keys))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int { return cmpU64(uint64(keys[a]), uint64(keys[b])) })
	var out []domain.Key
	var outWhy []Reason
	// Past a few keys, only the keys that can cover k are tried: those of
	// k's input and flags on a prefix of k's path (domain.Covers), found by
	// an index of the keys by their key without GE and FieldsGE.
	var bucket map[domain.Key][]int
	var pre []domain.PathID
	if len(keys) > indexFrom {
		bucket = make(map[domain.Key][]int, len(keys))
		for j, o := range keys {
			n := indexKey(o)
			bucket[n] = append(bucket[n], j)
		}
	}
	for _, i := range idx {
		k := keys[i]
		// o covers k strictly, or equally and comes first: k goes.
		covers := func(j int) bool {
			o := keys[j]
			return j != i && domain.Covers(ps, o, k) && (o != k || j < i)
		}
		covered := false
		if bucket != nil {
			pre = prefixes(ps, k.Path(), pre)
			n := indexKey(k)
		search:
			for _, q := range pre {
				for _, j := range bucket[n.WithPath(q, false)] {
					if covers(j) {
						covered = true
						break search
					}
				}
			}
		} else {
			for j := range keys {
				if covers(j) {
					covered = true
					break
				}
			}
		}
		if covered {
			continue
		}
		out = append(out, k)
		if why != nil {
			outWhy = append(outWhy, why[i])
		}
	}
	return out, outWhy
}

func widenPairs(ps domain.Paths, pairs []StorePair, why []Reason) ([]StorePair, []Reason) {
	pairs, why = antichainPairs(ps, pairs, why)
	// Pass 0 cuts both keys of a pair through their second load (cutTwo),
	// which keeps a pair inside the memory its first load reaches (a store
	// within a captured record's variable stays there); pass 1 cuts through
	// the first load, pass 2 to the roots, pass 3 drops the sources' type
	// filters (widenKeys).
	for pass := 0; pass < 4; pass++ {
		n := map[int]int{}
		for _, p := range pairs {
			n[p.Src.Input()]++
		}
		over := false
		for in, c := range n {
			if c <= MaxStorePerInput {
				continue
			}
			over = true
			for i, p := range pairs {
				if p.Src.Input() != in {
					continue
				}
				switch pass {
				case 3:
					pairs[i].Src = p.Src.NoFilter()
				case 0:
					pairs[i] = StorePair{cutTwo(ps, p.Src), cutTwo(ps, p.Dst)}
				case 1:
					pairs[i] = StorePair{cutKey(ps, p.Src), cutKey(ps, p.Dst)}
				default:
					pairs[i] = StorePair{rootKey(ps, p.Src), rootKey(ps, p.Dst)}
				}
			}
		}
		if !over {
			break
		}
		mergePairFilters(ps, pairs)
		pairs, why = antichainPairs(ps, pairs, why)
	}
	return pairs, why
}

func antichainPairs(ps domain.Paths, pairs []StorePair, why []Reason) ([]StorePair, []Reason) {
	idx := make([]int, len(pairs))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		if c := cmpU64(uint64(pairs[a].Src), uint64(pairs[b].Src)); c != 0 {
			return c
		}
		return cmpU64(uint64(pairs[a].Dst), uint64(pairs[b].Dst))
	})
	var out []StorePair
	var outWhy []Reason
	// Past a few pairs, only the pairs that can cover p are tried: by
	// domain.Covers, those whose Src and Dst are of p's inputs and flags on
	// prefixes of p's paths, found by an index of the pairs by their keys
	// without GE and FieldsGE. A pair has at most MaxSteps+1 prefixes on
	// each side, so this is linear where comparing every two pairs was
	// quadratic (it was a third of the analysis of the translated
	// compiler).
	var bucket map[[2]domain.Key][]int
	var preSrc, preDst []domain.PathID
	if len(pairs) > indexFrom {
		bucket = make(map[[2]domain.Key][]int, len(pairs))
		for j, o := range pairs {
			n := [2]domain.Key{indexKey(o.Src), indexKey(o.Dst)}
			bucket[n] = append(bucket[n], j)
		}
	}
	for _, i := range idx {
		p := pairs[i]
		covers := func(j int) bool {
			o := pairs[j]
			return j != i && domain.Covers(ps, o.Src, p.Src) && domain.Covers(ps, o.Dst, p.Dst) && (o != p || j < i)
		}
		covered := false
		if bucket != nil {
			preSrc = prefixes(ps, p.Src.Path(), preSrc)
			preDst = prefixes(ps, p.Dst.Path(), preDst)
			ns, nd := indexKey(p.Src), indexKey(p.Dst)
		search:
			for _, qs := range preSrc {
				for _, qd := range preDst {
					for _, j := range bucket[[2]domain.Key{ns.WithPath(qs, false), nd.WithPath(qd, false)}] {
						if covers(j) {
							covered = true
							break search
						}
					}
				}
			}
		} else {
			for j := range pairs {
				if covers(j) {
					covered = true
					break
				}
			}
		}
		if covered {
			continue
		}
		out = append(out, p)
		if why != nil {
			outWhy = append(outWhy, why[i])
		}
	}
	return out, outWhy
}

func widenResults(ps domain.Paths, from []ResultKey) []ResultKey {
	from = antichainResults(ps, from)
	for pass := 0; pass < 3; pass++ { // pass 2: the type filters (widenKeys)
		n := map[int]int{}
		for _, rk := range from {
			n[rk.Key.Input()]++
		}
		over := false
		for in, c := range n {
			if c <= MaxEscPerInput {
				continue
			}
			over = true
			for i, rk := range from {
				if rk.Key.Input() != in {
					continue
				}
				// A cut key stands anywhere under the field; the field is
				// kept, since a GE key covers its extensions at any prefix
				// of the field (resultCovers), and a caller places it with
				// GE tags.
				switch pass {
				case 2:
					from[i].Key = rk.Key.NoFilter()
				case 0:
					from[i].Key = cutKey(ps, rk.Key)
				default:
					from[i] = ResultKey{Key: rootKey(ps, rk.Key), Field: domain.Root}
				}
			}
		}
		if !over {
			break
		}
		from = antichainResults(ps, from)
	}
	return from
}

func antichainResults(ps domain.Paths, from []ResultKey) []ResultKey {
	sorted := slices.Clone(from)
	slices.SortStableFunc(sorted, func(a, b ResultKey) int {
		if c := cmpU64(uint64(a.Key), uint64(b.Key)); c != 0 {
			return c
		}
		if c := cmpU64(uint64(a.Field), uint64(b.Field)); c != 0 {
			return c
		}
		return cmpBool(a.FieldGE, b.FieldGE)
	})
	var out []ResultKey
	for i, rk := range sorted {
		covered := false
		for j, o := range sorted {
			// o covers rk strictly, or they cover each other and o comes first.
			if j != i && resultCovers(ps, o, rk) && (!resultCovers(ps, rk, o) || j < i) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, rk)
		}
	}
	return out
}

// MaxDeferredPerCall bounds the deferred entries of one origin call: past
// it, the entries of that call are merged (their function value keys cut,
// then the input's root), never with another call's (the fallback join is
// per call).
const MaxDeferredPerCall = 16

// MaxDeferredPerInput bounds the deferred entries of one origin call whose
// function value keys name one input: past it, they merge into one, GE on
// the longest common prefix of their paths (widenDeferred).
const MaxDeferredPerInput = 4

// widenDeferred makes each entry's argument keys an antichain under the Esc
// cap, merges entries of one call with one function value key (the union
// of their arguments covers both), then, past MaxDeferredPerInput, the
// entries of one call whose function value keys name one input: one entry
// whose key is GE on the longest common prefix of their paths (a function forwarded through many
// paths would otherwise add an entry for each, and its callers re-solve for
// each), merges past MaxDeferredPerCall as said there, and returns an
// antichain under DeferredCovers, sorted.
func widenDeferred(ps domain.Paths, ds []Deferred) []Deferred {
	if len(ds) == 0 {
		return nil
	}
	type site struct {
		origin uint64
		site   uint64
	}
	merge := func(ds []Deferred, fn func(domain.Key) domain.Key) []Deferred {
		var out []Deferred
		at := map[[3]uint64]int{}
		for _, d := range ds {
			d.Fn = fn(d.Fn)
			k := [3]uint64{uint64(d.Origin), uint64(d.Site), uint64(d.Fn)}
			i, ok := at[k]
			if !ok {
				at[k] = len(out)
				out = append(out, Deferred{Fn: d.Fn, Origin: d.Origin, Site: d.Site, Args: append([]DeferredArg(nil), d.Args...)})
				continue
			}
			o := &out[i]
			for len(o.Args) < len(d.Args) {
				o.Args = append(o.Args, DeferredArg{})
			}
			for j, a := range d.Args {
				o.Args[j] = DeferredArg{Keys: append(append([]domain.Key(nil), o.Args[j].Keys...), a.Keys...), Heap: o.Args[j].Heap || a.Heap}
			}
		}
		return out
	}
	ds = merge(ds, func(k domain.Key) domain.Key { return k })
	// One entry per (call, input of the function value key).
	type group struct {
		origin, site uint64
		in           domain.Key // the key without path and GE, FieldsGE or Deep
	}
	common := map[group][]domain.Step{}
	count := map[group]int{}
	for _, d := range ds {
		g := group{uint64(d.Origin), uint64(d.Site), plainKey(d.Fn).WithPath(domain.Root, false)}
		st := ps.Steps(d.Fn.Path())
		if c, ok := common[g]; ok {
			n := 0
			for n < len(c) && n < len(st) && c[n] == st[n] {
				n++
			}
			common[g] = c[:n]
		} else {
			common[g] = st
		}
		count[g]++
	}
	ds = mergeBy(ds, func(d Deferred) domain.Key {
		g := group{uint64(d.Origin), uint64(d.Site), plainKey(d.Fn).WithPath(domain.Root, false)}
		if count[g] <= MaxDeferredPerInput {
			return d.Fn
		}
		p := domain.Root
		for _, st := range common[g] {
			p, _ = ps.Extend(p, st)
		}
		return d.Fn.NoFilter().WithPath(p, true)
	}, merge)
	for pass := 0; pass < 2; pass++ {
		n := map[site]int{}
		for _, d := range ds {
			n[site{uint64(d.Origin), uint64(d.Site)}]++
		}
		over := false
		for _, c := range n {
			over = over || c > MaxDeferredPerCall
		}
		if !over {
			break
		}
		ds = merge(ds, func(k domain.Key) domain.Key {
			if pass == 0 {
				return cutKey(ps, k)
			}
			return rootKey(ps, k)
		})
	}
	for i := range ds {
		for j := range ds[i].Args {
			ds[i].Args[j].Keys, _ = widenKeys(ps, ds[i].Args[j].Keys, nil, MaxEscPerInput)
		}
	}
	slices.SortStableFunc(ds, func(a, b Deferred) int {
		if c := cmpU64(uint64(a.Origin), uint64(b.Origin)); c != 0 {
			return c
		}
		if c := cmpU64(uint64(a.Site), uint64(b.Site)); c != 0 {
			return c
		}
		return cmpU64(uint64(a.Fn), uint64(b.Fn))
	})
	var out []Deferred
	for i, d := range ds {
		covered := false
		for j, o := range ds {
			if j != i && DeferredCovers(ps, o, d) && (!DeferredCovers(ps, d, o) || j < i) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, d)
		}
	}
	return out
}

// mergeBy maps each entry's function value key by fn and merges the
// entries that then have one (merge is widenDeferred's merge).
func mergeBy(ds []Deferred, fn func(Deferred) domain.Key, merge func([]Deferred, func(domain.Key) domain.Key) []Deferred) []Deferred {
	for i := range ds {
		ds[i].Fn = fn(ds[i])
	}
	return merge(ds, func(k domain.Key) domain.Key { return k })
}

// SortedLits sorts and de-duplicates a region's literals in place; with
// eir.NoFunc among them (some not named) it is that alone, which covers
// every other.
func SortedLits(ls []eir.FuncID) []eir.FuncID {
	if slices.Contains(ls, eir.NoFunc) {
		return []eir.FuncID{eir.NoFunc}
	}
	slices.Sort(ls)
	return slices.Compact(ls)
}

func sortedPaths(ps []domain.PathID) []domain.PathID {
	slices.Sort(ps)
	return slices.Compact(ps)
}

func sortedInts(xs []int) []int {
	slices.Sort(xs)
	return slices.Compact(xs)
}

func cmpBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case b:
		return -1
	}
	return 1
}

func cmpU64(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// indexFrom is the size past which an antichain is made with an index of
// its keys rather than by comparing every two.
const indexFrom = 16

// plainKey is k without GE, FieldsGE, Deep and a filter: what a key that
// covers k shares with it besides a prefix of its path (domain.Covers; an
// unfiltered key covers its filtered forms).
func plainKey(k domain.Key) domain.Key { return k.WithPath(k.Path(), false).NoFilter() }

// indexKey is plainKey without Borrowed either: the bucket of the keys that
// may cover k in an antichain (a Borrowed key covers its twin, domain.Covers).
func indexKey(k domain.Key) domain.Key {
	f := k.Flags()
	f.GE, f.FieldsGE, f.Deep, f.Borrowed, f.Counts = false, false, false, false, false
	return domain.MakeKey(k.Input(), k.Path(), f)
}

// prefixes returns the prefixes of p, Root first and p last, in buf. They
// are interned (a path's prefixes are made before it), so Extend only finds
// them.
func prefixes(ps domain.Paths, p domain.PathID, buf []domain.PathID) []domain.PathID {
	buf = append(buf[:0], domain.Root)
	q := domain.Root
	for _, st := range ps.Steps(p) {
		q, _ = ps.Extend(q, st)
		buf = append(buf, q)
	}
	return buf
}
