package eir

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// Views through a box (section 18.7, rule 5; section 18.3, "A local
// owner's memory"). A view obtained through an owned[T], shared[T] or
// synced[T] held in a local variable or parameter (p.field, &*p, a pointer
// method called on p) is bounded by that variable, as a view of a local
// owner is: the variable may drop the object (the last reference to a
// counted one) when its scope ends. EIR gives such a view the address of
// an ObjBoxView object of the variable: the solver treats a pointer into it
// as a frame pointer for every check of where it goes, and the object as
// heap memory for what is stored in it and loaded from it. A box held in a
// statement's temporary bounds the view by the statement. A box reached any
// other way (through a pointer, in a package variable, in heap memory, or
// in the caller's region through the record of a closure, section 11.5)
// gives a view of heap memory, which generation checks guard at run time
// (section 18.11).

// isBox reports whether T is owned[T], shared[T] or synced[T].
func isBox(T types2.Type) bool {
	if T == nil {
		return false
	}
	w, ok := unwrap(T).Underlying().(*types2.Wrapper)
	return ok && w.IsPointer()
}

// isCountedBox reports whether T is shared[T] or synced[T].
func isCountedBox(T types2.Type) bool {
	w, ok := unwrap(T).Underlying().(*types2.Wrapper)
	return ok && (w.Kind() == types2.WrapShared || w.Kind() == types2.WrapSynced)
}

// pointee returns what the pointer-like operand x points to: its value,
// or for a box held in a local, the box view.
func (fb *fnb) pointee(x ast.Expr) Reg {
	xt := fb.typeOf(x)
	r := fb.expr(x)
	if !isBox(xt) {
		return r
	}
	if v := fb.boxRoot(x); v != nil {
		if owner := fb.ownerOf(v); owner != nil {
			if owner != fb && owner.regionVars[v] {
				// v is in the caller's region (section 11.5), reached
				// here through the record of a closure that may be
				// returned: a box reached from another object, whose
				// views generation checks guard (section 18.3).
				return r
			}
			_, whole := ast.Unparen(x).(*ast.Ident)
			return fb.objAddr(owner, owner.boxViewObj(v, pointerElem(xt), whole))
		}
	}
	if call, ok := ast.Unparen(x).(*ast.CallExpr); ok {
		// A box a call returns, which the statement holds.
		o := fb.newObj(Object{Kind: ObjBoxView, Type: pointerElem(xt), Life: LifeStmt, Name: "the object of a temporary box", Pos: call.Pos()})
		return fb.addr(o)
	}
	return r
}

// boxRoot returns the local variable whose storage holds the box x, reached
// without a load: x, x.f, x[i] on an array; nil otherwise.
func (fb *fnb) boxRoot(x ast.Expr) *types2.Var {
	switch e := ast.Unparen(x).(type) {
	case *ast.Ident:
		return fb.b.localVar(e)
	case *ast.SelectorExpr:
		sel := fb.b.selections[e]
		if sel == nil || sel.Kind() != types2.FieldVal || sel.Indirect() {
			return nil
		}
		return fb.boxRoot(e.X)
	case *ast.IndexExpr:
		if _, ok := coreType(unwrap(fb.typeOf(e.X))).(*types2.Array); ok {
			return fb.boxRoot(e.X)
		}
	}
	return nil
}

// ownerOf returns the function, fb or one enclosing it, whose local v is.
func (fb *fnb) ownerOf(v *types2.Var) *fnb {
	for f := fb; f != nil; f = f.parent {
		if _, ok := f.objs[v]; ok || f.locals[v] {
			return f
		}
	}
	return nil
}

// boxViewObj returns a box view object, of type T, bounded by local
// variable v of fb: the one of v itself (whole, made the first time), or a
// new one for a box in a field or element of v.
func (fb *fnb) boxViewObj(v *types2.Var, T types2.Type, whole bool) ObjID {
	if o, ok := fb.boxViews[v]; ok && whole {
		return o
	}
	o := fb.newObj(Object{Kind: ObjBoxView, Type: T, Var: v, Life: LifeBlock, Name: "the object " + v.Name() + " refers to", Pos: v.Pos()})
	// It lives as v does: in the block that declares v, or the frame.
	fb.objBlock[o] = fb.varBlock[v]
	if h, ok := fb.objOfVarHead(v); ok {
		fb.iterHead[o] = h
	}
	if whole {
		if fb.boxViews == nil {
			fb.boxViews = map[*types2.Var]ObjID{}
		}
		fb.boxViews[v] = o
	}
	return o
}

// objOfVarHead returns the loop header block of v, if v is a variable a
// loop's header declares.
func (fb *fnb) objOfVarHead(v *types2.Var) (*Block, bool) {
	if o, ok := fb.objs[v]; ok {
		h, ok := fb.iterHead[o]
		return h, ok
	}
	return nil, false
}
