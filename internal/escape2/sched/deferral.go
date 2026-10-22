package sched

import (
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/types2"
)

// Deferred calls of func-value inputs (decision D49). A call in f of a func
// value that f was given (an input) is not resolved in f: f's summary keeps
// it as a deferred entry, and each caller resolves it against the closures
// it actually passes there (re-deferring it, one level up, when it passes
// an input of its own). Only where what a caller passes is not known does
// the per-signature join (the candidates of the original call) apply.
//
// For the scheduler this is what splits the SCCs that func-value calls tie
// together: f no longer depends on the candidates of a deferred call;
// instead each caller depends on what it passes in that position.
//
// Which calls are deferred is decided here, from the EIR alone, and the
// solver defers exactly those (Env.Deferred): the call graph and the solver
// must agree on which summaries a solve reads, and a solve that read one
// the graph had not ordered first would be unsound (the env refuses it).
//
// A call c in f is deferred when:
//   - it is a call of a func value (CallFunc) whose target is one input of f
//     of func type, through copies and phis only;
//   - every argument is derived from f's inputs (through copies, phis,
//     loads, fields and elements of them), from heap or static memory, or is
//     a constant;
//   - no result may hold a pointer.
// The solver applies a conservative default where, at run time, a deferred
// call's target or arguments still hold something else (a frame pointer
// stored through an input and read back): it reads no summary there.

// deferral is the scheduler's view of D49.
type deferral struct {
	p  *eir.Program
	c  *cands
	fl *funcFlow

	memo map[*eir.Call]bool
	// inputs[f][i]: the deferred calls (origin function and call) a call of
	// f defers through its input i, directly or by passing i on to a
	// callee that defers it.
	inputs []map[int][]origin
	// litMade[f]: the literals whose closures f makes.
	litMade []map[eir.FuncID]bool
}

type origin struct {
	f    eir.FuncID
	call *eir.Call
}

func newDeferral(p *eir.Program, c *cands, fl *funcFlow) *deferral {
	d := &deferral{p: p, c: c, fl: fl, memo: map[*eir.Call]bool{},
		inputs: make([]map[int][]origin, len(p.Funcs)), litMade: make([]map[eir.FuncID]bool, len(p.Funcs))}
	for _, f := range p.Funcs {
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				in := &b.Instrs[i]
				if in.Op == eir.OpClosure && in.Callee != eir.NoFunc {
					if d.litMade[f.ID] == nil {
						d.litMade[f.ID] = map[eir.FuncID]bool{}
					}
					d.litMade[f.ID][in.Callee] = true
				}
			}
		}
	}
	d.propagate()
	return d
}

// deferred reports whether call c in f is deferred (the predicate above).
func (d *deferral) deferred(f *eir.Func, c *eir.Call) bool {
	if c == nil || c.Kind != eir.CallFunc {
		return false
	}
	if v, ok := d.memo[c]; ok {
		return v
	}
	v := d.decide(f, c)
	d.memo[c] = v
	return v
}

func (d *deferral) decide(f *eir.Func, c *eir.Call) bool {
	defs := d.c.defsOf(f)
	in, ok := singleInput(defs, c.Target)
	if !ok || in >= len(f.Inputs) || sigOfType(f.Inputs[in].Type) == nil {
		return false
	}
	for _, a := range c.Args {
		if !inputDerived(defs, a, 0) {
			return false
		}
	}
	if c.Sig != nil {
		for i := 0; i < c.Sig.Results().Len(); i++ {
			if mayHoldPointers(c.Sig.Results().At(i).Type()) {
				return false
			}
		}
	}
	return true
}

func sigOfType(t types2.Type) *types2.Signature {
	if t == nil {
		return nil
	}
	s, _ := t.Underlying().(*types2.Signature)
	return s
}

// singleInput reports whether register r is one input of the function,
// through copies and phis only, and which.
func singleInput(defs []*eir.Instr, r eir.Reg) (int, bool) {
	which := -1
	seen := map[eir.Reg]bool{}
	var walk func(r eir.Reg) bool
	walk = func(r eir.Reg) bool {
		if seen[r] {
			return true
		}
		seen[r] = true
		if r == 0 || int(r) >= len(defs) || defs[r] == nil {
			return false
		}
		in := defs[r]
		switch in.Op {
		case eir.OpInput:
			if which >= 0 && which != in.Index {
				return false
			}
			which = in.Index
			return true
		case eir.OpCopy:
			return walk(in.X)
		case eir.OpPhi:
			for _, a := range in.Args {
				if !walk(a) {
					return false
				}
			}
			return true
		}
		return false
	}
	if !walk(r) || which < 0 {
		return 0, false
	}
	return which, true
}

// inputDerived reports whether register r is derived from the function's
// inputs (through copies, phis, loads, fields and elements of them), from
// heap or static memory, or is a constant (register 0).
func inputDerived(defs []*eir.Instr, r eir.Reg, depth int) bool {
	if r == 0 {
		return true
	}
	if depth > 32 || int(r) >= len(defs) || defs[r] == nil {
		return false
	}
	in := defs[r]
	switch in.Op {
	case eir.OpInput, eir.OpHeap:
		return true
	case eir.OpCopy, eir.OpLoad, eir.OpField, eir.OpElem:
		return inputDerived(defs, in.X, depth+1)
	case eir.OpPhi:
		for _, a := range in.Args {
			if !inputDerived(defs, a, depth+1) {
				return false
			}
		}
		return true
	}
	return false
}

// propagate computes inputs: the deferred calls each input of each
// function stands for, through calls that pass an input on.
func (d *deferral) propagate() {
	for _, f := range d.p.Funcs {
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				in := &b.Instrs[i]
				if in.Op == eir.OpCall && d.deferred(f, in.Call) {
					k, _ := singleInput(d.c.defsOf(f), in.Call.Target)
					d.add(f.ID, k, []origin{{f.ID, in.Call}})
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, f := range d.p.Funcs {
			defs := d.c.defsOf(f)
			for _, b := range f.Blocks {
				for i := range b.Instrs {
					in := &b.Instrs[i]
					if in.Op != eir.OpCall {
						continue
					}
					d.eachPassed(f, in.Call, func(g eir.FuncID, j int, a eir.Reg, os []origin) {
						if k, ok := singleInput(defs, a); ok && sigOfType(f.Inputs[k].Type) != nil {
							if d.add(f.ID, k, os) {
								changed = true
							}
						}
					})
				}
			}
		}
	}
}

func (d *deferral) add(f eir.FuncID, k int, os []origin) bool {
	if d.inputs[f] == nil {
		d.inputs[f] = map[int][]origin{}
	}
	grew := false
	for _, o := range os {
		dup := false
		for _, x := range d.inputs[f][k] {
			if x == o {
				dup = true
				break
			}
		}
		if !dup {
			d.inputs[f][k] = append(d.inputs[f][k], o)
			grew = true
		}
	}
	return grew
}

// eachPassed calls fn for each argument of call c (in f) that a callee g
// defers through its input j, with the deferred calls it stands for. The
// arguments are bound as the solver binds them (solve.binding).
func (d *deferral) eachPassed(f *eir.Func, c *eir.Call, fn func(g eir.FuncID, j int, a eir.Reg, os []origin)) {
	var callees []eir.FuncID
	switch c.Kind {
	case eir.CallStatic:
		callees = []eir.FuncID{c.Callee}
	case eir.CallIface, eir.CallTypeParam:
		callees = d.c.of(f, c)
	case eir.CallFunc:
		if d.deferred(f, c) {
			return // resolved by f's callers
		}
		callees = d.c.of(f, c)
	default:
		return
	}
	for _, g := range callees {
		if g == eir.NoFunc || int(g) >= len(d.p.Funcs) || d.inputs[g] == nil {
			continue
		}
		gf := d.p.Funcs[g]
		n := len(gf.Inputs)
		bind := make([]eir.Reg, n)
		k := 0
		if (c.Kind == eir.CallIface || c.Kind == eir.CallTypeParam) && c.Target != 0 && n > 0 {
			bind[0] = c.Target
			k = 1
		}
		for _, a := range c.Args {
			if k < n {
				bind[k] = a
			}
			k++
		}
		if c.Kind == eir.CallFunc && n == len(c.Args)+1 {
			bind[n-1] = c.Target
		}
		for j, os := range d.inputs[g] {
			if j < n && bind[j] != 0 {
				fn(g, j, bind[j], os)
			}
		}
	}
}

// deps returns the functions f's solve reads to resolve the deferred
// entries of its callees: for each argument passed where a callee defers,
// the literals f makes that flow there, and, where anything else may flow
// there, the candidates of the deferred calls (the fallback). An argument
// that is one of f's inputs is re-deferred: nothing is read.
func (d *deferral) deps(f *eir.Func) []eir.FuncID {
	defs := d.c.defsOf(f)
	set := map[eir.FuncID]bool{}
	var ev *flowEval
	if d.fl != nil {
		ev = newFlowEval(d.fl, f)
	}
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			in := &b.Instrs[i]
			if in.Op != eir.OpCall {
				continue
			}
			d.eachPassed(f, in.Call, func(g eir.FuncID, j int, a eir.Reg, os []origin) {
				if k, ok := singleInput(defs, a); ok && sigOfType(f.Inputs[k].Type) != nil {
					return // re-deferred
				}
				onlyOwn := false
				if ev != nil {
					s := ev.set(a)
					if s != nil && !s.top {
						onlyOwn = true
						for id := range s.ids {
							set[id] = true
							if !d.litMade[f.ID][id] {
								onlyOwn = false
							}
						}
					}
				}
				if !onlyOwn {
					for _, o := range os {
						for _, id := range d.c.of(d.p.Funcs[o.f], o.call) {
							set[id] = true
						}
					}
				}
			})
		}
	}
	return setIDs(set)
}
