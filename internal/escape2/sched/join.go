package sched

import (
	"encoding/binary"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// The summary of a dynamic call (decision D47, revised: no language rule
// for dynamic calls) is the join of the summaries of every function in its
// candidate set. The solver sees one virtual function per distinct
// candidate set, numbered past Program.Funcs, whose summary is that join:
// the solver applies it as it applies any callee (solve/call.go,
// applyCandidate), so the join is computed once per set and solve, not once
// per call site and candidate.
//
// A virtual function is not a node of the call graph: a function that makes
// a dynamic call depends on every candidate directly (callGraph.deps), so
// it is solved after them, and solved again in its SCC whenever one of them
// grows. Sets of one function need no virtual function, and sets with a
// bodyless function stay as they are, since the solver gives a bodyless
// callee its default effects rather than a summary.

type virtuals struct {
	p     *eir.Program
	paths domain.Paths
	index map[string]eir.FuncID // candidate set, encoded, to its virtual function
	sets  [][]eir.FuncID        // by virtual function, less len(p.Funcs)

	// cache holds each virtual function's join, valid while gen has not
	// moved: gen counts the changes of every summary.
	cache   []*summary.Summary
	cacheAt []uint64
	gen     uint64
}

func newVirtuals(p *eir.Program, paths domain.Paths) *virtuals {
	return &virtuals{p: p, paths: paths, index: map[string]eir.FuncID{}, gen: 1}
}

// of returns the candidates the solver applies for a candidate set: the set
// itself, or its virtual function.
func (v *virtuals) of(ids []eir.FuncID) []eir.FuncID {
	if len(ids) < 2 {
		return ids
	}
	for _, id := range ids {
		if int(id) >= len(v.p.Funcs) || v.p.Funcs[id].Bodyless {
			return ids
		}
	}
	key := make([]byte, 0, 4*len(ids))
	for _, id := range ids {
		key = binary.LittleEndian.AppendUint32(key, uint32(id))
	}
	if vid, ok := v.index[string(key)]; ok {
		return []eir.FuncID{vid}
	}
	vid := eir.FuncID(len(v.p.Funcs) + len(v.sets))
	v.index[string(key)] = vid
	v.sets = append(v.sets, append([]eir.FuncID(nil), ids...))
	v.cache = append(v.cache, nil)
	v.cacheAt = append(v.cacheAt, 0)
	return []eir.FuncID{vid}
}

// isVirtual reports whether id is a virtual function.
func (v *virtuals) isVirtual(id eir.FuncID) bool {
	i := int(id) - len(v.p.Funcs)
	return i >= 0 && i < len(v.sets)
}

// changed notes that some summary changed: every join must be recomputed.
func (v *virtuals) changed() { v.gen++ }

// summary returns the join for virtual function id, given the current
// summary of each member (nil: bottom).
func (v *virtuals) summary(id eir.FuncID, sum func(eir.FuncID) *summary.Summary) *summary.Summary {
	i := int(id) - len(v.p.Funcs)
	if v.cacheAt[i] == v.gen && v.cache[i] != nil {
		return v.cache[i]
	}
	s := join(v.paths, v.sets[i], sum)
	v.cache[i], v.cacheAt[i] = s, v.gen
	return s
}

// join returns the join of the summaries of ids: every entry of each, the
// most inputs of any, made an antichain again by summary.Widen (whose caps
// only widen). Each Esc and Store entry keeps a reason naming the candidate
// it comes from and its index there.
func join(ps domain.Paths, ids []eir.FuncID, sum func(eir.FuncID) *summary.Summary) *summary.Summary {
	out := &summary.Summary{}
	var escWhy, storeWhy []summary.Reason
	for _, id := range ids {
		s := sum(id)
		if s == nil {
			continue
		}
		if s.Inputs > out.Inputs {
			out.Inputs = s.Inputs
		}
		for e, k := range s.Esc {
			out.Esc = append(out.Esc, k)
			escWhy = append(escWhy, summary.Reason{Site: ^uint32(0), Callee: uint32(id), Entry: uint32(e)})
		}
		for j, p := range s.Store {
			out.Store = append(out.Store, p)
			storeWhy = append(storeWhy, summary.Reason{Site: ^uint32(0), Callee: uint32(id), Entry: uint32(len(s.Esc) + j)})
		}
		out.HeapStore = append(out.HeapStore, s.HeapStore...)
		for len(out.Res) < len(s.Res) {
			out.Res = append(out.Res, summary.Result{})
		}
		for r, res := range s.Res {
			out.Res[r].From = append(out.Res[r].From, res.From...)
			out.Res[r].Heap = appendPaths(out.Res[r].Heap, res.Heap)
			out.Res[r].CountedHeap = appendPaths(out.Res[r].CountedHeap, res.CountedHeap)
		}
		out.Region.Builds = out.Region.Builds || s.Region.Builds
		out.Region.OuterResults = appendInts(out.Region.OuterResults, s.Region.OuterResults)
	}
	out.Reasons = append(escWhy, storeWhy...)
	summary.Widen(ps, out)
	return out
}

func appendPaths(a, b []domain.PathID) []domain.PathID {
	for _, p := range b {
		found := false
		for _, q := range a {
			if p == q {
				found = true
				break
			}
		}
		if !found {
			a = append(a, p)
		}
	}
	return a
}

func appendInts(a, b []int) []int {
	for _, x := range b {
		found := false
		for _, y := range a {
			if x == y {
				found = true
				break
			}
		}
		if !found {
			a = append(a, x)
		}
	}
	return a
}
