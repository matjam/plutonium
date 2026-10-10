// Command optdemo runs the -O2 optimiser pipeline of internal/opt over SSA
// functions, then compiles them with internal/arch/amd64, and prints the
// optimised SSA and the machine code. go2pu translates it with opt, and
// the Go and Plutonium builds must print the same text.
package main

import (
	"fmt"

	"github.com/matjam/plutonium/internal/arch/amd64"
	"github.com/matjam/plutonium/internal/opt"
	"github.com/matjam/plutonium/internal/ssa"
)

func main() {
	p := ssa.NewProgram()
	p.Funcs = append(p.Funcs, divs(p), slots(p), sum(p))
	if err := opt.ForLevel(2).Run(p, nil); err != nil {
		fmt.Println("opt:", err)
		return
	}
	for _, f := range p.Funcs {
		fmt.Print(f)
		amd64.Prepare(f)
		code, err := amd64.Compile(f, true)
		if err != nil {
			fmt.Println("compile:", err)
			continue
		}
		for _, b := range code.Bytes {
			fmt.Printf("%02x", b)
		}
		fmt.Println()
		fmt.Print(code.Asm)
	}
}

func newFunc(p *ssa.Program, name string, params, results []*ssa.Type) *ssa.Func {
	f := ssa.NewFunc(name)
	f.Sym = p.Sym("demo."+name, ssa.SymFunc)
	f.Params = params
	f.Results = results
	return f
}

// divs divides by constants, which the optimiser turns into multiplications
// by magic numbers.
func divs(p *ssa.Program) *ssa.Func {
	f := newFunc(p, "divs", []*ssa.Type{ssa.TI64, ssa.TI64}, []*ssa.Type{ssa.TI64})
	e := f.Entry
	mem := e.NewValue(ssa.OpInitMem, ssa.TMem)
	x := e.NewValueI(ssa.OpArg, ssa.TI64, 0)
	y := e.NewValueI(ssa.OpArg, ssa.TI64, 1)
	a := e.NewValue(ssa.OpDivu, ssa.TI64, x, f.ConstInt(ssa.TI64, 7))
	b := e.NewValue(ssa.OpDiv, ssa.TI64, y, f.ConstInt(ssa.TI64, 10))
	c := e.NewValue(ssa.OpModu, ssa.TI64, x, f.ConstInt(ssa.TI64, 1000003))
	d := e.NewValue(ssa.OpDivu, ssa.TI64, y, f.ConstInt(ssa.TI64, -3))
	s := e.NewValue(ssa.OpAdd, ssa.TI64, a, b)
	s = e.NewValue(ssa.OpAdd, ssa.TI64, s, c)
	s = e.NewValue(ssa.OpAdd, ssa.TI64, s, d)
	e.Kind = ssa.BlockRet
	e.SetControl(mem, s)
	return f
}

// slots keeps a value in a stack slot, which scalar replacement and
// mem2reg promote to a register.
func slots(p *ssa.Program) *ssa.Func {
	f := newFunc(p, "slots", []*ssa.Type{ssa.TI64}, []*ssa.Type{ssa.TI64})
	e := f.Entry
	mem := e.NewValue(ssa.OpInitMem, ssa.TMem)
	x := e.NewValueI(ssa.OpArg, ssa.TI64, 0)
	slot := f.NewSlot("v", 8, 8)
	slot.Leaves = []ssa.Leaf{{Off: 0, Type: ssa.TI64}}
	addr := e.NewValueA(ssa.OpSlotAddr, ssa.TPtr, slot)
	m1 := e.NewValueA(ssa.OpStore, ssa.TMem, ssa.TI64, addr, x, mem)
	v := e.NewValue(ssa.OpLoad, ssa.TI64, addr, m1)
	dbl := e.NewValue(ssa.OpAdd, ssa.TI64, v, v)
	m2 := e.NewValueA(ssa.OpStore, ssa.TMem, ssa.TI64, addr, dbl, m1)
	w := e.NewValue(ssa.OpLoad, ssa.TI64, addr, m2)
	e.Kind = ssa.BlockRet
	e.SetControl(m2, w)
	return f
}

// sum adds 0..n-1 in a loop.
func sum(p *ssa.Program) *ssa.Func {
	f := newFunc(p, "sum", []*ssa.Type{ssa.TI64}, []*ssa.Type{ssa.TI64})
	entry := f.Entry
	mem := entry.NewValue(ssa.OpInitMem, ssa.TMem)
	n := entry.NewValueI(ssa.OpArg, ssa.TI64, 0)
	zero := f.ConstInt(ssa.TI64, 0)
	one := f.ConstInt(ssa.TI64, 1)
	header := f.NewBlock(ssa.BlockIf)
	body := f.NewBlock(ssa.BlockPlain)
	exit := f.NewBlock(ssa.BlockRet)
	entry.AddEdgeTo(header)
	header.AddEdgeTo(body)
	header.AddEdgeTo(exit)
	body.AddEdgeTo(header)
	i := header.NewValue(ssa.OpPhi, ssa.TI64)
	s := header.NewValue(ssa.OpPhi, ssa.TI64)
	header.SetControl(header.NewValue(ssa.OpLess, ssa.TBool, i, n))
	snext := body.NewValue(ssa.OpAdd, ssa.TI64, s, i)
	inext := body.NewValue(ssa.OpAdd, ssa.TI64, i, one)
	i.AddArg(zero)
	i.AddArg(inext)
	s.AddArg(zero)
	s.AddArg(snext)
	exit.SetControl(mem, s)
	return f
}
