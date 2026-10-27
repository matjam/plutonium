// Ported from internal/opt/memopt_test.go at the go-cutover tag.

package opt

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/pu"
)

// slotPastCall builds a function that clears a slot, makes a call, and
// returns the slot's second word loaded back. With leak set, the call is
// given the slot's address; otherwise only a call that never returns, on
// another path, is.
func slotPastCall(t *testing.T, p *ssa.Program, leak bool) *ssa.Func {
	t.Helper()
	g, fail := p.Sym("g", ssa.SymFunc), p.Sym("fail", ssa.SymFunc)
	f := ssa.NewFunc("f")
	f.Params = compat.Append[*ssa.Type](nil, ssa.TBool)
	f.Results = compat.Append[*ssa.Type](nil, ssa.TI64)
	b := f.Entry
	mem := b.NewValue(ssa.OpInitMem, ssa.TMem)
	cond := b.NewValueI(ssa.OpArg, ssa.TBool, 0)
	s := f.NewSlot("box", 16, 8)
	addr := b.NewValueA(ssa.OpSlotAddr, ssa.TPtr, s)
	m := b.NewValueI(ssa.OpZero, ssa.TMem, 16, addr, mem)
	b.Kind = ssa.BlockIf
	b.SetControl(cond)
	panics := f.NewBlock(ssa.BlockExit)
	rest := f.NewBlock(ssa.BlockRet)
	b.AddEdgeTo(panics)
	b.AddEdgeTo(rest)

	pc := panics.NewValueA(ssa.OpCall, memTuple(), fail, addr, m)
	pc.Call = pu.New(ssa.CallInfo{ABI: ssa.ABIInternal, NoReturn: true})
	panics.SetControl(panics.NewValueI(ssa.OpSelectN, ssa.TMem, 0, pc))

	var call *ssa.Value
	if leak {
		call = rest.NewValueA(ssa.OpCall, memTuple(), g, addr, m)
	} else {
		call = rest.NewValueA(ssa.OpCall, memTuple(), g, m)
	}
	call.Call = pu.New(ssa.CallInfo{ABI: ssa.ABIInternal})
	m = rest.NewValueI(ssa.OpSelectN, ssa.TMem, 0, call)
	word := rest.NewValueI(ssa.OpOffPtr, ssa.TPtr, 8, addr)
	l := rest.NewValue(ssa.OpLoad, ssa.TI64, word, m)
	rest.SetControl(m, l)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestMemOptPrivateSlot checks that a load from a slot no returning call
// is given sees past calls, and reads a cleared word as 0.
func TestMemOptPrivateSlot(t *testing.T) {
	p := ssa.NewProgram()
	f := slotPastCall(t, p, false)
	MemOpt(f)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	ret := f.Blocks[len(f.Blocks)-1].Controls
	if ret[1].Op != ssa.OpConst || ret[1].AuxInt != 0 {
		t.Errorf("want the load to be 0:\n%s", f.String())
	}

	f = slotPastCall(t, p, true)
	MemOpt(f)
	ret = f.Blocks[len(f.Blocks)-1].Controls
	if ret[1].Op != ssa.OpLoad {
		t.Errorf("the call may write the slot; want the load kept:\n%s", f.String())
	}
}
