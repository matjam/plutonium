package x86_64_test

import (
	"testing"

	x86 "github.com/matjam/plutonium/internal/codegen/x86_64"
	"github.com/matjam/plutonium/internal/ir"
)

func TestCompileSimpleAdd(t *testing.T) {
	fn := ir.NewFunction("add", nil, ir.I32)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I32)
	b := fn.NewParam("b", ir.I32)
	sum := entry.BinOp(ir.OpAdd, ir.I32, a, b)
	entry.Ret(ir.I32, sum)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
	// Verify function ends with RET (0xC3)
	if mf.Code[len(mf.Code)-1] != 0xC3 {
		t.Errorf("expected RET (0xC3) at end, got 0x%02X", mf.Code[len(mf.Code)-1])
	}
}

func TestCompileRetVoid(t *testing.T) {
	fn := ir.NewFunction("noop", nil, ir.Void)
	entry := fn.NewBlock("entry")
	entry.RetVoid()

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	if mf.Code[len(mf.Code)-1] != 0xC3 {
		t.Errorf("expected RET, got 0x%02X", mf.Code[len(mf.Code)-1])
	}
}

func TestCompileIfBranch(t *testing.T) {
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

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code for branching function")
	}
}

func TestBackendMetadata(t *testing.T) {
	b := x86.New()
	if b.Name() != "x86_64" {
		t.Errorf("got name %q, want x86_64", b.Name())
	}
	if b.Triple() != "x86_64-linux" {
		t.Errorf("got triple %q", b.Triple())
	}
	if b.WordSize() != 8 {
		t.Errorf("got word size %d, want 8", b.WordSize())
	}
}
