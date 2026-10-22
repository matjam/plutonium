package refsolve

import (
	"fmt"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
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
	k := fmt.Sprintf("%p %d %d", in, origin, site)
	dc := s.dcallIdx[k]
	if dc == nil {
		dc = &dcall{in: in, origin: origin, site: site, target: value{}, keys: map[domain.Key]bool{}}
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
func (s *solver) deferredOf(in *eir.Instr, sm *summary.Summary, bind func(int) value) {
	for _, d := range sm.Deferred {
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
				if x.t.kind == kObj || x.tag != domain.Root || x.g {
					argsBad = true
				}
			}
		}
		for x := range dc.target {
			switch t := x.t; {
			case t.kind == kObj && !t.ge && t.path == domain.Root && s.closureOf[t.id] != 0:
				s.applyClosure(dc, s.closureOf[t.id]-1, x)
			case t.kind == kIn && (argsBad || s.f.External):
				s.fallback(dc)
			case t.kind == kIn:
				if k := keyOf(t); !dc.keys[k] {
					dc.keys[k] = true
					s.changed = true
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
	f := s.env.Program().Funcs[fn]
	s.applyBound(dc.in, fn, bindArgs(dc, len(f.Inputs), single(x)))
}

// fallback applies the per-signature join of the origin call's candidates
// at dcall dc.
func (s *solver) fallback(dc *dcall) {
	if !dc.fellBack {
		dc.fellBack = true
		s.changed = true
	}
	if dc.origin == s.f.ID {
		// A deferred call of the function's own reads no candidate.
		s.keep(dc)
		return
	}
	funcs := s.env.Program().Funcs
	origin, call := s.f, dc.in.Call
	if dc.origin != s.f.ID {
		origin = funcs[dc.origin]
		call = siteCall(origin, dc.site)
	}
	cands := s.env.Candidates(origin, call)
	for _, cand := range cands {
		if cand != eir.NoFunc && !s.env.CanRead(s.f, cand) {
			s.keep(dc)
			return
		}
	}
	for _, cand := range cands {
		n := len(dc.args)
		if int(cand) < len(funcs) {
			n = len(funcs[cand].Inputs)
		} else if sm := s.env.Summary(cand); sm != nil {
			n = sm.Inputs
		}
		s.applyBound(dc.in, cand, bindArgs(dc, n, dc.target))
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
func (s *solver) applyBound(in *eir.Instr, cand eir.FuncID, bind func(int) value) {
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
	s.applySummary(in, sm, bind, cand)
}

// deferredOut is the Deferred entries of the summary (solve/defer.go).
func (s *solver) deferredOut() []summary.Deferred {
	var out []summary.Deferred
	for _, dc := range s.dcalls {
		if dc.argsBad || len(dc.keys) == 0 {
			continue
		}
		args := make([]summary.DeferredArg, len(dc.args))
		for j, a := range dc.args {
			for x := range a {
				switch x.t.kind {
				case kIn:
					args[j].Keys = append(args[j].Keys, keyOf(x.t))
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
