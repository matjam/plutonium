package summary

import (
	"slices"

	"github.com/matjam/plutonium/internal/escape2/domain"
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
	if s.Region.Builds && !t.Region.Builds {
		return false
	}
	for _, r := range s.Region.OuterResults {
		if !slices.Contains(t.Region.OuterResults, r) {
			return false
		}
	}
	return true
}

// isBottom reports whether s has no effects at all.
func isBottom(s *Summary) bool {
	if len(s.Esc) > 0 || len(s.Store) > 0 || len(s.HeapStore) > 0 || len(s.Deferred) > 0 || s.Region.Builds || len(s.Region.OuterResults) > 0 {
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
	anywhere := a.FieldGE || a.Key.GE()
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
// input's root with GE, which covers every key of the input. Reasons, when
// present, follow their entries; an entry that absorbs others keeps its own.
func Widen(ps domain.Paths, s *Summary) {
	if s == nil {
		return
	}
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
	s.Deferred = widenDeferred(ps, s.Deferred)
	if escWhy != nil || storeWhy != nil {
		s.Reasons = append(append([]Reason(nil), escWhy...), storeWhy...)
	} else {
		s.Reasons = nil
	}
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

// CutKey is the widening of one key: its path cut, GE set.
func CutKey(ps domain.Paths, k domain.Key) domain.Key {
	return k.WithPath(ps.Cut(k.Path()), true)
}

func cutKey(ps domain.Paths, k domain.Key) domain.Key { return CutKey(ps, k) }

// rootKey covers every key of k's input with k's flags.
func rootKey(k domain.Key) domain.Key {
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
	for pass := 0; pass < 2; pass++ {
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
				if pass == 0 {
					keys[i] = cutKey(ps, k)
				} else {
					keys[i] = rootKey(k)
				}
			}
		}
		if !over {
			break
		}
		keys, why = antichainKeys(ps, keys, why)
	}
	return keys, why
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
	for _, i := range idx {
		k := keys[i]
		covered := false
		for j, o := range keys {
			if j == i {
				continue
			}
			// o covers k strictly, or equally and comes first: k goes.
			if domain.Covers(ps, o, k) && (o != k || j < i) {
				covered = true
				break
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
	for pass := 0; pass < 2; pass++ {
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
				if pass == 0 {
					pairs[i] = StorePair{cutKey(ps, p.Src), cutKey(ps, p.Dst)}
				} else {
					pairs[i] = StorePair{rootKey(p.Src), rootKey(p.Dst)}
				}
			}
		}
		if !over {
			break
		}
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
	for _, i := range idx {
		p := pairs[i]
		covered := false
		for j, o := range pairs {
			if j == i {
				continue
			}
			if domain.Covers(ps, o.Src, p.Src) && domain.Covers(ps, o.Dst, p.Dst) && (o != p || j < i) {
				covered = true
				break
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
	for pass := 0; pass < 2; pass++ {
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
				if pass == 0 {
					from[i].Key = cutKey(ps, rk.Key)
				} else {
					from[i] = ResultKey{Key: rootKey(rk.Key), Field: domain.Root}
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

// widenDeferred makes each entry's argument keys an antichain under the Esc
// cap, merges entries of one call with one function value key (the union
// of their arguments covers both), merges past MaxDeferredPerCall as said
// there, and returns an antichain under DeferredCovers, sorted.
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
			return rootKey(k)
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
