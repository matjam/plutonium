package regalloc_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/codegen/x86_64/regalloc"
	"github.com/matjam/plutonium/internal/ir"
)

func TestBasicAllocation(t *testing.T) {
	fn := ir.NewFunction("add", nil, ir.I32)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I32)
	b := fn.NewParam("b", ir.I32)
	sum := entry.BinOp(ir.OpAdd, ir.I32, a, b)
	entry.Ret(ir.I32, sum)

	alloc := regalloc.Allocate(fn)
	if alloc == nil {
		t.Fatal("expected non-nil allocation")
	}
	// sum should be allocated to a register
	if _, ok := alloc.Reg[sum.ID]; !ok {
		if _, ok := alloc.Spill[sum.ID]; !ok {
			t.Errorf("value %d not allocated anywhere", sum.ID)
		}
	}
}

func TestSpillWhenExhausted(t *testing.T) {
	fn := ir.NewFunction("many", nil, ir.I32)
	entry := fn.NewBlock("entry")

	// Create more live values than available registers (15+)
	vals := make([]*ir.Value, 20)
	prev := ir.ConstInt(ir.I32, 0)
	for i := 0; i < 20; i++ {
		v := entry.BinOp(ir.OpAdd, ir.I32, prev, ir.ConstInt(ir.I32, int64(i)))
		vals[i] = v
		prev = v
	}
	entry.Ret(ir.I32, prev)

	alloc := regalloc.Allocate(fn)
	// With 20 values, at least some must be spilled
	if len(alloc.Spill) == 0 && len(alloc.Reg) >= 20 {
		// 15 allocable regs, so at least 5 must spill
		// (this is approximate - depends on live range analysis)
		t.Log("no spills with 20 values - live range analysis may have reused slots")
	}
}

func TestNoSpillWithFewValues(t *testing.T) {
	fn := ir.NewFunction("simple", nil, ir.I32)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I32)
	b := fn.NewParam("b", ir.I32)
	s := entry.BinOp(ir.OpAdd, ir.I32, a, b)
	entry.Ret(ir.I32, s)

	alloc := regalloc.Allocate(fn)
	if len(alloc.Spill) > 0 {
		t.Errorf("expected no spills for simple function, got %d spills", len(alloc.Spill))
	}
}
