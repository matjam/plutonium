package eir

import (
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// The bodies the lowerer gives functions of pluto/rt that are declared
// without one (lower/sched.go, lower/global.go): the scheduler hooks, which
// call a method of the program's //pu:scheduler variable; the late hooks
// (rt.Sleep, rt.mainWait), which call the function that needs no scheduler
// or the one that parks, chosen once the whole program is lowered; and
// pluto.main (rt.programMain), the hosted entry, which initializes every
// package, calls main.main and drops the package variables.

// schedHooks are the scheduler hooks of pluto/rt and the method of the
// bound scheduler each calls (lower/sched.go schedHooks).
var schedHooks = map[string]string{
	"schedSpawn": "Spawn", "schedPark": "Park", "schedWake": "Wake", "schedYield": "Yield",
	"schedInit": "Init", "schedInInterrupt": "InInterrupt", "schedNow": "Now", "schedNowOpt": "Now",
	"schedArmTimer": "ArmTimer", "schedFreeTask": "FreeTask", "schedNewTask": "NewTask",
}

// lateHooks are the late hooks of pluto/rt and the two functions each may
// call (lower/sched.go lateHooks). EIR calls both: which one the lowerer
// chooses depends on whether the program starts the scheduler.
var lateHooks = map[string][2]string{
	"Sleep":    {"sleepDirect", "sleepParked"},
	"mainWait": {"mainWaitDirect", "mainWaitParked"},
}

// hookBody builds the body the lowerer gives a body-less function, and
// reports whether it gives it one.
func (b *builder) hookBody(f *Func, d *declRef) bool {
	fn := f.Key.Obj
	isRT := fn.Pkg() != nil && fn.Pkg().Path() == "pluto/rt"
	method, isSched := schedHooks[fn.Name()]
	late, isLate := lateHooks[fn.Name()]
	entry := hasExternName(d.decl, "pluto.main")
	if !entry && (!isRT || !isSched && !isLate) {
		return false
	}
	fb := b.newFnb(f, d.pkg, nil)
	fb.addressed = map[*types2.Var]bool{}
	var in []Reg
	for i := 0; i < f.Sig.Params().Len(); i++ {
		p := f.Sig.Params().At(i)
		f.Inputs = append(f.Inputs, Input{Var: p, Type: p.Type()})
		in = append(in, fb.input(i))
	}
	var res []Reg
	switch {
	case entry:
		f.External = true
		b.entryBody(fb)
	case isSched:
		res = b.schedHookBody(fb, method, in)
	case isLate:
		for _, name := range late {
			if g := b.rtFunc(name); g != nil {
				res = fb.emitCall(&Call{Kind: CallStatic, Callee: b.funcOf(g), Sig: g.Type().(*types2.Signature), Args: in, Implicit: ImplWrapper}, token.NoPos)
			}
		}
	}
	for i := 0; i < f.Sig.Results().Len() && i < len(res); i++ {
		fb.sink(SinkReturn, res[i], i, token.NoPos)
	}
	fb.cur = nil
	fb.finish()
	return true
}

// hasExternName reports whether a declaration is //pu:extern name.
func hasExternName(fd *ast.FuncDecl, name string) bool {
	if fd.Doc == nil {
		return false
	}
	for _, c := range fd.Doc.List {
		if f := strings.Fields(strings.TrimPrefix(c.Text, "//pu:")); len(f) == 2 && strings.HasPrefix(c.Text, "//pu:extern") && f[1] == name {
			return true
		}
	}
	return false
}

// entryBody is pluto.main: every package's initialization in order, then
// main.main, then the drops of the package variables (lower/shutdown.go).
func (b *builder) entryBody(fb *fnb) {
	noArgs := types2.NewSignatureType(nil, nil, nil, nil, nil, false)
	for _, p := range b.prog.Packages {
		fb.emitCall(&Call{Kind: CallStatic, Callee: b.p.Lookup(FuncKey{Init: p.Types}), Sig: noArgs, Implicit: ImplWrapper}, token.NoPos)
	}
	if b.prog.Main != nil {
		if m, ok := b.prog.Main.Types.Scope().Lookup("main").(*types2.Func); ok {
			fb.emitCall(&Call{Kind: CallStatic, Callee: b.funcOf(m), Sig: m.Type().(*types2.Signature), Implicit: ImplWrapper}, token.NoPos)
		}
	}
	for i := len(b.prog.Packages) - 1; i >= 0; i-- {
		p := b.prog.Packages[i]
		for _, init := range p.Info.InitOrder {
			for _, v := range init.Lhs {
				if v.Name() != "_" {
					fb.dropAt(fb.heap(HeapStatic), v.Type(), token.NoPos)
				}
			}
			// The static objects the initializer made: &T{...} and the
			// arrays of slice literals, which live until the program ends.
			ast.Inspect(init.Rhs, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.FuncLit:
					return false
				case *ast.CompositeLit:
					T := b.types[n].Type
					if s, ok := coreType(T).(*types2.Slice); ok {
						T = s.Elem()
					}
					if T != nil {
						fb.dropAt(fb.heap(HeapStatic), T, token.NoPos)
					}
				}
				return true
			})
		}
		for _, name := range p.Types.Scope().Names() {
			if v, ok := p.Types.Scope().Lookup(name).(*types2.Var); ok && !initialized(p.Info.InitOrder, v) {
				fb.dropAt(fb.heap(HeapStatic), v.Type(), token.NoPos)
			}
		}
	}
}

// initialized reports whether v has an initializer.
func initialized(order []*types2.Initializer, v *types2.Var) bool {
	for _, init := range order {
		for _, w := range init.Lhs {
			if w == v {
				return true
			}
		}
	}
	return false
}

// schedHookBody calls method of the program's //pu:scheduler variable with
// the hook's arguments.
func (b *builder) schedHookBody(fb *fnb, method string, in []Reg) []Reg {
	v := b.scheduler()
	if v == nil {
		return nil
	}
	obj, path, _ := types2.LookupFieldOrMethod(v.Type(), true, v.Pkg(), method)
	m, ok := obj.(*types2.Func)
	if !ok {
		return nil // an optional method the backend lacks
	}
	T := v.Type()
	addr := fb.heap(HeapStatic)
	if el := pointerElem(T); el != nil {
		addr, T = fb.load(addr, T), el
	}
	_, wantPtr := m.Type().(*types2.Signature).Recv().Type().(*types2.Pointer)
	recv := fb.receiverAt(addr, T, path, wantPtr, m)
	return fb.wrapperCall(m, append([]Reg{recv}, in...))
}

// scheduler returns the program's //pu:scheduler variable: its own, or the
// runtime's default (lower/sched.go findScheduler).
func (b *builder) scheduler() *types2.Var {
	var dflt *types2.Var
	for _, p := range b.prog.Packages {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.VAR {
					continue
				}
				for _, spec := range gd.Specs {
					vs := spec.(*ast.ValueSpec)
					doc := vs.Doc
					if doc == nil {
						doc = gd.Doc
					}
					if doc == nil || !hasLine(doc, "//pu:scheduler") || len(vs.Names) == 0 {
						continue
					}
					v, _ := p.Info.Defs[vs.Names[0]].(*types2.Var)
					if v == nil {
						continue
					}
					if p.Path != "pluto/rt" {
						return v
					}
					if dflt == nil {
						dflt = v
					}
				}
			}
		}
	}
	return dflt
}

// hasLine reports whether a comment group has the line text.
func hasLine(doc *ast.CommentGroup, text string) bool {
	for _, c := range doc.List {
		if strings.TrimSpace(c.Text) == text {
			return true
		}
	}
	return false
}
