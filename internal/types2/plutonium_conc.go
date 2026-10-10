package types2

// Plutonium rules for values that cross puroutines (language spec sections
// 4.7, 4.8 and 6.3): go statement arguments and channel elements must be
// sendable, and must not be views of the sending function's frame.

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	. "github.com/matjam/plutonium/internal/types2/errors"
)

// Unsendable returns the part of T that keeps a value of type T from
// crossing puroutines: a shared[T] reference, whose count is not atomic,
// directly or in any field or element. It returns nil if T is sendable.
// Views (pointers, slices, strings), interfaces, functions and channels
// are not looked into; a type parameter is judged when it is instantiated.
func Unsendable(T Type) Type { return unsendable(T, map[Type]bool{}) }

func unsendable(T Type, seen map[Type]bool) Type {
	if T == nil || seen[T] {
		return nil
	}
	seen[T] = true
	switch u := Unalias(T).Underlying().(type) {
	case *Wrapper:
		if u.kind == WrapShared {
			return T
		}
		if u.kind == WrapRing || u.IsPointer() || u.isTransparent() {
			return unsendable(u.elem, seen)
		}
	case *Struct:
		for _, f := range u.fields {
			if bad := unsendable(f.typ, seen); bad != nil {
				return bad
			}
		}
	case *Array:
		return unsendable(u.elem, seen)
	}
	return nil
}

// checkSendable reports a value of type T, described by what, that would
// cross puroutines but is not sendable.
func (check *Checker) checkSendable(at positioner, T Type, what string) {
	if !isValid(T) {
		return
	}
	if bad := Unsendable(T); bad != nil {
		if Identical(bad, T) {
			check.errorf(at, NotSendable,"%s %s is not sendable: a shared reference's count is not atomic, so it cannot cross puroutines; use synced[T] (spec section 4.7)", what, T)
		} else {
			check.errorf(at, NotSendable,"%s %s is not sendable: it contains %s, whose count is not atomic, so it cannot cross puroutines; use synced[T] (spec section 4.7)", what, T, bad)
		}
	}
}

// goStmtArgs checks what a go statement hands to the new puroutine: its
// arguments, and the receiver of a method it calls.
func (check *Checker) goStmtArgs(call *ast.CallExpr) {
	for _, a := range call.Args {
		if T := check.exprType(a); T != nil {
			if tup, ok := T.(*Tuple); ok {
				for _, v := range tup.vars {
					check.checkSendable(a, v.typ, "go statement argument of type")
				}
				continue
			}
			check.checkSendable(a, T, "go statement argument of type")
		}
		check.checkFrameView(a, "passed to a go statement")
	}
	if se, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if sel := check.Selections[se]; sel != nil && sel.kind == MethodVal {
			sig, _ := sel.obj.Type().(*Signature)
			if sig != nil && sig.recv != nil {
				if _, ptr := sig.recv.typ.(*Pointer); ptr {
					if !isPointerTyped(check.exprType(se.X)) {
						if root := check.frameRoot(se.X); root != nil {
							check.errorf(se.X, FrameViewSent,"go statement calls a pointer method on %s, a variable in this function's frame, which the puroutine may outlive (spec section 6.3); allocate it with new or make it a package-level variable", root.name)
						}
					}
				} else if T := check.exprType(se.X); T != nil {
					check.checkSendable(se.X, T, "go statement receiver of type")
				}
			}
		}
	}
}

// checkFrameView reports e, a value handed to another puroutine, if it is
// a view of the current function's frame that the compiler can see
// syntactically: the address of a local variable or of a part of one, a
// slice of a local array, or the address of a composite literal. Other
// views are left to the escape checks of spec section 6.3.
func (check *Checker) checkFrameView(e ast.Expr, what string) {
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op != token.AND {
			return
		}
		if _, lit := ast.Unparen(x.X).(*ast.CompositeLit); lit {
			check.errorf(x, FrameViewSent,"%s is %s, but it points into this function's frame, which the receiving puroutine may outlive (spec section 6.3); allocate it with new(T{...})", ExprString(x), what)
			return
		}
		if root := check.frameRoot(x.X); root != nil {
			check.errorf(x, FrameViewSent,"%s is %s, but %s lives in this function's frame, which the receiving puroutine may outlive (spec section 6.3); allocate it with new or make it a package-level variable", ExprString(x), what, root.name)
		}
	case *ast.SliceExpr:
		if T := check.exprType(x.X); T != nil {
			if _, arr := T.Underlying().(*Array); arr {
				if root := check.frameRoot(x.X); root != nil {
					check.errorf(x, FrameViewSent,"%s is %s, but it is a slice of %s, which lives in this function's frame and which the receiving puroutine may outlive (spec section 6.3)", ExprString(x), what, root.name)
				}
			}
		}
	}
}

// frameRoot returns the local variable whose storage e is part of, or nil
// if e is not (it is reached through a pointer or slice, or is a package
// variable).
func (check *Checker) frameRoot(e ast.Expr) *Var {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		v, _ := check.Uses[x].(*Var)
		if v == nil || v.IsField() || v.pkg == nil || v.parent == nil || v.parent == v.pkg.scope || v.parent == Universe {
			return nil
		}
		return v
	case *ast.SelectorExpr:
		if sel := check.Selections[x]; sel == nil || sel.kind != FieldVal || sel.indirect {
			return nil
		}
		if isPointerTyped(check.exprType(x.X)) {
			return nil
		}
		return check.frameRoot(x.X)
	case *ast.IndexExpr:
		if T := check.exprType(x.X); T != nil {
			if _, arr := T.Underlying().(*Array); arr {
				return check.frameRoot(x.X)
			}
		}
	}
	return nil
}

// exprType returns the recorded type of e, or nil.
func (check *Checker) exprType(e ast.Expr) Type {
	if check.Types == nil {
		return nil
	}
	if tv, ok := check.Types[e]; ok {
		return tv.Type
	}
	return nil
}

func isPointerTyped(T Type) bool {
	if T == nil {
		return false
	}
	switch u := T.Underlying().(type) {
	case *Pointer:
		return true
	case *Wrapper:
		return u.IsPointer()
	}
	return false
}
