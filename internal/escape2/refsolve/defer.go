package refsolve

import (
	"fmt"
	"slices"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// Deferred calls (decision D49; rules.go, "Deferred calls"), as the solver
// has them (solve/defer.go): one dcall per (call of this function, origin
// call), whose function value and arguments gather what every resolution
// at that call passes.

type dcall struct {
	in       *eir.Instr
	origin   eir.FuncID
	site     eir.SiteID
	target   value
	args     []value
	fellBack bool
	argsBad  bool
	keys     map[domain.Key]bool
	noDefer  bool // past solve.MaxDeferredOrigins: resolved here (capDeferred)
	fb       bool // made inside a fallback: resolved here, never deferred nor counted (solve's dcall.fb)
}

// ownDeferrable reports whether call in of the function's own code is a
// dcall: a call of a function value whose results hold no pointers.
func ownDeferrable(in *eir.Instr) bool {
	c := in.Call
	if c.Kind != eir.CallFunc || c.Target == 0 {
		return false
	}
	for _, r := range c.Results {
		if r != 0 {
			return false
		}
	}
	return true
}

// newDcall adds target and args to the dcall of call in for the origin call.
func (s *solver) newDcall(in *eir.Instr, target value, args []value, origin eir.FuncID, site eir.SiteID) {
	fb := s.fbDepth > 0
	k := fmt.Sprintf("%p %d %d %v", in, origin, site, fb)
	dc := s.dcallIdx[k]
	if dc == nil {
		dc = &dcall{in: in, origin: origin, site: site, target: value{}, keys: map[domain.Key]bool{}, fb: fb, noDefer: fb}
		s.dcallIdx[k] = dc
		s.dcalls = append(s.dcalls, dc)
		s.changed = true
	}
	for len(dc.args) < len(args) {
		dc.args = append(dc.args, value{})
		s.changed = true
	}
	if s.addCovered(dc.target, target) {
		s.changed = true
	}
	for j, a := range args {
		if s.addCovered(dc.args[j], a) {
			s.changed = true
		}
	}
}

// ownDcall is the dcall of call in of the function's own code.
func (s *solver) ownDcall(in *eir.Instr) {
	c := in.Call
	args := make([]value, len(c.Args))
	for i, a := range c.Args {
		args[i] = s.val(a)
	}
	s.newDcall(in, s.val(c.Target), args, s.f.ID, c.Site)
}

// deferredOf makes the dcalls of sm's Deferred entries, applied at in with
// binding bind.
func (s *solver) deferredOf(in *eir.Instr, sm *summary.Summary, bind func(int) value, skip int) {
	for _, d := range sm.Deferred {
		if skip >= 0 && deferredOnly(d, skip) {
			continue
		}
		args := make([]value, len(d.Args))
		for j, a := range d.Args {
			v := value{}
			for _, k := range a.Keys {
				v.addAll(s.valueOfKey(k, bind))
			}
			if a.Heap {
				v.add(ptr(tok{kind: kHeap}))
			}
			args[j] = v
		}
		s.newDcall(in, s.valueOfKey(d.Fn, bind), args, d.Origin, d.Site)
	}
}

// resolve resolves every dcall against what its function value holds.
func (s *solver) resolve() {
	for i := 0; i < len(s.dcalls); i++ {
		dc := s.dcalls[i]
		argsBad := false
		for _, a := range dc.args {
			for x := range a {
				if x.t.kind == kObj {
					argsBad = true
				}
			}
		}
		for x := range dc.target {
			switch t := x.t; {
			case t.kind == kObj && !t.ge && t.path == domain.Root && s.closureOf[t.id] != 0:
				s.applyClosure(dc, s.closureOf[t.id]-1, x)
			case t.kind == kObj && s.objs[t.id].isReg:
				// A closure a callee returned (solve/regionlits.go).
				lits, ok := s.regionLits[t.id]
				if !ok {
					s.fallback(dc)
					break
				}
				for _, fn := range lits {
					if fn == eir.NoFunc || int(fn) >= len(s.env.Program().Funcs) {
						s.fallback(dc)
						continue
					}
					s.applyClosure(dc, fn, ptr(t))
				}
			case t.kind == kIn && s.f.External:
				s.fallback(dc)
			case t.kind == kIn:
				if k := keyOf(t); !dc.keys[k] {
					dc.keys[k] = true
					s.changed = true
				}
				if argsBad || dc.noDefer {
					s.fallback(dc)
				}
			default:
				s.fallback(dc)
			}
		}
		if argsBad != dc.argsBad {
			dc.argsBad = argsBad
			s.changed = true
		}
	}
	s.capDeferred()
}

// capDeferred marks the dcalls whose origin calls rank past
// solve.MaxDeferredOrigins among those with Deferred keys of one input
// (solve/defer.go): they resolve here.
func (s *solver) capDeferred() {
	groups := map[int][]uint64{}
	for _, dc := range s.dcalls {
		if dc.fb {
			continue
		}
		g := uint64(dc.origin)<<32 | uint64(uint32(dc.site))
		for k := range dc.keys {
			in := k.Input()
			if !slices.Contains(groups[in], g) {
				groups[in] = append(groups[in], g)
			}
		}
	}
	for in, gs := range groups {
		slices.Sort(gs)
		if len(gs) <= solve.MaxDeferredOrigins {
			continue
		}
		past := map[uint64]bool{}
		for _, g := range gs[solve.MaxDeferredOrigins:] {
			past[g] = true
		}
		for _, dc := range s.dcalls {
			if dc.noDefer || !past[uint64(dc.origin)<<32|uint64(uint32(dc.site))] {
				continue
			}
			for k := range dc.keys {
				if k.Input() == in {
					dc.noDefer = true
					s.changed = true
					s.fallback(dc)
					break
				}
			}
		}
	}
}

// keep applies the default of a bodyless callee at dcall dc, with its
// arguments and its function value as the inputs (solve/defer.go).
func (s *solver) keep(dc *dcall) {
	n := len(dc.args) + 1
	s.applySummary(dc.in, bodylessSummary(&eir.Func{Inputs: make([]eir.Input, n)}), bindArgs(dc, n, dc.target), eir.NoFunc)
}

// bindArgs is the binding of a candidate with n inputs at dcall dc: input i
// is argument i, and the last the function value's record when the
// candidate has one input more than the arguments.
func bindArgs(dc *dcall, n int, record value) func(int) value {
	return func(i int) value {
		if i < len(dc.args) {
			return dc.args[i]
		}
		if i == n-1 && n == len(dc.args)+1 {
			return record
		}
		return value{}
	}
}

// applyClosure applies the summary of closure fn, whose record x this
// function made, at dcall dc.
func (s *solver) applyClosure(dc *dcall, fn eir.FuncID, x tv) {
	if !s.env.CanRead(s.f, fn) {
		s.fallback(dc)
		return
	}
	// The dcalls of one call that resolve to the same record join their
	// arguments in one application (solve/defer.go).
	f := s.env.Program().Funcs[fn]
	n := len(f.Inputs)
	if n > 0 && n == len(dc.args)+1 {
		n--
	}
	args := make([]value, n)
	for j := range args {
		args[j] = value{}
	}
	for _, d := range s.dcalls {
		if d.in != dc.in {
			continue
		}
		has := false
		for y := range d.target {
			if y.t == x.t {
				has = true
				break
			}
		}
		if !has {
			continue
		}
		for j, a := range d.args {
			if j < n {
				args[j].addAll(a)
			}
		}
	}
	joined := &dcall{in: dc.in, args: args}
	s.applyBound(dc.in, fn, bindArgs(joined, len(f.Inputs), single(ptr(x.t))), -1)
}

// fallback applies the per-signature join of the origin call's candidates
// at dcall dc.
func (s *solver) fallback(dc *dcall) {
	if !dc.fellBack {
		dc.fellBack = true
		s.changed = true
	}
	funcs := s.env.Program().Funcs
	origin := s.f // the origin call by its site (solve/defer.go)
	if dc.origin != s.f.ID {
		origin = funcs[dc.origin]
	}
	call := siteCall(origin, dc.site)
	cands := s.env.Candidates(origin, call)
	for _, cand := range cands {
		if cand != eir.NoFunc && !s.env.CanRead(s.f, cand) {
			s.keep(dc)
			return
		}
	}
	for _, cand := range cands {
		s.groupFallback(dc.in, cand, false)
		if s.f.External || dc.noDefer {
			s.groupFallback(dc.in, cand, true)
		}
	}
}

// groupFallback applies the fallback group of cand at call in (solve's
// joinFallback): the union of what the dcalls of that call that fell back
// to cand pass (inFull: those that make no Deferred entry).
func (s *solver) groupFallback(in *eir.Instr, cand eir.FuncID, inFull bool) {
	funcs := s.env.Program().Funcs
	var members []*dcall
	n := -1
	for _, d := range s.dcalls {
		if d.in != in || !d.fellBack {
			continue
		}
		if inFull && !s.f.External && !d.noDefer {
			continue
		}
		origin := s.f
		if d.origin != s.f.ID {
			origin = funcs[d.origin]
		}
		cands := s.env.Candidates(origin, siteCall(origin, d.site))
		if !slices.Contains(cands, cand) || slices.ContainsFunc(cands, func(c eir.FuncID) bool {
			return c != eir.NoFunc && !s.env.CanRead(s.f, c)
		}) {
			continue // kept instead (fallback)
		}
		members = append(members, d)
		if n < 0 {
			n = len(d.args)
			if int(cand) < len(funcs) {
				n = len(funcs[cand].Inputs)
			} else if sm := s.env.Summary(cand); sm != nil {
				n = sm.Inputs
			}
		}
	}
	if len(members) == 0 {
		return
	}
	record := n == len(members[0].args)+1 && n > 0
	if inFull && !record {
		return
	}
	na := n
	if record {
		na--
	}
	args := make([]value, na)
	for j := range args {
		args[j] = value{}
	}
	ins, other := value{}, value{}
	for _, d := range members {
		for j, a := range d.args {
			if j < na {
				args[j].addAll(a)
			}
		}
		for x := range s.rest(d.target) {
			if x.t.kind == kIn {
				ins.add(x)
			} else {
				other.add(x)
			}
		}
	}
	joined := &dcall{in: in, args: args}
	s.fbDepth++
	defer func() { s.fbDepth-- }()
	switch {
	case !record:
		s.applyBound(in, cand, bindArgs(joined, n, value{}), -1)
	case inFull:
		s.applyBound(in, cand, bindArgs(joined, n, ins), -1)
	default:
		s.applyBound(in, cand, bindArgs(joined, n, other), -1)
		s.applyBound(in, cand, bindArgs(joined, n, ins), n-1)
	}
}

// siteCall is the call at site of function f.
func siteCall(f *eir.Func, site eir.SiteID) *eir.Call {
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			if c := b.Instrs[i].Call; c != nil && c.Site == site {
				return c
			}
		}
	}
	panic("internal compiler error: escape2/refsolve: a deferred call's origin has no such call site")
}

// applyBound applies the summary of cand at in with binding bind: nil is
// the bottom summary, except for a bodyless function.
func (s *solver) applyBound(in *eir.Instr, cand eir.FuncID, bind func(int) value, skip int) {
	funcs := s.env.Program().Funcs
	var sm *summary.Summary
	switch {
	case cand == eir.NoFunc:
		sm = bodylessSummary(&eir.Func{Inputs: make([]eir.Input, len(in.Call.Args)), Results: len(in.Call.Results), Bodyless: true})
	case int(cand) < len(funcs) && funcs[cand].Bodyless && s.env.Summary(cand) == nil:
		sm = bodylessSummary(funcs[cand])
	default:
		sm = s.env.Summary(cand)
	}
	if sm == nil {
		return
	}
	s.applySummarySkip(in, sm, bind, cand, skip)
}

// deferredOnly reports whether Deferred entry d names input i alone.
func deferredOnly(d summary.Deferred, i int) bool {
	if d.Fn.Input() != i {
		return false
	}
	for _, a := range d.Args {
		for _, k := range a.Keys {
			if k.Input() != i {
				return false
			}
		}
	}
	return true
}

// deferredOut is the Deferred entries of the summary (solve/defer.go).
func (s *solver) deferredOut() []summary.Deferred {
	var out []summary.Deferred
	for _, dc := range s.dcalls {
		if len(dc.keys) == 0 || dc.noDefer {
			continue
		}
		args := make([]summary.DeferredArg, len(dc.args))
		for j, a := range dc.args {
			if dc.argsBad {
				continue // resolved here with its arguments (fallback)
			}
			for x := range a {
				switch x.t.kind {
				case kIn:
					k := keyOf(x.t)
					if (x.tag != domain.Root || x.g) && !k.Deep() {
						k = k.WithPath(k.Path(), true) // anywhere in the value (solve/defer.go, argKey)
					}
					args[j].Keys = append(args[j].Keys, k)
				case kHeap, kCHeap:
					args[j].Heap = true
				}
			}
		}
		for k := range dc.keys {
			out = append(out, summary.Deferred{Fn: k, Args: args, Origin: dc.origin, Site: dc.site})
		}
	}
	return out
}
