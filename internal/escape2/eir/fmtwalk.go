package eir

import (
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// pluto/fmt's walk of a value by its type descriptor (plan invariant I7: a
// library function that converts pointers to uintptr and back, and offsets
// them with unsafe.Add, has a contract that states its effects). The walk
// reads the value it is given, everything that value reaches, and calls the
// formatting methods of what it reaches (String, Error, GoString, and the
// containers' FmtList, FmtSlots and FmtSlot) with views of it; it keeps
// nothing itself (the printer's saved state, curT and curP, is restored
// before it returns). EIR gives each function of the walk that body in
// place of its source: a loop over everything reachable from its pointer
// parameters (offsets anywhere in an object, eir.AnyStep, then loads),
// calling each formatting method on it through its interface, and walking
// what the methods return (the values FmtList and FmtSlot return; the
// strings of String, Error and GoString are only printed). The analysis
// then checks the methods the walk
// may call, on what they may be called with, and nothing else of the
// walk's unsafe code.

// fmtWalk are the methods of pluto/fmt's printer that make up the walk.
var fmtWalk = map[string]bool{
	"printArg": true, "handleMethods": true, "printPayload": true,
	"printValue": true, "printWord": true, "printWordBits": true, "printKind": true,
	"printList": true, "printStruct": true, "printLister": true, "printMap": true,
	"fmtPointer": true, "fmtBytes": true,
}

// fmtWalkMethods are the methods the walk calls, by the interface of
// pluto/fmt (or the universe, for error) that declares them.
var fmtWalkMethods = []struct{ iface, method string }{
	{"Stringer", "String"}, {"GoStringer", "GoString"}, {"error", "Error"},
	{"Lister", "FmtList"}, {"Mapper", "FmtSlots"}, {"Mapper", "FmtSlot"},
}

// IsFmtWalk reports whether fn is a function of pluto/fmt's walk, whose
// EIR body is its contract and not its source.
func IsFmtWalk(fn *types2.Func) bool {
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != "pluto/fmt" || !fmtWalk[fn.Name()] {
		return false
	}
	sig, _ := fn.Type().(*types2.Signature)
	if sig == nil || sig.Recv() == nil {
		return false
	}
	p, ok := types2.Unalias(sig.Recv().Type()).(*types2.Pointer)
	if !ok {
		return false
	}
	n, ok := types2.Unalias(p.Elem()).(*types2.Named)
	return ok && n.Obj().Name() == "printer"
}

// walkVar is the SSA variable of the walk's loop.
var walkVar = types2.NewVar(token.NoPos, nil, "walk", types2.Typ[types2.UnsafePointer])

// fmtWalkBody builds the contract body of a function of the walk.
func (b *builder) fmtWalkBody(f *Func, d *declRef) {
	fb := b.newFnb(f, d.pkg, nil)
	fb.addressed = map[*types2.Var]bool{}
	sig := f.Sig
	var vars []*types2.Var
	vars = append(vars, sig.Recv())
	for i := 0; i < sig.Params().Len(); i++ {
		vars = append(vars, sig.Params().At(i))
	}
	// What the walk starts from: every pointer the parameters hold (not
	// the printer, whose own memory it only writes bytes into).
	var seed, printer, arg Reg
	for i, v := range vars {
		f.Inputs = append(f.Inputs, Input{Var: v, Type: v.Type()})
		r := fb.input(i)
		switch {
		case i == 0:
			printer = r
		case hasPointers(v.Type()):
			seed = fb.either(seed, r)
			if arg == 0 {
				arg = r
			}
		}
	}
	if f.Key.Obj.Name() == "printArg" {
		// printArg keeps a %w operand: it pushes it into p.wrapped, a
		// vec.Fixed[error] whose storage the error's maker provides
		// (errorf.p): an element of p.wrapped.buf.
		pt := sig.Recv().Type().(*types2.Pointer).Elem()
		if st, ok := pt.Underlying().(*types2.Struct); ok {
			for i := 0; i < st.NumFields(); i++ {
				if st.Field(i).Name() != "wrapped" {
					continue
				}
				fx, ok := st.Field(i).Type().Underlying().(*types2.Struct)
				if !ok || fx.NumFields() == 0 {
					break
				}
				// Only an operand whose dynamic type is an error is
				// wrapped: the assertion's result (an interface box of a
				// type that is not an error is never one).
				errT := types2.Universe.Lookup("error").Type()
				e := fb.emitDst(Instr{Op: OpAssert, X: arg, Type: errT, Pos: f.Pos})
				buf := fb.load(fb.field(printer, i, 0), fx.Field(0).Type())
				fb.store(fb.elem(buf), e, errT)
				break
			}
		}
	}
	type method struct {
		I types2.Type
		m *types2.Func
	}
	var methods []method
	for _, w := range fmtWalkMethods {
		var obj types2.Object
		if w.iface == "error" {
			obj = types2.Universe.Lookup("error")
		} else {
			obj = d.pkg.Types.Scope().Lookup(w.iface)
		}
		tn, ok := obj.(*types2.TypeName)
		if !ok {
			continue
		}
		iface, ok := tn.Type().Underlying().(*types2.Interface)
		if !ok {
			continue
		}
		for i := 0; i < iface.NumMethods(); i++ {
			if m := iface.Method(i); m.Name() == w.method {
				methods = append(methods, method{tn.Type(), m})
			}
		}
	}
	if seed != 0 {
		fb.writeVar(walkVar, seed)
		head := fb.newBlock()
		fb.jump(head)
		fb.startBlock(head)
		v := fb.readVar(walkVar)
		next := fb.load(fb.field(v, AnyStep), types2.Typ[types2.UnsafePointer])
		for _, m := range methods {
			msig := m.m.Type().(*types2.Signature)
			args := make([]Reg, msig.Params().Len())
			res := fb.emitCall(&Call{Kind: CallIface, Method: m.m, Iface: m.I, Target: v, Sig: msig, Args: args, Implicit: ImplWrapper}, f.Pos)
			for i, r := range res {
				// The walk walks the interface values FmtList and
				// FmtSlot return; not the strings String, Error and
				// GoString return, which it prints: their bytes hold
				// no pointers, and a string is no interface value
				// whose methods it would call.
				if b, ok := msig.Results().At(i).Type().Underlying().(*types2.Basic); ok && b.Info()&types2.IsString != 0 {
					continue
				}
				next = fb.either(next, r)
			}
		}
		fb.writeVar(walkVar, fb.either(v, next))
		loop := fb.ensureBlock()
		exit := fb.newBlock()
		fb.edge(loop, head)
		fb.edge(loop, exit)
		fb.cur = nil
		fb.seal(head)
		fb.seal(exit)
		fb.startBlock(exit)
	}
	fb.cur = nil
	fb.finish()
}
