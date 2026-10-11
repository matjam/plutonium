package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
)

// Error propagation as or clauses (language spec section 11.27). A Go
// function that passes an error up unchanged,
//
//	x, err := f()
//	if err != nil {
//		return nil, err
//	}
//
// or
//
//	if err := g(); err != nil {
//		return err
//	}
//
// translates to x := f() or return and g() or return, when the return is a
// plain propagation: the function's results are unnamed and its last is
// error, the return gives err as the last result and the zero value as
// every other, and err is used nowhere else. Anything else, such as a
// wrapped error or named results, is left as Go wrote it.

// An orRewrite is one propagation that becomes an or statement.
type orRewrite struct {
	lhs  []ast.Expr // the values' operands; nil for none
	tok  token.Token
	call *ast.CallExpr
}

// findOrRewrites records the propagations of the file's functions.
func (t *fileTr) findOrRewrites() {
	t.orAssign = map[*ast.AssignStmt]*orRewrite{}
	t.orIf = map[*ast.IfStmt]*orRewrite{}
	ast.Inspect(t.file, func(n ast.Node) bool {
		var sig *types.Signature
		var body *ast.BlockStmt
		switch n := n.(type) {
		case *ast.FuncDecl:
			if obj, ok := t.info.Defs[n.Name].(*types.Func); ok {
				sig, _ = obj.Type().(*types.Signature)
			}
			body = n.Body
		case *ast.FuncLit:
			sig, _ = t.info.Types[n].Type.(*types.Signature)
			body = n.Body
		default:
			return true
		}
		if sig != nil && body != nil {
			t.findOrIn(sig, body)
		}
		return true
	})
}

// findOrIn records the propagations of a function body with signature sig,
// outside the function literals in it.
func (t *fileTr) findOrIn(sig *types.Signature, body *ast.BlockStmt) {
	res := sig.Results()
	n := res.Len()
	if n == 0 || !types.Identical(res.At(n-1).Type(), types.Universe.Lookup("error").Type()) {
		return
	}
	for i := 0; i < n; i++ {
		if res.At(i).Name() != "" {
			return // named results: the short form keeps their values
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		var list []ast.Stmt
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.BlockStmt:
			list = n.List
		case *ast.CaseClause:
			list = n.Body
		case *ast.CommClause:
			list = n.Body
		default:
			return true
		}
		for i, s := range list {
			ifs, ok := s.(*ast.IfStmt)
			if !ok {
				continue
			}
			errVar := t.propagation(ifs, res)
			if errVar == nil {
				continue
			}
			if as, ok := ifs.Init.(*ast.AssignStmt); ok {
				if r := t.orSource(as, errVar, ifs, ifs); r != nil {
					t.orIf[ifs] = r
				}
			} else if i > 0 && ifs.Init == nil {
				if as, ok := list[i-1].(*ast.AssignStmt); ok {
					if r := t.orSource(as, errVar, as, ifs); r != nil {
						t.orAssign[as] = r
						t.orIf[ifs] = nil // removed
					}
				}
			}
		}
		return true
	})
}

// propagation returns the variable err of an if statement of the form
// if err != nil { return zero, ..., err } in a function with results res,
// or nil.
func (t *fileTr) propagation(ifs *ast.IfStmt, res *types.Tuple) *types.Var {
	if ifs.Else != nil || len(ifs.Body.List) != 1 {
		return nil
	}
	cond, ok := ifs.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.NEQ || !t.isNil(cond.Y) {
		return nil
	}
	id, ok := cond.X.(*ast.Ident)
	if !ok {
		return nil
	}
	errVar, ok := t.info.Uses[id].(*types.Var)
	if !ok {
		return nil
	}
	ret, ok := ifs.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != res.Len() {
		return nil
	}
	last, ok := ret.Results[len(ret.Results)-1].(*ast.Ident)
	if !ok || t.info.Uses[last] != errVar {
		return nil
	}
	for i, e := range ret.Results[:len(ret.Results)-1] {
		if !t.isZero(e, res.At(i).Type()) {
			return nil
		}
	}
	return errVar
}

// orSource returns the rewrite of as, the statement that declares errVar
// and the values of the propagation in ifs, or nil if the propagation is
// not plain. The rewritten span runs from first to ifs.
func (t *fileTr) orSource(as *ast.AssignStmt, errVar *types.Var, first ast.Node, ifs *ast.IfStmt) *orRewrite {
	if as.Tok != token.DEFINE || len(as.Rhs) != 1 || len(as.Lhs) == 0 {
		return nil
	}
	call, ok := as.Rhs[0].(*ast.CallExpr)
	if !ok {
		return nil
	}
	if tv := t.info.Types[call.Fun]; tv.IsType() || tv.IsBuiltin() {
		return nil
	}
	errID, ok := as.Lhs[len(as.Lhs)-1].(*ast.Ident)
	if !ok || t.info.Defs[errID] != errVar || t.isHeapDef(errID) {
		return nil
	}
	// err is used only by the propagation.
	if len(t.usesOf[errVar]) != 2 {
		return nil
	}
	var results []types.Type
	switch T := t.typeOf(call).(type) {
	case *types.Tuple:
		for i := 0; i < T.Len(); i++ {
			results = append(results, T.At(i).Type())
		}
	default:
		results = []types.Type{T}
	}
	if len(results) != len(as.Lhs) || !types.Identical(results[len(results)-1], types.Universe.Lookup("error").Type()) {
		return nil
	}
	if _, ok := t.scopeEdits[as]; ok {
		return nil
	}
	// No comment would be lost.
	for _, g := range t.file.Comments {
		if g.Pos() >= first.Pos() && g.End() <= ifs.End() {
			return nil
		}
	}
	r := &orRewrite{call: call, tok: token.ASSIGN}
	values := as.Lhs[:len(as.Lhs)-1]
	for _, l := range values {
		id, ok := l.(*ast.Ident)
		if !ok {
			return nil
		}
		if t.isHeapDef(id) {
			return nil
		}
		if id.Name != "_" {
			r.lhs = values
		}
		if id.Name != "_" && t.info.Defs[id] != nil {
			r.tok = token.DEFINE
		}
	}
	return r
}

// orText returns the or statement of r.
func (t *fileTr) orText(r *orRewrite) string {
	var b strings.Builder
	if len(r.lhs) > 0 {
		for i, l := range r.lhs {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(t.render(l))
		}
		b.WriteString(" " + r.tok.String() + " ")
	}
	b.WriteString(t.render(r.call))
	b.WriteString(" or return")
	return b.String()
}

// isNil reports whether e is the predeclared nil.
func (t *fileTr) isNil(e ast.Expr) bool {
	_, ok := t.info.Uses[identOf(e)].(*types.Nil)
	return ok
}

func identOf(e ast.Expr) *ast.Ident {
	id, _ := ast.Unparen(e).(*ast.Ident)
	return id
}

// isZero reports whether e is written as the zero value of T: nil, a
// constant zero, false or "", or an empty composite literal of a struct or
// array type.
func (t *fileTr) isZero(e ast.Expr, T types.Type) bool {
	if t.isNil(e) {
		return true
	}
	tv := t.info.Types[e]
	if tv.Value != nil {
		switch tv.Value.Kind() {
		case constant.Bool:
			return !constant.BoolVal(tv.Value)
		case constant.String:
			return constant.StringVal(tv.Value) == ""
		case constant.Int, constant.Float:
			return constant.Sign(tv.Value) == 0
		}
		return false
	}
	if lit, ok := ast.Unparen(e).(*ast.CompositeLit); ok && len(lit.Elts) == 0 && types.Identical(tv.Type, T) {
		switch T.Underlying().(type) {
		case *types.Struct, *types.Array:
			return true
		}
	}
	return false
}
