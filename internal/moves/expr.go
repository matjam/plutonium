package moves

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// expr walks an expression whose value is used where it is, or copied into
// something that does not own it (an ordinary interface lends it).
func (f *fn) expr(e ast.Expr) {
	switch e := e.(type) {
	case nil:
	case *ast.Ident:
		if v := f.varOf(e); v != nil {
			f.use(e, v)
		}
	case *ast.ParenExpr:
		f.expr(e.X)
	case *ast.BasicLit:
	case *ast.FuncLit:
		f.funcLit(e)
	case *ast.CompositeLit:
		f.temp(e)
		f.compLit(e)
	case *ast.SelectorExpr:
		f.selector(e)
	case *ast.IndexExpr:
		if f.containerOfOwners(e) {
			f.indexErr(e)
		}
		f.expr(e.X)
		f.expr(e.Index)
	case *ast.IndexListExpr:
		f.expr(e.X)
	case *ast.SliceExpr:
		f.expr(e.X)
		f.expr(e.Low)
		f.expr(e.High)
		f.expr(e.Max)
	case *ast.StarExpr:
		f.expr(e.X)
	case *ast.UnaryExpr:
		f.expr(e.X)
	case *ast.BinaryExpr:
		f.expr(e.X)
		f.expr(e.Y)
	case *ast.CallExpr:
		f.temp(e)
		f.call(e, mvArg)
	case *ast.TypeAssertExpr:
		f.temp(e)
		f.expr(e.X)
		if e.Type != nil {
			f.assertTo(e.X, f.c.typeOf(e.Type), e.Pos())
		}
	case *ast.KeyValueExpr:
		f.expr(e.Key)
		f.expr(e.Value)
	}
}

// consume walks an expression whose value goes to a place of type dst. If
// dst is an owner type the value moves there: out of a variable, which then
// holds nothing, and never out of anything else.
func (f *fn) consume(e ast.Expr, dst types2.Type, mv move) {
	if e == nil {
		return
	}
	if !isOwner(dst) || !isOwner(f.c.typeOf(e)) {
		f.expr(e)
		return
	}
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		v, ok := f.c.objOf(x).(*types2.Var)
		switch {
		case !ok:
		case isGlobal(v):
			f.placeErr(x, placeGlobal)
		default:
			f.moveVar(x, v, mv)
		}
	case *ast.SelectorExpr:
		if sel := f.c.selections[x]; sel != nil && sel.Kind() == types2.FieldVal {
			f.expr(x.X)
			f.placeErr(x, placeField)
			return
		}
		if v, ok := f.c.objOf(x.Sel).(*types2.Var); ok && isGlobal(v) {
			f.placeErr(x, placeGlobal)
			return
		}
		f.expr(x)
	case *ast.IndexExpr:
		f.expr(x)
		if !types2.IndexesByMethod(f.c.typeOf(x.X)) {
			f.placeErr(x, placeElem)
		}
	case *ast.StarExpr:
		f.expr(x.X)
		f.placeErr(x, placePointer)
	case *ast.CallExpr:
		if tv := f.c.types[x.Fun]; tv.IsType() && len(x.Args) == 1 {
			// A conversion between owner types moves its operand.
			f.consume(x.Args[0], tv.Type, move{kind: mvConv, to: types2.TypeString(tv.Type, f.c.qualifier())})
			return
		}
		f.consumed = x
		f.expr(x)
	default:
		// A composite literal, a call's result, a type assertion: a value
		// that nothing else holds, which moves where it goes.
		f.consumed = ast.Unparen(e)
		f.expr(e)
	}
}

// temp records an owner value that e makes and nothing takes: lowering
// drops it at the end of the statement.
func (f *fn) temp(e ast.Expr) {
	if f.consumed == e {
		f.consumed = nil
		return
	}
	if f.report && isOwner(f.c.typeOf(e)) {
		f.c.info.Temps[e] = true
	}
}

// selector walks x.f, x.M or pkg.Name used as a value.
func (f *fn) selector(e *ast.SelectorExpr) {
	sel := f.c.selections[e]
	if sel == nil {
		return // a qualified identifier
	}
	if sel.Kind() != types2.MethodVal {
		f.expr(e.X)
		return
	}
	// A method value binds its receiver.
	fun, _ := sel.Obj().(*types2.Func)
	if fun != nil && fun.Name() == "Drop" {
		if f.report {
			f.c.report(f.c.dropDiag(e, dropMethodValue))
		}
		f.expr(e.X)
		return
	}
	recv := methodRecv(fun)
	if recv != nil && pointerElem(recv) == nil && isOwner(recv) && len(sel.Index()) == 1 {
		f.consume(e.X, recv, move{kind: mvMethodValue, to: exprText(e)})
		return
	}
	f.expr(e.X)
}

// methodRecv returns the receiver type of a method, or nil.
func methodRecv(fn *types2.Func) types2.Type {
	if fn == nil {
		return nil
	}
	sig, _ := fn.Type().(*types2.Signature)
	if sig == nil || sig.Recv() == nil {
		return nil
	}
	return sig.Recv().Type()
}

func pointerElem(T types2.Type) types2.Type {
	if p, ok := T.Underlying().(*types2.Pointer); ok {
		return p.Elem()
	}
	return nil
}

// call walks a call. kind is how its arguments move: mvGo for a go
// statement, mvArg otherwise.
func (f *fn) call(e *ast.CallExpr, kind moveKind) {
	tv := f.c.types[e.Fun]
	if tv.IsType() {
		for _, a := range e.Args {
			f.consume(a, tv.Type, move{kind: mvConv, to: types2.TypeString(tv.Type, f.c.qualifier())})
		}
		return
	}
	if tv.IsBuiltin() {
		f.builtin(e)
		return
	}
	name := exprText(e.Fun)
	if se, ok := ast.Unparen(e.Fun).(*ast.SelectorExpr); ok {
		if sel := f.c.selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
			f.methodCall(e, se, sel, kind)
		} else {
			f.expr(e.Fun)
		}
	} else {
		f.expr(e.Fun)
	}
	sig, _ := f.c.typeOf(e.Fun).Underlying().(*types2.Signature)
	if sig == nil {
		for _, a := range e.Args {
			f.expr(a)
		}
		return
	}
	f.args(e, sig, name, kind)
}

// args walks a call's arguments, moving owners into parameters that take
// them by value.
func (f *fn) args(e *ast.CallExpr, sig *types2.Signature, name string, kind moveKind) {
	params := sig.Params()
	n := params.Len()
	if len(e.Args) == 1 && n > 1 {
		f.multi(e.Args[0]) // f(g())
		return
	}
	variadic := sig.Variadic() && !e.Ellipsis.IsValid()
	for i, a := range e.Args {
		var pt types2.Type
		switch {
		case variadic && i >= n-1:
			if s, ok := params.At(n - 1).Type().Underlying().(*types2.Slice); ok {
				pt = s.Elem()
			}
			if isOwner(pt) && isOwner(f.c.typeOf(a)) {
				if f.report {
					f.c.report(f.c.variadicDiag(a, name, pt))
				}
				f.expr(a)
				continue
			}
		case i < n:
			pt = params.At(i).Type()
		}
		f.consume(a, pt, move{kind: kind, to: name, typ: pt})
	}
}

// methodCall walks the receiver of a method call x.M(...).
func (f *fn) methodCall(e *ast.CallExpr, se *ast.SelectorExpr, sel *types2.Selection, kind moveKind) {
	fun, _ := sel.Obj().(*types2.Func)
	recv := methodRecv(fun)
	if fun != nil && fun.Name() == "Drop" && len(e.Args) == 0 {
		f.dropCall(se, recv)
		return
	}
	if recv == nil || pointerElem(recv) != nil || !isOwner(recv) {
		f.expr(se.X) // a pointer receiver borrows; an interface lends
		return
	}
	if len(sel.Index()) > 1 {
		// A value method promoted from an embedded owner would move the
		// embedded field out of x.
		f.expr(se.X)
		f.placeErr(se, placeEmbedded)
		return
	}
	if kind != mvGo {
		kind = mvRecv
	}
	f.consume(se.X, recv, move{kind: kind, to: exprText(se), typ: recv})
}

// dropCall walks x.Drop(): it ends the life of an owner held in a variable,
// and may not be called in any other way.
func (f *fn) dropCall(se *ast.SelectorExpr, recv types2.Type) {
	xt := f.c.typeOf(se.X)
	if types2.IsInterface(xt) {
		if !isOwner(xt) && f.report {
			f.c.report(f.c.dropDiag(se, dropIface))
		}
		f.expr(se.X)
		return
	}
	if id, ok := ast.Unparen(se.X).(*ast.Ident); ok {
		if v := f.varOf(id); v != nil && isOwner(v.Type()) {
			f.moveVar(id, v, move{kind: mvDrop, to: exprText(se)})
			return
		}
		if v := f.varOf(id); v != nil && pointerElem(v.Type()) != nil {
			if f.report {
				f.c.report(f.c.dropDiag(se, dropPointer))
			}
			f.expr(se.X)
			return
		}
	}
	f.expr(se.X)
	if f.report {
		why := dropField
		if pointerElem(xt) != nil {
			why = dropPointer
		}
		f.c.report(f.c.dropDiag(se, why))
	}
}

// builtin walks a call of a predeclared function.
func (f *fn) builtin(e *ast.CallExpr) {
	name := ""
	switch fun := ast.Unparen(e.Fun).(type) {
	case *ast.Ident:
		name = fun.Name
	case *ast.SelectorExpr:
		name = fun.Sel.Name
	}
	switch name {
	case "copy":
		if len(e.Args) == 2 {
			if s, ok := f.c.typeOf(e.Args[0]).Underlying().(*types2.Slice); ok && isOwner(s.Elem()) && f.report {
				f.c.report(f.c.copyDiag(e, s.Elem(), "copy"))
			}
		}
		for _, a := range e.Args {
			f.expr(a)
		}
	default:
		// take(&place) borrows the place; len, new, make and the rest only
		// look at their operands.
		for _, a := range e.Args {
			f.expr(a)
		}
	}
}

// compLit walks a composite literal: each element moves into it.
func (f *fn) compLit(e *ast.CompositeLit) {
	T := f.c.typeOf(e)
	if T == nil {
		return
	}
	if p := pointerElem(T); p != nil {
		T = p // an elided &T{...}
	}
	to := exprText(e)
	switch u := T.Underlying().(type) {
	case *types2.Struct:
		for i, el := range e.Elts {
			x, ft := el, types2.Type(nil)
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				x = kv.Value
				if id, ok := kv.Key.(*ast.Ident); ok {
					for j := 0; j < u.NumFields(); j++ {
						if u.Field(j).Name() == id.Name {
							ft = u.Field(j).Type()
						}
					}
				}
			} else if i < u.NumFields() {
				ft = u.Field(i).Type()
			}
			f.consume(x, ft, move{kind: mvStore, to: to})
		}
	case *types2.Array:
		f.elems(e, u.Elem(), to)
	case *types2.Slice:
		if isOwner(u.Elem()) && len(e.Elts) > 0 && f.report {
			f.c.report(f.c.sliceLitDiag(e, u.Elem()))
		}
		f.elems(e, u.Elem(), to)
	default:
		for _, el := range e.Elts {
			f.expr(el)
		}
	}
}

func (f *fn) elems(e *ast.CompositeLit, elem types2.Type, to string) {
	for _, el := range e.Elts {
		x := el
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			f.expr(kv.Key)
			x = kv.Value
		}
		f.consume(x, elem, move{kind: mvStore, to: to})
	}
}

// funcLit checks a function literal where it is made: the owners it
// captures must hold a value here, and its body is checked on its own.
func (f *fn) funcLit(lit *ast.FuncLit) {
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if v := f.varOf(id); v != nil && f.vars[v] && f.c.uses[id] != nil {
			f.use(id, v)
		}
		return true
	})
	if f.report {
		sig, _ := f.c.typeOf(lit).(*types2.Signature)
		f.c.checkFunc(lit.Type, nil, lit.Body, sig)
	}
}

// assertTo checks a type assertion of x to T: taking an owner out of an
// ordinary interface would make a second owner of the value it lends.
func (f *fn) assertTo(x ast.Expr, T types2.Type, pos token.Pos) {
	xt := f.c.typeOf(x)
	if !isOwner(T) || types2.IsInterface(T) || xt == nil || isOwner(xt) || !f.report {
		return
	}
	f.c.report(f.c.assertDiag(x, xt, T, pos))
}

// containerOfOwners reports whether x[k] calls Index on a container whose
// elements are owners.
func (f *fn) containerOfOwners(e *ast.IndexExpr) bool {
	if !types2.IndexesByMethod(f.c.typeOf(e.X)) {
		return false
	}
	return isOwner(f.c.typeOf(e))
}

func (f *fn) indexErr(e *ast.IndexExpr) {
	if f.report {
		f.c.report(f.c.indexDiag(e))
	}
}

// A placeKind says what kind of place a value cannot move out of.
type placeKind uint8

const (
	placeField placeKind = iota
	placeElem
	placePointer
	placeGlobal
	placeEmbedded
)

func (f *fn) placeErr(e ast.Expr, k placeKind) {
	if f.report {
		f.c.report(f.c.placeDiag(e, k))
	}
}
