// Translated by go2pu from internal/moves/expr.go at the go-cutover tag; edited by hand since.

package moves

import (
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
)

// expr walks an expression whose value is used where it is, or copied into
// something that does not own it (an ordinary interface lends it).
func (f *fn) expr(e ast.Expr) {
	// A type is not evaluated, so nothing in it is read or moved. The
	// explicit type argument vec.Vec[int] of vec.New[vec.Vec[int]] is an
	// instantiation (spec 11.16), not a read of a container (11.11).
	if f.c.types[e].IsType() {
		return
	}
	switch e.(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr:
		f.checkView(e) // views.go
	}
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
		if f.readOnly(e) {
			return
		}
		f.selector(e)
	case *ast.IndexExpr:
		if f.containerOfOwners(e) {
			f.indexErr(e)
		} else if f.readOnly(e) {
			return
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
		if e.Op == token.AND {
			f.addrOf(e) // empty.go
			return
		}
		f.expr(e.X)
	case *ast.BinaryExpr:
		f.expr(e.X)
		if e.Op == token.LAND || e.Op == token.LOR {
			// The right operand may not be evaluated: what it moves is
			// moved on some paths only.
			skipped := f.env.clone()
			f.expr(e.Y)
			f.env = join(f.env, skipped)
			return
		}
		f.expr(e.Y)
	case *ast.CallExpr:
		f.temp(e)
		f.call(e, mvArg)
	case *ast.TypeAssertExpr:
		f.temp(e)
		if e.Type != nil && f.assertMoves(e.X, f.c.typeOf(e.Type)) {
			if f.assertPlace(e.X, exprText(e.Type), false) {
				return
			}
			// Moves the owner out of the interface variable, which lowering
			// clears as for any move.
			f.consume(e.X, f.c.typeOf(e.X), move{kind: mvAssert, to: exprText(e)})
			return
		}
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
	f.checkView(e)
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
	if recv != nil && pointerElem(recv) == nil && isOwner(recv) && !types2.IsInterface(recv) {
		// A consuming method has no method value (sections 11.9 and 18.5,
		// Methods): it would move the receiver when it is made, not when
		// it is called.
		if f.report {
			f.c.report(f.c.methodValueDiag(e, recv))
		}
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
	// What the callee stores of its arguments into the memory of others
	// (views.go), as the arguments are before the call moves any.
	stores := f.argViews(e)
	defer f.callStores(stores)
	// The views the callee may free through (frees.p).
	freed := f.freeArgs(e)
	defer f.freeViews(e, freed)
	// The local arenas the callee may reset or release (frees.p).
	reclaimed := f.reclaimArgs(e)
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
	f.reclaim(e, reclaimed) // views.go
}

// args walks a call's arguments, moving owners into parameters that take
// them by value.
func (f *fn) args(e *ast.CallExpr, sig *types2.Signature, name string, kind moveKind) {
	params := sig.Params()
	n := params.Len()
	if f.c.spreads(e.Args, n) {
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
	if fun != nil && types2.BoxMethodOf(fun) != nil {
		f.expr(se.X) // Clone and Upgrade only look at the reference
		return
	}
	if kind == mvGo && pointerElem(recv) != nil {
		if w := types2.GoBoxReceiver(sel); w != nil && w.Kind() == types2.WrapOwned {
			// task p.Serve() moves the owned box into the task, which
			// drops it when the method returns (language spec section 12.12).
			f.consume(se.X, f.c.typeOf(se.X), move{kind: mvGoRecv, to: exprText(se), typ: f.c.typeOf(se.X)})
			return
		}
	}
	if recv == nil || pointerElem(recv) != nil || !isOwner(recv) || types2.IsInterface(recv) {
		f.expr(se.X) // a pointer receiver borrows; an interface lends
		if pointerElem(recv) != nil {
			f.forget(se.X) // the method may change it (empty.go)
		}
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
	case "new":
		// new(v) stores v in the new variable: an owner moves into it.
		for i, a := range e.Args {
			if tv := f.c.types[a]; i == 0 && !tv.IsType() {
				f.consume(a, tv.Type, move{kind: mvStore, to: exprText(e)})
				continue
			}
			f.expr(a)
		}
	case "take":
		// take(&place) borrows the place, and leaves it empty (empty.go).
		if amp := f.takeArg(e); amp != nil {
			f.take(amp)
			if id, ok := ast.Unparen(amp.X).(*ast.Ident); ok {
				if v := f.varOf(id); v != nil && !f.captured(v) {
					// The whole variable now holds the zero value: views of
					// what it held are stale (views.go).
					f.invalidate(v, move{kind: mvTake, pos: id.Pos(), to: exprText(e)})
				}
			} else if v, path, box, ok := f.placePath(amp.X); ok && !box && v != nil && !f.captured(v) {
				// take(&h.l) empties the part h.l: the views of it are
				// stale.
				f.invalidatePart(v, path, move{kind: mvTake, pos: amp.X.Pos(), to: exprText(e)})
			}
			return
		}
		for _, a := range e.Args {
			f.expr(a)
		}
	default:
		// len, make and the rest only look at their operands.
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
					var pkg *types2.Package
					if v, ok := f.c.uses[id].(*types2.Var); ok {
						pkg = v.Pkg()
					}
					// The field may be promoted through embedded
					// structs (language spec section 11.3).
					if path := types2.LiteralFields(T, pkg, id.Name); len(path) > 0 {
						ft = path[len(path)-1].Type()
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
	if t := f.c.scoped.Lits[lit]; t != nil {
		f.scopedTask(t)
		return
	}
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
	f.litPins(lit) // views.go
	if f.report {
		sig, _ := f.c.typeOf(lit).(*types2.Signature)
		f.c.checkFunc(lit.Type, nil, lit.Body, sig)
	}
}

// assertTo checks a type assertion of x to T: taking an owner out of an
// ordinary interface would make a second owner of the value it lends.
// assertMoves reports whether asserting x to T moves an owner out of x: x
// is an owning interface and T an owner type or another owning interface.
func (f *fn) assertMoves(x ast.Expr, T types2.Type) bool {
	xt := f.c.typeOf(x)
	if xt == nil || !isOwner(xt) {
		return false
	}
	if types2.UnionVariants(xt) != nil {
		return true // an owner union's payload, or the union itself
	}
	return T != nil && types2.IsInterface(xt) && isOwner(T)
}

// assertOut walks the operand of a comma-ok assertion or a type switch
// that moves an owner out of x: x must be a variable, which holds nothing
// afterwards (where the assertion fails it still does, which lowering
// arranges and the if statement of a comma-ok assertion follows).
func (f *fn) assertOut(x ast.Expr, to, typ string, isSwitch bool) bool {
	if f.assertPlace(x, typ, isSwitch) {
		return false
	}
	if id, ok := ast.Unparen(x).(*ast.Ident); ok {
		if v := f.varOf(id); v != nil {
			f.moveOut(id, v, move{kind: mvAssert, to: to})
			return true
		}
	}
	// Not a variable: as for any move, the place would still believe it
	// holds the owner.
	f.consume(x, f.c.typeOf(x), move{kind: mvAssert, to: to})
	return false
}

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
	if f.c.types[e].IsType() || !types2.IndexesByMethod(f.c.typeOf(e.X)) {
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

// assertPlace reports a moving assertion to typ, or a type switch with a
// binding, whose operand x is a field, an element or *p (sections 11.13
// and 12.9): it would move out of a place that still holds the value.
func (f *fn) assertPlace(x ast.Expr, typ string, isSwitch bool) bool {
	var k placeKind
	switch p := ast.Unparen(x).(type) {
	case *ast.SelectorExpr:
		if sel := f.c.selections[p]; sel == nil || sel.Kind() != types2.FieldVal {
			return false
		}
		k = placeField
	case *ast.IndexExpr:
		if types2.IndexesByMethod(f.c.typeOf(p.X)) {
			return false
		}
		k = placeElem
	case *ast.StarExpr:
		k = placePointer
	default:
		return false
	}
	f.checkView(x)
	f.expr(x)
	if f.report {
		f.c.report(f.c.assertPlaceDiag(x, k, typ, isSwitch))
	}
	return true
}

func (f *fn) placeErr(e ast.Expr, k placeKind) {
	if f.report {
		f.c.report(f.c.placeDiag(e, k))
	}
}
