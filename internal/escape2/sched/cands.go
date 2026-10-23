package sched

import (
	"sort"

	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/types2"
)

// Candidate sets (plan section (b), stage B; invariant I2): for each dynamic
// call, every function that may run there.
//
//   - An interface call of method M reaches the method M of every dynamic
//     type an interface value can have. Dynamic types come only from
//     conversions (plan section 3, change 2): every type the program
//     converts to an interface, with generic conversions substituted over
//     the instantiation closure, and the by-value parts of those types,
//     which the formatting walk of pluto/fmt makes interface values of
//     without a conversion in the source. Method lookup is by method set of
//     the type (both T and *T, which is a superset), never by the type of an
//     abstract location.
//   - A call of a method of a type parameter reaches the method of that name
//     of every type the parameter is instantiated with, and, where that type
//     is an interface, the interface call's set.
//   - A call of a func value reaches every function used as a func value
//     whose signature may match the call's.
//
// Where a set cannot be bounded (a generic conversion or type parameter
// whose instantiations are open), the set is every function of the program
// that could be the target by name and signature. A set is never cut short.
//
// The function a dynamic call reaches may be a synthesized wrapper (itab
// adapter, promoted method, pointer wrapper, method value) or the method it
// wraps; both are in the set, which is sound whichever the lowerer calls.

// dispatchRule decides what a dynamic call may reach: the callees whose
// summaries the solver applies there, which the call graph orders first.
// The rule of stage 2 is union over the candidate set. The owner's decision
// on a language rule for dynamic calls (plan, "Open") plugs in here: a
// type-level rule returns no callees for the calls it governs and gives the
// solver the summary the type fixes instead; deferred dispatch (stage 3)
// returns the callees only where a target can come from the heap.
type dispatchRule interface {
	callees(f *eir.Func, c *eir.Call) []eir.FuncID
}

// unionRule is the rule of stage 2: every candidate.
type unionRule struct{ c *cands }

func (r unionRule) callees(f *eir.Func, c *eir.Call) []eir.FuncID { return r.c.of(f, c) }

// cands computes and memoizes candidate sets.
type cands struct {
	p    *eir.Program
	tt   *typeTable
	inst *instClosure

	// dyn are the dynamic types interface values can have; dynOpen says a
	// generic conversion could not be bounded, so any type may be one.
	dyn     []types2.Type
	dynOpen bool

	methodsByName map[string][]eir.FuncID // declared methods, by name
	synthByName   map[string][]eir.FuncID // synthesized functions wrapping a method, by its name
	funcValues    []eir.FuncID            // every function that may be a func value

	ifaceMemo map[*types2.Func][]eir.FuncID
	tpMemo    map[tpKey][]eir.FuncID
	funcMemo  map[*types2.Signature][]eir.FuncID
	sigMemo   map[*types2.Signature]map[typeID]bool // nil: open
	defs      map[eir.FuncID][]*eir.Instr           // localClosures' definitions, by function
	namedMemo map[*types2.Func][]eir.FuncID         // allNamedMemo
	flow      *funcFlow                             // func values' origins, once computed
	dropsMemo []eir.FuncID                          // ifaceDrops
	wrappers  map[typeID][]types2.Type              // pointerWrappers, by the pointer's element type
	funcWraps map[typeID][]types2.Type              // pointerWrappers of a signature, by it
}

type tpKey struct {
	tp     *types2.TypeParam
	method string
}

func newCands(p *eir.Program, tt *typeTable, inst *instClosure) *cands {
	c := &cands{
		p: p, tt: tt, inst: inst,
		methodsByName: map[string][]eir.FuncID{},
		synthByName:   map[string][]eir.FuncID{},
		ifaceMemo:     map[*types2.Func][]eir.FuncID{},
		tpMemo:        map[tpKey][]eir.FuncID{},
		funcMemo:      map[*types2.Signature][]eir.FuncID{},
		sigMemo:       map[*types2.Signature]map[typeID]bool{},
		defs:          map[eir.FuncID][]*eir.Instr{},
		namedMemo:     map[*types2.Func][]eir.FuncID{},
	}
	c.index()
	c.dynamicTypes()
	return c
}

// index builds the method indexes and the func-value set.
func (c *cands) index() {
	fv := map[eir.FuncID]bool{}
	for _, f := range c.p.Funcs {
		k := f.Key
		switch {
		case k.Obj != nil:
			if k.Obj.Signature().Recv() != nil {
				c.methodsByName[k.Obj.Name()] = append(c.methodsByName[k.Obj.Name()], f.ID)
			}
		case k.Synth != nil:
			if k.Synth.Method != nil {
				c.synthByName[k.Synth.Method.Name()] = append(c.synthByName[k.Synth.Method.Name()], f.ID)
			}
			switch k.Synth.Kind {
			case eir.SynthMethodValue, eir.SynthMethodExpr, eir.SynthIfaceMethodValue, eir.SynthVariant:
				fv[f.ID] = true
			}
		case k.Lit != nil, k.Range != nil:
			// Every literal and range body is a func value: a closure.
			fv[f.ID] = true
		}
		for _, id := range f.Facts.FuncValues {
			fv[id] = true
		}
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				if in := &b.Instrs[i]; in.Op == eir.OpClosure && in.Callee != eir.NoFunc {
					fv[in.Callee] = true
				}
			}
		}
	}
	for id := range fv {
		if int(id) < len(c.p.Funcs) {
			c.funcValues = append(c.funcValues, id)
		}
	}
	sortIDs(c.funcValues)
}

// dynamicTypes computes the set of dynamic types.
func (c *cands) dynamicTypes() {
	seen := map[typeID]bool{}
	var add func(t types2.Type, depth int)
	add = func(t types2.Type, depth int) {
		if t == nil || depth > 64 {
			return
		}
		t = types2.Unalias(t)
		if c.tt.hasTypeParam(t) {
			c.dynOpen = true // a part the closure did not substitute
			return
		}
		id := c.tt.intern(t)
		if seen[id] {
			return
		}
		seen[id] = true
		if _, isIface := t.Underlying().(*types2.Interface); !isIface {
			c.dyn = append(c.dyn, c.tt.types[id])
		}
		// The by-value parts, and what pointers point to: the formatting
		// walk calls methods of the values it reaches.
		switch u := t.Underlying().(type) {
		case *types2.Struct:
			for i := 0; i < u.NumFields(); i++ {
				add(u.Field(i).Type(), depth+1)
			}
		case *types2.Array:
			add(u.Elem(), depth+1)
		case *types2.Slice:
			add(u.Elem(), depth+1)
		case *types2.Map:
			add(u.Key(), depth+1)
			add(u.Elem(), depth+1)
		case *types2.Pointer:
			add(u.Elem(), depth+1)
		case *types2.Wrapper:
			add(u.Elem(), depth+1)
		case *types2.TaggedUnion:
			for i := 0; i < u.NumVariants(); i++ {
				if v := u.Variant(i); v.HasPayload() {
					add(v.Payload(), depth+1)
				}
			}
		}
	}
	for _, f := range c.p.Funcs {
		for _, t := range f.Facts.Converted {
			for _, s := range c.substAll(t) {
				add(s, 0)
			}
		}
	}
}

// substAll returns the concrete types t stands for over the instantiation
// closure; it sets dynOpen when they cannot be bounded.
func (c *cands) substAll(t types2.Type) []types2.Type {
	out, ok := substAll(c.tt, c.inst, t)
	if !ok {
		c.dynOpen = true
	}
	return out
}

// substAll returns every type t becomes when its type parameters are
// replaced by their instantiations, and false if that set is not bounded.
func substAll(tt *typeTable, inst *instClosure, t types2.Type) ([]types2.Type, bool) {
	if !tt.hasTypeParam(t) {
		return []types2.Type{t}, true
	}
	qs := typeParamsIn(t)
	choices := make([][]types2.Type, len(qs))
	combos := 1
	for i, q := range qs {
		set, ok := inst.instantiated(q)
		if !ok {
			return nil, false
		}
		choices[i] = set
		combos *= len(set)
		if combos == 0 {
			return nil, true // never instantiated: the code never runs
		}
		if combos > instMaxCombos {
			return nil, false
		}
	}
	var out []types2.Type
	pick := make([]types2.Type, len(qs))
	var rec func(i int)
	rec = func(i int) {
		if i == len(qs) {
			out = append(out, types2.Subst(nil, t, qs, pick))
			return
		}
		for _, s := range choices[i] {
			pick[i] = s
			rec(i + 1)
		}
	}
	rec(0)
	return out, true
}

// of returns the candidate set of a dynamic call, sorted.
func (c *cands) of(f *eir.Func, call *eir.Call) []eir.FuncID {
	switch call.Kind {
	case eir.CallIface:
		if call.Method == nil {
			// The drop of an owning interface's dynamic value.
			return c.ifaceDrops()
		}
		if ids, ok := c.localBoxes(f, call); ok {
			return ids
		}
		return c.iface(call.Method)
	case eir.CallTypeParam:
		tp, _ := types2.Unalias(call.Iface).(*types2.TypeParam)
		if call.Method == nil && call.Implicit == eir.ImplDrop {
			return c.typeParamDrops(tp)
		}
		return c.typeParam(tp, call.Method)
	case eir.CallFunc:
		if ids := c.localClosures(f, call.Target); ids != nil {
			return ids
		}
		if c.flow != nil {
			if ids, ok := c.flow.targetsOf(call); ok {
				return ids
			}
		}
		return c.funcs(call.Sig)
	}
	return nil
}

// localBoxes returns the methods an interface call may reach when the
// function itself boxed its receiver: the receiver is defined, through
// copies, phis, and conversions and assertions to interface types (which
// keep the dynamic type), only by conversions of values of concrete types
// to an interface. Only the boxes' own dynamic types decide, never the
// static type of where the value went: a dynamic type without the method
// contributes nothing (the assertion to an interface that has it fails
// first), so a string boxed and asserted to error calls no Error. Anything
// else on the way (an input, a load, a call's result, a type with type
// parameters) gives false: the set is not known locally.
func (c *cands) localBoxes(f *eir.Func, call *eir.Call) ([]eir.FuncID, bool) {
	defs := c.defsOf(f)
	seen := map[eir.Reg]bool{}
	var types []types2.Type
	work := []eir.Reg{call.Target}
	for len(work) > 0 {
		x := work[len(work)-1]
		work = work[:len(work)-1]
		if seen[x] {
			continue
		}
		seen[x] = true
		if x == 0 || int(x) >= len(defs) || defs[x] == nil {
			return nil, false
		}
		in := defs[x]
		switch in.Op {
		case eir.OpCopy:
			work = append(work, in.X)
		case eir.OpPhi:
			work = append(work, in.Args...)
		case eir.OpAssert:
			if !types2.IsInterface(in.Type) {
				return nil, false
			}
			work = append(work, in.X)
		case eir.OpConv:
			if in.Type == nil {
				return nil, false
			}
			if types2.IsInterface(in.Type) {
				work = append(work, in.X)
				continue
			}
			open := false
			walkType(in.Type, func(u types2.Type) bool {
				if _, ok := u.(*types2.TypeParam); ok {
					open = true
				}
				return !open
			})
			if open {
				return nil, false
			}
			types = append(types, in.Type)
		default:
			return nil, false
		}
	}
	set := map[eir.FuncID]bool{}
	for _, t := range types {
		c.methodsOf(t, call.Method, set, true)
	}
	return setIDs(set), true
}

// localClosures returns the literals a func value can be when the function
// itself made it: register r is defined, through copies and phis only, by
// closures of known literals (a deferred literal, a literal called where it
// is written). The value is then one of those closures and nothing else,
// so the call reaches exactly their functions. Any other definition on the
// way (an input, a load, a call's result) gives nil: the set is not known
// locally.
func (c *cands) localClosures(f *eir.Func, r eir.Reg) []eir.FuncID {
	defs := c.defsOf(f)
	seen := map[eir.Reg]bool{}
	set := map[eir.FuncID]bool{}
	work := []eir.Reg{r}
	for len(work) > 0 {
		x := work[len(work)-1]
		work = work[:len(work)-1]
		if seen[x] {
			continue
		}
		seen[x] = true
		if x == 0 || int(x) >= len(defs) || defs[x] == nil {
			return nil
		}
		in := defs[x]
		switch in.Op {
		case eir.OpCopy:
			work = append(work, in.X)
		case eir.OpPhi:
			work = append(work, in.Args...)
		case eir.OpClosure:
			if in.Callee == eir.NoFunc || int(in.Callee) >= len(c.p.Funcs) {
				return nil
			}
			set[in.Callee] = true
		case eir.OpLoad:
			// A load from a frame object only this function's loads and
			// stores reach (a deferred call's temporary): what it holds is
			// what was stored there.
			srcs, ok := c.objStores(f, in.X)
			if !ok {
				return nil
			}
			work = append(work, srcs...)
		default:
			return nil
		}
	}
	if len(set) == 0 {
		return nil
	}
	return setIDs(set)
}

// objStores returns the values stored at the place register addr names,
// when that place is a whole frame object whose address is only ever the
// address operand of loads and stores in f: nothing else can write it, so
// these are all it can hold (besides its zero value, which no call reaches).
func (c *cands) objStores(f *eir.Func, addr eir.Reg) ([]eir.Reg, bool) {
	defs := c.defsOf(f)
	if addr == 0 || int(addr) >= len(defs) || defs[addr] == nil || defs[addr].Op != eir.OpAddr || len(defs[addr].Path) != 0 {
		return nil, false
	}
	obj := defs[addr].Obj
	var srcs []eir.Reg
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			in := &b.Instrs[i]
			// Every use of an address of obj must be a load's or store's
			// address of the whole object.
			uses := func(r eir.Reg) bool {
				return r != 0 && int(r) < len(defs) && defs[r] != nil && defs[r].Op == eir.OpAddr && defs[r].Obj == obj
			}
			switch in.Op {
			case eir.OpAddr:
				if in.Obj == obj && len(in.Path) != 0 {
					return nil, false
				}
				continue
			case eir.OpLoad:
				continue
			case eir.OpStore:
				if uses(in.Y) {
					return nil, false
				}
				if uses(in.X) {
					srcs = append(srcs, in.Y)
				}
				continue
			}
			if uses(in.X) || uses(in.Y) {
				return nil, false
			}
			for _, a := range in.Args {
				if uses(a) {
					return nil, false
				}
			}
			if in.Call != nil {
				if uses(in.Call.Target) {
					return nil, false
				}
				for _, a := range in.Call.Args {
					if uses(a) {
						return nil, false
					}
				}
			}
		}
	}
	return srcs, true
}

// defsOf returns the instruction defining each register of f (nil for none
// or a call's result), built once per function.
func (c *cands) defsOf(f *eir.Func) []*eir.Instr {
	if d, ok := c.defs[f.ID]; ok {
		return d
	}
	d := make([]*eir.Instr, f.NumRegs+1)
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			in := &b.Instrs[i]
			if in.Op != eir.OpCall && in.Dst != 0 && int(in.Dst) < len(d) {
				d[in.Dst] = in
			}
		}
	}
	c.defs[f.ID] = d
	return d
}

// iface returns the methods an interface call of m may reach.
func (c *cands) iface(m *types2.Func) []eir.FuncID {
	if m == nil {
		return nil
	}
	if ids, ok := c.ifaceMemo[m]; ok {
		return ids
	}
	var ids []eir.FuncID
	if c.dynOpen {
		ids = c.allNamed(m)
	} else {
		set := map[eir.FuncID]bool{}
		for _, t := range c.dyn {
			c.methodsOf(t, m, set, true)
		}
		ids = setIDs(set)
	}
	c.ifaceMemo[m] = ids
	return ids
}

// typeParam returns the methods a call of m on a value of type parameter tp
// may reach.
func (c *cands) typeParam(tp *types2.TypeParam, m *types2.Func) []eir.FuncID {
	if m == nil {
		return nil
	}
	if tp == nil {
		return c.allNamed(m)
	}
	key := tpKey{c.inst.canon(tp), m.Name()}
	if ids, ok := c.tpMemo[key]; ok {
		return ids
	}
	var ids []eir.FuncID
	types, ok := c.inst.instantiated(tp)
	if !ok {
		ids = c.allNamed(m)
	} else {
		set := map[eir.FuncID]bool{}
		for _, t := range types {
			if _, isIface := t.Underlying().(*types2.Interface); isIface {
				for _, id := range c.iface(m) {
					set[id] = true
				}
				continue
			}
			c.methodsOf(t, m, set, false)
		}
		ids = setIDs(set)
	}
	c.tpMemo[key] = ids
	return ids
}

// funcs returns the func values a call of signature sig may reach. A
// signature that mentions type parameters stands for its instantiations
// (sigSet): the call reaches a func value if some instantiation of its
// signature is identical to some instantiation of the call's. Code whose
// type parameters are never instantiated never runs: a call there reaches
// nothing, and a func value made there is never one. Where an
// instantiation set is open, type parameters match anything (mayMatch).
func (c *cands) funcs(sig *types2.Signature) []eir.FuncID {
	if sig == nil {
		return append([]eir.FuncID(nil), c.funcValues...)
	}
	if ids, ok := c.funcMemo[sig]; ok {
		return ids
	}
	callSet, callOK := c.sigSet(sig)
	var ids []eir.FuncID
	if !callOK || len(callSet) > 0 {
		for _, id := range c.funcValues {
			fs := c.p.Funcs[id].Sig
			if fs == nil {
				ids = append(ids, id)
				continue
			}
			candSet, candOK := c.sigSet(fs)
			switch {
			case candOK && len(candSet) == 0:
				// made only in code that never runs
			case !callOK || !candOK:
				if c.tt.sigMayMatch(fs, sig, 0) {
					ids = append(ids, id)
				}
			default:
				for t := range candSet {
					if callSet[t] {
						ids = append(ids, id)
						break
					}
				}
			}
		}
	}
	c.funcMemo[sig] = ids
	return ids
}

// sigSet returns the concrete signatures sig stands for, by their IDs in
// the type table, and false if they cannot be bounded. A signature with no
// type parameters stands for itself.
func (c *cands) sigSet(sig *types2.Signature) (map[typeID]bool, bool) {
	if set, ok := c.sigMemo[sig]; ok {
		return set, set != nil
	}
	plain := types2.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
	types, ok := substAll(c.tt, c.inst, plain)
	if !ok {
		c.sigMemo[sig] = nil
		return nil, false
	}
	set := map[typeID]bool{}
	for _, t := range types {
		set[c.tt.intern(t)] = true
	}
	c.sigMemo[sig] = set
	return set, true
}

// methodsOf adds the functions that may run for method m of dynamic type t:
// the method t's method set (or *t's) has under that name, if its signature
// may match m's, and the wrappers of a method of that name for t or *t.
//
// For an interface call (iface), the itab holds an adapter where the
// method needs one: it takes the interface's data word (for a type that is
// not direct, the box) and does the load the source method lacks. Where an
// adapter of that name for t exists, it alone is the candidate: binding the
// source method of a value receiver to the box pointer would read the box
// as the value. Without an adapter, the method and the other wrappers stay,
// the sound fallback.
func (c *cands) methodsOf(t types2.Type, m *types2.Func, set map[eir.FuncID]bool, iface bool) {
	if iface {
		found := false
		for _, id := range c.synthByName[m.Name()] {
			s := c.p.Funcs[id].Key.Synth
			if s.Kind == eir.SynthItabAdapter && s.Recv != nil && c.tt.mayMatch(s.Recv, t) {
				set[id] = true
				found = true
			}
		}
		if found {
			return
		}
	}
	obj, _, _ := types2.LookupFieldOrMethod(t, true, m.Pkg(), m.Name())
	fn, ok := obj.(*types2.Func)
	if !ok {
		return
	}
	if !c.tt.sigMayMatch(fn.Signature(), m.Signature(), 0) {
		return
	}
	found := false
	if id := c.p.Lookup(eir.FuncKey{Obj: fn.Origin()}); id != eir.NoFunc {
		set[id] = true
		found = true
	}
	ptr := types2.NewPointer(t)
	for _, id := range c.synthByName[m.Name()] {
		s := c.p.Funcs[id].Key.Synth
		if s.Recv == nil || c.tt.mayMatch(s.Recv, t) || c.tt.mayMatch(s.Recv, ptr) {
			set[id] = true
			found = true
		}
	}
	if !found {
		// t has the method, but no function of the program stands for
		// it: it is promoted from an embedded interface (and dispatches
		// again on that field's dynamic type), or its wrapper was not
		// made. The call may then run any function of that name and
		// signature; never nothing.
		for _, id := range c.allNamedMemo(m) {
			set[id] = true
		}
	}
}

// ifaceDrops returns what the drop of an interface's dynamic value may run
// (an interface call with no method, section 18.6): the Drop method and
// drop glue of every dynamic type, or of every type when the dynamic types
// are open. It is the same for every owning interface: the dynamic types
// are not narrowed by interface, which is sound and computed once.
func (c *cands) ifaceDrops() []eir.FuncID {
	if c.dropsMemo != nil {
		return c.dropsMemo
	}
	var types []types2.Type
	if !c.dynOpen {
		types = c.dyn
	}
	c.dropsMemo = c.dropsOf(types, !c.dynOpen)
	return c.dropsMemo
}

// dropsOf returns the Drop methods and drop glues of the given types (or
// pointers to them), or all of them when known is false.
func (c *cands) dropsOf(types []types2.Type, known bool) []eir.FuncID {
	set := map[eir.FuncID]bool{}
	for _, f := range c.p.Funcs {
		var recv types2.Type
		switch {
		case f.Key.Synth != nil && f.Key.Synth.Kind == eir.SynthDropGlue:
			recv = f.Key.Synth.Recv
		case f.Key.Obj != nil && f.Key.Obj.Name() == "Drop" && f.Key.Obj.Signature().Recv() != nil:
			recv = f.Key.Obj.Signature().Recv().Type()
		default:
			continue
		}
		if !known || recv == nil {
			set[f.ID] = true
			continue
		}
		for _, t := range types {
			if c.tt.mayMatch(recv, t) || c.tt.mayMatch(recv, types2.NewPointer(t)) {
				set[f.ID] = true
				break
			}
		}
	}
	return setIDs(set)
}

// typeParamDrops returns what the drop of a value of type parameter tp may
// run (a call with no method and Implicit ImplDrop, section 18.6): for each
// type tp is instantiated with, its drop glue and its Drop method; every
// drop glue and Drop method of the program when the instantiations are
// open, or when tp is not known.
func (c *cands) typeParamDrops(tp *types2.TypeParam) []eir.FuncID {
	var types []types2.Type
	known := false
	if tp != nil {
		types, known = c.inst.instantiated(tp)
	}
	return c.dropsOf(types, known)
}

// allNamedMemo is allNamed, memoized by method.
func (c *cands) allNamedMemo(m *types2.Func) []eir.FuncID {
	if ids, ok := c.namedMemo[m]; ok {
		return ids
	}
	ids := c.allNamed(m)
	c.namedMemo[m] = ids
	return ids
}

// allNamed returns every method or method wrapper named as m whose
// signature may match m's: the set when dynamic types are not bounded.
func (c *cands) allNamed(m *types2.Func) []eir.FuncID {
	set := map[eir.FuncID]bool{}
	for _, id := range c.methodsByName[m.Name()] {
		if c.tt.sigMayMatch(c.p.Funcs[id].Key.Obj.Signature(), m.Signature(), 0) {
			set[id] = true
		}
	}
	for _, id := range c.synthByName[m.Name()] {
		set[id] = true
	}
	return setIDs(set)
}

func setIDs(set map[eir.FuncID]bool) []eir.FuncID {
	ids := make([]eir.FuncID, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sortIDs(ids)
	return ids
}

func sortIDs(ids []eir.FuncID) {
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
}

// pointerWrappers are the dynamic types whose interface data word may be a
// pointer to a value of type u other than *u itself: a struct of one field
// or an array of one element that holds such a pointer, at any depth
// (wrappedPointer); known is false when the dynamic types are open. They
// are the dynamic types a direct interface value may have when its word is
// a pointer to a u, besides *u.
//
// When u is a signature, the word is a func value of it (a closure record
// is a func value's data word): the dynamic types are then the named func
// types whose underlying type is u, and the structs of one field and arrays
// of one element holding one of those or a func of type u (wrappedFunc).
func (c *cands) pointerWrappers(u types2.Type) ([]types2.Type, bool) {
	if c.dynOpen {
		return nil, false
	}
	if c.wrappers == nil {
		c.wrappers = map[typeID][]types2.Type{}
		c.funcWraps = map[typeID][]types2.Type{}
		for _, t := range c.dyn {
			if p := wrappedPointer(t, 0); p != nil {
				id := c.tt.intern(p.Elem())
				c.wrappers[id] = append(c.wrappers[id], t)
			}
			if sig := wrappedFunc(t, 0); sig != nil {
				id := c.tt.intern(sig)
				c.funcWraps[id] = append(c.funcWraps[id], t)
			}
		}
	}
	index := c.wrappers
	if sig, ok := u.Underlying().(*types2.Signature); ok {
		index, u = c.funcWraps, sig
	}
	id, ok := c.tt.lookup(u)
	if !ok {
		return nil, true
	}
	return index[id], true
}

// wrappedFunc is the signature of the func value that is the whole data
// word of t, when t is a named func type, or a struct of one field or an
// array of one element holding a func value, at any depth; nil otherwise,
// and for an unnamed func type itself.
func wrappedFunc(t types2.Type, depth int) *types2.Signature {
	if depth > 16 {
		return nil
	}
	switch u := t.Underlying().(type) {
	case *types2.Signature:
		if _, unnamed := types2.Unalias(t).(*types2.Signature); unnamed && depth == 0 {
			return nil
		}
		return u
	case *types2.Struct:
		if u.NumFields() == 1 {
			return wrappedFunc(u.Field(0).Type(), depth+1)
		}
	case *types2.Array:
		if u.Len() == 1 {
			return wrappedFunc(u.Elem(), depth+1)
		}
	}
	return nil
}

// wrappedPointer is the pointer that is the whole data word of t, when t
// is a struct of one field or an array of one element holding one, at any
// depth; nil otherwise, and for a pointer type itself.
func wrappedPointer(t types2.Type, depth int) *types2.Pointer {
	if depth > 16 {
		return nil
	}
	switch u := t.Underlying().(type) {
	case *types2.Pointer:
		if depth == 0 {
			return nil
		}
		return u
	case *types2.Struct:
		if u.NumFields() == 1 {
			return wrappedPointer(u.Field(0).Type(), depth+1)
		}
	case *types2.Array:
		if u.Len() == 1 {
			return wrappedPointer(u.Elem(), depth+1)
		}
	}
	return nil
}
