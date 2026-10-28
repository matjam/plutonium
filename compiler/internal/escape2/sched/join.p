// Translated by go2pu from internal/escape2/sched/join.go at the go-cutover tag; edited by hand since.

package sched

import (
	"compiler/compat"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/solve"
	"compiler/internal/escape2/summary"
	"compiler/internal/scratch"
	"compiler/pu"
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
	iface []bool                // by virtual function: the set of a call through an interface
	field []bool                // by virtual function: the set of a call of pluto/fmt's walk (eir.Call.Fields)

	// cache holds each virtual function's join, valid until a member's
	// summary changes (dirty); memberOf lists the virtual functions each
	// function is a member of.
	cache    []*summary.Summary
	dirty    []bool
	memberOf map[eir.FuncID][]int
}

func newVirtuals(p *eir.Program, paths domain.Paths) *virtuals {
	return pu.New(virtuals{p: p, paths: paths, index: map[string]eir.FuncID{}, memberOf: map[eir.FuncID][]int{}})
}

// of returns the candidates the solver applies for a candidate set: the set
// itself, or its virtual function. A set of a call through an interface
// (iface) has a virtual function of its own: its members' receivers are the
// interface's data word, which a value method's filter needs (join); so
// does a set of a call of pluto/fmt's walk (fields, eir.Call.Fields).
func (v *virtuals) of(ids []eir.FuncID, iface, fields bool) []eir.FuncID {
	if len(ids) < 2 {
		return ids
	}
	for _, id := range ids {
		if int(id) >= len(v.p.Funcs) || v.p.Funcs[id].Bodyless {
			return ids
		}
	}
	// The key is text: the self-hosted compiler's strings hold valid
	// UTF-8, and its conversion of other bytes would make distinct keys
	// equal.
	key := compat.MakeCap[byte](0, 8*len(ids)+1)
	if iface {
		key = compat.AppendPlain(key, 'i')
	}
	if fields {
		key = compat.AppendPlain(key, 'f')
	}
	for _, id := range ids {
		key = compat.AppendInt(key, int64(id), 10)
		key = compat.AppendPlain(key, ' ')
	}
	if vid, ok := v.index[compat.String(key)]; ok {
		return compat.Clone([]eir.FuncID{vid})
	}
	vid := eir.FuncID(len(v.p.Funcs) + len(v.sets))
	v.index[compat.String(key)] = vid
	v.sets = compat.Append(v.sets, compat.AppendPlain([]eir.FuncID(nil), ids...))
	v.iface = compat.AppendPlain(v.iface, iface)
	v.field = compat.AppendPlain(v.field, fields)
	v.cache = compat.Append(v.cache, nil)
	v.dirty = compat.AppendPlain(v.dirty, true)
	for _, id := range ids {
		v.memberOf[id] = compat.AppendPlain(v.memberOf[id], len(v.sets)-1)
	}
	return compat.Clone([]eir.FuncID{vid})
}

// isVirtual reports whether id is a virtual function.
func (v *virtuals) isVirtual(id eir.FuncID) bool {
	i := int(id) - len(v.p.Funcs)
	return i >= 0 && i < len(v.sets)
}

// changed notes that any summaries may have changed: every join must be
// recomputed.
func (v *virtuals) changed() {
	for i := range v.dirty {
		v.dirty[i] = true
	}
}

// changedFunc notes that the summary of id changed: the joins it is a
// member of must be recomputed, and no others. With a virtual function per
// distinct candidate set there are thousands of joins, and recomputing all
// of them on every change dominated the analysis.
func (v *virtuals) changedFunc(id eir.FuncID) {
	for _, i := range v.memberOf[id] {
		v.dirty[i] = true
	}
}

// summary returns the join for virtual function id, given the current
// summary of each member (nil: bottom).
func (v *virtuals) summary(id eir.FuncID, sum func(eir.FuncID) *summary.Summary) *summary.Summary {
	i := int(id) - len(v.p.Funcs)
	if !v.dirty[i] && v.cache[i] != nil {
		return v.cache[i]
	}
	// The join's work is garbage once it is done: in a solve it runs in
	// the solve's scratch memory, and only the joined summary, which the
	// cache keeps past the solve, is copied to the caller's memory.
	s := join(v.p, v.paths, v.sets[i], v.iface[i], v.field[i], sum)
	r := scratch.Outer()
	s = clone(s)
	r.Restore()
	v.cache[i], v.dirty[i] = s, false
	return s
}

// join returns the join of the summaries of ids: every entry of each, the
// most inputs of any, made an antichain again by summary.Widen (whose caps
// only widen). Each Esc and Store entry keeps a reason naming the candidate
// it comes from and its index there. iface says the set is a call's through
// an interface (solve.RecvFilter); fields, that it is a call of pluto/fmt's
// walk (eir.Call.Fields), whose members of a pointer receiver take the
// address of a value the receiver holds in place too (fieldsKey).
func join(p *eir.Program, ps domain.Paths, ids []eir.FuncID, iface, fields bool, sum func(eir.FuncID) *summary.Summary) *summary.Summary {
	out := pu.New(summary.Summary{})
	var escWhy, storeWhy []summary.Reason
	for _, id := range ids {
		s := sum(id)
		if s == nil {
			continue
		}
		// A member that only a receiver of its type reaches (a method of
		// a pointer receiver, an interface adapter, through an interface
		// a value method): its entries of the receiver take that type's
		// filter at the receiver (D51, solve.RecvFilter).
		recv := func(k domain.Key) domain.Key { return k }
		if p != nil {
			fid := solve.RecvFilterID(ps.Filters(), p, id, iface)
			switch {
			case iface && solve.ValueRecvByData(p, id):
				// A value receiver given what the data word points
				// at (solve.ValueRecvByData): a load past the data
				// word, which takes the filter (loadKey).
				recv = func(k domain.Key) domain.Key {
					if k.Input() != 0 || k.HasFilter() {
						return k
					}
					return loadKey(ps, k, fid, fields)
				}
			case fid != 0:
				recv = func(k domain.Key) domain.Key {
					if k.Input() != 0 || k.HasFilter() {
						return k
					}
					return k.WithFilter(ps, fid, 0)
				}
			}
		}
		// extra is a member's entry of its receiver as the address
		// of a value the walk's value holds in place (fieldsKey).
		extra := func(k domain.Key) (domain.Key, bool) { return k, false }
		if p != nil && fields && solve.PtrRecv(p, id) != nil {
			fid := solve.HoldsFilterID(ps.Filters(), p, id)
			extra = func(k domain.Key) (domain.Key, bool) { return fieldsKey(ps, k, fid) }
		}
		if s.Inputs > out.Inputs {
			out.Inputs = s.Inputs
		}
		// The reason of an entry names the member and the entry there, and
		// carries the member's position and sink kind for it, so a message
		// can name the line without following the member.
		why := func(e int) summary.Reason {
			r := summary.Reason{Site: ^uint32(0), Callee: uint32(id), Entry: uint32(e)}
			if len(s.Reasons) == len(s.Esc)+len(s.Store) && e < len(s.Reasons) {
				r.Pos, r.Sink = s.Reasons[e].Pos, s.Reasons[e].Sink
			}
			return r
		}
		for e, k := range s.Esc {
			out.Esc = compat.AppendPlain(out.Esc, recv(k))
			escWhy = compat.Append(escWhy, why(e))
			if x, ok := extra(k); ok {
				out.Esc = compat.AppendPlain(out.Esc, x)
				escWhy = compat.Append(escWhy, why(e))
			}
		}
		for j, sp := range s.Store {
			src := sp.Src
			sp.Src = recv(src)
			out.Store = compat.Append(out.Store, sp)
			storeWhy = compat.Append(storeWhy, why(len(s.Esc)+j))
			if x, ok := extra(src); ok {
				sp.Src = x
				out.Store = compat.Append(out.Store, sp)
				storeWhy = compat.Append(storeWhy, why(len(s.Esc)+j))
			}
		}
		out.HeapStore = compat.AppendPlain(out.HeapStore, s.HeapStore...)
		for len(out.Res) < len(s.Res) {
			out.Res = compat.Append(out.Res, summary.Result{})
		}
		for r, res := range s.Res {
			for _, rk := range res.From {
				k := rk.Key
				rk.Key = recv(k)
				out.Res[r].From = compat.Append(out.Res[r].From, rk)
				if x, ok := extra(k); ok {
					rk.Key = x
					out.Res[r].From = compat.Append(out.Res[r].From, rk)
				}
			}
			out.Res[r].Heap = appendPaths(out.Res[r].Heap, res.Heap)
			out.Res[r].CountedHeap = appendPaths(out.Res[r].CountedHeap, res.CountedHeap)
		}
		for _, d := range s.Deferred {
			fn := d.Fn
			d.Fn = recv(fn)
			if len(d.Args) > 0 {
				args := compat.Make[summary.DeferredArg](len(d.Args))
				for i, a := range d.Args {
					args[i] = summary.DeferredArg{Heap: a.Heap, Keys: compat.Make[domain.Key](len(a.Keys))}
					for j, k := range a.Keys {
						args[i].Keys[j] = recv(k)
					}
					for _, k := range a.Keys {
						if x, ok := extra(k); ok {
							args[i].Keys = compat.AppendPlain(args[i].Keys, x)
						}
					}
				}
				d.Args = args
			}
			out.Deferred = compat.Append(out.Deferred, d)
			if x, ok := extra(fn); ok {
				d.Fn = x
				out.Deferred = compat.Append(out.Deferred, d)
			}
		}
		out.Region.Builds = out.Region.Builds || s.Region.Builds
		out.Region.Self = out.Region.Self || s.Region.Self
		out.Region.OuterResults = appendInts(out.Region.OuterResults, s.Region.OuterResults)
		out.Region.Lits = compat.AppendPlain(out.Region.Lits, s.Region.Lits...)
	}
	out.Reasons = compat.Append(escWhy, storeWhy...)
	summary.Widen(ps, out)
	return out
}

// fieldsKey is member key k of its receiver (input 0) as the address of a
// value that holds the receiver's type in place, which pluto/fmt's walk
// calls a method of pointer receiver on (eir.Call.Fields): any offset in a
// value that filter fid admits (solve.HoldsFilter, 0: any value), for k
// the receiver itself or any offset in it; a load at any offset, then
// anything (Deep), for a path of it with a load; what any offset reaches,
// for one of offsets alone that is GE (or Arena), as the field's place in
// the value is not known. false when k is not of the receiver, or has a
// filter of its own.
func fieldsKey(ps domain.Paths, k domain.Key, fid domain.FilterID) (domain.Key, bool) {
	if k.Input() != 0 || k.HasFilter() {
		return k, false
	}
	f := k.Flags()
	load := f.Deep && !f.GE // with GE, Deep is ignored
	for _, st := range ps.Steps(k.Path()) {
		if st == domain.StepLoad {
			load = true
		}
	}
	switch {
	case load:
		// Offsets, a load, then anything: a load at any offset, then
		// anything (Deep). The value the address points into is not
		// itself in it.
		f.GE, f.FieldsGE, f.Deep = false, false, true
	case f.GE || f.Arena:
		f.GE, f.FieldsGE, f.Deep = true, false, false
	default:
		f.GE, f.FieldsGE, f.Deep = false, true, false
	}
	f.Arena = false
	x := domain.MakeKey(0, domain.Root, f)
	if fid != 0 {
		x = x.WithFilter(ps, fid, 0)
	}
	return x, true
}

// loadKey is key k of a value receiver (input 0) as a key of the data word
// that points at the value (solve.ValueRecvByData): a load, then k's path,
// with filter fid at the data word. For pluto/fmt's walk (fields), which
// gives the method the address of a value of its type anywhere in the
// walked value, it is what a load at any offset reaches (Deep): the place
// of k's path in the walked value is not known.
func loadKey(ps domain.Paths, k domain.Key, fid domain.FilterID, fields bool) domain.Key {
	f := k.Flags()
	var x domain.Key
	if fields {
		f.GE, f.FieldsGE, f.Deep, f.Arena = false, false, true, false
		x = domain.MakeKey(0, domain.Root, f)
	} else {
		q, cut := ps.Extend(domain.Root, domain.StepLoad)
		for _, st := range ps.Steps(k.Path()) {
			if cut {
				break
			}
			q, cut = ps.Extend(q, st)
		}
		if cut {
			f.GE, f.FieldsGE, f.Deep = true, false, false
		}
		x = domain.MakeKey(0, q, f)
	}
	return x.WithFilter(ps, fid, 0)
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
			a = compat.AppendPlain(a, p)
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
			a = compat.AppendPlain(a, x)
		}
	}
	return a
}
