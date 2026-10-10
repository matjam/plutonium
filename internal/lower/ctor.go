package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Closure constructors.
//
// A function whose body is a single return statement of a function literal
// that captures its parameters, such as
//
//	func Count(n int) iter.Seq[int] {
//		return func(yield func(int) bool) { ... n ... }
//	}
//
// returns a closure that would outlive its frame (language spec section
// 4.5). A call of such a function is expanded where it is made instead: its
// arguments are stored in fresh variables of the caller's frame and the
// literal's closure is built there, so the closure lives as long as the
// caller's frame. A function whose body returns a call of a closure
// constructor is one too. Using a closure constructor other than by
// calling it (as a func value, a method value, or an interface method) is
// an error, since there is no caller's frame to build the closure in.

// A ctorInfo describes a closure constructor.
type ctorInfo struct {
	decl   *funcDecl
	result ast.Expr // a *ast.FuncLit, or a *ast.CallExpr of another constructor
}

// closureCtor returns fn's description if it is a closure constructor.
func (l *Lowerer) closureCtor(fn *types2.Func) *ctorInfo {
	fn = fn.Origin()
	if c, ok := l.ctors[fn]; ok {
		return c
	}
	l.ctors[fn] = nil // a constructor returning a call of itself is not one
	d := l.decls[fn]
	if d == nil || d.decl.Body == nil || len(d.decl.Body.List) != 1 {
		return nil
	}
	ret, ok := d.decl.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil
	}
	info := d.pkg.Info
	switch x := ast.Unparen(ret.Results[0]).(type) {
	case *ast.FuncLit:
		if !capturesFrom(info, x, d.decl) {
			return nil
		}
	case *ast.CallExpr:
		g := calledFunc(info, x)
		if g == nil || l.closureCtor(g) == nil {
			return nil
		}
	default:
		return nil
	}
	c := &ctorInfo{decl: d, result: ast.Unparen(ret.Results[0])}
	l.ctors[fn] = c
	return c
}

// capturesFrom reports whether lit refers to a variable declared in decl
// outside lit.
func capturesFrom(info *types2.Info, lit *ast.FuncLit, decl *ast.FuncDecl) bool {
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := info.Uses[id].(*types2.Var); ok && !v.IsField() {
				p := v.Pos()
				if p >= decl.Pos() && p < decl.End() && (p < lit.Pos() || p >= lit.End()) {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// calledFunc returns the declared function or concrete method a call
// names statically, or nil.
func calledFunc(info *types2.Info, call *ast.CallExpr) *types2.Func {
	fun := ast.Unparen(call.Fun)
	switch ix := fun.(type) {
	case *ast.IndexExpr:
		fun = ix.X
	case *ast.IndexListExpr:
		fun = ix.X
	}
	if se, ok := fun.(*ast.SelectorExpr); ok {
		if sel := info.Selections[se]; sel != nil {
			if sel.Kind() != types2.MethodVal || isInterface(sel.Recv()) || len(sel.Index()) != 1 {
				return nil
			}
			if _, ok := types2.Unalias(info.Types[se.X].Type).(*types2.TypeParam); ok {
				return nil
			}
			fn, _ := sel.Obj().(*types2.Func)
			return fn
		}
	}
	if id := staticFuncIdent(fun, info); id != nil {
		fn, _ := info.Uses[id].(*types2.Func)
		return fn
	}
	return nil
}

// expandCtor lowers a call of the closure constructor c, instantiated with
// targs, with its receiver (or nil) and arguments already evaluated, and
// returns the closure as a value of type T.
func (s *fnState) expandCtor(c *ctorInfo, targs []types2.Type, recv *value, args []value, T types2.Type, call token.Pos) value {
	d := c.decl
	firstValue, firstBlock := s.f.NumValues(), s.f.NumBlocks()
	defer s.moveToCall(firstValue, firstBlock, call)
	pkg, info, tmap := s.pkg, s.info, s.tmap
	s.pkg, s.info, s.tmap = d.pkg, d.pkg.Info, nil
	if len(targs) > 0 {
		s.tmap = newTypeMap(s.l, d.obj, targs)
	}
	sig := d.obj.Type().(*types2.Signature)
	var params []*types2.Var
	var vals []value
	if recv != nil {
		params = append(params, sig.Recv())
		vals = append(vals, *recv)
	}
	for i := 0; i < sig.Params().Len(); i++ {
		params = append(params, sig.Params().At(i))
	}
	vals = append(vals, args...)
	for i, p := range params {
		PT := s.subst(p.Type())
		addr := s.temp(PT, varName(p))
		s.storeTo(addr, vals[i], PT)
		s.vars[p] = addr
	}

	var rec *ssa.Value
	switch x := c.result.(type) {
	case *ast.FuncLit:
		rec = s.closure(x)
	case *ast.CallExpr:
		rec = s.callExpr(x).v
	}

	for _, p := range params {
		delete(s.vars, p)
	}
	s.pkg, s.info, s.tmap = pkg, info, tmap
	return scalarValue(rec, T)
}

// checkNotCtor reports a closure constructor used other than by a call.
func (l *Lowerer) checkNotCtor(fn *types2.Func, pos token.Pos, what string) {
	if l.closureCtor(fn) == nil {
		return
	}
	l.ctorMisused[fn.Origin()] = true
	l.errorf(pos, "%s returns a closure that captures its parameters, which is built in its caller's frame; it can only be called directly, not %s", fn.Name(), what)
}

// checkCtorBody reports a closure constructor whose body is being lowered
// as a function of its own: something calls it indirectly (an interface
// method, for example), and its closure would outlive its frame.
func (l *Lowerer) checkCtorBody(job *funcJob) {
	if job.decl == nil || job.pkg == nil {
		return
	}
	fn, _ := job.pkg.Info.Defs[job.decl.Name].(*types2.Func)
	if fn == nil || l.closureCtor(fn) == nil || l.ctorMisused[fn] {
		return
	}
	l.ctorMisused[fn] = true
	l.errorf(job.decl.Name.Pos(), "%s returns a closure that captures its parameters, which is built in its caller's frame; it can only be called directly, not through a func value, a method value or an interface", fn.Name())
}
