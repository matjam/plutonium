package ssa

import (
	"fmt"

	"github.com/matjam/plutonium/internal/syntax/token"
)

// A Value is one SSA value: the result of an operation.
type Value struct {
	ID     int
	Op     Op
	Type   *Type
	Args   []*Value
	AuxInt int64
	Aux    any       // *Slot, *Sym or *Type, depending on Op
	Call   *CallInfo // calls only
	Block  *Block
	Pos    token.Pos
	Uses   int32 // number of uses as an argument or block control
	// Inl, when nonzero, is the 1-based index in Block.Func.Inlines of the
	// inlined call this value came from; Pos is then a position in the
	// inlined function.
	Inl int32

	argStorage [3]*Value
}

func (v *Value) String() string { return fmt.Sprintf("v%d", v.ID) }

// CopyPos gives v the source position of w together with its inline site.
// A pass that copies or replaces a value copies its position this way,
// never Pos alone: a position from an inlined body without its Inl is
// symbolised as a line of the function it was inlined into.
func (v *Value) CopyPos(w *Value) { v.Pos, v.Inl = w.Pos, w.Inl }

// AddArg appends an argument.
func (v *Value) AddArg(a *Value) {
	v.Args = append(v.Args, a)
	a.Uses++
}

// SetArg replaces argument i.
func (v *Value) SetArg(i int, a *Value) {
	v.Args[i].Uses--
	v.Args[i] = a
	a.Uses++
}

// ResetArgs removes all arguments.
func (v *Value) ResetArgs() {
	for _, a := range v.Args {
		a.Uses--
	}
	v.Args = v.argStorage[:0]
}

// SetArgs replaces all arguments.
func (v *Value) SetArgs(args ...*Value) {
	v.ResetArgs()
	for _, a := range args {
		v.AddArg(a)
	}
}

// Reset turns v into op with no arguments, keeping its ID, block and type.
func (v *Value) Reset(op Op) {
	v.ResetArgs()
	v.Op = op
	v.AuxInt = 0
	v.Aux = nil
	v.Call = nil
}

// CopyOf turns v into a copy of w.
func (v *Value) CopyOf(w *Value) {
	if v == w {
		panic("ssa: value copying itself")
	}
	v.Reset(OpCopy)
	v.AddArg(w)
}

// MemArg returns v's memory argument, or nil.
func (v *Value) MemArg() *Value {
	if v.Op.HasMemArg() {
		return v.Args[len(v.Args)-1]
	}
	return nil
}

// IsConst reports whether v is an integer, bool or pointer constant.
func (v *Value) IsConst() bool { return v.Op == OpConst }

// StoreType returns the type stored by an OpStore.
func (v *Value) StoreType() *Type { return v.Aux.(*Type) }

// IsVolatile reports whether a load or store is volatile.
func (v *Value) IsVolatile() bool {
	return (v.Op == OpLoad || v.Op == OpStore) && v.AuxInt&MemVolatile != 0
}

// A BlockKind says how control leaves a block.
type BlockKind uint8

const (
	BlockInvalid BlockKind = iota
	BlockPlain             // one successor
	BlockIf                // Controls[0] is a bool; Succs[0] if true, Succs[1] if false
	BlockRet               // return; Controls are the memory state then the results
	BlockExit              // does not return (panic path); Controls[0] is the memory state
)

var blockKindNames = [...]string{"Invalid", "Plain", "If", "Ret", "Exit"}

func (k BlockKind) String() string { return blockKindNames[k] }

// Branch prediction for BlockIf.
const (
	Unlikely = -1
	Unknown  = 0
	Likely   = 1
)

// An Edge is a control flow edge: the block at the other end and the index of
// this edge in that block's Preds (for a successor) or Succs (for a predecessor).
type Edge struct {
	B *Block
	I int
}

// A Block is a basic block.
type Block struct {
	ID       int
	Kind     BlockKind
	Values   []*Value // phis first, then the rest in a valid order
	Controls []*Value
	Succs    []Edge
	Preds    []Edge
	Func     *Func
	Likely   int8 // branch prediction for BlockIf
	Pos      token.Pos
	Inl      int32 // inline site of Pos, as for Value.Inl
}

func (b *Block) String() string { return fmt.Sprintf("b%d", b.ID) }

// CopyPos gives b the source position of c together with its inline site.
func (b *Block) CopyPos(c *Block) { b.Pos, b.Inl = c.Pos, c.Inl }

// SetControl replaces the block's controls.
func (b *Block) SetControl(vs ...*Value) {
	for _, c := range b.Controls {
		c.Uses--
	}
	b.Controls = append(b.Controls[:0], vs...)
	for _, c := range vs {
		c.Uses++
	}
}

// AddEdgeTo adds an edge from b to c.
func (b *Block) AddEdgeTo(c *Block) {
	i := len(b.Succs)
	j := len(c.Preds)
	b.Succs = append(b.Succs, Edge{c, j})
	c.Preds = append(c.Preds, Edge{b, i})
}

// RemoveSucc removes successor i of b, fixing up the predecessor list and
// phis of the successor.
func (b *Block) RemoveSucc(i int) {
	e := b.Succs[i]
	c := e.B
	c.removePred(e.I)
	n := len(b.Succs) - 1
	if i != n {
		// Move the last successor into slot i.
		last := b.Succs[n]
		b.Succs[i] = last
		last.B.Preds[last.I].I = i
	}
	b.Succs[n] = Edge{}
	b.Succs = b.Succs[:n]
}

// removePred removes predecessor i of b and the matching phi arguments.
func (b *Block) removePred(i int) {
	n := len(b.Preds) - 1
	if i != n {
		last := b.Preds[n]
		b.Preds[i] = last
		last.B.Succs[last.I].I = i
	}
	b.Preds[n] = Edge{}
	b.Preds = b.Preds[:n]
	for _, v := range b.Values {
		if v.Op != OpPhi {
			continue
		}
		v.Args[i].Uses--
		v.Args[i] = v.Args[n]
		v.Args[n] = nil
		v.Args = v.Args[:n]
	}
}

// NewValue appends a new value to b.
func (b *Block) NewValue(op Op, t *Type, args ...*Value) *Value {
	v := b.Func.newValue(op, t, b)
	for _, a := range args {
		v.AddArg(a)
	}
	b.Values = append(b.Values, v)
	return v
}

// NewValueBefore inserts a new value into b immediately before at.
func (b *Block) NewValueBefore(at *Value, op Op, t *Type, args ...*Value) *Value {
	v := b.Func.newValue(op, t, b)
	if !v.Pos.IsValid() && at != nil {
		v.CopyPos(at)
	}
	for _, a := range args {
		v.AddArg(a)
	}
	i := 0
	for i < len(b.Values) && b.Values[i] != at {
		i++
	}
	b.Values = append(b.Values, nil)
	copy(b.Values[i+1:], b.Values[i:])
	b.Values[i] = v
	return v
}

// NewValueI is NewValue with an AuxInt.
func (b *Block) NewValueI(op Op, t *Type, auxint int64, args ...*Value) *Value {
	v := b.NewValue(op, t, args...)
	v.AuxInt = auxint
	return v
}

// NewValueA is NewValue with an Aux.
func (b *Block) NewValueA(op Op, t *Type, aux any, args ...*Value) *Value {
	v := b.NewValue(op, t, args...)
	v.Aux = aux
	return v
}

// A Slot is a stack slot: a local variable, temporary or spill area.
type Slot struct {
	ID    int
	Name  string // for debugging
	Size  int64
	Align int64

	// Leaves, if known, are the scalar components of the slot's type in
	// offset order. Scalar replacement splits a slot into one slot per leaf
	// when every access matches a leaf.
	Leaves []Leaf

	// Pads are the byte ranges of the slot's type that hold no data: the
	// padding between fields and after the last one. Lowering derives them
	// from the type's layout, independently of the leaves.
	Pads []Pad

	// Offset is assigned by frame layout, relative to the frame base.
	Offset int64
}

func (s *Slot) String() string { return fmt.Sprintf("%s#%d", s.Name, s.ID) }

// A Leaf is one scalar component of an aggregate.
type Leaf struct {
	Off  int64
	Type *Type
}

// A Pad is a range of padding bytes in an aggregate.
type Pad struct {
	Off, Size int64
}

// LeavesCover reports whether the slot's leaves and pads tile it exactly:
// every byte belongs to exactly one leaf or pad. A slot whose leaves miss
// a byte holding data cannot be split into its leaves, since a block copy
// through the split slot would lose that byte.
func (s *Slot) LeavesCover() bool {
	type span struct{ off, end int64 }
	spans := make([]span, 0, len(s.Leaves)+len(s.Pads))
	for _, l := range s.Leaves {
		spans = append(spans, span{l.Off, l.Off + l.Type.Size})
	}
	for _, p := range s.Pads {
		if p.Size <= 0 {
			return false
		}
		spans = append(spans, span{p.Off, p.Off + p.Size})
	}
	// Insertion sort by offset: there are few spans.
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0 && spans[j].off < spans[j-1].off; j-- {
			spans[j], spans[j-1] = spans[j-1], spans[j]
		}
	}
	var at int64
	for _, sp := range spans {
		if sp.off != at || sp.end <= sp.off {
			return false
		}
		at = sp.end
	}
	return at == s.Size
}

// A SymKind classifies a symbol.
type SymKind uint8

const (
	SymFunc   SymKind = iota // function defined in this program
	SymData                  // data defined in this program
	SymExtern                // defined elsewhere (assembly, C, linker script)
)

// A Sym is a linker symbol.
type Sym struct {
	Name string
	Kind SymKind
	// PerCPU marks a //pu:percpu variable: the symbol, in section
	// .percpu linked at address 0, is the variable's offset in each
	// processor's per-CPU block, and accesses are relative to GS.
	PerCPU bool
}

func (s *Sym) String() string { return s.Name }

// ABI selects a calling convention.
type ABI uint8

const (
	ABIInternal ABI = iota // Plutonium calls: aggregates already flattened to scalars
	ABIC                   // System V C calls
)

// CallInfo describes how a call passes its arguments and results.
// Arguments and results are scalars; lowering has already flattened
// aggregates. The backend assigns integer-class scalars to integer
// registers and float scalars to float registers in order, and anything
// left over to the stack in argument order.
type CallInfo struct {
	ABI      ABI
	Results  []*Type
	Variadic bool   // C variadic call: the backend sets AL to the number of vector registers used
	OnStack  []bool // per argument: force to the stack (C memory-class aggregates)
	NoReturn bool
}

// Pragma flags on a function.
type Pragma uint16

const (
	PragmaInline Pragma = 1 << iota
	PragmaNoInline
	PragmaNoBounds
	PragmaNoReturn
	PragmaInterrupt
	// PragmaNoSIMD (//pu:nosimd) keeps the optimiser from introducing
	// vector instructions the function's own code does not ask for.
	PragmaNoSIMD
)

// MayVectorize reports whether optimisations may introduce vector
// instructions into f: not into NoSSE functions (kernel mode), which may
// not touch the SSE registers at all, interrupt handlers, or functions
// marked //pu:nosimd.
func (f *Func) MayVectorize() bool {
	return !f.NoSSE && f.Pragmas&(PragmaNoSIMD|PragmaInterrupt) == 0
}

// A Func is a function in SSA form.
type Func struct {
	Name    string
	Sym     *Sym
	Exports []string // additional global symbol names (//pu:export)
	Params  []*Type  // flattened parameter types
	Results []*Type  // flattened result types
	ABI     ABI
	Pragmas Pragma
	Section string
	Pos     token.Pos
	// End, if valid, is the end of the function's source: every position
	// in its own code (Inl 0) lies in [Pos, End]. Lowering sets it for
	// functions declared in source; synthesized ones leave it unset.
	End token.Pos
	Pkg string // import path of the package that declares it, if any

	// Inlines records the calls inlined into this function, so that a
	// position in an inlined body can be traced back to its call site.
	Inlines []InlineSite

	// pos is the position given to new values (see SetPos).
	pos token.Pos

	// NoSSE forbids the function any SSE, x87 or other vector state: it
	// runs where nothing saves those registers (kernel mode without
	// //pu:fpu, abi.md section 10.2). Lowering rejects float operations in
	// such a function; no pass may introduce float or vector operations
	// into it (a vectoriser must leave it alone, and the inliner never
	// inlines a function without NoSSE into one with it); and the backend
	// keeps float-typed loads, stores, copies and zero constants in
	// general-purpose registers and fails on anything else that would
	// need an XMM register.
	NoSSE bool

	// ParamOnStack forces parameters to the stack, as CallInfo.OnStack does
	// for calls (ABIC functions only; nil otherwise).
	ParamOnStack []bool

	Blocks []*Block
	Entry  *Block
	Slots  []*Slot

	nextValue int
	nextBlock int
	nextSlot  int

	// Cached analyses, invalidated by passes that change the CFG.
	cachedDom   *DomTree
	cachedLoops *LoopNest
	cachedOrder []*Block
}

// NewFunc returns an empty function with an entry block.
func NewFunc(name string) *Func {
	f := &Func{Name: name}
	f.Entry = f.NewBlock(BlockPlain)
	return f
}

// An InlineSite is a call that the inliner replaced with the callee's body.
type InlineSite struct {
	Parent int32     // 1-based index of the enclosing inlined call, or 0
	Func   string    // the inlined function's name
	Pos    token.Pos // position of the call
	// Start and End are the inlined function's Func.Pos and Func.End: the
	// positions of values from this call lie between them.
	Start, End token.Pos
}

// SetPos sets the position given to values created from now on and returns
// the previous one. Lowering sets it as it walks the source; passes that
// leave it unset create values without a position, which the backend fills
// in from the surrounding code.
func (f *Func) SetPos(p token.Pos) token.Pos {
	old := f.pos
	f.pos = p
	return old
}

// An InlineBase maps the inline sites of an inlined function to those of
// the function it was inlined into (see AddInline).
type InlineBase int32

// Map returns the Inl in f of a value whose Inl in g was inl.
func (base InlineBase) Map(inl int32) int32 {
	if inl == 0 {
		return int32(base)
	}
	return inl + int32(base)
}

// AddInline records that call, a call in f, is being replaced by the body of
// g. It returns the mapping of a value's Inl in g to its Inl in f. (A value
// rather than a closure, so that the self-hosted translation can return
// it: Plutonium closures do not outlive their frame.)
func (f *Func) AddInline(call *Value, g *Func) InlineBase {
	base := int32(len(f.Inlines)) + 1 // index of the new site
	f.Inlines = append(f.Inlines, InlineSite{Parent: call.Inl, Func: g.Name, Pos: call.Pos, Start: g.Pos, End: g.End})
	for _, s := range g.Inlines {
		if s.Parent == 0 {
			s.Parent = base
		} else {
			s.Parent += base
		}
		f.Inlines = append(f.Inlines, s)
	}
	return InlineBase(base)
}

func (f *Func) newValue(op Op, t *Type, b *Block) *Value {
	v := &Value{ID: f.nextValue, Op: op, Type: t, Block: b, Pos: f.pos}
	v.Args = v.argStorage[:0]
	f.nextValue++
	return v
}

// NumValues returns one more than the largest value ID.
func (f *Func) NumValues() int { return f.nextValue }

// NumBlocks returns one more than the largest block ID.
func (f *Func) NumBlocks() int { return f.nextBlock }

// NewBlock adds a new block.
func (f *Func) NewBlock(kind BlockKind) *Block {
	b := &Block{ID: f.nextBlock, Kind: kind, Func: f}
	f.nextBlock++
	f.Blocks = append(f.Blocks, b)
	f.InvalidateCFG()
	return b
}

// NewSlot adds a stack slot.
func (f *Func) NewSlot(name string, size, align int64) *Slot {
	if align < 1 {
		align = 1
	}
	s := &Slot{ID: f.nextSlot, Name: name, Size: size, Align: align}
	f.nextSlot++
	f.Slots = append(f.Slots, s)
	return s
}

// InvalidateCFG discards cached control flow analyses.
func (f *Func) InvalidateCFG() {
	f.cachedDom = nil
	f.cachedLoops = nil
	f.cachedOrder = nil
}

// RemoveDeadBlocks deletes blocks with no predecessors other than the entry,
// repeating until none are left.
func (f *Func) RemoveDeadBlocks() {
	reach := f.Reachable()
	live := f.Blocks[:0]
	for _, b := range f.Blocks {
		if reach[b.ID] {
			live = append(live, b)
			continue
		}
		for len(b.Succs) > 0 {
			b.RemoveSucc(len(b.Succs) - 1)
		}
		for _, v := range b.Values {
			v.ResetArgs()
		}
		b.SetControl()
		b.Values = nil
	}
	clear(f.Blocks[len(live):])
	f.Blocks = live
	f.InvalidateCFG()
}

// Reachable returns the set of blocks reachable from the entry, by block ID.
func (f *Func) Reachable() []bool {
	reach := make([]bool, f.NumBlocks())
	stack := []*Block{f.Entry}
	reach[f.Entry.ID] = true
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, e := range b.Succs {
			if !reach[e.B.ID] {
				reach[e.B.ID] = true
				stack = append(stack, e.B)
			}
		}
	}
	return reach
}

// ConstInt returns a new integer constant in the entry block. Constants are
// placed in the entry so they dominate every use; CSE merges duplicates.
// The value is stored in canonical form (see Canon).
func (f *Func) ConstInt(t *Type, c int64) *Value {
	return f.Entry.prependValue(OpConst, t, Canon(t, c))
}

// Canon returns the canonical AuxInt of an integer constant of type t: its
// low t.Size bytes, sign-extended to 64 bits. Booleans are 0 or 1.
func Canon(t *Type, c int64) int64 {
	switch {
	case t.IsBool():
		if c != 0 {
			return 1
		}
		return 0
	case t.Kind == KInt && t.Size < 8:
		shift := 64 - 8*t.Size
		return c << shift >> shift
	}
	return c
}

// ZeroExt returns the canonical constant c of type t as an unsigned value.
func ZeroExt(t *Type, c int64) uint64 {
	if t.Size >= 8 || t.Kind != KInt {
		return uint64(c)
	}
	return uint64(c) & (1<<(8*t.Size) - 1)
}

// ConstBool returns a new boolean constant.
func (f *Func) ConstBool(c bool) *Value {
	var n int64
	if c {
		n = 1
	}
	return f.Entry.prependValue(OpConst, TBool, n)
}

// prependValue inserts an argument-less value at the start of b, after any
// phis, so that it dominates everything else in b.
func (b *Block) prependValue(op Op, t *Type, auxint int64) *Value {
	v := b.Func.newValue(op, t, b)
	v.AuxInt = auxint
	i := 0
	for i < len(b.Values) && b.Values[i].Op == OpPhi {
		i++
	}
	b.Values = append(b.Values, nil)
	copy(b.Values[i+1:], b.Values[i:])
	b.Values[i] = v
	return v
}
