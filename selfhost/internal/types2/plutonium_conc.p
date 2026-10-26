// Translated by go2pu from internal/types2/plutonium_conc.go at the go-cutover tag; edited by hand since.

package types2

// Plutonium rules for values that cross tasks (language spec sections
// 12.12, 19.2 and 18.3): task statement arguments and channel elements must be
// sendable, and must not be views of the sending function's frame.

import (
	"selfhost/internal/syntax/ast"
	"selfhost/internal/syntax/token"
	. "selfhost/internal/types2/errors"
	"selfhost/pu"
)

// Unsendable returns the part of T that keeps a value of type T from
// crossing tasks: a shared[T] reference, whose count is not atomic,
// directly or in any field, element, payload, referent of a box, or target
// of a pointer or slice (spec section 8.8); a map's key and element count
// too. It returns nil if T is sendable. Two tasks that copy a shared[T]
// out of memory they both reach through a pointer would change its count
// without atomic operations. What an interface holds depends on the types
// the whole program converts to it, which the race analysis checks;
// functions and channels are sendable (a channel's element type is checked
// where the channel type is written); a type parameter is judged when it
// is instantiated.
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
		// volatile, aligned, ring, owned, synced, and weak, whose
		// weak[shared[T]] counts as the shared reference does.
		return unsendable(u.elem, seen)
	case *Struct:
		for _, f := range u.fields {
			if bad := unsendable(f.typ, seen); bad != nil {
				return bad
			}
		}
	case *Array:
		return unsendable(u.elem, seen)
	case *Pointer:
		return unsendable(u.base, seen)
	case *Slice:
		return unsendable(u.elem, seen)
	case *Map:
		if bad := unsendable(u.key, seen); bad != nil {
			return bad
		}
		return unsendable(u.elem, seen)
	case *TaggedUnion:
		for _, v := range u.variants {
			if bad := unsendable(v.typ, seen); bad != nil {
				return bad
			}
		}
	}
	return nil
}

// checkSendable reports a value of type T, described by what, that would
// cross tasks but is not sendable.
func (check *Checker) checkSendable(at positioner, T Type, what string) {
	if !isValid(T) {
		return
	}
	if bad := Unsendable(T); bad != nil {
		if Identical(bad, T) {
			check.errorf(at, NotSendable, "%s %s is not sendable: a shared reference's count is not atomic, so it cannot cross tasks; use synced[T] (spec section 19.2)", pu.Box(what), T)
		} else {
			check.errorf(at, NotSendable, "%s %s is not sendable: it contains %s, whose count is not atomic, so it cannot cross tasks; use synced[T] (spec section 19.2)", pu.Box(what), T, bad)
		}
	}
}

// GoBoxReceiver returns the box type when sel, the selection a go
// statement's function is, calls a pointer method of T directly on an
// owned[T], shared[T] or synced[T] (task p.Serve()), and nil otherwise. The
// task then takes the box itself (spec section 12.12): an owned box
// moves into it, which drops the box when the method returns, and a synced
// one is counted for it; a shared one is not sendable. A box of a named
// type, or a method promoted from a field, takes the ordinary rule for a
// pointer into the object.
func GoBoxReceiver(sel *Selection) *Wrapper {
	if sel == nil || sel.kind != MethodVal || len(sel.index) != 1 {
		return nil
	}
	w, _ := Unalias(sel.recv).(*Wrapper)
	if w == nil || w.kind != WrapOwned && w.kind != WrapShared && w.kind != WrapSynced {
		return nil
	}
	sig, _ := sel.obj.Type().(*Signature)
	if sig == nil || sig.recv == nil {
		return nil
	}
	if _, ptr := sig.recv.typ.(*Pointer); !ptr {
		return nil
	}
	return w
}

// taskStmtArgs checks what a task statement hands to the new task: its
// arguments, and the receiver of a method it calls.
func (check *Checker) taskStmtArgs(call *ast.CallExpr) {
	for _, a := range call.Args {
		if T := check.exprType(a); T != nil {
			if tup, ok := T.(*Tuple); ok {
				for _, v := range tup.vars {
					check.checkSendable(a, v.typ, "task statement argument of type")
				}
				continue
			}
			check.checkSendable(a, T, "task statement argument of type")
		}
		check.checkFrameView(a, "passed to a task statement")
	}
	if se, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if sel := check.Selections[se]; sel != nil && sel.kind == MethodVal {
			sig, _ := sel.obj.Type().(*Signature)
			if sig != nil && sig.recv != nil {
				if _, ptr := sig.recv.typ.(*Pointer); ptr {
					if GoBoxReceiver(sel) != nil {
						// The box itself goes to the task.
						if T := check.exprType(se.X); T != nil {
							check.checkSendable(se.X, T, "task statement receiver of type")
						}
					} else if !isPointerTyped(check.exprType(se.X)) {
						if root := check.frameRoot(se.X); root != nil {
							check.errorf(se.X, FrameViewSent, "task statement calls a pointer method on %s, a variable in this function's frame, which the task may outlive (spec section 18.3); make it a package-level variable, or give the task a function that takes an owner", root.name)
						}
					}
				} else if T := check.exprType(se.X); T != nil {
					check.checkSendable(se.X, T, "task statement receiver of type")
				}
			}
		}
	}
}

// checkFrameView reports e, a value handed to another task, if it is
// a view of the current function's frame that the compiler can see
// syntactically: the address of a local variable or of a part of one, a
// slice of a local array, or the address of a composite literal. Other
// views are left to the escape checks of spec section 18.3.
func (check *Checker) checkFrameView(e ast.Expr, what string) {
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op != token.AND {
			return
		}
		if _, lit := ast.Unparen(x.X).(*ast.CompositeLit); lit {
			check.errorf(x, FrameViewSent, "%s is %s, but it points into this function's frame, which the receiving task may outlive (spec section 18.3); allocate it with new(T{...}), whose owned[T] then moves to the task", pu.Box(ExprString(x)), pu.Box(what))
			return
		}
		if root := check.frameRoot(x.X); root != nil {
			check.errorf(x, FrameViewSent, "%s is %s, but %s lives in this function's frame, which the receiving task may outlive (spec section 18.3); allocate it with new, whose owned[T] then moves to the task, or make it a package-level variable", pu.Box(ExprString(x)), what, root.name)
		}
	case *ast.SliceExpr:
		if T := check.exprType(x.X); T != nil {
			if _, arr := T.Underlying().(*Array); arr {
				if root := check.frameRoot(x.X); root != nil {
					check.errorf(x, FrameViewSent, "%s is %s, but it is a slice of %s, which lives in this function's frame and which the receiving task may outlive (spec section 18.3)", pu.Box(ExprString(x)), pu.Box(what), pu.Box(root.name))
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
		return u.IsBox()
	}
	return false
}

// errorConversion reports the conversion of x to T when T is error or an
// interface that embeds it and the type of x is not sendable (language spec
// section 8.8): every error is sendable, so a value that holds a shared[T]
// cannot become one. The message names the shared[T] and shows
// alloc.Synced. A conversion between interfaces, of nil, or of a type
// parameter's value is not checked here.
func (check *Checker) errorConversion(x *operand, T Type) {
	if T == nil || !isNonTypeParamInterface(T) || !x.isValid() || x.isNil() {
		return
	}
	V := x.typ()
	if !isValid(V) || isUntyped(V) || isTypeParam(V) || IsInterface(V) {
		return
	}
	i, _ := T.Underlying().(*Interface)
	if i == nil || !Implements(i, Universe.Lookup("error").Type().Underlying().(*Interface)) {
		return
	}
	bad := Unsendable(V)
	if bad == nil {
		return
	}
	if Identical(bad, V) {
		check.errorf(x, UnsendableToError, "cannot convert %s to %s: a shared reference's count is not atomic and every error may cross tasks; use alloc.Synced (spec section 8.8)", x, T)
	} else {
		check.errorf(x, UnsendableToError, "cannot convert %s to %s: it contains %s, whose count is not atomic, and every error may cross tasks; use alloc.Synced (spec section 8.8)", x, T, bad)
	}
}
