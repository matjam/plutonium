// Translated by go2pu from tools/go2pu/testdata/amd64demo/main.go at the go-cutover tag; edited by hand since.

// Command amd64demo compiles SSA functions to x86-64 machine code with
// internal/arch/amd64 and prints the code, relocations and listing. go2pu
// translates it with amd64 and ssa, and TestDifferential checks that the
// Go and Plutonium builds print the same text.
package main

import (
	"pluto/fmt"
	"pluto/math"
	"selfhost/compat"
	"selfhost/internal/arch/amd64"
	"selfhost/internal/ssa"
	"selfhost/pu"
)

func main() {
	p := ssa.NewProgram()
	for _, f := range []*ssa.Func{abs(p), sum(p), memory(p), caller(p), floats(p)} {
		amd64.Prepare(f)
		if err := f.Verify(); err != nil {
			fmt.Println("verify:", err)
			continue
		}
		code, err := amd64.Compile(f, true)
		if err != nil {
			fmt.Println("compile:", err)
			continue
		}
		fmt.Printf("== %s: %d bytes\n", code.Sym, len(code.Bytes))
		for i := 0; i < len(code.Bytes); i += 32 {
			for _, b := range code.Bytes[i:min(i+32, len(code.Bytes))] {
				compat.Printfb("%02x", compat.ArgU(uint64(b)))
			}
			fmt.Println()
		}
		for _, r := range code.Relocs {
			fmt.Printf("reloc off=%d sym=%s type=%d addend=%d\n", r.Off, r.Sym, r.Type, r.Addend)
		}
		for _, c := range code.Consts {
			fmt.Printf("const %s", c.Sym)
			for _, b := range c.Bytes {
				compat.Printfb(" %02x", compat.ArgU(uint64(b)))
			}
			fmt.Println()
		}
		compat.Printfb("%v", compat.ArgS(code.Asm))
	}
}

func newFunc(p *ssa.Program, name string, params, results []*ssa.Type) *ssa.Func {
	f := ssa.NewFunc(name)
	f.Sym = p.Sym(compat.Concat("demo.", name), ssa.SymFunc)
	f.Params = params
	f.Results = results
	return f
}

// abs returns |x| through a phi.
func abs(p *ssa.Program) *ssa.Func {
	f := newFunc(p, "abs", compat.Clone([]*ssa.Type{ssa.TI64}), compat.Clone([]*ssa.Type{ssa.TI64}))
	entry := f.Entry
	mem := entry.NewValue(ssa.OpInitMem, ssa.TMem)
	x := entry.NewValueI(ssa.OpArg, ssa.TI64, 0)
	cmp := entry.NewValue(ssa.OpLess, ssa.TBool, x, f.ConstInt(ssa.TI64, 0))
	entry.Kind = ssa.BlockIf
	entry.SetControl(cmp)
	then := f.NewBlock(ssa.BlockPlain)
	els := f.NewBlock(ssa.BlockPlain)
	join := f.NewBlock(ssa.BlockRet)
	entry.AddEdgeTo(then)
	entry.AddEdgeTo(els)
	then.AddEdgeTo(join)
	els.AddEdgeTo(join)
	neg := then.NewValue(ssa.OpNeg, ssa.TI64, x)
	phi := join.NewValue(ssa.OpPhi, ssa.TI64, neg, x)
	join.SetControl(mem, phi)
	return f
}

// sum adds 0..n-1 in a loop.
func sum(p *ssa.Program) *ssa.Func {
	f := newFunc(p, "sum", compat.Clone([]*ssa.Type{ssa.TI64}), compat.Clone([]*ssa.Type{ssa.TI64}))
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

// memory stores two arguments through a pointer and returns their
// difference loaded back.
func memory(p *ssa.Program) *ssa.Func {
	f := newFunc(p, "memory", compat.Clone([]*ssa.Type{ssa.TPtr, ssa.TI32, ssa.TI32}), compat.Clone([]*ssa.Type{ssa.TI32}))
	e := f.Entry
	mem := e.NewValue(ssa.OpInitMem, ssa.TMem)
	ptr := e.NewValueI(ssa.OpArg, ssa.TPtr, 0)
	a := e.NewValueI(ssa.OpArg, ssa.TI32, 1)
	b := e.NewValueI(ssa.OpArg, ssa.TI32, 2)
	q := e.NewValueI(ssa.OpOffPtr, ssa.TPtr, 4, ptr)
	m1 := e.NewValueA(ssa.OpStore, ssa.TMem, ssa.TI32, ptr, a, mem)
	m2 := e.NewValueA(ssa.OpStore, ssa.TMem, ssa.TI32, q, b, m1)
	x := e.NewValue(ssa.OpLoad, ssa.TI32, ptr, m2)
	y := e.NewValue(ssa.OpLoad, ssa.TI32, q, m2)
	d := e.NewValue(ssa.OpSub, ssa.TI32, x, y)
	e.Kind = ssa.BlockRet
	e.SetControl(m2, d)
	return f
}

// caller calls abs and sum and adds the results.
func caller(p *ssa.Program) *ssa.Func {
	f := newFunc(p, "caller", compat.Clone([]*ssa.Type{ssa.TI64}), compat.Clone([]*ssa.Type{ssa.TI64}))
	e := f.Entry
	mem := e.NewValue(ssa.OpInitMem, ssa.TMem)
	x := e.NewValueI(ssa.OpArg, ssa.TI64, 0)
	tup := ssa.TupleOf(compat.Clone([]*ssa.Type{ssa.TMem, ssa.TI64})...)
	c1 := e.NewValueA(ssa.OpCall, tup, p.Sym("demo.abs", ssa.SymFunc), x, mem)
	c1.Call = pu.New(ssa.CallInfo{Results: compat.Clone([]*ssa.Type{ssa.TI64})})
	m1 := e.NewValueI(ssa.OpSelectN, ssa.TMem, 0, c1)
	r1 := e.NewValueI(ssa.OpSelectN, ssa.TI64, 1, c1)
	c2 := e.NewValueA(ssa.OpCall, tup, p.Sym("demo.sum", ssa.SymFunc), r1, m1)
	c2.Call = pu.New(ssa.CallInfo{Results: compat.Clone([]*ssa.Type{ssa.TI64})})
	m2 := e.NewValueI(ssa.OpSelectN, ssa.TMem, 0, c2)
	r2 := e.NewValueI(ssa.OpSelectN, ssa.TI64, 1, c2)
	s := e.NewValue(ssa.OpAdd, ssa.TI64, r1, r2)
	e.Kind = ssa.BlockRet
	e.SetControl(m2, s)
	return f
}

// floats computes sqrt(x*x + 2.5).
func floats(p *ssa.Program) *ssa.Func {
	f := newFunc(p, "floats", compat.Clone([]*ssa.Type{ssa.TF64}), compat.Clone([]*ssa.Type{ssa.TF64}))
	e := f.Entry
	mem := e.NewValue(ssa.OpInitMem, ssa.TMem)
	x := e.NewValueI(ssa.OpArg, ssa.TF64, 0)
	c := e.NewValueI(ssa.OpConstFloat, ssa.TF64, int64(math.Float64bits(2.5)))
	sq := e.NewValue(ssa.OpFMul, ssa.TF64, x, x)
	sum := e.NewValue(ssa.OpFAdd, ssa.TF64, sq, c)
	r := e.NewValue(ssa.OpFSqrt, ssa.TF64, sum)
	e.Kind = ssa.BlockRet
	e.SetControl(mem, r)
	return f
}
