package eir

import (
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// The function builder: blocks, registers, frame objects, and the SSA form
// of the locals whose address is never taken, built on the fly as Braun et
// al. do ("Simple and Efficient Construction of Static Single Assignment
// Form", CC 2013), the way internal/lower builds its memory state.

// bblock is a block under construction.
type bblock struct {
	b      *Block
	phis   []Instr
	defs   map[*types2.Var]Reg // the current register of each variable at the end of the block
	sealed bool
	// incomplete are the phis made before the block was sealed, by
	// variable, whose operands are filled in when it is.
	incomplete map[*types2.Var]int // index in phis
}

// emptyBlock reports whether nothing has been put in b yet.
func (bb *bblock) emptyBlock() bool { return len(bb.b.Instrs) == 0 && len(bb.phis) == 0 }

// newBlock makes a block with no predecessors yet.
func (fb *fnb) newBlock() *bblock {
	b := &Block{ID: BlockID(len(fb.f.Blocks)), Loop: -1}
	fb.f.Blocks = append(fb.f.Blocks, b)
	bb := &bblock{b: b, defs: map[*types2.Var]Reg{}}
	fb.blocks = append(fb.blocks, bb)
	return bb
}

// edge adds the edge from to to.
func (fb *fnb) edge(from, to *bblock) {
	if from == nil || to == nil {
		return
	}
	from.b.Succs = append(from.b.Succs, to.b.ID)
	to.b.Preds = append(to.b.Preds, from.b.ID)
}

// jump ends the current block with a jump to to, if the current block is
// reachable, and leaves no current block.
func (fb *fnb) jump(to *bblock) {
	if fb.cur != nil {
		fb.edge(fb.cur, to)
	}
	fb.cur = nil
}

// startBlock makes b the current block.
func (fb *fnb) startBlock(b *bblock) { fb.cur = b }

// ensureBlock gives code that follows a jump a block of its own: dead code,
// which internal/lower lowers too, so its calls are calls of the program.
func (fb *fnb) ensureBlock() *bblock {
	if fb.cur == nil {
		fb.cur = fb.newBlock()
		fb.seal(fb.cur)
	}
	return fb.cur
}

// newReg returns a fresh register.
func (fb *fnb) newReg() Reg {
	fb.f.NumRegs++
	return Reg(fb.f.NumRegs)
}

// emit appends an instruction to the current block.
func (fb *fnb) emit(in Instr) {
	b := fb.ensureBlock()
	if !in.Pos.IsValid() {
		in.Pos = fb.pos
	}
	b.b.Instrs = append(b.b.Instrs, in)
}

// emitDst emits an instruction that defines a fresh register, and returns
// the register.
func (fb *fnb) emitDst(in Instr) Reg {
	in.Dst = fb.newReg()
	fb.emit(in)
	return in.Dst
}

// newObj adds a frame object.
func (fb *fnb) newObj(o Object) ObjID {
	if o.Life == 0 {
		o.Life = LifeFrame
	}
	if o.Pos == token.NoPos {
		o.Pos = fb.pos
	}
	if fb.f.Key.Init != nil {
		// A package initializer's literals, boxes and records are static
		// memory: what they make outlives the initializer.
		o.Life = LifeHeap
	}
	o.Loop = -1
	fb.f.Objects = append(fb.f.Objects, o)
	return ObjID(len(fb.f.Objects) - 1)
}

// addr returns a register holding &obj.path.
func (fb *fnb) addr(obj ObjID, path ...int) Reg {
	return fb.emitDst(Instr{Op: OpAddr, Obj: obj, Path: path})
}

// load returns a register holding *x, a location of type T; 0 if T holds
// no pointers.
func (fb *fnb) load(x Reg, T types2.Type) Reg {
	if x == 0 || !hasPointers(T) {
		return 0
	}
	return fb.emitDst(Instr{Op: OpLoad, X: x, Type: T})
}

// store stores y into *x, a location of type T.
func (fb *fnb) store(x, y Reg, T types2.Type) {
	if x == 0 || y == 0 || !hasPointers(T) {
		return
	}
	fb.emit(Instr{Op: OpStore, X: x, Y: y, Type: T})
}

// field returns &(*x).path.
func (fb *fnb) field(x Reg, path ...int) Reg {
	if x == 0 {
		return 0
	}
	if len(path) == 0 {
		return x
	}
	return fb.emitDst(Instr{Op: OpField, X: x, Path: path})
}

// elem returns &(*x)[i] for the array or slice x points to.
func (fb *fnb) elem(x Reg) Reg {
	if x == 0 {
		return 0
	}
	return fb.emitDst(Instr{Op: OpElem, X: x})
}

// sel returns x.path of a struct value.
func (fb *fnb) sel(x Reg, path ...int) Reg {
	if x == 0 {
		return 0
	}
	if len(path) == 0 {
		return x
	}
	return fb.emitDst(Instr{Op: OpSelect, X: x, Path: path})
}

// heap returns a pointer to heap or static memory.
func (fb *fnb) heap(k HeapKind) Reg { return fb.emitDst(Instr{Op: OpHeap, Heap: k}) }

// sink sends x to a sink.
func (fb *fnb) sink(k SinkKind, x Reg, index int, pos token.Pos) {
	if x == 0 {
		return
	}
	fb.emit(Instr{Op: OpSink, Sink: k, X: x, Index: index, Pos: pos})
}

// ----------------------------------------------------------------------------
// SSA variables

// writeVar records that v holds r at the end of the current block.
func (fb *fnb) writeVar(v *types2.Var, r Reg) {
	b := fb.ensureBlock()
	b.defs[v] = r
}

// readVar returns the register v holds in the current block.
func (fb *fnb) readVar(v *types2.Var) Reg {
	return fb.readVarIn(v, fb.ensureBlock())
}

func (fb *fnb) readVarIn(v *types2.Var, b *bblock) Reg {
	if r, ok := b.defs[v]; ok {
		return r
	}
	var r Reg
	switch {
	case !b.sealed:
		r = fb.newPhi(b)
		if b.incomplete == nil {
			b.incomplete = map[*types2.Var]int{}
		}
		b.incomplete[v] = len(b.phis) - 1
	case len(b.b.Preds) == 0:
		r = 0 // never assigned on the way here: the zero value
	case len(b.b.Preds) == 1:
		r = fb.readVarIn(v, fb.blocks[b.b.Preds[0]])
	default:
		r = fb.newPhi(b)
		k := len(b.phis) - 1
		b.defs[v] = r // break cycles
		fb.fillPhi(v, b, k)
	}
	b.defs[v] = r
	return r
}

// newPhi adds an empty phi to b and returns its register.
func (fb *fnb) newPhi(b *bblock) Reg {
	r := fb.newReg()
	b.phis = append(b.phis, Instr{Op: OpPhi, Dst: r, Pos: fb.pos})
	return r
}

// fillPhi gives phi k of b its operands, one per predecessor.
func (fb *fnb) fillPhi(v *types2.Var, b *bblock, k int) {
	args := make([]Reg, len(b.b.Preds))
	for i, p := range b.b.Preds {
		args[i] = fb.readVarIn(v, fb.blocks[p])
	}
	b.phis[k].Args = args
}

// seal records that every predecessor of b is known.
func (fb *fnb) seal(b *bblock) {
	if b == nil || b.sealed {
		return
	}
	b.sealed = true
	for v, k := range b.incomplete {
		fb.fillPhi(v, b, k)
	}
	b.incomplete = nil
}

// finishBlocks seals every block and puts each block's phis first.
func (fb *fnb) finishBlocks() {
	for _, b := range fb.blocks {
		fb.seal(b)
	}
	for _, b := range fb.blocks {
		if len(b.phis) > 0 {
			b.b.Instrs = append(append([]Instr(nil), b.phis...), b.b.Instrs...)
			b.phis = nil
		}
	}
}
