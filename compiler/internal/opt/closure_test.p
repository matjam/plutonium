// Ported from internal/opt/closure_test.go at the go-cutover tag.

package opt

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/pu"
)

// memTuple returns the type of a call's result that is memory alone, in Go
// ssa.TupleOf(ssa.TMem). TupleOf keeps its slice, so it is given a copy
// rather than the call's variadic array.
func memTuple() *ssa.Type {
	return ssa.TupleOf(compat.Clone([]*ssa.Type{ssa.TMem})...)
}

// recordLoad builds a function that stores code pointers to the functions
// first and second into word 0 of a closure record, then the address of a
// slot into word 1, and returns both words loaded back.
func recordLoad(t *testing.T, first, second *ssa.Sym) *ssa.Func {
	t.Helper()
	f := ssa.NewFunc("rec")
	f.Results = compat.Append[*ssa.Type](nil, ssa.TPtr, ssa.TPtr)
	b := f.Entry
	b.Kind = ssa.BlockRet
	mem := b.NewValue(ssa.OpInitMem, ssa.TMem)
	rec := f.NewSlot("closure", 16, 8)
	rec.Closure = true
	x := f.NewSlot("x", 8, 8)
	addr := b.NewValueA(ssa.OpSlotAddr, ssa.TPtr, rec)
	word1 := b.NewValueI(ssa.OpOffPtr, ssa.TPtr, 8, addr)
	code1 := b.NewValueA(ssa.OpFuncAddr, ssa.TPtr, first)
	code2 := b.NewValueA(ssa.OpFuncAddr, ssa.TPtr, second)
	xa := b.NewValueA(ssa.OpSlotAddr, ssa.TPtr, x)
	m := b.NewValueA(ssa.OpStore, ssa.TMem, ssa.TPtr, addr, code1, mem)
	m = b.NewValueA(ssa.OpStore, ssa.TMem, ssa.TPtr, addr, code2, m)
	m = b.NewValueA(ssa.OpStore, ssa.TMem, ssa.TPtr, word1, xa, m)
	// A call that receives the record could, as far as memory goes,
	// write to it; the record's flag says it does not.
	call := b.NewValue(ssa.OpCallInd, memTuple(), code2, addr, m)
	call.Call = pu.New(ssa.CallInfo{ABI: ssa.ABIInternal})
	m = b.NewValueI(ssa.OpSelectN, ssa.TMem, 0, call)
	l0 := b.NewValue(ssa.OpLoad, ssa.TPtr, addr, m)
	l1 := b.NewValue(ssa.OpLoad, ssa.TPtr, word1, m)
	b.SetControl(m, l0, l1)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestForwardClosureLoads(t *testing.T) {
	p := ssa.NewProgram()
	g, h := p.Sym("g", ssa.SymFunc), p.Sym("h", ssa.SymFunc)

	f := recordLoad(t, g, g)
	if !ForwardClosureLoads(f) {
		t.Fatal("nothing forwarded")
	}
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	ret := f.Entry.Controls
	if ret[1].Op != ssa.OpFuncAddr || ret[1].Aux != g || ret[2].Op != ssa.OpSlotAddr {
		t.Errorf("loads not forwarded:\n%s", f.String())
	}

	// Stores of different code pointers leave word 0 alone; word 1 is
	// still known.
	f = recordLoad(t, g, h)
	ForwardClosureLoads(f)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	ret = f.Entry.Controls
	if ret[1].Op != ssa.OpLoad || ret[2].Op != ssa.OpSlotAddr {
		t.Errorf("want word 0 loaded and word 1 forwarded:\n%s", f.String())
	}
}
