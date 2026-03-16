package ir

import "fmt"

// NewFunction creates a new IR function.
func NewFunction(name string, params []*Value, retType *Type) *Function {
	fn := &Function{
		Name:    name,
		RetType: retType,
		nextID:  1,
	}
	fn.Params = params
	return fn
}

// NewParam creates a new function parameter value.
func (fn *Function) NewParam(name string, typ *Type) *Value {
	v := &Value{ID: fn.nextID, Kind: ValParam, Type: typ, Name: name}
	fn.nextID++
	fn.Params = append(fn.Params, v)
	return v
}

// NewBlock creates a new basic block in the function.
// Labels are made unique by appending a numeric suffix.
func (fn *Function) NewBlock(label string) *BasicBlock {
	uniqueLabel := fmt.Sprintf("%s.%d", label, fn.nextBlockID)
	fn.nextBlockID++
	bb := &BasicBlock{Label: uniqueLabel, fn: fn}
	fn.Blocks = append(fn.Blocks, bb)
	return bb
}

func (fn *Function) newValue(typ *Type) *Value {
	v := &Value{ID: fn.nextID, Kind: ValInstr, Type: typ}
	fn.nextID++
	return v
}

// --- BasicBlock instruction builders ---

// Alloca allocates a stack slot of the given type.
// Returns a value with the element type (for simplicity in v0.1).
func (bb *BasicBlock) Alloca(typ *Type) *Value {
	result := bb.fn.newValue(typ)
	instr := &Instruction{Op: OpAlloca, Result: result, AllocType: typ}
	bb.Instrs = append(bb.Instrs, instr)
	return result
}

// AllocaAligned allocates a stack slot of the given type with explicit
// alignment. If align is 0 or ≤ 8, default alignment is used.
func (bb *BasicBlock) AllocaAligned(typ *Type, align int) *Value {
	result := bb.fn.newValue(typ)
	instr := &Instruction{Op: OpAlloca, Result: result, AllocType: typ, AllocAlign: align}
	bb.Instrs = append(bb.Instrs, instr)
	return result
}

// Load loads a value from a pointer.
func (bb *BasicBlock) Load(typ *Type, ptr *Value) *Value {
	result := bb.fn.newValue(typ)
	instr := &Instruction{Op: OpLoad, Result: result, Args: []*Value{ptr}}
	bb.Instrs = append(bb.Instrs, instr)
	return result
}

// LoadVolatile loads a value from a pointer, marking the load as volatile.
// Volatile loads must not be elided, coalesced, or reordered by any optimizer.
func (bb *BasicBlock) LoadVolatile(typ *Type, ptr *Value) *Value {
	result := bb.fn.newValue(typ)
	instr := &Instruction{Op: OpLoad, Result: result, Args: []*Value{ptr}, IsVolatile: true}
	bb.Instrs = append(bb.Instrs, instr)
	return result
}

// StoreVolatile stores a value to a pointer, marking the store as volatile.
// Volatile stores must not be elided, coalesced, or reordered by any optimizer.
func (bb *BasicBlock) StoreVolatile(typ *Type, val, ptr *Value) {
	instr := &Instruction{Op: OpStore, Args: []*Value{val, ptr}, IsVolatile: true}
	bb.Instrs = append(bb.Instrs, instr)
}

// LoadBSwap loads a value from a pointer, optionally byte-swapping the result.
// When bswap is true, the codegen emits BSWAP immediately after the load.
func (bb *BasicBlock) LoadBSwap(typ *Type, ptr *Value, bswap bool) *Value {
	result := bb.fn.newValue(typ)
	instr := &Instruction{Op: OpLoad, Result: result, Args: []*Value{ptr}, NeedsBSwap: bswap}
	bb.Instrs = append(bb.Instrs, instr)
	return result
}

// Store stores a value to a pointer (no result).
func (bb *BasicBlock) Store(typ *Type, val, ptr *Value) {
	instr := &Instruction{Op: OpStore, Args: []*Value{val, ptr}}
	bb.Instrs = append(bb.Instrs, instr)
}

// StoreBSwap stores a value to a pointer, optionally byte-swapping before the store.
// When bswap is true, the codegen emits BSWAP on the value before storing.
func (bb *BasicBlock) StoreBSwap(typ *Type, val, ptr *Value, bswap bool) {
	instr := &Instruction{Op: OpStore, Args: []*Value{val, ptr}, NeedsBSwap: bswap}
	bb.Instrs = append(bb.Instrs, instr)
}

// BinOp emits a binary operation.
func (bb *BasicBlock) BinOp(op OpCode, typ *Type, a, b *Value) *Value {
	result := bb.fn.newValue(typ)
	instr := &Instruction{Op: op, Result: result, Args: []*Value{a, b}}
	bb.Instrs = append(bb.Instrs, instr)
	return result
}

// Cmp emits a comparison, returning a bool value.
func (bb *BasicBlock) Cmp(cmp CmpOp, typ *Type, a, b *Value) *Value {
	result := bb.fn.newValue(Bool)
	instr := &Instruction{Op: OpCmp, Result: result, CmpOp: cmp, Args: []*Value{a, b}}
	bb.Instrs = append(bb.Instrs, instr)
	return result
}

// GEP emits a get-element-pointer instruction with 8-byte element size.
func (bb *BasicBlock) GEP(ptr *Value, indices []*Value) *Value {
	return bb.GEPSized(ptr, indices, 8)
}

// GEPSized emits a get-element-pointer instruction with a specified element size.
func (bb *BasicBlock) GEPSized(ptr *Value, indices []*Value, elemSize int64) *Value {
	result := bb.fn.newValue(ptr.Type)
	instr := &Instruction{Op: OpGEP, Result: result, Args: append([]*Value{ptr}, indices...), ElemSize: elemSize}
	bb.Instrs = append(bb.Instrs, instr)
	return result
}

// Call emits a function call.
func (bb *BasicBlock) Call(fn *Function, retType *Type, args ...*Value) *Value {
	var result *Value
	if retType != nil && retType.Kind != TyVoid {
		result = bb.fn.newValue(retType)
	}
	instr := &Instruction{Op: OpCall, Result: result, Callee: fn, Args: args}
	bb.Instrs = append(bb.Instrs, instr)
	return result
}

// Convert emits a type conversion.
func (bb *BasicBlock) Convert(op OpCode, from, to *Type, val *Value) *Value {
	result := bb.fn.newValue(to)
	instr := &Instruction{Op: op, Result: result, Args: []*Value{val}, FromType: from, ToType: to}
	bb.Instrs = append(bb.Instrs, instr)
	return result
}

// ExtractField extracts a field from a struct value.
func (bb *BasicBlock) ExtractField(typ *Type, base *Value, idx int) *Value {
	result := bb.fn.newValue(typ)
	instr := &Instruction{Op: OpExtractField, Result: result, Args: []*Value{base}, FieldIdx: idx}
	bb.Instrs = append(bb.Instrs, instr)
	return result
}

// InsertField stores a value into a struct field.
func (bb *BasicBlock) InsertField(typ *Type, base *Value, idx int, val *Value) {
	instr := &Instruction{Op: OpInsertField, Args: []*Value{base, val}, FieldIdx: idx}
	bb.Instrs = append(bb.Instrs, instr)
}

// BSwap emits a byte-swap instruction (endianness conversion).
func (bb *BasicBlock) BSwap(typ *Type, val *Value) *Value {
	result := bb.fn.newValue(typ)
	instr := &Instruction{Op: OpBSwap, Result: result, Args: []*Value{val}}
	bb.Instrs = append(bb.Instrs, instr)
	return result
}

// --- Terminators ---

// Ret terminates the block with a return value.
func (bb *BasicBlock) Ret(typ *Type, val *Value) {
	bb.Term = &Terminator{Kind: TermRet, RetVal: val, RetType: typ}
}

// RetVoid terminates the block with a void return.
func (bb *BasicBlock) RetVoid() {
	bb.Term = &Terminator{Kind: TermRetVoid}
}

// Br terminates the block with an unconditional branch.
func (bb *BasicBlock) Br(target *BasicBlock) {
	bb.Term = &Terminator{Kind: TermBr, Then: target}
}

// CondBr terminates the block with a conditional branch.
func (bb *BasicBlock) CondBr(cond *Value, then, els *BasicBlock) {
	bb.Term = &Terminator{Kind: TermCondBr, Cond: cond, Then: then, Else: els}
}

// Unreachable marks the block as unreachable.
func (bb *BasicBlock) Unreachable() {
	bb.Term = &Terminator{Kind: TermUnreachable}
}
