package refsolve

import (
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// B builds an eir.Func by hand, for tests and the generator: it numbers
// registers, sites and positions, and keeps the CFG edges in order (a phi's
// arguments are by predecessor in the order of Edge calls).
type B struct {
	F   *eir.Func
	cur eir.BlockID
	pos token.Pos
}

// NewFunc starts a function with n inputs and r results and an entry block.
func NewFunc(id eir.FuncID, inputs, results int) *B {
	f := &eir.Func{ID: id, Name: "f", Results: results}
	for i := 0; i < inputs; i++ {
		f.Inputs = append(f.Inputs, eir.Input{Type: types2.Typ[types2.UnsafePointer]})
	}
	b := &B{F: f}
	b.NewBlock()
	return b
}

// NewBlock adds a block and makes it current.
func (b *B) NewBlock() eir.BlockID {
	id := eir.BlockID(len(b.F.Blocks))
	b.F.Blocks = append(b.F.Blocks, &eir.Block{ID: id, Loop: -1})
	b.cur = id
	return id
}

// At makes block id current.
func (b *B) At(id eir.BlockID) { b.cur = id }

// Edge adds a CFG edge.
func (b *B) Edge(from, to eir.BlockID) {
	b.F.Blocks[from].Succs = append(b.F.Blocks[from].Succs, to)
	b.F.Blocks[to].Preds = append(b.F.Blocks[to].Preds, from)
}

// Loop declares a natural loop over blocks (header first) inside parent (-1
// for none) and returns its index.
func (b *B) Loop(parent int, blocks ...eir.BlockID) int {
	l := len(b.F.Loops)
	b.F.Loops = append(b.F.Loops, eir.Loop{Header: blocks[0], Blocks: blocks, Parent: parent})
	for _, id := range blocks {
		b.F.Blocks[id].Loop = l
	}
	return l
}

// Obj adds a frame object.
func (b *B) Obj(kind eir.ObjKind, life eir.Lifetime, loop int, t types2.Type) eir.ObjID {
	b.F.Objects = append(b.F.Objects, eir.Object{Kind: kind, Type: t, Life: life, Loop: loop})
	return eir.ObjID(len(b.F.Objects) - 1)
}

func (b *B) reg() eir.Reg {
	b.F.NumRegs++
	return eir.Reg(b.F.NumRegs)
}

// Emit appends in to the current block, giving it a position and, when
// def, a fresh Dst.
func (b *B) Emit(in eir.Instr, def bool) eir.Reg {
	b.pos++
	in.Pos = b.pos
	if def {
		in.Dst = b.reg()
	}
	blk := b.F.Blocks[b.cur]
	if in.Op == eir.OpPhi {
		// phis first
		n := 0
		for n < len(blk.Instrs) && blk.Instrs[n].Op == eir.OpPhi {
			n++
		}
		blk.Instrs = append(blk.Instrs, eir.Instr{})
		copy(blk.Instrs[n+1:], blk.Instrs[n:])
		blk.Instrs[n] = in
		return in.Dst
	}
	blk.Instrs = append(blk.Instrs, in)
	return in.Dst
}

// Addr is r = &o.path.
func (b *B) Addr(o eir.ObjID, path ...int) eir.Reg {
	return b.Emit(eir.Instr{Op: eir.OpAddr, Obj: o, Path: path}, true)
}

// Heap is r = a fresh allocation.
func (b *B) Heap() eir.Reg { return b.Emit(eir.Instr{Op: eir.OpHeap, Heap: eir.HeapAlloc}, true) }

// CHeap is r = a counted reference.
func (b *B) CHeap() eir.Reg { return b.Emit(eir.Instr{Op: eir.OpHeap, Heap: eir.HeapCounted}, true) }

// In is r = input i.
func (b *B) In(i int) eir.Reg { return b.Emit(eir.Instr{Op: eir.OpInput, Index: i}, true) }

// Load is r = *x.
func (b *B) Load(x eir.Reg) eir.Reg { return b.Emit(eir.Instr{Op: eir.OpLoad, X: x}, true) }

// LoadCap is r = *x, the load of a captured location's address from a
// closure record (eir.Instr.Cap, domain.StepCap).
func (b *B) LoadCap(x eir.Reg) eir.Reg {
	return b.Emit(eir.Instr{Op: eir.OpLoad, X: x, Cap: true}, true)
}

// Store is *x = y.
func (b *B) Store(x, y eir.Reg) { b.Emit(eir.Instr{Op: eir.OpStore, X: x, Y: y}, false) }

// Field is r = &(*x).path.
func (b *B) Field(x eir.Reg, path ...int) eir.Reg {
	return b.Emit(eir.Instr{Op: eir.OpField, X: x, Path: path}, true)
}

// Elem is r = &(*x)[i].
func (b *B) Elem(x eir.Reg) eir.Reg { return b.Emit(eir.Instr{Op: eir.OpElem, X: x}, true) }

// Select is r = x.path.
func (b *B) Select(x eir.Reg, path ...int) eir.Reg {
	return b.Emit(eir.Instr{Op: eir.OpSelect, X: x, Path: path}, true)
}

// Compose is r = a struct whose field fields[i] holds args[i].
func (b *B) Compose(args []eir.Reg, fields [][]int) eir.Reg {
	return b.Emit(eir.Instr{Op: eir.OpCompose, Args: args, Fields: fields}, true)
}

// Copy is r = x.
func (b *B) Copy(x eir.Reg) eir.Reg { return b.Emit(eir.Instr{Op: eir.OpCopy, X: x}, true) }

// Assert is r = x.(T).
func (b *B) Assert(x eir.Reg) eir.Reg { return b.Emit(eir.Instr{Op: eir.OpAssert, X: x}, true) }

// Conv is r = conv x, boxed into o (eir.NoObj: not boxed).
func (b *B) Conv(x eir.Reg, o eir.ObjID) eir.Reg {
	return b.Emit(eir.Instr{Op: eir.OpConv, X: x, Obj: o}, true)
}

// Closure is r = a closure whose record o holds args.
func (b *B) Closure(o eir.ObjID, args ...eir.Reg) eir.Reg {
	return b.Emit(eir.Instr{Op: eir.OpClosure, Obj: o, Args: args}, true)
}

// MethodVal is r = a method value whose record o holds x.
func (b *B) MethodVal(o eir.ObjID, x eir.Reg) eir.Reg {
	return b.Emit(eir.Instr{Op: eir.OpMethodVal, Obj: o, X: x}, true)
}

// Sink sends x to kind (result idx for a return).
func (b *B) Sink(kind eir.SinkKind, x eir.Reg, idx int) {
	b.Emit(eir.Instr{Op: eir.OpSink, Sink: kind, X: x, Index: idx}, false)
}

// Phi is r = one of args, by predecessor.
func (b *B) Phi(args ...eir.Reg) eir.Reg {
	return b.Emit(eir.Instr{Op: eir.OpPhi, Args: args}, true)
}

// Drop ends the lifetime of o.
func (b *B) Drop(o eir.ObjID) { b.Emit(eir.Instr{Op: eir.OpDrop, Obj: o}, false) }

// Call emits a call with nres results and returns them.
func (b *B) Call(c eir.Call, nres int) []eir.Reg {
	c.Site = eir.SiteID(b.F.NumSites)
	b.F.NumSites++
	c.Results = nil
	for i := 0; i < nres; i++ {
		c.Results = append(c.Results, b.reg())
	}
	b.Emit(eir.Instr{Op: eir.OpCall, Call: &c}, false)
	return c.Results
}

// Static is a static call of callee.
func (b *B) Static(callee eir.FuncID, nres int, args ...eir.Reg) []eir.Reg {
	return b.Call(eir.Call{Kind: eir.CallStatic, Callee: callee, Args: args}, nres)
}
