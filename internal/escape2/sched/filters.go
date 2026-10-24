package sched

import (
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// Type filters (D51) are interned as solves meet them, and parallel solves
// meet them in an order their timing decides; so do the joins. The order
// changes no set a summary stands for, only the IDs its keys name and so
// the order of its sorted entries. canonicalFilters renumbers them when
// the analysis is over (domain.Filters.Canonical) and renames every
// summary's keys, and the entries the findings name, so that the results
// do not depend on the number of workers or their timing.
func canonicalFilters(env *env, findings [][]solve.Finding) {
	rename := env.paths.Filters().Canonical()
	if rename == nil {
		return
	}
	key := func(k domain.Key) domain.Key {
		if id, pos := k.Filter(); id != 0 {
			return k.WithFilterRaw(rename[id], pos)
		}
		return k
	}
	// newIndex maps, for each summary renamed, an entry's old index to its
	// new one.
	newIndex := map[eir.FuncID][]int{}
	done := map[*summary.Summary]bool{}
	do := func(id eir.FuncID, s *summary.Summary) {
		if s == nil || done[s] {
			return
		}
		done[s] = true
		type entry struct{ a, b domain.Key }
		var old []entry
		for i := range s.Esc {
			s.Esc[i] = key(s.Esc[i])
			old = append(old, entry{s.Esc[i], 0})
		}
		for i := range s.Store {
			s.Store[i].Src, s.Store[i].Dst = key(s.Store[i].Src), key(s.Store[i].Dst)
			old = append(old, entry{s.Store[i].Src, s.Store[i].Dst})
		}
		for i := range s.HeapStore {
			s.HeapStore[i] = key(s.HeapStore[i])
		}
		for r := range s.Res {
			for i := range s.Res[r].From {
				s.Res[r].From[i].Key = key(s.Res[r].From[i].Key)
			}
		}
		for i := range s.Deferred {
			d := &s.Deferred[i]
			d.Fn = key(d.Fn)
			for j := range d.Args {
				for k := range d.Args[j].Keys {
					d.Args[j].Keys[k] = key(d.Args[j].Keys[k])
				}
			}
		}
		summary.Widen(env.paths, s)
		at := map[entry]int{}
		for i, k := range s.Esc {
			at[entry{k, 0}] = i
		}
		for i, p := range s.Store {
			at[entry{p.Src, p.Dst}] = len(s.Esc) + i
		}
		ix := make([]int, len(old))
		for i, e := range old {
			if j, ok := at[e]; ok {
				ix[i] = j
			} else {
				ix[i] = i // (Widen of a widened summary keeps every entry)
			}
		}
		newIndex[id] = ix
	}
	for id, s := range env.sums {
		do(eir.FuncID(id), s)
	}
	for i, s := range env.virt.cache {
		do(eir.FuncID(len(env.p.Funcs)+i), s)
	}
	for _, fs := range findings {
		for i := range fs {
			f := &fs[i]
			if ix, ok := newIndex[f.Callee]; ok && f.Entry >= 0 && f.Entry < len(ix) {
				f.Entry = ix[f.Entry]
			}
		}
	}
}
