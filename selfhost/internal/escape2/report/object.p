// Translated by go2pu from internal/escape2/report/object.go at the go-cutover tag; edited by hand since.

package report

import (
	"selfhost/internal/escape2/eir"
	"selfhost/internal/escape2/solve"
	"selfhost/internal/syntax/ast"
	"selfhost/internal/syntax/token"
	"selfhost/internal/types2"
	"selfhost/pu"
)

// locOf describes the object of a finding.
func (r *reporter) locOf(fn *eir.Func, fd solve.Finding) *loc {
	l := pu.New(loc{fn: r.frameName(fn), loop: -1})
	if fd.Obj == eir.NoObj || int(fd.Obj) >= len(fn.Objects) {
		// A region of a call (section 11.5): the closures the call returns.
		l.kind = lkCtor
		if in := callAt(fn, fd.Site); in != nil {
			l.pos = in.Pos
			if ce := r.callExprFor(in); ce != nil {
				l.node = ce
			}
		}
		return l
	}
	o := &fn.Objects[fd.Obj]
	l.pos, l.typ, l.v, l.loop = o.Pos, o.Type, o.Var, o.Loop
	info := r.info(o.Pos)
	node := r.exprAt(o.Pos)
	switch o.Kind {
	case eir.ObjVar:
		l.kind = lkVar
		if isResult(fn, o.Var) {
			l.kind = lkResult
		} else if isParam(fn, o.Var) {
			l.kind = lkParam
		}
	case eir.ObjParam:
		l.kind = lkParam
		if isResult(fn, o.Var) {
			l.kind = lkResult
		}
	case eir.ObjTemp:
		l.kind = lkTemp
		if u, ok := node.(*ast.UnaryExpr); ok && u.Op == token.AND {
			if cl, ok := ast.Unparen(u.X).(*ast.CompositeLit); ok {
				l.kind, l.node = lkLit, cl
			}
		}
		if cl, ok := node.(*ast.CompositeLit); ok {
			l.kind, l.node = lkLit, cl
		}
	case eir.ObjBox:
		l.kind, l.node = lkBox, node
		if tp, ok := types2.Unalias(l.typ).(*types2.TypeParam); ok {
			// A value of a type parameter is copied only for type
			// arguments that are not one pointer-shaped word.
			l.tparam = tp
			l.tparg = r.copiedTypeArg(fn, tp)
		}
		if T := l.typ; refNoun(T) != "" || isBoxPtr(T) {
			// An ordinary interface lends a counted reference or a box: it
			// views the object, it holds no copy of it.
			l.kind, l.ref = lkObject, refNoun(T)
			if id, ok := ast.Unparen(node).(*ast.Ident); ok && info != nil {
				if v, ok := info.Uses[id].(*types2.Var); ok {
					l.v, l.pos = v, v.Pos()
				}
			}
		}
	case eir.ObjBoxView:
		l.kind = lkObject
		if o.Var != nil {
			l.typ, l.pos = o.Var.Type(), o.Var.Pos()
		}
		if o.Life == eir.LifeStmt && !o.ErrHolder {
			// The object of a box a call returns, which the statement
			// holds as a temporary owner and drops at its end (section
			// 18.3, "A local owner's memory"; section 18.6).
			for _, n := range r.enclosing(o.Pos) {
				if ce, ok := n.(*ast.CallExpr); ok && ce.Pos() == o.Pos && info != nil {
					if tv, ok := info.Types[ce]; ok && tv.Type != nil {
						l.kind, l.node, l.object, l.typ = lkStmtTemp, ce, true, tv.Type
					}
				}
			}
		}
	case eir.ObjClosure:
		l.kind = lkClosure
		lit, _ := ast.Unparen(node).(*ast.FuncLit)
		if lit == nil {
			if rs, ok := innermost[*ast.RangeStmt](r.enclosing(o.Pos)); ok && rs.Pos() == o.Pos {
				l.kind, l.node = lkLoopBody, rs
				l.captures = captured(rs.Body, rs.Pos(), rs.End(), info)
				break
			}
			for _, n := range r.enclosing(o.Pos) {
				if fl, ok := n.(*ast.FuncLit); ok && fl.Pos() == o.Pos {
					lit = fl
				}
			}
		}
		if lit != nil {
			l.node = lit
			l.captures = captured(lit.Body, lit.Pos(), lit.End(), info)
		}
	case eir.ObjMethodVal:
		l.kind, l.node = lkMethodValue, node
	case eir.ObjSliceLit:
		l.kind, l.node = lkSliceLit, node
	case eir.ObjVarargs:
		l.kind = lkVarargs
		if ce, ok := innermost[*ast.CallExpr](r.enclosing(o.Pos)); ok {
			l.node = ce
		}
	case eir.ObjStmtTemp:
		l.kind, l.node = lkStmtTemp, node
		if ce, ok := node.(*ast.CallExpr); ok {
			if se, ok := ast.Unparen(ce.Fun).(*ast.SelectorExpr); ok {
				l.method = se.Sel.Name
				l.node = se.X
			}
		} else {
			l.object = true
		}
		l.v = nil
	case eir.ObjRegion:
		l.kind = lkCtor
	case eir.ObjArenaWindow:
		l.kind = lkArena
		if info != nil && l.v == nil {
			if ce, ok := innermost[*ast.CallExpr](r.enclosing(o.Pos)); ok {
				l.node = ce
			}
		}
	default:
		l.kind = lkTemp
	}
	return l
}

func isParam(fn *eir.Func, v *types2.Var) bool {
	if v == nil {
		return false
	}
	for _, in := range fn.Inputs {
		if in.Var == v {
			return true
		}
	}
	return false
}

func isResult(fn *eir.Func, v *types2.Var) bool {
	if v == nil || fn.Sig == nil {
		return false
	}
	res := fn.Sig.Results()
	for i := 0; i < res.Len(); i++ {
		if res.At(i) == v {
			return true
		}
	}
	return false
}

// copiedTypeArg returns a type argument the program gives type parameter
// tp of fn whose values an interface holds as a copy (not one
// pointer-shaped word), or nil.
func (r *reporter) copiedTypeArg(fn *eir.Func, tp *types2.TypeParam) types2.Type {
	if fn.Key.Obj == nil {
		return nil
	}
	gen := fn.Key.Obj.Origin()
	sig, _ := gen.Type().(*types2.Signature)
	var recvType *types2.TypeName
	if sig != nil && sig.Recv() != nil {
		t := sig.Recv().Type()
		if p, ok := t.(*types2.Pointer); ok {
			t = p.Elem()
		}
		if n, ok := types2.Unalias(t).(*types2.Named); ok {
			recvType = n.Origin().Obj()
		}
	}
	var best types2.Type
	for _, pkg := range r.prog.Packages {
		for id, inst := range pkg.Info.Instances {
			ok := false
			switch obj := pkg.Info.Uses[id].(type) {
			case *types2.Func:
				ok = obj.Origin() == gen && sig != nil && sig.TypeParams().Len() > tp.Index() && sig.TypeParams().At(tp.Index()) == tp
			case *types2.TypeName:
				ok = recvType != nil && obj == recvType
			}
			if !ok || inst.TypeArgs == nil || tp.Index() >= inst.TypeArgs.Len() {
				continue
			}
			if T := inst.TypeArgs.At(tp.Index()); !oneWord(T) {
				if best == nil || types2.TypeString(T, nil) < types2.TypeString(best, nil) {
					best = T // the same one on every run
				}
			}
		}
	}
	return best
}

// oneWord reports whether an interface holds a value of T in its data word
// itself, as the lowerer stores it: a pointer-shaped value, a number, a
// map, a channel, a function value, a box.
func oneWord(T types2.Type) bool {
	if types2.PointerShaped(T) {
		return true
	}
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		return u.Kind() != types2.String && u.Kind() != types2.UntypedString
	case *types2.Pointer, *types2.Signature, *types2.Chan, *types2.Map:
		return true
	case *types2.Wrapper:
		switch {
		case u.IsBox():
			return true
		case u.Kind() == types2.WrapVolatile:
			return oneWord(u.Elem())
		}
	case *types2.Packed:
		b := u.Bits()
		return b == 8 || b == 16 || b == 32 || b == 64
	}
	return false
}
