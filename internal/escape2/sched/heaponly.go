package sched

import (
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/types2"
)

// Heap-only field classes (plan invariant I6), computed once in stage B as a
// greatest fixed point over the functions' source facts.
//
// A field class is (underlying struct type, field index): Node, Raw,
// type Alt Node and struct{n int; next *int} with the same fields share
// one. A class is heap-only when every pointer ever written within that
// field of any location holding the struct is a heap pointer (allocation,
// static memory, nil, a counted reference). A load from a heap-only class
// gives heap pointers; that is all the solver asks (Facts.HeapOnly).
//
// A write is any instruction that puts a value into memory that is, or
// contains, a field of a class:
//
//   - a store whose address is a field path (OpField/OpAddr/OpElem chains
//     from a base whose type is known) writes every class along the path,
//     and, when the value stored is a struct, every class within it;
//   - a store through an address of unknown origin, of a location type L,
//     may write any class whose field holds L by value and whose address
//     the program lets go anywhere but a direct load or store (an escaped
//     class); with an unknown base type behind a field step, any class
//     holding L at all;
//   - the struct values eir.Facts.FieldWrites lists, and any struct value
//     passed to a contract-table entry or a bodyless function, write every
//     class within their type; a pointer or slice passed there escapes the
//     classes of the struct types it points to, and lets the callee store
//     into any location of the non-struct types it points to.
//
// The sources of a written pointer are traced back through registers
// (copies, phis, field selects, compositions, conversions): allocations and
// static memory are heap; a load of a class is heap if that class is; an
// input is heap if every caller passes heap there (only for functions every
// caller of which is a static call the program makes); a call result is
// heap if every return of every possible callee is. Anything else (a frame
// address, a load from a place of no class, a closure record, an input of a
// function called from outside or through a func value or interface) is
// not. The fixed point starts with every class, input and result heap and
// withdraws what a non-heap source reaches; it is sound alone, so no
// validation round is needed.
//
// A generic struct (one mentioning type parameters) shares a group with
// every struct it may instantiate to; a group is heap-only only if all its
// classes are. Writes through memory reinterpreted with unsafe.Pointer stay
// outside the rule (sections 18.3, 18.12).

// hoAtom is one source requirement: a node that must hold, or unknown.
type hoAtom int32

const hoUnknown hoAtom = -1

// hoNodeKind says what a node of the fixed point is.
type hoNodeKind uint8

const (
	hoClass     hoNodeKind = iota + 1 // a field class: a = struct typeID, b = field
	hoParam                           // input b of function a, at path c
	hoResult                          // result b of function a, at path c
	hoLStore                          // stores of location type a through unknown addresses (escaped classes)
	hoLStoreAll                       // stores of location type a whose container is unknown (all classes)
)

type hoKey struct {
	kind hoNodeKind
	a, b int32
	c    string // path, encoded
}

// heapOnly is the stage B heap-only computation and its result.
type heapOnly struct {
	p     *eir.Program
	tt    *typeTable
	inst  *instClosure
	cands *cands

	// per-function state, built once
	defs    [][]hoDef // by FuncID, by register
	staticS [][]hoSite
	dynS    map[eir.FuncID]bool // functions some dynamic call or func value may reach
	returns [][]hoReturn

	keys  []hoKey
	index map[hoKey]hoAtom
	reqs  [][]hoAtom // by node: atoms that must all hold
	val   []bool
	done  []bool // requirements collected

	escaped map[hoKey]bool // class keys whose field address escapes
	lstores map[typeID]bool
	lAll    map[typeID]bool
	poison  bool // a write of a type that cannot be bounded: nothing is heap-only

	structGroup map[typeID]int32 // struct typeID to group (union-find root)
	memo        map[hoKey]bool   // classHolds answers
	uf          []int32
}

type hoDef struct {
	in  *eir.Instr
	res int // for a call result, its index; -1 otherwise
}

type hoSite struct {
	f    *eir.Func
	call *eir.Call
}

type hoReturn struct {
	f  *eir.Func
	in *eir.Instr
}

func newHeapOnly(p *eir.Program, tt *typeTable, inst *instClosure, c *cands) *heapOnly {
	h := &heapOnly{
		p: p, tt: tt, inst: inst, cands: c,
		dynS:        map[eir.FuncID]bool{},
		index:       map[hoKey]hoAtom{},
		escaped:     map[hoKey]bool{},
		lstores:     map[typeID]bool{},
		lAll:        map[typeID]bool{},
		structGroup: map[typeID]int32{},
		memo:        map[hoKey]bool{},
	}
	h.prepare()
	h.collectWrites()
	h.groupStructs()
	h.solve()
	return h
}

// prepare indexes definitions, call sites and returns.
func (h *heapOnly) prepare() {
	n := len(h.p.Funcs)
	h.defs = make([][]hoDef, n)
	h.staticS = make([][]hoSite, n)
	h.returns = make([][]hoReturn, n)
	for _, id := range h.cands.funcValues {
		h.dynS[id] = true
	}
	for _, f := range h.p.Funcs {
		defs := make([]hoDef, f.NumRegs+1)
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				in := &b.Instrs[i]
				if in.Dst != 0 && int(in.Dst) < len(defs) {
					defs[in.Dst] = hoDef{in: in, res: -1}
				}
				switch in.Op {
				case eir.OpCall:
					c := in.Call
					for j, r := range c.Results {
						if r != 0 && int(r) < len(defs) {
							defs[r] = hoDef{in: in, res: j}
						}
					}
					switch c.Kind {
					case eir.CallStatic:
						if c.Callee != eir.NoFunc && int(c.Callee) < n {
							h.staticS[c.Callee] = append(h.staticS[c.Callee], hoSite{f, c})
						}
					case eir.CallIface, eir.CallTypeParam, eir.CallFunc:
						for _, id := range h.cands.of(f, c) {
							h.dynS[id] = true
						}
					}
				case eir.OpSink:
					if in.Sink == eir.SinkReturn {
						h.returns[f.ID] = append(h.returns[f.ID], hoReturn{f, in})
					}
				}
			}
		}
		h.defs[f.ID] = defs
	}
}

// node returns the atom of a key, making the node if it is new.
func (h *heapOnly) node(k hoKey) hoAtom {
	if a, ok := h.index[k]; ok {
		return a
	}
	a := hoAtom(len(h.keys))
	h.keys = append(h.keys, k)
	h.index[k] = a
	h.reqs = append(h.reqs, nil)
	h.done = append(h.done, false)
	return a
}

func (h *heapOnly) require(target hoAtom, atoms []hoAtom) {
	h.reqs[target] = append(h.reqs[target], atoms...)
}

// classKey is the key of field i of struct type s (its underlying struct).
func (h *heapOnly) classKey(s *types2.Struct, i int) hoKey {
	return hoKey{kind: hoClass, a: int32(h.tt.intern(s)), b: int32(i)}
}

// --- register types -------------------------------------------------------

// regType infers the type of register r of f, or nil.
func (h *heapOnly) regType(f *eir.Func, r eir.Reg, depth int) types2.Type {
	if r == 0 || int(r) >= len(h.defs[f.ID]) || depth > 32 {
		return nil
	}
	d := h.defs[f.ID][r]
	in := d.in
	if in == nil {
		return nil
	}
	if d.res >= 0 {
		if s := in.Call.Sig; s != nil && d.res < s.Results().Len() {
			return s.Results().At(d.res).Type()
		}
		return nil
	}
	switch in.Op {
	case eir.OpAddr:
		if int(in.Obj) < len(f.Objects) {
			if t := walkPath(f.Objects[in.Obj].Type, in.Path); t != nil {
				return types2.NewPointer(t)
			}
		}
	case eir.OpInput:
		if in.Index < len(f.Inputs) {
			return f.Inputs[in.Index].Type
		}
	case eir.OpLoad, eir.OpAssert, eir.OpCompose, eir.OpHeap:
		return in.Type
	case eir.OpField:
		if in.Type != nil {
			return in.Type
		}
		if pt := pointee(h.regType(f, in.X, depth+1)); pt != nil {
			if t := walkPath(pt, in.Path); t != nil {
				return types2.NewPointer(t)
			}
		}
	case eir.OpElem:
		if in.Type != nil {
			return in.Type
		}
		if t := elemOf(pointee(h.regType(f, in.X, depth+1))); t != nil {
			return types2.NewPointer(t)
		}
		if t := elemOf(h.regType(f, in.X, depth+1)); t != nil {
			return types2.NewPointer(t)
		}
	case eir.OpSelect:
		return walkPath(h.regType(f, in.X, depth+1), in.Path)
	case eir.OpCopy:
		return h.regType(f, in.X, depth+1)
	case eir.OpConv:
		return in.To
	case eir.OpPhi:
		for _, a := range in.Args {
			if t := h.regType(f, a, depth+1); t != nil {
				return t
			}
		}
	}
	return in.Type
}

func pointee(t types2.Type) types2.Type {
	if t == nil {
		return nil
	}
	if p, ok := t.Underlying().(*types2.Pointer); ok {
		return p.Elem()
	}
	return nil
}

func elemOf(t types2.Type) types2.Type {
	if t == nil {
		return nil
	}
	switch u := t.Underlying().(type) {
	case *types2.Array:
		return u.Elem()
	case *types2.Slice:
		return u.Elem()
	}
	return nil
}

// walkPath returns the type at path within t, or nil.
func walkPath(t types2.Type, path []int) types2.Type {
	for _, s := range path {
		if t == nil {
			return nil
		}
		switch u := t.Underlying().(type) {
		case *types2.Struct:
			if s < 0 || s >= u.NumFields() {
				return nil
			}
			t = u.Field(s).Type()
		case *types2.Array:
			if s != eir.ElemStep {
				return nil
			}
			t = u.Elem()
		default:
			return nil
		}
	}
	return t
}

// --- locations ------------------------------------------------------------

// loc is a place in memory: a base type and the steps into it.
type loc struct {
	base  types2.Type // nil: unknown
	steps []int
	// fieldStep: there is a field step below an unknown base.
	fieldStep bool
}

// addrLoc follows the address chain of register r of f.
func (h *heapOnly) addrLoc(f *eir.Func, r eir.Reg, depth int) loc {
	if r == 0 || int(r) >= len(h.defs[f.ID]) || depth > 32 {
		return loc{}
	}
	d := h.defs[f.ID][r]
	if d.in == nil || d.res >= 0 {
		return loc{}
	}
	in := d.in
	switch in.Op {
	case eir.OpAddr:
		if int(in.Obj) < len(f.Objects) {
			return loc{base: f.Objects[in.Obj].Type, steps: in.Path}
		}
	case eir.OpField:
		l := h.addrLoc(f, in.X, depth+1)
		if l.base == nil {
			if pt := pointee(h.regType(f, in.X, depth+1)); pt != nil {
				return loc{base: pt, steps: in.Path}
			}
			return loc{fieldStep: l.fieldStep || hasField(in.Path)}
		}
		return loc{base: l.base, steps: concat(l.steps, in.Path)}
	case eir.OpElem:
		l := h.addrLoc(f, in.X, depth+1)
		if l.base != nil {
			if _, isArr := walkPath(l.base, l.steps).Underlying().(*types2.Array); isArr {
				return loc{base: l.base, steps: concat(l.steps, []int{eir.ElemStep})}
			}
		}
		// An element of a slice, or of an array of unknown place: a
		// location of the element type, in no struct we can name.
		t := elemOf(pointee(h.regType(f, in.X, depth+1)))
		if t == nil {
			t = elemOf(h.regType(f, in.X, depth+1))
		}
		return loc{base: t, fieldStep: l.fieldStep}
	case eir.OpCopy:
		return h.addrLoc(f, in.X, depth+1)
	}
	return loc{}
}

func hasField(path []int) bool {
	for _, s := range path {
		if s >= 0 {
			return true
		}
	}
	return false
}

func concat(a, b []int) []int {
	out := make([]int, 0, len(a)+len(b))
	return append(append(out, a...), b...)
}

// classesAlong returns the classes the steps from base pass through: for
// each struct on the way, the field taken.
func (h *heapOnly) classesAlong(base types2.Type, steps []int) []hoKey {
	var out []hoKey
	t := base
	for _, s := range steps {
		if t == nil {
			break
		}
		switch u := t.Underlying().(type) {
		case *types2.Struct:
			if s < 0 || s >= u.NumFields() {
				return out
			}
			out = append(out, h.classKey(u, s))
			t = u.Field(s).Type()
		case *types2.Array:
			t = u.Elem()
		default:
			return out
		}
	}
	return out
}

// expand returns the concrete types t stands for, and false if unbounded.
func (h *heapOnly) expand(t types2.Type) ([]types2.Type, bool) {
	if t == nil {
		return nil, false
	}
	if !h.tt.hasTypeParam(t) {
		return []types2.Type{t}, true
	}
	if tp, ok := types2.Unalias(t).(*types2.TypeParam); ok {
		return h.inst.instantiated(tp)
	}
	// A generic struct or composite: its own classes are grouped with
	// those of its instantiations (groupStructs).
	return []types2.Type{t}, true
}

// --- origins --------------------------------------------------------------

type originKey struct {
	r    eir.Reg
	path string
}

type originState struct {
	f       *eir.Func
	memo    map[originKey][]hoAtom
	onStack map[originKey]bool
}

func encPath(path []int) string {
	b := make([]byte, 0, len(path)*2)
	for _, s := range path {
		b = append(b, byte(s+2), byte((s+2)>>8))
	}
	return string(b)
}

func decPath(s string) []int {
	out := make([]int, 0, len(s)/2)
	for i := 0; i+1 < len(s); i += 2 {
		out = append(out, int(s[i])|int(s[i+1])<<8-2)
	}
	return out
}

// origin returns the atoms the pointers within register r's value at path
// come from. complete is false when a cycle cut the answer short (it must
// not be memoized then).
func (h *heapOnly) origin(st *originState, r eir.Reg, path []int) (atoms []hoAtom, complete bool) {
	f := st.f
	if r == 0 || int(r) >= len(h.defs[f.ID]) {
		return []hoAtom{hoUnknown}, true
	}
	k := originKey{r, encPath(path)}
	if a, ok := st.memo[k]; ok {
		return a, true
	}
	if st.onStack[k] {
		return nil, false
	}
	st.onStack[k] = true
	atoms, complete = h.origin1(st, r, path)
	delete(st.onStack, k)
	if complete {
		st.memo[k] = atoms
	}
	return atoms, complete
}

func (h *heapOnly) origin1(st *originState, r eir.Reg, path []int) ([]hoAtom, bool) {
	f := st.f
	d := h.defs[f.ID][r]
	in := d.in
	if in == nil {
		return []hoAtom{hoUnknown}, true
	}
	if d.res >= 0 {
		return h.resultOrigin(f, in.Call, d.res, path), true
	}
	switch in.Op {
	case eir.OpHeap:
		return nil, true
	case eir.OpInput:
		if in.Index < len(f.Inputs) && f.Inputs[in.Index].Var != nil && h.paramsKnown(f.ID) {
			return []hoAtom{h.node(hoKey{kind: hoParam, a: int32(f.ID), b: int32(in.Index), c: encPath(path)})}, true
		}
		return []hoAtom{hoUnknown}, true
	case eir.OpLoad:
		return h.loadOrigin(f, in, path), true
	case eir.OpField, eir.OpElem:
		if len(path) != 0 {
			return []hoAtom{hoUnknown}, true
		}
		return h.origin(st, in.X, nil)
	case eir.OpSelect:
		return h.origin(st, in.X, concat(in.Path, path))
	case eir.OpCopy:
		return h.origin(st, in.X, path)
	case eir.OpConv:
		if in.Obj != eir.NoObj {
			return []hoAtom{hoUnknown}, true // boxed in the frame
		}
		return h.origin(st, in.X, path)
	case eir.OpAssert:
		if len(path) == 0 && isDirect(in.Type) {
			return h.origin(st, in.X, nil)
		}
		return []hoAtom{hoUnknown}, true
	case eir.OpCompose:
		var out []hoAtom
		complete := true
		for j, a := range in.Args {
			if j >= len(in.Fields) {
				return []hoAtom{hoUnknown}, true
			}
			fp := in.Fields[j]
			switch {
			case prefixOf(path, fp): // the arg is within the part asked for
				x, c := h.origin(st, a, nil)
				out, complete = append(out, x...), complete && c
			case prefixOf(fp, path): // the part asked for is within the arg
				x, c := h.origin(st, a, path[len(fp):])
				out, complete = append(out, x...), complete && c
			}
		}
		return out, complete
	case eir.OpPhi:
		var out []hoAtom
		complete := true
		for _, a := range in.Args {
			x, c := h.origin(st, a, path)
			out, complete = append(out, x...), complete && c
		}
		return out, complete
	}
	// OpAddr (a frame address), closures and method values (records in
	// the frame), and anything else.
	return []hoAtom{hoUnknown}, true
}

func prefixOf(prefix, p []int) bool {
	if len(prefix) > len(p) {
		return false
	}
	for i := range prefix {
		if prefix[i] != p[i] {
			return false
		}
	}
	return true
}

// isDirect reports whether a value of type t is held in an interface's data
// word itself (not in a box).
func isDirect(t types2.Type) bool {
	if t == nil {
		return false
	}
	switch t.Underlying().(type) {
	case *types2.Pointer, *types2.Map, *types2.Chan, *types2.Signature:
		return true
	}
	return false
}

// paramsKnown reports whether every caller of f is a static call the
// program makes, so that its inputs are bound only there.
func (h *heapOnly) paramsKnown(id eir.FuncID) bool {
	f := h.p.Funcs[id]
	return !f.External && !f.Bodyless && !h.dynS[id] && f.Key.Lit == nil && f.Key.Range == nil
}

// loadOrigin is the origin of a value loaded by in, at path within it.
func (h *heapOnly) loadOrigin(f *eir.Func, in *eir.Instr, path []int) []hoAtom {
	l := h.addrLoc(f, in.X, 0)
	var cls []hoKey
	if l.base != nil {
		cls = h.classesAlong(l.base, concat(l.steps, path))
	} else if in.Type != nil {
		// The place is unknown, but the value is of type in.Type: the
		// pointers at path within it are in the classes of that type.
		types, ok := h.expand(in.Type)
		if !ok {
			return []hoAtom{hoUnknown}
		}
		var atoms []hoAtom
		for _, t := range types {
			c := h.classesAlong(t, path)
			if len(c) == 0 {
				return []hoAtom{hoUnknown}
			}
			atoms = append(atoms, h.node(c[len(c)-1]))
		}
		return atoms
	}
	if len(cls) == 0 {
		return []hoAtom{hoUnknown}
	}
	// Any one class the pointer is in will do: the deepest.
	return []hoAtom{h.node(cls[len(cls)-1])}
}

// resultOrigin is the origin of result j of call c, at path.
func (h *heapOnly) resultOrigin(f *eir.Func, c *eir.Call, j int, path []int) []hoAtom {
	var callees []eir.FuncID
	switch c.Kind {
	case eir.CallStatic:
		callees = []eir.FuncID{c.Callee}
	case eir.CallIface, eir.CallTypeParam, eir.CallFunc:
		callees = h.cands.of(f, c)
	default:
		return []hoAtom{hoUnknown}
	}
	var out []hoAtom
	for _, g := range callees {
		if g == eir.NoFunc || int(g) >= len(h.p.Funcs) || h.p.Funcs[g].Bodyless {
			return []hoAtom{hoUnknown}
		}
		out = append(out, h.node(hoKey{kind: hoResult, a: int32(g), b: int32(j), c: encPath(path)}))
	}
	return out
}

// --- writes ---------------------------------------------------------------

// collectWrites records the requirements of every write in the program.
func (h *heapOnly) collectWrites() {
	for _, f := range h.p.Funcs {
		st := &originState{f: f, memo: map[originKey][]hoAtom{}, onStack: map[originKey]bool{}}
		uses := h.addrUses(f)
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				in := &b.Instrs[i]
				switch in.Op {
				case eir.OpStore:
					h.store(st, in)
				case eir.OpField, eir.OpElem, eir.OpAddr:
					if uses[in.Dst] {
						h.escape(f, in.Dst)
					}
				case eir.OpCall:
					h.opaqueCall(st, in.Call)
				}
			}
		}
		for _, w := range f.Facts.FieldWrites {
			h.within(st, w.Type, w.From, nil)
		}
	}
	h.expandNodes()
}

// addrUses returns the registers holding addresses that are used other than
// as the address of a load or store or the base of a further field or
// element step.
func (h *heapOnly) addrUses(f *eir.Func) map[eir.Reg]bool {
	out := map[eir.Reg]bool{}
	mark := func(r eir.Reg) {
		if r == 0 || int(r) >= len(h.defs[f.ID]) {
			return
		}
		if d := h.defs[f.ID][r]; d.in != nil && d.res < 0 {
			switch d.in.Op {
			case eir.OpField, eir.OpElem, eir.OpAddr:
				out[r] = true
			}
		}
	}
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			in := &b.Instrs[i]
			switch in.Op {
			case eir.OpLoad:
			case eir.OpStore:
				mark(in.Y)
			case eir.OpField, eir.OpElem:
			default:
				mark(in.X)
				mark(in.Y)
				for _, a := range in.Args {
					mark(a)
				}
				if in.Call != nil {
					mark(in.Call.Target)
					for _, a := range in.Call.Args {
						mark(a)
					}
				}
			}
		}
	}
	return out
}

// escape marks the classes along the address in register r as escaped. An
// address that leads into a field through a copy or phi escapes too: copies
// and phis of addresses are uses addrUses marks.
func (h *heapOnly) escape(f *eir.Func, r eir.Reg) {
	l := h.addrLoc(f, r, 0)
	if l.base == nil {
		if l.fieldStep {
			h.poison = true // a field of an unknown struct escapes
		}
		return
	}
	for _, k := range h.classesAlong(l.base, l.steps) {
		h.escaped[k] = true
	}
	if t := walkPath(l.base, l.steps); t != nil {
		h.escapeWithin(t, 0)
	}
}

// escapeWithin marks every class within type t as escaped: a pointer to a
// whole struct is a way to the addresses of its fields.
func (h *heapOnly) escapeWithin(t types2.Type, depth int) {
	if depth > 16 || t == nil {
		return
	}
	types, ok := h.expand(t)
	if !ok {
		h.poison = true
		return
	}
	for _, t := range types {
		switch u := t.Underlying().(type) {
		case *types2.Struct:
			for i := 0; i < u.NumFields(); i++ {
				h.escaped[h.classKey(u, i)] = true
				h.escapeWithin(u.Field(i).Type(), depth+1)
			}
		case *types2.Array:
			h.escapeWithin(u.Elem(), depth+1)
		}
	}
}

// store records the requirements of a store.
func (h *heapOnly) store(st *originState, in *eir.Instr) {
	f := st.f
	l := h.addrLoc(f, in.X, 0)
	L := in.Type
	if l.base != nil {
		if L == nil {
			L = walkPath(l.base, l.steps)
		}
		atoms, _ := h.origin(st, in.Y, nil)
		for _, k := range h.classesAlong(l.base, l.steps) {
			h.require(h.node(k), atoms)
		}
	} else {
		if L == nil {
			L = pointee(h.regType(f, in.X, 0))
		}
		if L == nil {
			h.poison = true
			return
		}
		h.unknownStore(st, L, in.Y, l.fieldStep)
	}
	if L != nil {
		h.within(st, L, in.Y, nil)
	}
}

// unknownStore records a store of a value of type L through an address of
// unknown place.
func (h *heapOnly) unknownStore(st *originState, L types2.Type, y eir.Reg, all bool) {
	types, ok := h.expand(L)
	if !ok {
		h.poison = true
		return
	}
	atoms, _ := h.origin(st, y, nil)
	for _, t := range types {
		kind := hoLStore
		if all {
			kind = hoLStoreAll
		}
		id := h.tt.intern(t)
		if all {
			h.lAll[id] = true
		} else {
			h.lstores[id] = true
		}
		h.require(h.node(hoKey{kind: kind, a: int32(id)}), atoms)
	}
}

// within records that the value in register r (at path) is written into a
// location of type t: every class within t gets the pointers at the
// matching path.
func (h *heapOnly) within(st *originState, t types2.Type, r eir.Reg, path []int) {
	types, ok := h.expand(t)
	if !ok {
		h.poison = true
		return
	}
	for _, t := range types {
		h.within1(st, t, r, path, 0)
	}
}

func (h *heapOnly) within1(st *originState, t types2.Type, r eir.Reg, path []int, depth int) {
	if depth > 16 {
		h.poison = true
		return
	}
	switch u := t.Underlying().(type) {
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			ft := u.Field(i).Type()
			if !mayHoldPointers(ft) {
				continue
			}
			p := concat(path, []int{i})
			atoms, _ := h.origin(st, r, p)
			h.require(h.node(h.classKey(u, i)), atoms)
			h.within1(st, ft, r, p, depth+1)
		}
	case *types2.Array:
		if mayHoldPointers(u.Elem()) {
			h.within1(st, u.Elem(), r, concat(path, []int{eir.ElemStep}), depth+1)
		}
	case *types2.TypeParam:
		types, ok := h.inst.instantiated(u)
		if !ok {
			h.poison = true
			return
		}
		for _, s := range types {
			h.within1(st, s, r, path, depth+1)
		}
	}
}

// mayHoldPointers reports whether a value of type t may hold a pointer.
func mayHoldPointers(t types2.Type) bool {
	switch u := t.Underlying().(type) {
	case *types2.Basic:
		return u.Kind() == types2.String || u.Kind() == types2.UnsafePointer || u.Kind() == types2.UntypedNil
	case *types2.Packed:
		return false
	case *types2.Array:
		return mayHoldPointers(u.Elem())
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if mayHoldPointers(u.Field(i).Type()) {
				return true
			}
		}
		return false
	}
	return true
}

// opaqueCall records what a call whose effects are not code the analysis
// sees may write: a contract-table entry or a bodyless function.
func (h *heapOnly) opaqueCall(st *originState, c *eir.Call) {
	opaque := c.Kind == eir.CallContract
	if c.Kind == eir.CallStatic && c.Callee != eir.NoFunc && int(c.Callee) < len(h.p.Funcs) && h.p.Funcs[c.Callee].Bodyless {
		opaque = true
	}
	if !opaque {
		return
	}
	for _, a := range c.Args {
		t := h.regType(st.f, a, 0)
		if t == nil {
			h.poison = true
			return
		}
		if _, isStruct := t.Underlying().(*types2.Struct); isStruct {
			h.within(st, t, a, nil)
			continue
		}
		// The callee may store the value into a location of its type.
		h.unknownStore(st, t, a, false)
		// Through a pointer or slice it may reach the fields of the
		// structs it points to, and store any value of a non-struct
		// element type there.
		var elem types2.Type
		switch u := t.Underlying().(type) {
		case *types2.Pointer:
			elem = u.Elem()
		case *types2.Slice:
			elem = u.Elem()
		}
		if elem == nil {
			continue
		}
		h.escapeWithin(elem, 0)
		if _, isStruct := elem.Underlying().(*types2.Struct); !isStruct && mayHoldPointers(elem) {
			types, ok := h.expand(elem)
			if !ok {
				h.poison = true
				return
			}
			for _, et := range types {
				id := h.tt.intern(et)
				h.lstores[id] = true
				h.require(h.node(hoKey{kind: hoLStore, a: int32(id)}), []hoAtom{hoUnknown})
			}
		}
	}
}

// expandNodes collects the requirements of input and result nodes, which
// may make further nodes, until none is left.
func (h *heapOnly) expandNodes() {
	sts := map[eir.FuncID]*originState{}
	state := func(f *eir.Func) *originState {
		st := sts[f.ID]
		if st == nil {
			st = &originState{f: f, memo: map[originKey][]hoAtom{}, onStack: map[originKey]bool{}}
			sts[f.ID] = st
		}
		return st
	}
	for i := 0; i < len(h.keys); i++ {
		if h.done[i] {
			continue
		}
		h.done[i] = true
		k := h.keys[i]
		switch k.kind {
		case hoParam:
			path := decPath(k.c)
			for _, s := range h.staticS[k.a] {
				var atoms []hoAtom
				if int(k.b) < len(s.call.Args) {
					atoms, _ = h.origin(state(s.f), s.call.Args[k.b], path)
				} else {
					atoms = []hoAtom{hoUnknown}
				}
				h.require(hoAtom(i), atoms)
			}
		case hoResult:
			path := decPath(k.c)
			for _, r := range h.returns[k.a] {
				if r.in.Index == int(k.b) {
					atoms, _ := h.origin(state(r.f), r.in.X, path)
					h.require(hoAtom(i), atoms)
				}
			}
		}
	}
}

// --- grouping and the fixed point ----------------------------------------

func (h *heapOnly) find(x int32) int32 {
	for h.uf[x] != x {
		h.uf[x] = h.uf[h.uf[x]]
		x = h.uf[x]
	}
	return x
}

// groupStructs puts every generic struct in one group with each struct it
// may instantiate to.
func (h *heapOnly) groupStructs() {
	var structs []typeID
	seen := map[typeID]bool{}
	for _, k := range h.keys {
		if k.kind == hoClass && !seen[typeID(k.a)] {
			seen[typeID(k.a)] = true
			structs = append(structs, typeID(k.a))
		}
	}
	for k := range h.escaped {
		if !seen[typeID(k.a)] {
			seen[typeID(k.a)] = true
			structs = append(structs, typeID(k.a))
		}
	}
	h.uf = make([]int32, len(structs))
	for i, s := range structs {
		h.uf[i] = int32(i)
		h.structGroup[s] = int32(i)
	}
	for i, a := range structs {
		if !h.tt.hasTypeParam(h.tt.types[a]) {
			continue
		}
		for j, b := range structs {
			if i != j && h.tt.mayMatch(h.tt.types[a], h.tt.types[b]) {
				h.uf[h.find(int32(i))] = h.find(int32(j))
			}
		}
	}
}

// solve computes the greatest fixed point: every node holds unless an
// unknown source or a node that does not hold reaches it. Class nodes of
// one group, and escaped classes with the unknown-address stores of the
// types they hold, are tied together first.
func (h *heapOnly) solve() {
	n := len(h.keys)
	// Classes of one group share their fate: tie each to its group's
	// representative both ways.
	groupRep := map[[2]int32]hoAtom{}
	for i := 0; i < n; i++ {
		k := h.keys[i]
		if k.kind != hoClass {
			continue
		}
		g := [2]int32{h.find(h.structGroup[typeID(k.a)]), k.b}
		if rep, ok := groupRep[g]; ok {
			h.reqs[rep] = append(h.reqs[rep], hoAtom(i))
			h.reqs[i] = append(h.reqs[i], rep)
		} else {
			groupRep[g] = hoAtom(i)
		}
	}
	// Stores through unknown addresses reach escaped classes (and, with an
	// unknown container, every class) of the types they store.
	for i := 0; i < n; i++ {
		k := h.keys[i]
		if k.kind != hoClass {
			continue
		}
		s, _ := h.tt.types[k.a].(*types2.Struct)
		if s == nil || int(k.b) >= s.NumFields() {
			continue
		}
		ft := s.Field(int(k.b)).Type()
		esc := h.escaped[k]
		for _, L := range h.byValue(ft) {
			id, ok := h.tt.lookup(L)
			if !ok {
				continue
			}
			if esc && h.lstores[id] {
				h.reqs[i] = append(h.reqs[i], h.index[hoKey{kind: hoLStore, a: int32(id)}])
			}
			if h.lAll[id] {
				h.reqs[i] = append(h.reqs[i], h.index[hoKey{kind: hoLStoreAll, a: int32(id)}])
			}
		}
	}
	h.val = make([]bool, n)
	rdeps := make([][]hoAtom, n)
	var work []hoAtom
	for i := 0; i < n; i++ {
		h.val[i] = true
		for _, a := range h.reqs[i] {
			if a == hoUnknown {
				h.val[i] = false
			} else {
				rdeps[a] = append(rdeps[a], hoAtom(i))
			}
		}
		if !h.val[i] {
			work = append(work, hoAtom(i))
		}
	}
	for len(work) > 0 {
		a := work[len(work)-1]
		work = work[:len(work)-1]
		for _, b := range rdeps[a] {
			if h.val[b] {
				h.val[b] = false
				work = append(work, b)
			}
		}
	}
}

// byValue returns t and the types held by value within it (struct fields,
// array elements), expanded over instantiations.
func (h *heapOnly) byValue(t types2.Type) []types2.Type {
	var out []types2.Type
	var rec func(t types2.Type, depth int)
	rec = func(t types2.Type, depth int) {
		if t == nil || depth > 16 {
			return
		}
		out = append(out, t)
		switch u := t.Underlying().(type) {
		case *types2.Struct:
			for i := 0; i < u.NumFields(); i++ {
				rec(u.Field(i).Type(), depth+1)
			}
		case *types2.Array:
			rec(u.Elem(), depth+1)
		}
	}
	types, ok := h.expand(t)
	if !ok {
		return []types2.Type{t}
	}
	for _, t := range types {
		rec(t, 0)
	}
	return out
}

// heapOnly answers Facts.HeapOnly.
func (h *heapOnly) heapOnly(T types2.Type, field int) bool {
	if h.poison || T == nil {
		return false
	}
	types, ok := h.expand(T)
	if !ok {
		return false
	}
	for _, t := range types {
		s, isStruct := t.Underlying().(*types2.Struct)
		if !isStruct || field < 0 || field >= s.NumFields() {
			return false
		}
		if !h.classHolds(s, field) {
			return false
		}
	}
	return true
}

// classHolds reports whether class (s, field) is heap-only, together with
// every class of a struct it may be an instance of or instantiate to.
func (h *heapOnly) classHolds(s *types2.Struct, field int) bool {
	key := hoKey{kind: hoClass, a: int32(h.tt.intern(s)), b: int32(field)}
	if v, ok := h.memo[key]; ok {
		return v
	}
	v := h.classHolds1(s, field, key)
	h.memo[key] = v
	return v
}

func (h *heapOnly) classHolds1(s *types2.Struct, field int, key hoKey) bool {
	if a, ok := h.index[key]; ok && !h.val[a] {
		return false
	}
	ft := s.Field(field).Type()
	// Stores through unknown addresses: those of unknown container reach
	// every class, those of unknown address the escaped ones. (For a class
	// with a node, solve has tied them in already.)
	if !h.lstoresHold(ft, h.escaped[key]) {
		return false
	}
	// Generic structs and their instances share their classes.
	generic := h.tt.hasTypeParam(s)
	for k, a := range h.index {
		if k.kind != hoClass || k.b != key.b || k.a == key.a {
			continue
		}
		other := h.tt.types[k.a]
		if (generic || h.tt.hasTypeParam(other)) && h.tt.mayMatch(other, s) && !h.val[a] {
			return false
		}
	}
	return true
}

// lstoresHold reports whether the unknown-address stores of the types held
// within ft hold: those of unknown container always, those of unknown
// address when the class is escaped.
func (h *heapOnly) lstoresHold(ft types2.Type, escaped bool) bool {
	for _, L := range h.byValue(ft) {
		id, ok := h.tt.lookup(L)
		if !ok {
			continue
		}
		if a, ok := h.index[hoKey{kind: hoLStoreAll, a: int32(id)}]; ok && !h.val[a] {
			return false
		}
		if escaped {
			if a, ok := h.index[hoKey{kind: hoLStore, a: int32(id)}]; ok && !h.val[a] {
				return false
			}
		}
	}
	return true
}
