// Translated by go2pu from internal/escape/sugar.go at the go-cutover tag; edited by hand since.

package escape

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
)

// Method calls that statements make by sugar: a range over a container
// calls its All method (language spec section 12.10), and an index
// expression on a container calls Index, or SetIndex when it is assigned to
// (section 11.11). They are checked as the calls they stand for.

// sugarCall returns the call expression x.name() stands for, made once per
// node so that what the analysis keeps by node (the region of a call that
// returns closures) is the same in every walk.
func (a *analyzer) sugarCall(node any, x ast.Expr, name string) *ast.CallExpr {
	if a.sugar == nil {
		a.sugar = map[any]*ast.CallExpr{}
	}
	if e := a.sugar[node]; e != nil {
		return e
	}
	sel := pu.New(ast.SelectorExpr{X: x, Sel: pu.New(ast.Ident{NamePos: x.End(), Name: name})})
	e := pu.New(ast.CallExpr{Fun: sel, Lparen: x.End(), Rparen: x.End()})
	a.sugar[node] = e
	return e
}

// A sugarNode names one of the calls an expression stands for: an index
// expression may stand for a call of Index and one of SetIndex.
type sugarNode struct {
	e    ast.Expr
	name string
}

// sugarTarget is target for the method call x.name(...) that a statement
// makes by sugar. ok is false if x has no such method.
func (w *walker) sugarTarget(x ast.Expr, name string) (t callTarget, ok bool) {
	T := w.typeOf(x)
	if T == nil {
		return callTarget{}, false
	}
	obj, path, _ := types2.LookupFieldOrMethod(T, true, w.f.pkg.Types, name)
	fn, ok := obj.(*types2.Func)
	if !ok {
		return callTarget{}, false
	}
	sig := fn.Type().(*types2.Signature)
	label := compat.Concat(exprText(x), ".", name)
	if r := sig.Recv(); r != nil && isInterface(r.Type()) {
		_, byName := types2.Unalias(T).(*types2.TypeParam)
		if p := pointerElem(T); p != nil {
			_, byName = types2.Unalias(p).(*types2.TypeParam)
		}
		return callTarget{sig: sig, recv: w.value(x), hasRecv: true, recvExpr: x, recvKey: x, method: name, byName: byName, name: label}, true
	}
	f := w.a.funcs[fn.Origin()]
	recvExpr := x
	if r := sig.Recv(); r != nil && pointerElem(r.Type()) != nil && pointerElem(T) == nil {
		recvExpr = pu.New(ast.UnaryExpr{OpPos: x.Pos(), Op: token.AND, X: x})
	}
	return callTarget{static: f, sig: sig, recv: w.receiverOf(x, fn, path), hasRecv: true, recvExpr: recvExpr, recvKey: x, name: w.funcLabel(f, fn)}, true
}

// applyTarget applies the call e of t to args (receiver first) and returns
// what its nres results may point to.
func (w *walker) applyTarget(e *ast.CallExpr, t callTarget, args []tset, cs *callSite, nres int) []tset {
	switch {
	case t.static != nil:
		return w.regionResults(e, t, args, w.apply(t.static, args, cs, t.name, recvExact, nres))
	case t.method != "":
		return w.regionResults(e, t, args, w.dynIface(t, args, cs, nres))
	case t.dynFunc:
		return w.regionResults(e, t, args, w.dynFuncCall(t, args, cs, e.Fun, nres))
	}
	return compat.Make[tset](nres)
}

// indexSugar reports whether the index expression e calls the indexing
// methods of its operand's type (section 11.11), and returns its operand
// and index.
func (w *walker) indexSugar(e ast.Expr) (x, index ast.Expr, ok bool) {
	ix, isIndex := ast.Unparen(e).(*ast.IndexExpr)
	if !isIndex {
		return nil, nil, false
	}
	xt := w.typeOf(ix.X)
	if xt == nil || w.a.types[ix.X].IsType() || !types2.IndexesByMethod(xt) {
		return nil, nil, false
	}
	return ix.X, ix.Index, true
}

// indexCall applies x.name(index, vals...), the call an index expression
// e on x stands for, and returns its nres results. ok is false if x has no
// such method.
func (w *walker) indexCall(e ast.Expr, x, index ast.Expr, name string, vals []tset, valExprs []ast.Expr, nres int) ([]tset, bool) {
	t, ok := w.sugarTarget(x, name)
	if !ok || t.sig.Params().Len() != 1+len(vals) {
		return nil, false
	}
	call := w.a.sugarCall(pu.Box(sugarNode{e, name}), x, name)
	args := []tset{t.recv, w.conv(w.value(index), w.typeOf(index), t.sig.Params().At(0).Type(), index)}
	exprs := compat.Clone([]ast.Expr{t.recvExpr, index})
	for i, v := range vals {
		args = compat.Append(args, v)
		exprs = compat.Append(exprs, valExprs[i])
	}
	cs := &callSite{site: call, pos: e.Pos(), exprs: exprs, sig: t.sig}
	return w.applyTarget(call, t, args, cs, nres), true
}
