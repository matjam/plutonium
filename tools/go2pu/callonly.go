package main

import (
	"go/ast"
	"go/token"
	"go/types"
)

// Function literals that are only called.
//
// Go's escape analysis reports a function literal as escaping when the
// callee it is passed to keeps it somewhere, as ast.Inspect does when it
// converts the function to the ast.Visitor interface. Plutonium decides
// again, on the callee as compiled (language spec section 11.5): a function
// value that is passed down and only ever called does not outlive the frame
// of the function that made it, and is accepted. So these literals need no
// note and no conversion:
//
//   - an argument of a function in callOnlyParams (the translation of
//     ast.Inspect is the common one; the callee is a package that is itself
//     translated, where the value is only called);
//   - an argument of a call of a push iterator, a func value of the shape
//     func(yield func(...) bool) such as an iter.Seq, which only calls yield;
//   - a literal held in a local variable that is only called, or assigned
//     another such literal, such as `mark := func(e ast.Expr) {...}` used in
//     an Inspect callback: Go reports it because the callback that captures
//     it escapes.
//
// Everything else Go reports still gets its note, and the Plutonium compiler
// has the last word on it.

// callOnlyParams are the functions whose func parameter (by index; for a
// method the receiver is not counted) is only called by the callee.
var callOnlyParams = map[string]int{
	"go/ast.Inspect": 1,
	goModule + "/internal/syntax/ast.Inspect":                 1,
	"(" + goModule + "/internal/syntax/constraint.Expr).Eval": 0,
	"(go/build/constraint.Expr).Eval":                         0,
}

// callOnlyLiteral reports whether the function literal lit is only called
// by whatever receives it.
func (t *translator) callOnlyLiteral(lit *ast.FuncLit) bool {
	e := ast.Node(lit)
	for {
		p, ok := t.parent[e].(*ast.ParenExpr)
		if !ok {
			break
		}
		e = p
	}
	switch p := t.parent[e].(type) {
	case *ast.CallExpr:
		for i, a := range p.Args {
			if a == e {
				return t.callOnlyArg(p, i)
			}
		}
	case *ast.AssignStmt:
		for i, r := range p.Rhs {
			if r != e || len(p.Lhs) != len(p.Rhs) {
				continue
			}
			if id, ok := ast.Unparen(p.Lhs[i]).(*ast.Ident); ok {
				return t.callOnlyVar(t.info.ObjectOf(id))
			}
		}
	case *ast.ValueSpec:
		for i, v := range p.Values {
			if v == e && len(p.Names) == len(p.Values) {
				return t.callOnlyVar(t.info.ObjectOf(p.Names[i]))
			}
		}
	}
	return false
}

// callOnlyArg reports whether the callee of call only calls its argument i.
func (t *translator) callOnlyArg(call *ast.CallExpr, i int) bool {
	fun := ast.Unparen(call.Fun)
	var obj types.Object
	switch f := fun.(type) {
	case *ast.Ident:
		obj = t.info.Uses[f]
	case *ast.SelectorExpr:
		obj = t.info.Uses[f.Sel]
	}
	if fn, ok := obj.(*types.Func); ok {
		if idx, ok := callOnlyParams[fn.FullName()]; ok {
			return idx == i
		}
		return t.callOnlyParam(fn, i)
	}
	// A func value (a variable, or the result of a call) of the shape
	// func(yield func(...) bool).
	sig, ok := t.info.TypeOf(fun).Underlying().(*types.Signature)
	if !ok || sig.Params().Len() != 1 || sig.Results().Len() != 0 || i != 0 {
		return false
	}
	y, ok := sig.Params().At(0).Type().Underlying().(*types.Signature)
	if !ok || y.Results().Len() != 1 {
		return false
	}
	b, ok := y.Results().At(0).Type().Underlying().(*types.Basic)
	return ok && b.Kind() == types.Bool
}

// callOnlyVar reports whether the local variable v is used only as the
// function of a call, or assigned a function literal.
func (t *translator) callOnlyVar(v types.Object) bool {
	if v == nil || v.Parent() == nil || v.Parent() == t.pkg.Scope() {
		return false
	}
	if t.callOnly == nil {
		t.callOnly = map[types.Object]bool{}
	}
	if ok, done := t.callOnly[v]; done {
		return ok
	}
	ok := true
	for id, obj := range t.info.Uses {
		if obj != v {
			continue
		}
		if !t.callOnlyUse(id) {
			ok = false
			break
		}
	}
	t.callOnly[v] = ok
	return ok
}

// callOnlyUse reports whether the use id of a variable is a call of it, or
// the target of an assignment of a function literal.
func (t *translator) callOnlyUse(id *ast.Ident) bool {
	var e ast.Node = id
	for {
		switch p := t.parent[e].(type) {
		case *ast.ParenExpr:
			e = p
			continue
		case *ast.StarExpr:
			e = p
			continue
		case *ast.CallExpr:
			if p.Fun != e {
				// Passed to a function that only calls it.
				for i, a := range p.Args {
					if a == e {
						return t.callOnlyArg(p, i)
					}
				}
				return false
			}
			// `go f()` and `defer f()` run it later.
			switch t.parent[p].(type) {
			case *ast.GoStmt, *ast.DeferStmt:
				return false
			}
			return true
		case *ast.AssignStmt:
			for i, l := range p.Lhs {
				if l == e && len(p.Lhs) == len(p.Rhs) {
					_, isLit := ast.Unparen(p.Rhs[i]).(*ast.FuncLit)
					return isLit
				}
			}
		}
		return false
	}
}

// isGuardRecover reports whether d is
//
//	defer func() { if recover() != nil { ... } }()
//
// a deferred function that only reacts to a panic having happened.
func (t *translator) isGuardRecover(d *ast.DeferStmt) bool {
	lit, ok := d.Call.Fun.(*ast.FuncLit)
	if !ok || len(d.Call.Args) != 0 || lit.Type.Params.NumFields() != 0 || len(lit.Body.List) != 1 {
		return false
	}
	ifs, ok := lit.Body.List[0].(*ast.IfStmt)
	if !ok || ifs.Init != nil || ifs.Else != nil {
		return false
	}
	cmp, ok := ifs.Cond.(*ast.BinaryExpr)
	if !ok || cmp.Op != token.NEQ {
		return false
	}
	call, ok := cmp.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "recover" {
		return false
	}
	_, builtin := t.info.Uses[id].(*types.Builtin)
	nilId, ok := cmp.Y.(*ast.Ident)
	return builtin && ok && nilId.Name == "nil"
}

// guardVars returns the names of the local variables of the enclosing
// function that the deferred guard d refers to, in order of first use.
func (t *translator) guardVars(d *ast.DeferStmt) []string {
	lit := d.Call.Fun.(*ast.FuncLit)
	var names []string
	seen := map[types.Object]bool{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := t.info.Uses[id].(*types.Var)
		if ok && !seen[v] && !v.IsField() && v.Parent() != nil && v.Parent() != t.pkg.Scope() &&
			(v.Pos() < lit.Pos() || v.Pos() > lit.End()) {
			seen[v] = true
			names = append(names, v.Name())
		}
		return true
	})
	return names
}

// callOnlyParam reports whether the function fn of this package only calls
// its parameter i: every use of the parameter is a call, or hands it to
// another function that only calls it. A recursion through the function
// assumes it does.
func (t *translator) callOnlyParam(fn *types.Func, i int) bool {
	if t.callOnlyParams == nil {
		t.callOnlyParams = map[callOnlyKey]bool{}
	}
	key := callOnlyKey{fn, i}
	if ok, known := t.callOnlyParams[key]; known {
		return ok
	}
	t.callOnlyParams[key] = true // assume, for a recursive function
	sig := fn.Type().(*types.Signature)
	ok := i < sig.Params().Len() && (!sig.Variadic() || i != sig.Params().Len()-1)
	if ok && fn.Pkg() != t.pkg {
		ok = false
	}
	if ok {
		pv := sig.Params().At(i)
		if t.funcDecls == nil {
			t.funcDecls = map[*types.Func]bool{}
			for n := range t.parent {
				if fd, isDecl := n.(*ast.FuncDecl); isDecl && fd.Body != nil {
					if f, isFn := t.info.Defs[fd.Name].(*types.Func); isFn {
						t.funcDecls[f] = true
					}
				}
			}
		}
		ok = t.funcDecls[fn]
		for id, obj := range t.info.Uses {
			if ok && obj == pv && !t.callOnlyUse(id) {
				ok = false
			}
		}
	}
	t.callOnlyParams[key] = ok
	return ok
}

type callOnlyKey struct {
	fn *types.Func
	i  int
}

// callOnlyEscape reports whether v is a variable that Go moves to the heap
// only because function literals that Go reports as escaping use it, all
// of which are only called: they cannot outlive the frame in Plutonium, so
// v stays in it. It reports false whenever another use might let v outlive
// the frame (its address is taken, or is passed on).
func (tc *translator) callOnlyEscape(v *types.Var, def *ast.Ident) bool {
	decl := tc.nearestFunc(def)
	nested := false
	for _, u := range tc.usesOf[v] {
		for f := tc.nearestFunc(u); f != decl; f = tc.nearestFunc(f) {
			lit, ok := f.(*ast.FuncLit)
			if !ok || !tc.callOnlyLiteral(lit) {
				return false
			}
			nested = true
		}
		if !tc.plainUse(u) {
			return false
		}
	}
	return nested
}
