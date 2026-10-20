package main

import (
	"go/ast"
	"go/types"
)

// Values returned in an interface.
//
// A value that does not fit the data word of an interface (a string, a
// struct, a slice) is boxed by the compiler in the frame that makes the
// conversion, and the box must not outlive it (language spec section 18.3).
// Go boxes it on the heap. When a function returns such a value as an
// interface, go2pu returns a pointer to a copy allocated in the arena
// instead, pu.New(v): the pointer is the data word, and the method set of
// *T has the methods of T, so the interface is satisfied and the callers
// notice nothing, provided nothing asks the interface for a T. If the
// package asserts a T anywhere (x.(T), case T:), the return keeps its note.
//
// Only types of the package being translated qualify, so that the whole
// program that could assert them is visible.

// boxedReturn reports whether the result expression e of a return, of the
// type T, becomes a pointer.
func (t *fileTr) boxedReturn(e ast.Expr, T types.Type) bool {
	named, ok := T.(*types.Named)
	if !ok || named.Obj().Pkg() != t.pkg {
		return false
	}
	if _, isIface := T.Underlying().(*types.Interface); isIface {
		return false
	}
	return !t.asserted(T)
}

// asserted reports whether the package asserts the type T on an interface
// value, in an assertion or a type switch.
func (t *fileTr) asserted(T types.Type) bool {
	if t.assertedTypes == nil {
		t.assertedTypes = []types.Type{}
		add := func(e ast.Expr) {
			if tv, ok := t.info.Types[e]; ok && tv.Type != nil {
				t.assertedTypes = append(t.assertedTypes, tv.Type)
			}
		}
		for n := range t.parent {
			switch n := n.(type) {
			case *ast.TypeAssertExpr:
				if n.Type != nil {
					add(n.Type)
				}
			case *ast.CaseClause:
				if _, ok := t.parent[t.parent[n]].(*ast.TypeSwitchStmt); ok {
					for _, e := range n.List {
						add(e)
					}
				}
			}
		}
	}
	for _, a := range t.assertedTypes {
		if types.Identical(a, T) {
			return true
		}
	}
	return false
}
