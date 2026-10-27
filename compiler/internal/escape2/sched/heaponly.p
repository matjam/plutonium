// Translated by go2pu from internal/escape2/sched/heaponly.go at the go-cutover tag; edited by hand since.

package sched

import (
	"compiler/compat"
	"compiler/internal/escape2/contract"
	"compiler/internal/escape2/eir"
	"compiler/internal/types2"
	"compiler/pu"
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

	// For explanations (explain): the write being collected, the first
	// write that gave a node an unknown source, and the node whose
	// withdrawal withdrew each node.
	curF     *eir.Func
	curIn    *eir.Instr
	unkAt    map[hoAtom]hoWrite
	escAt    map[hoKey]hoWrite // explain: where each class first escaped
	poisonAt hoWrite
	causes   []hoAtom

	structGroup map[typeID]int32                       // struct typeID to group (union-find root)
	memo        map[hoKey]bool                         // classHolds answers
	vars        map[eir.FuncID]map[eir.ObjID][]eir.Reg // varStores, by function: the values stored into each plain variable
	rtypes      map[regKey]types2.Type
	rbusy       map[regKey]bool
	uf          []int32
}

// isUnsafePointer reports whether t is unsafe.Pointer.
func isUnsafePointer(t types2.Type) bool {
	b, ok := types2.Unalias(t).Underlying().(*types2.Basic)
	return ok && b.Kind() == types2.UnsafePointer
}

// setPoison records a write of a type that cannot be bounded: nothing is
// heap-only (and, for explanations, where).
func (h *heapOnly) setPoison() {
	if !h.poison && h.curF != nil {
		h.poisonAt = hoWrite{h.curF, h.curIn}
	}
	h.poison = true
}

// hoWrite is a write, for explanations.
type hoWrite struct {
	f  *eir.Func
	in *eir.Instr
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
	h := pu.New(heapOnly{
		p: p, tt: tt, inst: inst, cands: c,
		dynS:        map[eir.FuncID]bool{},
		index:       map[hoKey]hoAtom{},
		escaped:     map[hoKey]bool{},
		lstores:     map[typeID]bool{},
		lAll:        map[typeID]bool{},
		structGroup: map[typeID]int32{},
		memo:        map[hoKey]bool{},
		rtypes:      map[regKey]types2.Type{},
		rbusy:       map[regKey]bool{},
	})
	h.prepare()
	h.collectWrites()
	h.groupStructs()
	h.solve()
	return h
}

// prepare indexes definitions, call sites and returns.
func (h *heapOnly) prepare() {
	n := len(h.p.Funcs)
	h.defs = compat.Make[[]hoDef](n)
	h.staticS = compat.Make[[]hoSite](n)
	h.returns = compat.Make[[]hoReturn](n)
	for _, id := range h.cands.funcValues {
		h.dynS[id] = true
	}
	for _, f := range h.p.Funcs {
		defs := compat.Make[hoDef](f.NumRegs + 1)
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
							h.staticS[c.Callee] = compat.Append(h.staticS[c.Callee], hoSite{f, c})
						}
					case eir.CallIface, eir.CallTypeParam, eir.CallFunc:
						for _, id := range h.cands.of(f, c) {
							h.dynS[id] = true
						}
					}
				case eir.OpSink:
					if in.Sink == eir.SinkReturn {
						h.returns[f.ID] = compat.Append(h.returns[f.ID], hoReturn{f, in})
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
	h.keys = compat.Append(h.keys, k)
	h.index[k] = a
	h.reqs = compat.Append(h.reqs, nil)
	h.done = compat.AppendPlain(h.done, false)
	return a
}

func (h *heapOnly) require(target hoAtom, atoms []hoAtom) {
	h.reqs[target] = compat.AppendPlain(h.reqs[target], atoms...)
	for _, a := range atoms {
		if a == hoUnknown {
			if _, ok := h.unkAt[target]; !ok && h.curF != nil {
				if h.unkAt == nil {
					h.unkAt = map[hoAtom]hoWrite{}
				}
				h.unkAt[target] = hoWrite{h.curF, h.curIn}
			}
			break
		}
	}
}

// classKey is the key of field i of struct type s (its underlying struct).
func (h *heapOnly) classKey(s *types2.Struct, i int) hoKey {
	return hoKey{kind: hoClass, a: int32(h.tt.intern(s)), b: int32(i)}
}

// --- register types -------------------------------------------------------

// regType infers the type of register r of f, or nil. Each call is one
// query: it explores every register at most once (rbusy), so it is linear
// in the function however its phis join (a loop's phis, diamonds of them),
// and a register met again on a cycle has no answer on that path, so a
// phi takes the type of another argument. Types found are memoized, and so
// is the query's own answer, found or not.
func (h *heapOnly) regType(f *eir.Func, r eir.Reg) types2.Type {
	clear(h.rbusy)
	t := h.regTypeRec(f, r, 0)
	if r != 0 && int(r) < len(h.defs[f.ID]) {
		h.rtypes[regKey{f.ID, r}] = t
	}
	return t
}

func (h *heapOnly) regTypeRec(f *eir.Func, r eir.Reg, depth int) types2.Type {
	if r == 0 || int(r) >= len(h.defs[f.ID]) || depth > 64 {
		return nil
	}
	k := regKey{f.ID, r}
	if t, ok := h.rtypes[k]; ok {
		return t
	}
	if h.rbusy[k] {
		return nil
	}
	h.rbusy[k] = true
	t := h.regType1(f, r, depth)
	if t != nil {
		h.rtypes[k] = t
	}
	return t
}

type regKey struct {
	f eir.FuncID
	r eir.Reg
}

func (h *heapOnly) regType1(f *eir.Func, r eir.Reg, depth int) types2.Type {
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
			// The type of what X points to (eir.OpField).
			if steps, any := splitAny(in.Path); !any {
				if t := walkPath(in.Type, steps); t != nil {
					return types2.NewPointer(t)
				}
			}
			return nil
		}
		if pt := pointee(h.regTypeRec(f, in.X, depth+1)); pt != nil {
			if t := walkPath(pt, in.Path); t != nil {
				return types2.NewPointer(t)
			}
		}
	case eir.OpElem:
		if in.Type != nil {
			return in.Type
		}
		if t := elemOf(pointee(h.regTypeRec(f, in.X, depth+1))); t != nil {
			return types2.NewPointer(t)
		}
		if t := elemOf(h.regTypeRec(f, in.X, depth+1)); t != nil {
			return types2.NewPointer(t)
		}
	case eir.OpSelect:
		return walkPath(h.regTypeRec(f, in.X, depth+1), in.Path)
	case eir.OpCopy:
		return h.regTypeRec(f, in.X, depth+1)
	case eir.OpConv:
		return in.To
	case eir.OpClosure:
		// A func value: its captures are written into its record when it
		// is made (recordWrite), and a func type holds no struct field.
		if in.Obj != eir.NoObj && int(in.Obj) < len(f.Objects) && f.Objects[in.Obj].Type != nil {
			return f.Objects[in.Obj].Type
		}
		if int(in.Callee) < len(h.p.Funcs) && h.p.Funcs[in.Callee].Sig != nil {
			return h.p.Funcs[in.Callee].Sig
		}
	case eir.OpPhi:
		scalar := true
		for _, a := range in.Args {
			if a != 0 {
				scalar = false
			}
			if t := h.regTypeRec(f, a, depth+1); t != nil {
				return t
			}
		}
		if scalar && len(in.Args) > 0 {
			// A join of values that hold no pointers (register 0): any
			// pointer-free type says what it may write.
			return types2.Typ[types2.Uintptr]
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
	// any: the place is anywhere in the object at steps from base, at an
	// offset not known (eir.AnyStep: unsafe.Add, an unsafe.Pointer
	// converted to another pointer type). steps stop before the AnyStep.
	any bool
}

// phiLoc is addrLoc of phi r: the place of the definitions it joins
// (through other phis, a loop's included), when they all name the same one;
// unknown otherwise.
func (h *heapOnly) phiLoc(f *eir.Func, r eir.Reg, depth int) loc {
	seen := map[eir.Reg]bool{}
	var leaves []eir.Reg
	var walk func(x eir.Reg) bool
	walk = func(x eir.Reg) bool {
		if seen[x] {
			return true
		}
		seen[x] = true
		if x == 0 || int(x) >= len(h.defs[f.ID]) || len(seen) > 64 {
			return false
		}
		d := h.defs[f.ID][x]
		if d.in != nil && d.res < 0 && d.in.Op == eir.OpPhi {
			for _, a := range d.in.Args {
				if !walk(a) {
					return false
				}
			}
			return true
		}
		leaves = compat.AppendPlain(leaves, x)
		return true
	}
	if !walk(r) || len(leaves) == 0 {
		return loc{}
	}
	first := h.addrLoc(f, leaves[0], depth+1)
	for _, x := range leaves[1:] {
		if l := h.addrLoc(f, x, depth+1); !sameLoc(l, first) {
			return loc{}
		}
	}
	return first
}

// sameLoc reports whether a and b name the same place.
func sameLoc(a, b loc) bool {
	if a.any != b.any || a.fieldStep != b.fieldStep || len(a.steps) != len(b.steps) {
		return false
	}
	if (a.base == nil) != (b.base == nil) || a.base != nil && !types2.Identical(a.base, b.base) {
		return false
	}
	for i := range a.steps {
		if a.steps[i] != b.steps[i] {
			return false
		}
	}
	return true
}

// splitAny returns path up to its first eir.AnyStep, and whether it has one.
func splitAny(path []int) ([]int, bool) {
	for i, st := range path {
		if st == eir.AnyStep {
			return path[:i], true
		}
	}
	return path, false
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
			steps, any := splitAny(in.Path)
			return loc{base: f.Objects[in.Obj].Type, steps: steps, any: any}
		}
	case eir.OpField:
		l := h.addrLoc(f, in.X, depth+1)
		if l.any {
			return l // anywhere in the object stays anywhere in it
		}
		steps, any := splitAny(in.Path)
		if l.base == nil {
			if any && pointee(h.regType(f, in.X)) == nil {
				// Memory reinterpreted with unsafe.Pointer, of no type
				// known: its writes stay outside the rule (sections 18.3
				// and 18.12), and so do the places under it.
				return loc{any: true}
			}
			if pt := pointee(h.regType(f, in.X)); pt != nil {
				return loc{base: pt, steps: steps, any: any}
			}
			// (An unknown offset, any, is memory reinterpreted with
			// unsafe.Pointer, whose writes stay outside the rule,
			// sections 18.3 and 18.12: no field step of a struct.)
			return loc{fieldStep: l.fieldStep || hasField(steps)}
		}
		return loc{base: l.base, steps: concat(l.steps, steps), any: any}
	case eir.OpElem:
		l := h.addrLoc(f, in.X, depth+1)
		if l.any {
			return l
		}
		if l.base != nil {
			if _, isArr := walkPath(l.base, l.steps).Underlying().(*types2.Array); isArr {
				return loc{base: l.base, steps: concat(l.steps, []int{eir.ElemStep})}
			}
		}
		// An element of a slice, or of an array of unknown place: a
		// location of the element type, in no struct we can name.
		t := elemOf(pointee(h.regType(f, in.X)))
		if t == nil {
			t = elemOf(h.regType(f, in.X))
		}
		return loc{base: t, fieldStep: l.fieldStep}
	case eir.OpCopy:
		return h.addrLoc(f, in.X, depth+1)
	case eir.OpPhi:
		return h.phiLoc(f, r, depth)
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
	out := compat.MakeCap[int](0, len(a)+len(b))
	return compat.AppendPlain(compat.AppendPlain(out, a...), b...)
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
			out = compat.Append(out, h.classKey(u, s))
			t = u.Field(s).Type()
		case *types2.Array:
			if s != eir.ElemStep {
				return out
			}
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
		return compat.Clone([]types2.Type{t}), true
	}
	if tp, ok := types2.Unalias(t).(*types2.TypeParam); ok {
		return h.inst.instantiated(tp)
	}
	// A generic struct or composite: its own classes are grouped with
	// those of its instantiations (groupStructs).
	return compat.Clone([]types2.Type{t}), true
}

// --- origins --------------------------------------------------------------

type originKey struct {
	r    eir.Reg
	path string
}

// originState is the per-function state of origin: memoized answers, and
// Tarjan's stack over the (register, path) pairs being computed. Registers
// depend on one another in cycles through phis; the pairs of one cycle have
// the same origin, so when the head of a cycle is done every pair of it is
// memoized with the head's answer, and nothing is computed twice.
type originState struct {
	f     *eir.Func
	memo  map[originKey][]hoAtom
	pos   map[originKey]int // position on stack, for the pairs on it
	stack []originKey
	low   int // the lowest stack position a pending cycle reaches
}

func newOriginState(f *eir.Func) *originState {
	return pu.New(originState{f: f, memo: map[originKey][]hoAtom{}, pos: map[originKey]int{}, low: maxLow})
}

const maxLow = int(^uint(0) >> 1)

func encPath(path []int) string {
	b := compat.MakeCap[byte](0, len(path)*2)
	for _, s := range path {
		b = compat.AppendPlain(b, byte(s+2), byte((s+2)>>8))
	}
	return compat.String(b)
}

func decPath(s string) []int {
	out := compat.MakeCap[int](0, len(s)/2)
	for i := 0; i+1 < len(s); i += 2 {
		out = compat.AppendPlain(out, int(s[i])|int(s[i+1])<<8-2)
	}
	return out
}

// origin returns the atoms the pointers within register r's value at path
// come from. complete is false for a pair inside a cycle whose head is still
// being computed: its answer is then part of the head's, which the head
// memoizes for every pair of the cycle.
func (h *heapOnly) origin(st *originState, r eir.Reg, path []int) (atoms []hoAtom, complete bool) {
	f := st.f
	if r == 0 {
		return nil, true // no pointer: nil, a constant (eir registers)
	}
	if int(r) >= len(h.defs[f.ID]) {
		return compat.Clone([]hoAtom{hoUnknown}), true
	}
	k := originKey{r, encPath(path)}
	if a, ok := st.memo[k]; ok {
		return a, true
	}
	if p, ok := st.pos[k]; ok {
		if p < st.low {
			st.low = p
		}
		return nil, false
	}
	d := len(st.stack)
	st.stack = compat.Append(st.stack, k)
	st.pos[k] = d
	outer := st.low
	st.low = maxLow
	atoms, _ = h.origin1(st, r, path)
	atoms = dedupAtoms(atoms)
	low := st.low
	if low >= d {
		// k heads its cycle (or has none): its answer is every pair's.
		for _, m := range st.stack[d:] {
			st.memo[m] = atoms
			delete(st.pos, m)
		}
		st.stack = st.stack[:d]
		st.low = outer
		return atoms, true
	}
	if low < outer {
		outer = low
	}
	st.low = outer
	return atoms, false
}

func dedupAtoms(a []hoAtom) []hoAtom {
	if len(a) < 2 {
		return a
	}
	seen := make(map[hoAtom]bool, len(a))
	out := a[:0:0]
	for _, x := range a {
		if !seen[x] {
			seen[x] = true
			out = compat.AppendPlain(out, x)
		}
	}
	return out
}

func (h *heapOnly) origin1(st *originState, r eir.Reg, path []int) ([]hoAtom, bool) {
	f := st.f
	d := h.defs[f.ID][r]
	in := d.in
	if in == nil {
		return compat.Clone([]hoAtom{hoUnknown}), true
	}
	if d.res >= 0 {
		return h.resultOrigin(st, in.Call, d.res, path), true
	}
	switch in.Op {
	case eir.OpHeap:
		return nil, true
	case eir.OpInput:
		if in.Index < len(f.Inputs) && f.Inputs[in.Index].Var != nil && h.paramsKnown(f.ID) {
			return compat.Clone([]hoAtom{h.node(hoKey{kind: hoParam, a: int32(f.ID), b: int32(in.Index), c: encPath(path)})}), true
		}
		return compat.Clone([]hoAtom{hoUnknown}), true
	case eir.OpLoad:
		if vals, ok := h.varStores(f, in.X); ok {
			// A variable of the frame only loaded and stored: what was
			// stored into it.
			var out []hoAtom
			complete := true
			for _, y := range vals {
				x, c := h.origin(st, y, path)
				out, complete = compat.AppendPlain(out, x...), complete && c
			}
			return out, complete
		}
		return h.loadOrigin(f, in, path), true
	case eir.OpField, eir.OpElem:
		if len(path) != 0 {
			return compat.Clone([]hoAtom{hoUnknown}), true
		}
		return h.origin(st, in.X, nil)
	case eir.OpSelect:
		return h.origin(st, in.X, concat(in.Path, path))
	case eir.OpCopy:
		return h.origin(st, in.X, path)
	case eir.OpConv:
		if in.Obj != eir.NoObj {
			return compat.Clone([]hoAtom{hoUnknown}), true // boxed in the frame
		}
		return h.origin(st, in.X, path)
	case eir.OpAssert:
		if len(path) == 0 && isDirect(in.Type) {
			return h.origin(st, in.X, nil)
		}
		return compat.Clone([]hoAtom{hoUnknown}), true
	case eir.OpCompose:
		var out []hoAtom
		complete := true
		for j, a := range in.Args {
			if j >= len(in.Fields) {
				return compat.Clone([]hoAtom{hoUnknown}), true
			}
			fp := in.Fields[j]
			switch {
			case prefixOf(path, fp): // the arg is within the part asked for
				x, c := h.origin(st, a, nil)
				out, complete = compat.AppendPlain(out, x...), complete && c
			case prefixOf(fp, path): // the part asked for is within the arg
				x, c := h.origin(st, a, path[len(fp):])
				out, complete = compat.AppendPlain(out, x...), complete && c
			}
		}
		return out, complete
	case eir.OpPhi:
		var out []hoAtom
		complete := true
		for _, a := range in.Args {
			x, c := h.origin(st, a, path)
			out, complete = compat.AppendPlain(out, x...), complete && c
		}
		return out, complete
	case eir.OpRecount, eir.OpLend:
		// What X holds: a counted copy of it, or X lent to a call.
		return h.origin(st, in.X, path)
	case eir.OpArenaView, eir.OpBorrow:
		// The memory of an allocator X names (eir/arena.go), or a view
		// of a counted box's payload that its holder bounds
		// (eir/borrow.go): not heap memory that outlives every frame,
		// and a load of a heap-only field would lose the bound.
		return compat.Clone([]hoAtom{hoUnknown}), true
	}
	// OpAddr (a frame address), closures and method values (records in
	// the frame), and anything else.
	return compat.Clone([]hoAtom{hoUnknown}), true
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
// contractResult is the origin of result j of contract call c, at path:
// what its entry says the result holds (an argument, or what one points
// to), or unknown.
func (h *heapOnly) contractResult(st *originState, c *eir.Call, j int, path []int) []hoAtom {
	f := st.f
	e, ok := contract.Lookup(c.Contract)
	if !ok || j >= len(e.Effect.Results) || len(e.Effect.Results[j]) == 0 {
		return compat.Clone([]hoAtom{hoUnknown})
	}
	var T types2.Type
	if s := c.Sig; s != nil && j < s.Results().Len() {
		T = s.Results().At(j).Type()
	}
	var out []hoAtom
	for _, ref := range e.Effect.Results[j] {
		if ref.Arg < 0 || ref.Arg >= len(c.Args) || c.Args[ref.Arg] == 0 {
			return compat.Clone([]hoAtom{hoUnknown})
		}
		a := c.Args[ref.Arg]
		switch ref.Deref {
		case 0:
			x, _ := h.origin(st, a, path)
			out = compat.AppendPlain(out, x...)
		case 1:
			out = compat.AppendPlain(out, h.loadOrigin(f, &eir.Instr{Op: eir.OpLoad, X: a, Type: T}, path)...)
		default:
			return compat.Clone([]hoAtom{hoUnknown})
		}
	}
	return out
}

func (h *heapOnly) loadOrigin(f *eir.Func, in *eir.Instr, path []int) []hoAtom {
	l := h.addrLoc(f, in.X, 0)
	if l.any {
		l = loc{} // anywhere in the object: no class the load is from
	}
	var cls []hoKey
	if l.base != nil {
		cls = h.classesAlong(l.base, concat(l.steps, path))
	} else if in.Type != nil {
		// The place is unknown, but the value is of type in.Type: the
		// pointers at path within it are in the classes of that type.
		types, ok := h.expand(in.Type)
		if !ok {
			return compat.Clone([]hoAtom{hoUnknown})
		}
		var atoms []hoAtom
		for _, t := range types {
			c := h.classesAlong(t, path)
			if len(c) == 0 {
				return compat.Clone([]hoAtom{hoUnknown})
			}
			atoms = compat.AppendPlain(atoms, h.node(c[len(c)-1]))
		}
		return atoms
	}
	if len(cls) == 0 {
		return compat.Clone([]hoAtom{hoUnknown})
	}
	// Any one class the pointer is in will do: the deepest.
	return compat.Clone([]hoAtom{h.node(cls[len(cls)-1])})
}

// varStores returns, when address x is that of a plain variable of f (an
// ObjVar, ObjTemp or ObjParam of the frame whose every address is taken at one path
// and only loaded from and stored through), the values stored into it.
func (h *heapOnly) varStores(f *eir.Func, x eir.Reg) ([]eir.Reg, bool) {
	if x == 0 || int(x) >= len(h.defs[f.ID]) {
		return nil, false
	}
	d := h.defs[f.ID][x]
	if d.in == nil || d.res >= 0 || d.in.Op != eir.OpAddr || int(d.in.Obj) >= len(f.Objects) {
		return nil, false
	}
	if h.vars == nil {
		h.vars = map[eir.FuncID]map[eir.ObjID][]eir.Reg{}
	}
	vs, ok := h.vars[f.ID]
	if !ok {
		vs = h.plainVars(f)
		h.vars[f.ID] = vs
	}
	vals, ok := vs[d.in.Obj]
	return vals, ok
}

// plainVars finds the plain variables of f (varStores) and what is stored
// into each.
func (h *heapOnly) plainVars(f *eir.Func) map[eir.ObjID][]eir.Reg {
	defs := h.defs[f.ID]
	addrOf := func(r eir.Reg) (eir.ObjID, bool) {
		if r == 0 || int(r) >= len(defs) || defs[r].in == nil || defs[r].res >= 0 || defs[r].in.Op != eir.OpAddr {
			return 0, false
		}
		return defs[r].in.Obj, true
	}
	// Addresses within a variable (field and element steps from its
	// address, an unsafe reinterpretation of it): only loaded through, they
	// leave it plain; a store through one does not.
	derived := map[eir.Reg]eir.ObjID{}
	for changed := true; changed; {
		changed = false
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				in := &b.Instrs[i]
				if in.Op != eir.OpField && in.Op != eir.OpElem || in.Dst == 0 {
					continue
				}
				if _, done := derived[in.Dst]; done {
					continue
				}
				o, ok := addrOf(in.X)
				if !ok {
					o, ok = derived[in.X]
				}
				if ok {
					derived[in.Dst] = o
					changed = true
				}
			}
		}
	}
	bad := map[eir.ObjID]bool{}
	path := map[eir.ObjID][]int{}
	stores := map[eir.ObjID][]eir.Reg{}
	for o := range f.Objects {
		switch f.Objects[o].Kind {
		case eir.ObjVar, eir.ObjTemp, eir.ObjParam:
			if f.Objects[o].Life == eir.LifeHeap || f.Objects[o].Life == eir.LifeCaller {
				bad[eir.ObjID(o)] = true
			}
		default:
			bad[eir.ObjID(o)] = true
		}
	}
	use := func(r eir.Reg) {
		if o, ok := addrOf(r); ok {
			bad[o] = true // an address used another way
		}
		if o, ok := derived[r]; ok {
			bad[o] = true
		}
	}
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			in := &b.Instrs[i]
			switch in.Op {
			case eir.OpAddr:
				if int(in.Obj) >= len(f.Objects) {
					continue
				}
				if p, seen := path[in.Obj]; seen && !slicesEqual(p, in.Path) {
					bad[in.Obj] = true
				}
				path[in.Obj] = in.Path
			case eir.OpLoad:
				// the address of a load: a plain use
			case eir.OpStore:
				if o, ok := addrOf(in.X); ok {
					stores[o] = compat.AppendPlain(stores[o], in.Y)
				}
				if o, ok := derived[in.X]; ok {
					bad[o] = true // a store of a part
				}
				use(in.Y)
			case eir.OpField, eir.OpElem:
				if _, ok := derived[in.Dst]; !ok {
					use(in.X)
				}
				use(in.Y)
				for _, a := range in.Args {
					use(a)
				}
			default:
				use(in.X)
				use(in.Y)
				for _, a := range in.Args {
					use(a)
				}
				if c := in.Call; c != nil {
					use(c.Target)
					use(c.RecvAddr)
					for _, a := range c.Args {
						use(a)
					}
				}
			}
		}
	}
	out := map[eir.ObjID][]eir.Reg{}
	for o := range path {
		if !bad[o] {
			out[o] = stores[o]
		}
	}
	return out
}

func slicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// resultOrigin is the origin of result j of call c, at path.
func (h *heapOnly) resultOrigin(st *originState, c *eir.Call, j int, path []int) []hoAtom {
	f := st.f
	if isAllocatorAlloc(c) && j == 0 && len(path) == 0 {
		// Memory an allocator hands out through alloc.Allocator is heap
		// memory (section 18.9: an Allocator is unsafe code, which keeps
		// its memory alive; section 18.3 names an arena's current chunk
		// as a heap-only field), whichever implementation: what the
		// candidates' bodies do with their own memory (an arena's buffer,
		// a caller's for arena.NewUnsafe) is theirs to answer for
		// (section 18.12).
		return nil
	}
	if c.Kind == eir.CallContract {
		return h.contractResult(st, c, j, path)
	}
	var callees []eir.FuncID
	switch c.Kind {
	case eir.CallStatic:
		callees = []eir.FuncID{c.Callee}
	case eir.CallIface, eir.CallTypeParam, eir.CallFunc:
		callees = h.cands.of(f, c)
	default:
		return compat.Clone([]hoAtom{hoUnknown})
	}
	var out []hoAtom
	for _, g := range callees {
		if g == eir.NoFunc || int(g) >= len(h.p.Funcs) || h.p.Funcs[g].Bodyless {
			return compat.Clone([]hoAtom{hoUnknown})
		}
		out = compat.AppendPlain(out, h.node(hoKey{kind: hoResult, a: int32(g), b: int32(j), c: encPath(path)}))
	}
	return out
}

// --- writes ---------------------------------------------------------------

// collectWrites records the requirements of every write in the program.
func (h *heapOnly) collectWrites() {
	for _, f := range h.p.Funcs {
		st := newOriginState(f)
		uses := h.addrUses(f)
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				in := &b.Instrs[i]
				h.curF, h.curIn = f, in
				switch in.Op {
				case eir.OpStore:
					h.store(st, in)
				case eir.OpField, eir.OpElem, eir.OpAddr:
					if uses[in.Dst] {
						h.escape(f, in.Dst)
					}
				case eir.OpCall:
					h.opaqueCall(st, in.Call)
				case eir.OpConv:
					if in.Obj != eir.NoObj {
						// Boxed: X is written into the box, a place of
						// its type.
						h.boxWrite(st, in.Type, in.X)
					}
				case eir.OpClosure:
					if in.Obj != eir.NoObj {
						h.recordWrite(st, in.Obj, in.Args, false)
					}
				case eir.OpMethodVal:
					if in.Obj != eir.NoObj {
						h.recordWrite(st, in.Obj, []eir.Reg{in.X}, true)
					}
				}
			}
		}
		for _, w := range f.Facts.FieldWrites {
			h.within(st, w.Type, w.From, nil)
		}
	}
	h.expandNodes()
}

// boxWrite records the write of value x of type T into a box (an
// interface conversion that boxes).
func (h *heapOnly) boxWrite(st *originState, T types2.Type, x eir.Reg) {
	if T == nil {
		T = h.regType(st.f, x)
	}
	if T == nil {
		h.setPoison()
		return
	}
	h.within(st, T, x, nil)
}

// recordWrite records the writes of the values args into record obj (a
// closure's captures, a method value's receiver): each into a slot of its
// own type, not a struct field. A method value's record has the
// receiver's type (eir/synth.go).
func (h *heapOnly) recordWrite(st *originState, obj eir.ObjID, args []eir.Reg, recv bool) {
	f := st.f
	for _, a := range args {
		if a == 0 {
			continue
		}
		var T types2.Type
		if recv && int(obj) < len(f.Objects) {
			T = f.Objects[obj].Type
		}
		if T == nil {
			T = h.regType(f, a)
		}
		if T == nil {
			h.setPoison()
			return
		}
		h.within(st, T, a, nil)
	}
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
			h.setPoison() // a field of an unknown struct escapes
		}
		return
	}
	// The classes along the way to the place escape. Those within it do
	// not: a write through the escaped pointer to a field inside it takes
	// a field step, which lands in the field's class when the pointer's
	// type is known and makes the store reach every class (fieldStep)
	// when it is not; a write of the whole place writes the classes
	// within its type (store, within). (Opaque code is another matter:
	// opaqueCall.)
	for _, k := range h.classesAlong(l.base, l.steps) {
		h.markEscaped(k)
	}
}

func (h *heapOnly) markEscaped(k hoKey) {
	if !h.escaped[k] {
		if h.escAt == nil {
			h.escAt = map[hoKey]hoWrite{}
		}
		h.escAt[k] = hoWrite{h.curF, h.curIn}
	}
	h.escaped[k] = true
}

// escapeWithin marks every class within type t as escaped: a pointer to a
// whole struct is a way to the addresses of its fields.
func (h *heapOnly) escapeWithin(t types2.Type, depth int) {
	if depth > 16 || t == nil {
		return
	}
	types, ok := h.expand(t)
	if !ok {
		h.setPoison()
		return
	}
	for _, t := range types {
		switch u := t.Underlying().(type) {
		case *types2.Struct:
			for i := 0; i < u.NumFields(); i++ {
				h.markEscaped(h.classKey(u, i))
				h.escapeWithin(u.Field(i).Type(), depth+1)
			}
		case *types2.Array:
			h.escapeWithin(u.Elem(), depth+1)
		}
	}
}

// classesWithin returns every class within type t: the fields of the
// structs t holds by value, at any depth, over the instantiations of type
// parameters (an open one poisons).
func (h *heapOnly) classesWithin(t types2.Type) []hoKey {
	var out []hoKey
	var rec func(t types2.Type, depth int)
	rec = func(t types2.Type, depth int) {
		if depth > 16 || t == nil {
			return
		}
		types, ok := h.expand(t)
		if !ok {
			h.setPoison()
			return
		}
		for _, t := range types {
			switch u := t.Underlying().(type) {
			case *types2.Struct:
				for i := 0; i < u.NumFields(); i++ {
					out = compat.Append(out, h.classKey(u, i))
					rec(u.Field(i).Type(), depth+1)
				}
			case *types2.Array:
				rec(u.Elem(), depth+1)
			}
		}
	}
	rec(t, 0)
	return out
}

// store records the requirements of a store.
func (h *heapOnly) store(st *originState, in *eir.Instr) {
	f := st.f
	l := h.addrLoc(f, in.X, 0)
	L := in.Type
	if l.base == nil && l.any {
		return // through memory reinterpreted with unsafe.Pointer: not seen (section 18.3)
	}
	if l.base != nil && l.any {
		// Anywhere in the object: every class along the way to it and
		// every class within it may receive the value.
		atoms, _ := h.origin(st, in.Y, nil)
		for _, k := range h.classesAlong(l.base, l.steps) {
			h.require(h.node(k), atoms)
		}
		obj := walkPath(l.base, l.steps)
		if obj == nil {
			h.setPoison()
			return
		}
		for _, k := range h.classesWithin(obj) {
			h.require(h.node(k), atoms)
		}
	} else if l.base != nil {
		if L == nil {
			L = walkPath(l.base, l.steps)
		}
		atoms, _ := h.origin(st, in.Y, nil)
		for _, k := range h.classesAlong(l.base, l.steps) {
			h.require(h.node(k), atoms)
		}
	} else {
		if L == nil {
			L = pointee(h.regType(f, in.X))
		}
		if L == nil {
			h.setPoison()
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
		h.setPoison()
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
		h.setPoison()
		return
	}
	for _, t := range types {
		h.within1(st, t, r, path, 0)
	}
}

func (h *heapOnly) within1(st *originState, t types2.Type, r eir.Reg, path []int, depth int) {
	if depth > 16 {
		h.setPoison()
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
			h.setPoison()
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
	if c.Kind == eir.CallContract {
		if e, ok := contract.Lookup(c.Contract); ok && len(e.Effect.Stores) == 0 {
			// The entry says it stores nothing (plan invariant I7; the
			// solver takes its effects from the same entry).
			return
		}
	}
	// The parameter types say what the callee is given to store through
	// (a register's own type may not be known: the address of a package
	// variable); a parameter that is an unsafe.Pointer or an interface
	// says nothing, and the argument's type is taken instead.
	var params []types2.Type
	if c.Kind == eir.CallStatic && c.Callee != eir.NoFunc && int(c.Callee) < len(h.p.Funcs) {
		for _, in := range h.p.Funcs[c.Callee].Inputs {
			params = compat.Append(params, in.Type)
		}
	} else if c.Sig != nil {
		if r := c.Sig.Recv(); r != nil {
			params = compat.Append(params, r.Type())
		}
		for i := 0; i < c.Sig.Params().Len(); i++ {
			params = compat.Append(params, c.Sig.Params().At(i).Type())
		}
	}
	for j, a := range c.Args {
		if j < len(params) && params[j] != nil && isUnsafePointer(params[j]) {
			// What the callee writes through an unsafe.Pointer is memory
			// reinterpreted, outside the rule (sections 18.3, 18.12).
			continue
		}
		var t types2.Type
		if j < len(params) && params[j] != nil && !types2.IsInterface(params[j]) {
			t = params[j]
		} else {
			t = h.regType(st.f, a)
		}
		if t == nil {
			h.setPoison()
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
				h.setPoison()
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
			st = newOriginState(f)
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
				h.curF, h.curIn = s.f, nil
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
					h.curF, h.curIn = r.f, r.in
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
			structs = compat.AppendPlain(structs, typeID(k.a))
		}
	}
	for k := range h.escaped {
		if !seen[typeID(k.a)] {
			seen[typeID(k.a)] = true
			structs = compat.AppendPlain(structs, typeID(k.a))
		}
	}
	h.uf = compat.Make[int32](len(structs))
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
			h.reqs[rep] = compat.AppendPlain(h.reqs[rep], hoAtom(i))
			h.reqs[i] = compat.AppendPlain(h.reqs[i], rep)
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
				h.reqs[i] = compat.AppendPlain(h.reqs[i], h.index[hoKey{kind: hoLStore, a: int32(id)}])
			}
			if h.lAll[id] {
				h.reqs[i] = compat.AppendPlain(h.reqs[i], h.index[hoKey{kind: hoLStoreAll, a: int32(id)}])
			}
		}
	}
	h.val = compat.Make[bool](n)
	h.causes = compat.Make[hoAtom](n)
	rdeps := compat.Make[[]hoAtom](n)
	var work []hoAtom
	for i := 0; i < n; i++ {
		h.val[i] = true
		h.causes[i] = hoUnknown
		for _, a := range h.reqs[i] {
			if a == hoUnknown {
				h.val[i] = false
			} else {
				rdeps[a] = compat.AppendPlain(rdeps[a], hoAtom(i))
			}
		}
		if !h.val[i] {
			work = compat.AppendPlain(work, hoAtom(i))
		}
	}
	for len(work) > 0 {
		a := work[len(work)-1]
		work = work[:len(work)-1]
		for _, b := range rdeps[a] {
			if h.val[b] {
				h.val[b] = false
				h.causes[b] = a
				work = compat.AppendPlain(work, b)
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
		out = compat.Append(out, t)
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
		return compat.Clone([]types2.Type{t})
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
		if h.holdsCounted(s.Field(field).Type()) || !h.classHolds(s, field) {
			return false
		}
	}
	return true
}

// holdsCounted reports whether a field of type t may hold a counted box
// (shared[T], synced[T]): never heap-only, since what a load of it reads
// is bounded by the count its holder keeps, which the solver follows
// through the holder's token (eir/borrow.go).
func (h *heapOnly) holdsCounted(t types2.Type) bool {
	types, ok := h.expand(t)
	if !ok {
		return true
	}
	for _, t := range types {
		if w, ok := t.Underlying().(*types2.Wrapper); ok && (w.Kind() == types2.WrapShared || w.Kind() == types2.WrapSynced) {
			return true
		}
	}
	return false
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

// isAllocatorAlloc reports whether c calls the method Alloc of the
// allocator interface, pluto/rt.Allocator (alloc.Allocator).
func isAllocatorAlloc(c *eir.Call) bool {
	if c == nil || c.Kind != eir.CallIface || c.Method == nil || c.Method.Name() != "Alloc" || c.Iface == nil {
		return false
	}
	n, ok := types2.Unalias(c.Iface).(*types2.Named)
	return ok && n.Obj().Name() == "Allocator" && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "pluto/rt"
}

// explain returns why the class of field of struct type T is not
// heap-only: each node from it to the one a write gave an unknown source,
// and that write. For debugging (sched's TestExplain).
func (h *heapOnly) explain(T types2.Type, field int) []string {
	s, ok := T.Underlying().(*types2.Struct)
	if !ok {
		return compat.Clone([]string{"not a struct"})
	}
	key := h.classKey(s, field)
	a, ok := h.index[key]
	var out []string
	if !ok {
		out = compat.Append(out, "no class (no write seen)")
	}
	// The classes of the instances (or the generic struct) it shares with.
	for k, b := range h.index {
		if k.kind != hoClass || k.b != key.b || k.a == key.a || h.val[b] {
			continue
		}
		other := h.tt.types[k.a]
		if (h.tt.hasTypeParam(s) || h.tt.hasTypeParam(other)) && h.tt.mayMatch(other, s) {
			out = compat.Append(out, compat.Sprintf("shared with %v:", other))
			out = compat.Append(out, h.explainFrom(b)...)
		}
	}
	if !ok {
		return out
	}
	return compat.Append(out, h.explainFrom(a)...)
}

// explainFrom is the chain of explain from node a.
func (h *heapOnly) explainFrom(a hoAtom) []string {
	if h.poison {
		w := h.poisonAt
		where := "?"
		if w.f != nil {
			where = w.f.Name
			if w.in != nil {
				where = compat.Concat(where, compat.Concat(", ", h.p.InstrString(w.in), " at ", compat.Sprintfb("%v", compat.ArgI(int64(w.in.Pos)))))
			}
		}
		return compat.Clone([]string{compat.Concat("poisoned: a write of a type that cannot be bounded, in ", where)})
	}
	var out []string
	seen := map[hoAtom]bool{}
	for a != hoUnknown && !seen[a] && len(out) < 40 {
		seen[a] = true
		k := h.keys[a]
		line := compat.Sprintf("%s %v heap=%v", hoKindName(k.kind), k, h.val[a])
		if k.kind == hoClass || k.kind == hoLStore || k.kind == hoLStoreAll {
			line = compat.Concat(line, compat.Sprintf(" (%v)", h.tt.types[k.a]))
		}
		if w, ok := h.escAt[k]; ok && w.f != nil {
			what := "?"
			if w.in != nil {
				what = compat.Concat(h.p.InstrString(w.in), " at ", compat.Sprintfb("%v", compat.ArgI(int64(w.in.Pos))))
			}
			line = compat.Concat(line, compat.Sprintfb(" escaped in %s, %s", compat.ArgS(w.f.Name), compat.ArgS(what)))
		}
		if h.val[a] {
			out = compat.Append(out, line)
			break
		}
		if w, ok := h.unkAt[a]; ok && h.causes[a] == hoUnknown {
			what := "a call of it"
			if w.in != nil {
				what = h.p.InstrString(w.in)
			}
			line = compat.Concat(line, compat.Sprintfb(": an unknown source in %s, %s", compat.ArgS(w.f.Name), compat.ArgS(what)))
		}
		out = compat.Append(out, line)
		a = h.causes[a]
	}
	return out
}

func hoKindName(k hoNodeKind) string {
	switch k {
	case hoClass:
		return "class"
	case hoParam:
		return "param"
	case hoResult:
		return "result"
	case hoLStore:
		return "lstore"
	case hoLStoreAll:
		return "lstoreall"
	}
	return "?"
}
