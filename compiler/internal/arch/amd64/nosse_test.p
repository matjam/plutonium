// Ported from internal/arch/amd64/nosse_test.go at the go-cutover tag.

package amd64

import (
	"pluto/math"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/pu"
)

// errString returns the text of err, or "<nil>".
func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// noSSEFunc builds a NoSSE function that stores val (built by mk from a
// float constant) to a stack slot and returns.
func noSSEFunc(mk func(b *ssa.Block, c *ssa.Value) *ssa.Value) *ssa.Func {
	f := ssa.NewFunc("t")
	f.Sym = pu.New(ssa.Sym{Name: "t"})
	f.NoSSE = true
	b := f.Entry
	mem := b.NewValue(ssa.OpInitMem, ssa.TMem)
	c := b.NewValueI(ssa.OpConstFloat, ssa.TF64, int64(math.Float64bits(1.5)))
	val := mk(b, c)
	addr := b.NewValueA(ssa.OpSlotAddr, ssa.TPtr, f.NewSlot("x", 8, 8))
	st := b.NewValueA(ssa.OpStore, ssa.TMem, ssa.TF64, addr, val, mem)
	b.Kind = ssa.BlockRet
	b.SetControl(st)
	return f
}

// TestNoSSE checks that a NoSSE function moves float bits through
// general-purpose registers and refuses float arithmetic.
func TestNoSSE(t *testing.T) {
	f := noSSEFunc(func(b *ssa.Block, c *ssa.Value) *ssa.Value { return c })
	Prepare(f)
	code, err := Compile(f, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(code.Asm, "xmm") || len(code.Consts) != 0 {
		t.Errorf("float bits went through SSE:\n%s", code.Asm)
	}
	if !strings.Contains(code.Asm, "$4609434218613702656") {
		t.Errorf("want the constant's bits as an immediate:\n%s", code.Asm)
	}

	f = noSSEFunc(func(b *ssa.Block, c *ssa.Value) *ssa.Value { return b.NewValue(ssa.OpFAdd, ssa.TF64, c, c) })
	Prepare(f)
	if _, err := Compile(f, false); err == nil || !strings.Contains(err.Error(), "needs SSE") {
		t.Errorf("FAdd in a NoSSE function: err = %s", errString(err))
	}

	f = noSSEFunc(func(b *ssa.Block, c *ssa.Value) *ssa.Value { return b.NewValueI(ssa.OpArg, ssa.TF64, 0) })
	f.Params = compat.Append[*ssa.Type](nil, ssa.TF64)
	Prepare(f)
	if _, err := Compile(f, false); err == nil || !strings.Contains(err.Error(), "needs SSE") {
		t.Errorf("float argument in a NoSSE function: err = %s", errString(err))
	}
}

// TestCheckNoSSE checks the final scan of selected code.
func TestCheckNoSSE(t *testing.T) {
	mf := newMFunc("t")
	b := pu.New(MBlock{})
	mf.Blocks = compat.Append(mf.Blocks, b)
	b.Insts = insts(
		newInst(MInst{Op: MCALL, Sym: pu.New(ssa.Sym{Name: "g"}), ImplicitDefs: compat.Clone(callerSaved)}),
		newInst(MInst{Op: MMOV, Size: 8, Dst: RegOp(RAX), Src: ImmOp(1)}),
	)
	if err := checkNoSSE(mf); err != nil {
		t.Errorf("call clobbers and integer moves: %s", errString(err))
	}
	b.Insts = compat.Append(b.Insts, newInst(MInst{Op: MCOPY, Size: 8, Dst: RegOp(X1), Src: RegOp(X0)}))
	if err := checkNoSSE(mf); err == nil {
		t.Error("XMM copy not reported")
	}
	b.Insts = insts(newInst(MInst{Op: MXORPS, Size: 16, Dst: RegOp(RAX), Src: RegOp(RAX)}))
	if err := checkNoSSE(mf); err == nil {
		t.Error("SSE instruction not reported")
	}
}
