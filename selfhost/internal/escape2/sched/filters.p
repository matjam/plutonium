// Translated by go2pu from internal/escape2/sched/filters.go at the go-cutover tag; edited by hand since.

package sched

import (
	"selfhost/compat"
	"selfhost/internal/escape2/domain"
	"selfhost/internal/escape2/eir"
	"selfhost/internal/escape2/solve"
	"selfhost/internal/escape2/summary"
	"selfhost/internal/syntax/token"
)

// Type filters (D51) are interned as solves meet them, and parallel solves
// meet them in an order their timing decides; so do the joins. The order
// changes no set a summary stands for, only the IDs its keys name and so
// the order of its sorted entries. canonicalFilters renumbers them when
// the analysis is over (domain.Filters.Canonical) and renames every
// summary's keys, and the entries the findings and reasons name, so that
// the results do not depend on the number of workers or their timing.
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
			old = compat.Append(old, entry{s.Esc[i], 0})
		}
		for i := range s.Store {
			s.Store[i].Src, s.Store[i].Dst = key(s.Store[i].Src), key(s.Store[i].Dst)
			old = compat.Append(old, entry{s.Store[i].Src, s.Store[i].Dst})
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
		ix := compat.Make[int](len(old))
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
	// A reason names an entry of its callee's summary, which the renaming
	// may have moved as well.
	for s := range done {
		for i := range s.Reasons {
			r := &s.Reasons[i]
			if r.Callee == ^uint32(0) || r.Entry == ^uint32(0) {
				continue
			}
			if ix, ok := newIndex[eir.FuncID(r.Callee)]; ok && int(r.Entry) < len(ix) {
				r.Entry = uint32(ix[r.Entry])
			}
		}
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

// dedupFindings keeps one of a function's findings per (code, object,
// position): the one through the least (site, callee, entry). A solve
// records one per callee entry (solve, report), as which entry it meets
// first depends on the filters' IDs when it ran; once canonicalFilters has
// renumbered them, the least entry is the same whatever the timing.
func dedupFindings(fs []solve.Finding) []solve.Finding {
	type key struct {
		code string
		pos  token.Pos
		obj  eir.ObjID
		site eir.SiteID // a region's call, when obj is eir.NoObj
	}
	at := map[key]int{}
	out := fs[:0]
	for _, f := range fs {
		k := key{string(f.Code), f.Pos, f.Obj, ^eir.SiteID(0)}
		if f.Obj == eir.NoObj {
			k.site = f.Site
		}
		i, ok := at[k]
		if !ok {
			at[k] = len(out)
			out = compat.Append(out, f)
			continue
		}
		g := out[i]
		if f.Site < g.Site || f.Site == g.Site && (f.Callee < g.Callee || f.Callee == g.Callee && f.Entry < g.Entry) {
			out[i] = f
		}
	}
	return out
}
