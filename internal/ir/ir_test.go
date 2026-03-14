package ir_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/ir"
)

func TestBasicBlock(t *testing.T) {
	fn := ir.NewFunction("add", nil, ir.I32)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I32)
	b := fn.NewParam("b", ir.I32)
	sum := entry.BinOp(ir.OpAdd, ir.I32, a, b)
	entry.Ret(ir.I32, sum)

	if fn.Name != "add" {
		t.Errorf("got name %q, want %q", fn.Name, "add")
	}
	if len(fn.Blocks) != 1 {
		t.Errorf("got %d blocks, want 1", len(fn.Blocks))
	}
	if len(entry.Instrs) != 1 {
		t.Errorf("got %d instructions, want 1", len(entry.Instrs))
	}
	if entry.Term == nil {
		t.Error("expected terminator")
	}
}

func TestAlloca(t *testing.T) {
	fn := ir.NewFunction("foo", nil, ir.Void)
	entry := fn.NewBlock("entry")
	slot := entry.Alloca(ir.I32)
	if slot == nil {
		t.Error("expected non-nil alloca value")
	}
	if slot.Type.Kind != ir.TyI32 {
		t.Errorf("expected alloca type I32, got %v", slot.Type.Kind)
	}
	entry.RetVoid()
}

func TestLoadStore(t *testing.T) {
	fn := ir.NewFunction("inc", nil, ir.Void)
	entry := fn.NewBlock("entry")
	p := fn.NewParam("p", &ir.Type{Kind: ir.TyPtr, Elem: ir.I32})
	val := entry.Load(ir.I32, p)
	one := ir.ConstInt(ir.I32, 1)
	sum := entry.BinOp(ir.OpAdd, ir.I32, val, one)
	entry.Store(ir.I32, sum, p)
	entry.RetVoid()

	if len(entry.Instrs) != 3 { // load, add, store
		t.Errorf("got %d instrs, want 3", len(entry.Instrs))
	}
}

func TestBranch(t *testing.T) {
	fn := ir.NewFunction("abs", nil, ir.I32)
	entry := fn.NewBlock("entry")
	n := fn.NewParam("n", ir.I32)
	zero := ir.ConstInt(ir.I32, 0)
	cond := entry.Cmp(ir.CmpLT, ir.I32, n, zero)

	negBlock := fn.NewBlock("neg")
	posBlock := fn.NewBlock("pos")
	entry.CondBr(cond, negBlock, posBlock)

	neg := negBlock.BinOp(ir.OpSub, ir.I32, zero, n)
	negBlock.Ret(ir.I32, neg)
	posBlock.Ret(ir.I32, n)

	if len(fn.Blocks) != 3 {
		t.Errorf("got %d blocks, want 3", len(fn.Blocks))
	}
}

func TestCall(t *testing.T) {
	fn := ir.NewFunction("caller", nil, ir.I32)
	callee := ir.NewFunction("callee", nil, ir.I32)
	entry := fn.NewBlock("entry")
	result := entry.Call(callee, ir.I32)
	entry.Ret(ir.I32, result)
	_ = callee
}

func TestGEP(t *testing.T) {
	fn := ir.NewFunction("gep_test", nil, ir.Void)
	entry := fn.NewBlock("entry")
	sliceTy := &ir.Type{Kind: ir.TySlice, Elem: ir.I32}
	slicePtr := fn.NewParam("s", &ir.Type{Kind: ir.TyPtr, Elem: sliceTy})
	// Get pointer to the data field (index 0)
	dataPtr := entry.GEP(slicePtr, []*ir.Value{ir.ConstInt(ir.I64, 0), ir.ConstInt(ir.I32, 0)})
	_ = dataPtr
	entry.RetVoid()
}
