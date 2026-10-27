// Translated by go2pu from internal/escape/drops.go at the go-cutover tag; edited by hand since.

package escape

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
)

// Implicit drops (language spec section 18.6).
//
// An owner held in the frame is dropped where its scope ends, where a new
// value replaces it, and at the end of the statement that made it if
// nothing keeps it: its drop glue calls its Drop method with a pointer to
// it, then drops its parts. Those calls are checked as an explicit o.Drop()
// is. The analysis does not follow moves: every owner variable of the
// frame, parameter and temporary owner value is taken to be dropped where it
// is, which covers every path on which it may still hold a value (an early
// return, a call that ends the task with rt.ExitTask). Its drop is applied
// where the variable is declared or the temporary made: the frame's
// variables are followed without regard to the order of statements
// (computeTracked leaves them out of the control flow), so where in the
// function the call is applied makes no difference to what it finds.

// dropMethod returns T's own Drop method, or nil. Drop is never promoted
// from an embedded field: the field is dropped as a field (lower/drop.go
// has the same rule).
func dropMethod(T types2.Type) *types2.Func {
	if _, ok := types2.Unalias(T).(*types2.Named); !ok {
		return nil
	}
	sel := types2.NewMethodSet(types2.NewPointer(T)).Lookup(nil, "Drop")
	if sel == nil || len(sel.Index()) != 1 {
		return nil
	}
	fn, _ := sel.Obj().(*types2.Func)
	return fn
}

// dropsCall reports whether dropping a value of type T calls a Drop
// method of the program: its own, one of its parts', or, for an owning
// interface, its dynamic value's.
func (a *analyzer) dropsCall(T types2.Type) bool {
	if a.dropCalls == nil {
		a.dropCalls = map[types2.Type]bool{}
	}
	if v, ok := a.dropCalls[T]; ok {
		return v
	}
	a.dropCalls[T] = false // a recursive type holds itself through a box, already being asked about
	v := a.dropsCall1(T)
	a.dropCalls[T] = v
	return v
}

func (a *analyzer) dropsCall1(T types2.Type) bool {
	if tp, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		for _, t := range a.tparamTypes(tp) {
			if a.dropsCall(t) {
				return true
			}
		}
		return false
	}
	if !types2.IsOwner(T) {
		return false
	}
	if dropMethod(T) != nil {
		return true
	}
	if A := types2.OwnerBase(T); A != nil {
		return a.dropsCall(A)
	}
	switch u := underlying(T).(type) {
	case *types2.Wrapper:
		return u.Kind() == types2.WrapOwned && a.dropsCall(u.Elem())
	case *types2.Interface:
		return true
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants(); i++ {
			if v := u.Variant(i); v.HasPayload() && a.dropsCall(v.Payload()) {
				return true
			}
		}
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if a.dropsCall(u.Field(i).Type()) {
				return true
			}
		}
	case *types2.Array:
		return u.Len() > 0 && a.dropsCall(u.Elem())
	}
	return false
}

// dropAt applies the drop of the T that ptr points to: what was called v in
// messages, dropped at pos.
func (w *walker) dropAt(ptr tset, T types2.Type, v string, node ast.Node, pos token.Pos) {
	if len(ptr) == 0 || !w.a.dropsCall(T) {
		return
	}
	cs := &callSite{site: node, pos: pos, exprs: compat.Clone([]ast.Expr{pu.New(ast.Ident{NamePos: pos, Name: compat.Concat("&", v)})})}
	w.dropGlue(ptr, T, v, cs, map[types2.Type]bool{})
}

// dropGlue is the body of T's drop glue (lower/drop.go dropFields): Drop,
// then the parts.
func (w *walker) dropGlue(ptr tset, T types2.Type, v string, cs *callSite, seen map[types2.Type]bool) {
	if seen[T] || !w.a.dropsCall(T) {
		return
	}
	seen[T] = true
	defer delete(seen, T)
	if tp, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		for _, t := range w.a.tparamTypes(tp) {
			w.dropGlue(ptr, t, v, cs, seen)
		}
		return
	}
	if m := dropMethod(T); m != nil {
		f := w.a.funcs[m.Origin()]
		recv := ptr
		if pointerElem(m.Type().(*types2.Signature).Recv().Type()) == nil {
			recv = w.load(ptr) // a value receiver: a copy of the owner
		}
		label := compat.Sprintfb("%s (called when %s is dropped)", compat.ArgS(w.funcLabel(f, m)), compat.ArgS(v))
		cs.sig = m.Type().(*types2.Signature)
		w.apply(f, []tset{recv}, cs, label, recvExact, 0)
	}
	if A := types2.OwnerBase(T); A != nil {
		w.dropGlue(ptr, A, v, cs, seen)
		return
	}
	switch u := underlying(T).(type) {
	case *types2.Wrapper:
		switch u.Kind() {
		case types2.WrapOwned:
			w.dropGlue(w.load(ptr), u.Elem(), v, cs, seen)
		case types2.WrapRing:
			// A ring of owners drops the elements it holds, in place.
			w.dropGlue(intoSet(ptr, shapeElement), u.Elem(), v, cs, seen)
		}
	case *types2.Interface:
		// An owning interface drops its dynamic value: the Drop methods of
		// the types converted to it.
		sig := types2.NewSignatureType(nil, nil, nil, nil, nil, false)
		t := callTarget{sig: sig, method: "Drop", name: compat.Sprintfb("the dynamic value of %s (dropped with %s)", compat.ArgS(v), compat.ArgS(v))}
		w.dynIface(t, []tset{w.load(ptr)}, cs, 0)
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants(); i++ {
			if p := u.Variant(i); p.HasPayload() {
				w.dropGlue(intoSet(ptr, shapeOther), p.Payload(), v, cs, seen)
			}
		}
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			w.dropGlue(offset(ptr, i), u.Field(i).Type(), v, cs, seen)
		}
	case *types2.Array:
		if u.Len() > 0 {
			w.dropGlue(intoSet(ptr, shapeElement), u.Elem(), v, cs, seen)
		}
	}
}

// A dropVar is a variable of a function that may be dropped, with the node
// its drops are applied as calls at.
type dropVar struct {
	v    *types2.Var
	site ast.Node
}

// dropScopeVars applies the drops of the variables of fn (a declaration or
// function literal, whose scope sc is): its parameters and results, and the
// variables declared in its body outside the function literals in it, which
// are dropped with their own scopes.
func (w *walker) dropScopeVars(sc *scope, fn ast.Node) {
	if w.static || fn == nil {
		return
	}
	for _, d := range w.a.dropVarsOf(sc.fn, fn) {
		end := token.NoPos
		if s := d.v.Parent(); s != nil && s.End().IsValid() {
			end = s.End() - 1
		}
		if !end.IsValid() {
			end = fn.End() - 1
		}
		w.dropAt(compat.Clone(tset{frameTok(w.varLoc(d.v))}), d.v.Type(), d.v.Name(), d.site, end)
	}
}

// dropVarsOf lists the variables of f, declared in fn, whose drops call a
// Drop method.
func (a *analyzer) dropVarsOf(f *function, fn ast.Node) []dropVar {
	if f.dropVars != nil {
		return *f.dropVars
	}
	out := pu.Alloc[[]dropVar]()
	note := func(v *types2.Var) {
		if v == nil || v.Name() == "" || isGlobal(v) || v.IsField() || !a.dropsCall(v.Type()) {
			return // an unnamed result is the caller's
		}
		(*out) = compat.Append((*out), dropVar{v, pu.New(ast.Ident{NamePos: v.Pos(), Name: v.Name()})})
	}
	var lits []*ast.FuncLit
	ast.Inspect(fn, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			if x != fn {
				lits = compat.Append(lits, x)
				return false
			}
		case *ast.Ident:
			if v, ok := a.defs[x].(*types2.Var); ok {
				note(v)
			}
		}
		return true
	})
	inLit := func(p token.Pos) bool {
		for _, l := range lits {
			if l.Pos() <= p && p < l.End() {
				return true
			}
		}
		return false
	}
	for _, v := range a.implicitVars {
		if v.Pos() >= fn.Pos() && v.Pos() < fn.End() && !inLit(v.Pos()) {
			note(v)
		}
	}
	f.dropVars = out
	return (*out)
}

// discard applies the drop of the value v of e, which nothing keeps, if e
// makes a temporary owner (a call, or a composite literal): it is dropped at
// the end of its statement, from a temporary of the frame.
func (w *walker) discard(e ast.Expr, v tset) {
	T := w.typeOf(e)
	if w.static || T == nil || !isTemporary(e) || !w.a.dropsCall(T) {
		return
	}
	l := w.tmp(e, lkTemp)
	l.typ = T
	w.addContents(l, v)
	w.dropAt(compat.Clone(tset{frameTok(l)}), T, exprText(e), e, e.End())
}

// isTemporary reports whether e makes a new value, not one of a variable:
// a call or a composite literal.
func isTemporary(e ast.Expr) bool {
	switch ast.Unparen(e).(type) {
	case *ast.CallExpr, *ast.CompositeLit:
		return true
	}
	return false
}
