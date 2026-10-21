package sched

import (
	"testing"

	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/parser"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// typecheck checks src as package p and returns its package.
func typecheck(t *testing.T, src string) *types2.Package {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.p", "package p\n"+src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	conf := types2.Config{}
	pkg, err := conf.Check("p", fset, []*ast.File{f}, nil)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	return pkg
}

func lookupFunc(t *testing.T, pkg *types2.Package, name string) *types2.Func {
	t.Helper()
	fn, ok := pkg.Scope().Lookup(name).(*types2.Func)
	if !ok {
		t.Fatalf("no func %s", name)
	}
	return fn
}

func lookupType(t *testing.T, pkg *types2.Package, name string) types2.Type {
	t.Helper()
	tn, ok := pkg.Scope().Lookup(name).(*types2.TypeName)
	if !ok {
		t.Fatalf("no type %s", name)
	}
	return tn.Type()
}

// method returns method name of named type T.
func method(t *testing.T, T types2.Type, name string) *types2.Func {
	t.Helper()
	obj, _, _ := types2.LookupFieldOrMethod(T, true, nil, name)
	fn, ok := obj.(*types2.Func)
	if !ok {
		t.Fatalf("no method %s of %s", name, T)
	}
	return fn
}

// prog builds an eir.Program function by function.
type prog struct {
	p *eir.Program
}

func newProg() *prog {
	return &prog{p: &eir.Program{Sources: map[eir.FuncKey]eir.FuncID{}}}
}

// fn adds a function with one block.
func (pr *prog) fn(name string, key eir.FuncKey, sig *types2.Signature) *fbuild {
	id := eir.FuncID(len(pr.p.Funcs))
	f := &eir.Func{ID: id, Key: key, Name: name, Sig: sig, Blocks: []*eir.Block{{ID: 0, Loop: -1}}}
	if sig != nil {
		if r := sig.Recv(); r != nil {
			f.Inputs = append(f.Inputs, eir.Input{Var: r, Type: r.Type()})
		}
		for i := 0; i < sig.Params().Len(); i++ {
			v := sig.Params().At(i)
			f.Inputs = append(f.Inputs, eir.Input{Var: v, Type: v.Type()})
		}
		f.Results = sig.Results().Len()
	}
	pr.p.Funcs = append(pr.p.Funcs, f)
	if key != (eir.FuncKey{}) {
		pr.p.Sources[key] = id
	}
	return &fbuild{f: f}
}

// decl adds a declared function or method.
func (pr *prog) decl(fn *types2.Func) *fbuild {
	return pr.fn(fn.Name(), eir.FuncKey{Obj: fn}, fn.Signature())
}

type fbuild struct {
	f *eir.Func
}

func (b *fbuild) reg() eir.Reg {
	b.f.NumRegs++
	return eir.Reg(b.f.NumRegs)
}

// add appends an instruction, giving it a destination register if dst is
// true, and returns that register.
func (b *fbuild) add(in eir.Instr, dst bool) eir.Reg {
	if dst {
		in.Dst = b.reg()
	}
	if in.Op == eir.OpCall {
		in.Call.Site = eir.SiteID(b.f.NumSites)
		b.f.NumSites++
	}
	blk := b.f.Blocks[0]
	blk.Instrs = append(blk.Instrs, in)
	return in.Dst
}

func (b *fbuild) input(i int) eir.Reg {
	return b.add(eir.Instr{Op: eir.OpInput, Index: i}, true)
}

func (b *fbuild) heap() eir.Reg {
	return b.add(eir.Instr{Op: eir.OpHeap, Heap: eir.HeapAlloc}, true)
}

func (b *fbuild) obj(o eir.Object) eir.ObjID {
	b.f.Objects = append(b.f.Objects, o)
	return eir.ObjID(len(b.f.Objects) - 1)
}

func (b *fbuild) addr(o eir.ObjID, path ...int) eir.Reg {
	return b.add(eir.Instr{Op: eir.OpAddr, Obj: o, Path: path}, true)
}

func (b *fbuild) field(x eir.Reg, path ...int) eir.Reg {
	return b.add(eir.Instr{Op: eir.OpField, X: x, Path: path}, true)
}

func (b *fbuild) store(x, y eir.Reg, typ types2.Type) {
	b.add(eir.Instr{Op: eir.OpStore, X: x, Y: y, Type: typ}, false)
}

func (b *fbuild) load(x eir.Reg, typ types2.Type) eir.Reg {
	return b.add(eir.Instr{Op: eir.OpLoad, X: x, Type: typ}, true)
}

func (b *fbuild) call(c *eir.Call) {
	b.add(eir.Instr{Op: eir.OpCall, Call: c}, false)
}

// callStatic calls g with args and returns the result registers.
func (b *fbuild) callStatic(g *fbuild, args ...eir.Reg) []eir.Reg {
	c := &eir.Call{Kind: eir.CallStatic, Callee: g.f.ID, Sig: g.f.Sig, Args: args}
	for i := 0; i < g.f.Results; i++ {
		c.Results = append(c.Results, b.reg())
	}
	b.call(c)
	return c.Results
}

func (b *fbuild) ret(x eir.Reg, i int) {
	b.add(eir.Instr{Op: eir.OpSink, Sink: eir.SinkReturn, X: x, Index: i}, false)
}

func idsOf(fs ...*fbuild) []eir.FuncID {
	var out []eir.FuncID
	for _, f := range fs {
		out = append(out, f.f.ID)
	}
	sortIDs(out)
	return out
}

func sameIDs(a, b []eir.FuncID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
