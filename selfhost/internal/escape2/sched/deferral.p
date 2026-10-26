// Translated by go2pu from internal/escape2/sched/deferral.go at the go-cutover tag; edited by hand since.

package sched

import (
	"selfhost/compat"
	"selfhost/internal/escape2/eir"
	"selfhost/pu"
)

// Deferred calls of func-value inputs (decision D49). A call in f of a func
// value that f was given (reached from one of its inputs) is not resolved
// in f: f's summary keeps it as a deferred entry, and each caller resolves
// it against what it actually passes there, re-deferring it, one level up,
// where that is again something reached from one of its own inputs. Only
// where what a caller passes is not known does the per-signature join (the
// candidates of the original call) apply.
//
// For the scheduler, each caller also depends on what it passes in that
// position. f keeps its edges to the deferred call's candidates (graph.go):
// where the call's arguments turn out to point into f's frame when the
// solver runs, f resolves it against them. What D49 buys is precision, not
// smaller SCCs: on the translated compiler the largest SCC is the type
// checker's own recursion.
//
// Which calls are deferred is decided here, from the EIR alone, and the
// solver defers exactly those (Env.Deferred). The call graph must cover
// every summary a solve reads to resolve a deferred entry: the solver asks
// Env.CanRead before each such read, and where the graph does not cover it,
// it applies the conservative default (no summary read).
//
// A call c in f is deferred when:
//   - it is a call of a func value (CallFunc) whose target is reached from
//     one input of f through copies, phis, loads, fields and elements (a
//     range body calls the yield its record captured: a load of a field of
//     its record input);
//   - no argument visibly points into f's frame (framePtrArg): the address
//     of a frame object, a closure with a record in the frame (a range body
//     passed to the iterator f was given), or what f stored into its own
//     frame objects;
//   - no result may hold a pointer.
//
// Other arguments are not checked here: the solver checks what they hold
// when it runs (a frame pointer of f's makes the call take the
// conservative default, in f, with no summary read). Checking every
// argument statically would also stop the calls whose arguments are call
// results or loads that hold nothing but f's inputs and heap memory, the
// callback pattern (underIs's f(u), u := under(typ)), whose candidates'
// joins were what spread through the largest SCC. Checking none would
// defer the range bodies an iterator passes to the iterator it was given,
// which then fall to the conservative default.
//
// Where a caller f resolves a deferred entry, the value it passes there,
// below every path, is evaluated in f alone (reach):
//   - something reached from f's inputs: re-deferred, nothing is read;
//   - a closure f makes, directly or through f's own frame objects only f
//     writes, or through the captures of f's closures: its literal is read,
//     and the literal's own deferred entries are resolved in f as well (on
//     its record: against the captures of that closure; on its other
//     inputs, bound in the deferring function: as unknown);
//   - anything else is unknown: the candidates of the deferred call are
//     read (their join), and with them, as unknown in turn, the deferred
//     entries those candidates carry (fallback).

// deferral is the scheduler's view of D49.
type deferral struct {
	p *eir.Program
	c *cands

	memo map[*eir.Call]bool
	// inputs[f][i]: the deferred calls (origin function and call) a call of
	// f defers through its input i, directly or by passing what it reaches
	// from i on to a callee that defers it.
	inputs []map[int][]origin
	// argInputs[f]: the inputs of f that the arguments of a deferred call
	// f carries (its own, or one it re-defers) may be reached from.
	argInputs []map[int]bool
	// lit[L]: L is a literal, whose last input is its closure record.
	lit []bool
	// stores[o]: the stores into frame object o, by function and value.
	stores map[objKey][]storeSite
	cp     *captures
	// fallback memoizes the fallback closure of each deferred call, once
	// inputs are final.
	fallback map[*eir.Call][]eir.FuncID
	final    bool
}

type origin struct {
	f    eir.FuncID
	call *eir.Call
}

type storeSite struct {
	f eir.FuncID
	v eir.Reg
}

func newDeferral(p *eir.Program, c *cands, fl *funcFlow) *deferral {
	d := pu.New(deferral{p: p, c: c, memo: map[*eir.Call]bool{},
		inputs: compat.Make[map[int][]origin](len(p.Funcs)), argInputs: compat.Make[map[int]bool](len(p.Funcs)), lit: compat.Make[bool](len(p.Funcs)),
		stores: map[objKey][]storeSite{}, fallback: map[*eir.Call][]eir.FuncID{}})
	if fl != nil {
		d.cp = fl.cp
	} else {
		d.cp = newCaptures(&funcFlow{p: p, c: c})
	}
	for _, f := range p.Funcs {
		defs := c.defsOf(f)
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				in := &b.Instrs[i]
				switch in.Op {
				case eir.OpClosure:
					if in.Callee != eir.NoFunc && int(in.Callee) < len(p.Funcs) {
						d.lit[in.Callee] = true
					}
				case eir.OpStore:
					if objs, isPlace, _ := d.cp.placeObjs(f, defs, in.X); isPlace {
						for _, o := range objs {
							d.stores[o] = compat.Append(d.stores[o], storeSite{f.ID, in.Y})
						}
					}
				}
			}
		}
	}
	d.propagate()
	d.final = true
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
	if f.External || f.Bodyless || calledByRuntime(f) || (f.Key.Synth != nil && f.Key.Synth.Kind == eir.SynthTaskThunk) {
		// Called from where the program does not show: no caller would
		// resolve it, so it is resolved here, against its candidates.
		return false
	}
	defs := d.c.defsOf(f)
	if _, ok := oneInput(defs, c.Target); !ok {
		return false
	}
	for _, a := range c.Args {
		if framePtrArg(d, f, defs, a, map[eir.Reg]bool{}) {
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

// oneInput reports whether register r is reached from one input of the
// function, through copies, phis, loads, fields and elements, and which.
func oneInput(defs []*eir.Instr, r eir.Reg) (int, bool) {
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
		case eir.OpCopy, eir.OpLoad, eir.OpField, eir.OpElem:
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

// reach is what a register of f may hold below every path, evaluated in f
// alone: the closures f makes (their registers), the inputs it is reached
// from, and whether anything else.
type reach struct {
	closures []eir.Reg
	inputs   map[int]bool
	unknown  bool
}

type reacher struct {
	d    *deferral
	f    *eir.Func
	defs []*eir.Instr
	seen map[eir.Reg]bool
	objs map[eir.ObjID]bool
	out  reach
}

func (d *deferral) reachOf(f *eir.Func, r eir.Reg) reach {
	rc := &reacher{d: d, f: f, defs: d.c.defsOf(f), seen: map[eir.Reg]bool{}, objs: map[eir.ObjID]bool{}}
	rc.out.inputs = map[int]bool{}
	rc.reg(r)
	return rc.out
}

func (rc *reacher) reg(r eir.Reg) {
	if r == 0 || rc.seen[r] {
		return
	}
	rc.seen[r] = true
	if int(r) >= len(rc.defs) || rc.defs[r] == nil {
		rc.out.unknown = true // a call result, or nothing known
		return
	}
	in := rc.defs[r]
	switch in.Op {
	case eir.OpInput:
		rc.out.inputs[in.Index] = true
	case eir.OpCopy, eir.OpField, eir.OpElem:
		rc.reg(in.X)
	case eir.OpPhi:
		for _, a := range in.Args {
			rc.reg(a)
		}
	case eir.OpLoad:
		if o, ok := addrObj(rc.f, rc.defs, in.X); ok {
			rc.obj(o)
			return
		}
		rc.reg(in.X)
	case eir.OpAddr:
		o, ok := addrObj(rc.f, rc.defs, r)
		if !ok {
			rc.out.unknown = true
			return
		}
		rc.obj(o)
	case eir.OpClosure:
		if in.Callee == eir.NoFunc || int(in.Callee) >= len(rc.d.p.Funcs) {
			rc.out.unknown = true
			return
		}
		rc.out.closures = compat.AppendPlain(rc.out.closures, r)
		for _, a := range in.Args {
			rc.reg(a)
		}
	default:
		rc.out.unknown = true
	}
}

// obj adds what f's own frame object o may hold: what f stores into it, if
// only f stores into it and it is tracked.
func (rc *reacher) obj(o objKey) {
	if rc.objs[o.obj] {
		return
	}
	rc.objs[o.obj] = true
	if rc.d.cp.untracked[o] {
		rc.out.unknown = true
		return
	}
	for _, s := range rc.d.stores[o] {
		if s.f != rc.f.ID {
			rc.out.unknown = true
			continue
		}
		rc.reg(s.v)
	}
}

// resolution is what resolving the deferred entries of f's callees in f
// gives: the summaries read (deps) and the deferred calls f defers in turn
// through each input.
type resolution struct {
	deps    map[eir.FuncID]bool
	redefer map[int][]origin
}

// resolve resolves in f the deferred entries of its callees.
func (d *deferral) resolve(f *eir.Func) resolution {
	res := resolution{deps: map[eir.FuncID]bool{}, redefer: map[int][]origin{}}
	done := map[eir.Reg]map[*eir.Call]bool{}
	type item struct {
		os []origin
		r  eir.Reg
	}
	var work []item
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			in := &b.Instrs[i]
			if in.Op != eir.OpCall {
				continue
			}
			d.eachPassed(f, in.Call, func(_ eir.FuncID, _ int, a eir.Reg, os []origin) {
				work = compat.Append(work, item{os, a})
			})
		}
	}
	defs := d.c.defsOf(f)
	// A deferred call whose arguments, bound in f, may hold a pointer to
	// f's frame cannot be deferred further: f resolves it against its
	// candidates (the fallback).
	if d.final {
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				in := &b.Instrs[i]
				if in.Op == eir.OpCall {
					d.framePassed(f, defs, in.Call, res.deps)
				}
			}
		}
	}
	for len(work) > 0 {
		it := work[len(work)-1]
		work = work[:len(work)-1]
		// Each deferred call is resolved once per register.
		var os []origin
		for _, o := range it.os {
			if done[it.r] == nil {
				done[it.r] = map[*eir.Call]bool{}
			}
			if !done[it.r][o.call] {
				done[it.r][o.call] = true
				os = compat.Append(os, o)
			}
		}
		if len(os) == 0 {
			continue
		}
		rch := d.reachOf(f, it.r)
		for k := range rch.inputs {
			res.redefer[k] = appendOrigins(res.redefer[k], os)
		}
		if rch.unknown {
			d.unknown(os, res.deps)
		}
		for _, cr := range rch.closures {
			l := defs[cr].Callee
			res.deps[l] = true
			for j, os2 := range d.inputs[l] {
				if d.isRecord(l, j) {
					work = compat.Append(work, item{os2, cr})
				} else {
					d.unknown(os2, res.deps)
				}
			}
		}
	}
	return res
}

// framePassed adds to deps the fallback of every deferred call a callee of
// c carries when c passes it something that may point into f's frame: the
// arguments of those calls may hold it, and then f resolves them against
// their candidates instead of deferring them.
func (d *deferral) framePassed(f *eir.Func, defs []*eir.Instr, c *eir.Call, deps map[eir.FuncID]bool) {
	d.eachArgPassed(f, c, func(g eir.FuncID, a eir.Reg) {
		if mayFramePtr(d, f, defs, a, map[eir.Reg]bool{}) {
			for _, os := range d.inputs[g] {
				d.unknown(os, deps)
			}
		}
	})
}

// eachArgPassed calls fn for each argument of call c (in f) bound to an
// input of a callee g that the arguments of g's deferred calls take.
func (d *deferral) eachArgPassed(f *eir.Func, c *eir.Call, fn func(g eir.FuncID, a eir.Reg)) {
	d.eachBound(f, c, func(g eir.FuncID, bind []eir.Reg) {
		for j := range d.argInputs[g] {
			if j < len(bind) && bind[j] != 0 {
				fn(g, bind[j])
			}
		}
	})
}

// mayFramePtr reports whether register r of f may hold a pointer into f's
// frame: an address of a frame object, a closure with a record, a call's
// result, or what was stored into a frame object of f. What is loaded from
// f's inputs or from heap memory is not (it holds the callers' pointers).
func mayFramePtr(d *deferral, f *eir.Func, defs []*eir.Instr, r eir.Reg, seen map[eir.Reg]bool) bool {
	if r == 0 || seen[r] {
		return false
	}
	seen[r] = true
	if int(r) >= len(defs) || defs[r] == nil {
		return true // a call result
	}
	in := defs[r]
	switch in.Op {
	case eir.OpInput, eir.OpHeap:
		return false
	case eir.OpClosure:
		return in.Obj != eir.NoObj
	case eir.OpAddr:
		return true
	case eir.OpCopy, eir.OpField, eir.OpElem:
		return mayFramePtr(d, f, defs, in.X, seen)
	case eir.OpPhi:
		for _, a := range in.Args {
			if mayFramePtr(d, f, defs, a, seen) {
				return true
			}
		}
		return false
	case eir.OpLoad:
		if o, ok := addrObj(f, defs, in.X); ok {
			if d.cp.untracked[o] {
				return true
			}
			for _, s := range d.stores[o] {
				if s.f != f.ID || mayFramePtr(d, f, defs, s.v, seen) {
					return true
				}
			}
			return false
		}
		return mayFramePtr(d, f, defs, in.X, seen)
	}
	return true
}

// framePtrArg reports whether register r of f visibly holds a pointer into
// f's frame: an address of a frame object, a closure, method value or box
// with a record in the frame, a value built from one, or what f alone
// stored into a frame object (an untracked object counts). A call's result
// or a load from elsewhere is not looked into: the solver checks what it
// holds when it runs.
func framePtrArg(d *deferral, f *eir.Func, defs []*eir.Instr, r eir.Reg, seen map[eir.Reg]bool) bool {
	if r == 0 || seen[r] || int(r) >= len(defs) || defs[r] == nil {
		return false
	}
	seen[r] = true
	in := defs[r]
	switch in.Op {
	case eir.OpAddr:
		return true
	case eir.OpClosure, eir.OpMethodVal, eir.OpConv:
		if in.Obj != eir.NoObj {
			return true
		}
		return in.Op == eir.OpConv && framePtrArg(d, f, defs, in.X, seen)
	case eir.OpCopy, eir.OpField, eir.OpElem, eir.OpSelect:
		return framePtrArg(d, f, defs, in.X, seen)
	case eir.OpPhi, eir.OpCompose:
		for _, a := range in.Args {
			if framePtrArg(d, f, defs, a, seen) {
				return true
			}
		}
		return false
	case eir.OpLoad:
		if o, ok := addrObj(f, defs, in.X); ok {
			if d.cp.untracked[o] {
				return true
			}
			for _, s := range d.stores[o] {
				if s.f == f.ID && framePtrArg(d, f, defs, s.v, seen) {
					return true
				}
			}
			return false
		}
		return framePtrArg(d, f, defs, in.X, seen)
	}
	return false
}

// isRecord reports whether input j of l is its closure record.
func (d *deferral) isRecord(l eir.FuncID, j int) bool {
	lf := d.p.Funcs[l]
	return d.lit[l] && j == len(lf.Inputs)-1 && lf.Inputs[j].Var == nil
}

// unknown adds to deps the fallback of each deferred call: its candidates
// and, transitively, the fallbacks of the deferred calls they carry. While
// inputs are still growing (propagate) nothing is added: deps are wanted
// only once they are final.
func (d *deferral) unknown(os []origin, deps map[eir.FuncID]bool) {
	if !d.final {
		return
	}
	for _, o := range os {
		for _, id := range d.fallbackOf(o) {
			deps[id] = true
		}
	}
}

func (d *deferral) fallbackOf(o origin) []eir.FuncID {
	if ids, ok := d.fallback[o.call]; ok {
		return ids
	}
	set := map[eir.FuncID]bool{}
	seen := map[*eir.Call]bool{o.call: true}
	work := []origin{o}
	for len(work) > 0 {
		x := work[len(work)-1]
		work = work[:len(work)-1]
		for _, id := range d.c.of(d.p.Funcs[x.f], x.call) {
			if set[id] {
				continue
			}
			set[id] = true
			if id == eir.NoFunc || int(id) >= len(d.p.Funcs) {
				continue
			}
			for _, os := range d.inputs[id] {
				for _, y := range os {
					if !seen[y.call] {
						seen[y.call] = true
						work = compat.Append(work, y)
					}
				}
			}
		}
	}
	ids := setIDs(set)
	d.fallback[o.call] = ids
	return ids
}

func appendOrigins(s []origin, os []origin) []origin {
	for _, o := range os {
		dup := false
		for _, x := range s {
			if x == o {
				dup = true
				break
			}
		}
		if !dup {
			s = compat.Append(s, o)
		}
	}
	return s
}

// propagate computes inputs: the deferred calls each input of each
// function stands for, directly or through what its callees defer.
func (d *deferral) propagate() {
	for _, f := range d.p.Funcs {
		defs := d.c.defsOf(f)
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				in := &b.Instrs[i]
				if in.Op == eir.OpCall && d.deferred(f, in.Call) {
					k, _ := oneInput(defs, in.Call.Target)
					d.add(f.ID, k, []origin{{f.ID, in.Call}})
					for _, a := range in.Call.Args {
						for j := range d.reachOf(f, a).inputs {
							d.addArg(f.ID, j)
						}
					}
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, f := range d.p.Funcs {
			for k, os := range d.resolve(f).redefer {
				if d.add(f.ID, k, os) {
					changed = true
				}
			}
			// What f passes where a callee's deferred calls take their
			// arguments: f's inputs it is reached from.
			for _, b := range f.Blocks {
				for i := range b.Instrs {
					in := &b.Instrs[i]
					if in.Op != eir.OpCall {
						continue
					}
					d.eachArgPassed(f, in.Call, func(_ eir.FuncID, a eir.Reg) {
						for j := range d.reachOf(f, a).inputs {
							if d.addArg(f.ID, j) {
								changed = true
							}
						}
					})
				}
			}
		}
	}
}

func (d *deferral) addArg(f eir.FuncID, j int) bool {
	if d.argInputs[f] == nil {
		d.argInputs[f] = map[int]bool{}
	}
	if d.argInputs[f][j] {
		return false
	}
	d.argInputs[f][j] = true
	return true
}

func (d *deferral) add(f eir.FuncID, k int, os []origin) bool {
	if d.inputs[f] == nil {
		d.inputs[f] = map[int][]origin{}
	}
	n := len(d.inputs[f][k])
	d.inputs[f][k] = appendOrigins(d.inputs[f][k], os)
	return len(d.inputs[f][k]) > n
}

// eachPassed calls fn for each argument of call c (in f) that a callee g
// defers through its input j, with the deferred calls it stands for. The
// arguments are bound as the solver binds them (solve.binding).
func (d *deferral) eachPassed(f *eir.Func, c *eir.Call, fn func(g eir.FuncID, j int, a eir.Reg, os []origin)) {
	d.eachBound(f, c, func(g eir.FuncID, bind []eir.Reg) {
		for j, os := range d.inputs[g] {
			if j < len(bind) && bind[j] != 0 {
				fn(g, j, bind[j], os)
			}
		}
	})
}

// eachBound calls fn for each callee g of call c (in f) that carries
// deferred calls, with the registers bound to g's inputs as the solver
// binds them (solve.binding). A deferred call of f is resolved by f's
// callers: it binds nothing here.
func (d *deferral) eachBound(f *eir.Func, c *eir.Call, fn func(g eir.FuncID, bind []eir.Reg)) {
	var callees []eir.FuncID
	switch c.Kind {
	case eir.CallStatic:
		callees = []eir.FuncID{c.Callee}
	case eir.CallIface, eir.CallTypeParam:
		callees = d.c.of(f, c)
	case eir.CallFunc:
		if d.deferred(f, c) {
			return
		}
		callees = d.c.of(f, c)
	default:
		return
	}
	for _, g := range callees {
		if g == eir.NoFunc || int(g) >= len(d.p.Funcs) || (d.inputs[g] == nil && d.argInputs[g] == nil) {
			continue
		}
		gf := d.p.Funcs[g]
		n := len(gf.Inputs)
		bind := compat.Make[eir.Reg](n)
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
		fn(g, bind)
	}
}

// deps returns the functions f's solve reads to resolve the deferred
// entries of its callees.
func (d *deferral) deps(f *eir.Func) []eir.FuncID {
	return setIDs(d.resolve(f).deps)
}
