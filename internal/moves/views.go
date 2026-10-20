package moves

import (
	"slices"
	"strconv"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Views of local owners (language spec section 18.3).
//
// A view obtained from an owner held in a local variable (&c, c.Slice(), a
// slice of a local array of owners, a method value c.M) is bounded by that variable: it may not be used after the
// variable has been moved, dropped or given a new value in the same
// function. The walk follows, per variable of the function, the views of
// the function's owners it may hold and where in it (a path, as in
// empty.go). Moving an owner, or giving it a new value, makes every view of
// it stale, and reading a stale view is error E3313. Copies follow the
// views along: assignments, composite literals, call results that the
// escape analysis says derive from an argument, and stores a callee makes
// into memory another argument points to.
//
// A view stored where the walk cannot follow it (through a pointer, into a
// variable whose address is kept or that a function literal uses, into the
// heap) pins the owner instead: moving it or giving it a new value is then
// the error, since the view could be used afterwards without the walk
// seeing it.

// A view is a view of an owner variable held in a variable.
type view struct {
	of   *types2.Var // the owner viewed
	at   Path        // where in the holding variable
	pos  token.Pos   // where the view was made
	text string      // the expression that made it
	// inHeap: the view looks at memory the owner holds on the heap (what
	// c.Slice() returns, a field of an owned box), not at the variable's
	// own storage. A move does not release that memory; a view of it kept
	// where the walk cannot follow is left to generation checks (section
	// 18.11) rather than pinning the owner.
	inHeap bool
	// stale is how the owner lost the value the view looks at, once it
	// has; maybe says it did so on some paths only.
	stale *move
	maybe bool
}

// A pinMark records that a view of an owner went where the walk cannot
// follow it.
type pinMark struct {
	pos   token.Pos // where the view was made
	text  string    // the expression that made it
	where token.Pos // where it went
	how   string    // how: "stored in *q"
}

func (w view) key() string {
	return strconv.Itoa(int(w.pos)) + "|" + w.of.Name() + strconv.Itoa(int(w.of.Pos())) + "|" + pathKey(w.at)
}

// sortViews orders views by key, so that equal sets compare equal.
func sortViews(vs []view) []view {
	slices.SortFunc(vs, func(a, b view) int { return strings.Compare(a.key(), b.key()) })
	return slices.CompactFunc(vs, func(a, b view) bool { return a.key() == b.key() })
}

// joinViews merges the views a variable holds on two paths.
func joinViews(a, b []view) []view {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	byKey := map[string]view{}
	for _, w := range a {
		byKey[w.key()] = w
	}
	for _, w := range b {
		u, ok := byKey[w.key()]
		if !ok {
			byKey[w.key()] = w
			continue
		}
		switch {
		case u.stale == nil && w.stale == nil:
		case u.stale == nil:
			w.maybe = true
			byKey[w.key()] = w
		case w.stale == nil:
			u.maybe = true
			byKey[w.key()] = u
		default:
			u.maybe = u.maybe || w.maybe
			byKey[w.key()] = u
		}
	}
	out := make([]view, 0, len(byKey))
	for _, w := range byKey {
		out = append(out, w)
	}
	return sortViews(out)
}

func viewsEqual(a, b []view) bool {
	return slices.EqualFunc(a, b, func(x, y view) bool {
		return x.key() == y.key() && (x.stale == nil) == (y.stale == nil) && x.maybe == y.maybe
	})
}

// ----------------------------------------------------------------------------
// Which types carry views

// mayView reports whether a value of type T may hold a view: a pointer, a
// slice, a function value, an interface, or something holding one. Counted
// values and boxes point only to the heap.
func mayView(T types2.Type) bool { return mayViewSeen(T, map[types2.Type]bool{}) }

func mayViewSeen(T types2.Type, seen map[types2.Type]bool) bool {
	if T == nil || seen[T] {
		return false
	}
	seen[T] = true
	if _, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		return true
	}
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		return u.Kind() == types2.UnsafePointer
	case *types2.Pointer, *types2.Slice, *types2.Signature, *types2.Interface:
		return true
	case *types2.Array:
		return u.Len() > 0 && mayViewSeen(u.Elem(), seen)
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if mayViewSeen(u.Field(i).Type(), seen) {
				return true
			}
		}
	case *types2.Tuple:
		for i := 0; i < u.Len(); i++ {
			if mayViewSeen(u.At(i).Type(), seen) {
				return true
			}
		}
	case *types2.Wrapper:
		if u.IsBox() {
			return false
		}
		return mayViewSeen(u.Elem(), seen)
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants(); i++ {
			if v := u.Variant(i); v.HasPayload() && mayViewSeen(v.Payload(), seen) {
				return true
			}
		}
	case *types2.TypeParam:
		return true
	}
	return false
}

// ----------------------------------------------------------------------------
// What an expression views

// ownerVar returns the owner variable of this function that v is, or nil.
func (f *fn) ownerVar(v *types2.Var) *types2.Var {
	if v == nil || !f.vars[v] || !isOwner(v.Type()) {
		return nil
	}
	return v
}

// held returns the views variable v holds within path p, with their paths
// made relative to p.
func (f *fn) held(v *types2.Var, p Path) []view {
	if f.env == nil || v == nil {
		return nil
	}
	var out []view
	for _, w := range f.env[v].views {
		if !overlap(w.at, p) {
			continue
		}
		if len(w.at) > len(p) {
			w.at = slices.Clone(w.at[len(p):])
		} else {
			w.at = nil
		}
		out = append(out, w)
	}
	return out
}

// whole returns vs with their paths forgotten: they may be anywhere in the
// value.
func whole(vs []view) []view {
	out := make([]view, len(vs))
	for i, w := range vs {
		w.at = nil
		out[i] = w
	}
	return out
}

// selfView returns a new view of the owner that the place x is part of, if
// x is part of a local owner (or of the object of an owned box it holds),
// together with the views that part holds.
func (f *fn) selfView(x ast.Expr, made ast.Expr) []view {
	v, path, box, ok := f.placePath(x)
	if !ok || v == nil {
		return f.viewsOf(baseOf(x))
	}
	var out []view
	if box {
		path = nil
	}
	out = append(out, whole(f.held(v, path))...)
	if o := f.ownerVar(v); o != nil {
		out = append(out, view{of: o, pos: made.Pos(), text: exprText(made), inHeap: box})
	}
	return out
}

// baseOf strips the field selections, indexing and dereferences of a place
// down to the expression it starts from.
func baseOf(x ast.Expr) ast.Expr {
	for {
		switch e := ast.Unparen(x).(type) {
		case *ast.SelectorExpr:
			x = e.X
		case *ast.IndexExpr:
			x = e.X
		case *ast.StarExpr:
			x = e.X
		default:
			return x
		}
	}
}

// viewsOf returns the views of the function's owners that the value of e
// may hold. It walks nothing and reports nothing.
func (f *fn) viewsOf(e ast.Expr) []view {
	if e == nil || f.env == nil {
		return nil
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.viewsOf(x.X)
	case *ast.Ident:
		v := f.varOf(x)
		if v == nil || !mayView(v.Type()) {
			return nil
		}
		return f.held(v, nil)
	case *ast.UnaryExpr:
		if x.Op != token.AND {
			return nil
		}
		if lit, ok := ast.Unparen(x.X).(*ast.CompositeLit); ok {
			return whole(f.viewsOf(lit))
		}
		return f.selfView(x.X, x)
	case *ast.SliceExpr:
		if _, isArr := f.c.typeOf(x.X).Underlying().(*types2.Array); isArr {
			return f.selfView(x.X, x)
		}
		return f.viewsOf(x.X)
	case *ast.SelectorExpr:
		sel := f.c.selections[x]
		if sel == nil {
			return nil // a qualified identifier: a package variable or function
		}
		if sel.Kind() == types2.MethodVal {
			fun, _ := sel.Obj().(*types2.Func)
			if recv := methodRecv(fun); recv != nil && pointerElem(recv) != nil && pointerElem(f.c.typeOf(x.X)) == nil && !types2.IsInterface(f.c.typeOf(x.X)) {
				return f.selfView(x.X, x) // the method value takes &x.X
			}
			return f.viewsOf(x.X)
		}
		if !mayView(f.c.typeOf(x)) {
			return nil
		}
		return f.partViews(x)
	case *ast.IndexExpr:
		if !mayView(f.c.typeOf(x)) {
			return nil
		}
		if _, isArr := f.c.typeOf(x.X).Underlying().(*types2.Array); isArr {
			return f.partViews(x)
		}
		if types2.MapOf(f.c.typeOf(x.X)) != nil {
			return nil
		}
		return f.viewsOf(x.X)
	case *ast.StarExpr:
		if !mayView(f.c.typeOf(x)) {
			return nil
		}
		return f.viewsOf(x.X)
	case *ast.TypeAssertExpr:
		return f.viewsOf(x.X)
	case *ast.CompositeLit:
		return f.litViews(x)
	case *ast.CallExpr:
		return f.callViews(x)
	case *ast.KeyValueExpr:
		return f.viewsOf(x.Value)
	}
	return nil
}

// partViews returns the views a field or array element read from a local
// variable holds, and for a part of a local owner that may hold a view,
// a view of the owner: a slice or pointer field of an owner looks at what
// the owner holds.
func (f *fn) partViews(x ast.Expr) []view {
	v, path, box, ok := f.placePath(x)
	if !ok {
		return f.viewsOf(baseOf(x))
	}
	var out []view
	if box {
		out = whole(f.held(v, nil))
	} else {
		out = f.held(v, path)
	}
	if o := f.ownerVar(v); o != nil {
		out = append(out, view{of: o, pos: x.Pos(), text: exprText(x), inHeap: true})
	}
	return out
}

// litViews returns the views a composite literal holds, by field or
// element.
func (f *fn) litViews(lit *ast.CompositeLit) []view {
	T := f.c.typeOf(lit)
	if p := pointerElem(T); p != nil {
		T = p
	}
	var out []view
	add := func(step string, el ast.Expr, ft types2.Type) {
		for _, w := range f.viewsTo(el, ft) {
			if step != "" {
				w.at = append(Path{step}, w.at...)
			} else {
				w.at = nil
			}
			out = append(out, w)
		}
	}
	switch u := T.Underlying().(type) {
	case *types2.Struct:
		for i, el := range lit.Elts {
			x, idx := el, i
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				x, idx = kv.Value, -1
				if id, ok := kv.Key.(*ast.Ident); ok {
					for j := 0; j < u.NumFields(); j++ {
						if u.Field(j).Name() == id.Name {
							idx = j
						}
					}
				}
			}
			var ft types2.Type
			step := ""
			if idx >= 0 && idx < u.NumFields() {
				ft, step = u.Field(idx).Type(), "."+strconv.Itoa(idx)
			}
			add(step, x, ft)
		}
	default:
		for _, el := range lit.Elts {
			add("", el, nil)
		}
	}
	return out
}

// viewsTo returns the views e holds once converted to type T: an owner
// converted to an ordinary interface lends itself, so the interface is a
// view of it.
func (f *fn) viewsTo(e ast.Expr, T types2.Type) []view {
	out := f.viewsOf(e)
	if T != nil && types2.IsInterface(T) && !isOwner(T) && isOwner(f.c.typeOf(e)) {
		out = append(out, f.selfView(e, e)...)
	}
	return out
}

// callViews returns the views a call's results may hold: the views of the
// arguments the escape analysis says a result derives from.
func (f *fn) callViews(call *ast.CallExpr) []view {
	tv := f.c.types[call.Fun]
	if tv.IsType() {
		var out []view
		for _, a := range call.Args {
			out = append(out, f.viewsTo(a, tv.Type)...)
		}
		return out
	}
	if !mayView(f.c.typeOf(call)) {
		return nil
	}
	var out []view
	if tv.IsBuiltin() {
		id, _ := ast.Unparen(call.Fun).(*ast.Ident)
		if id != nil && (id.Name == "append" || id.Name == "min" || id.Name == "max") {
			for _, a := range call.Args {
				out = append(out, whole(f.viewsOf(a))...)
			}
		}
		return out
	}
	f.eachArg(call, func(key, val ast.Expr, T types2.Type) {
		af := f.c.calls.Arg(key)
		if af != nil && !af.Returned {
			return
		}
		for _, w := range whole(f.viewsTo(val, T)) {
			if call.Pos() <= w.pos && w.pos < call.End() {
				// Made by this call (c.Slice() takes &c): name the call. A
				// result loaded through the argument looks at what the
				// owner holds rather than at the owner.
				w.pos, w.text = call.Pos(), exprText(call)
				w.inHeap = w.inHeap || af != nil && !af.Self
			}
			out = append(out, w)
		}
	})
	return out
}

// eachArg calls fn for each argument of a call to a function or method,
// receiver first: key is the expression the escape analysis records facts
// under, val the value passed (&x for a receiver whose address the call
// takes) and T the parameter's type, if known.
func (f *fn) eachArg(call *ast.CallExpr, fn func(key, val ast.Expr, T types2.Type)) {
	if se := funSelector(call.Fun); se != nil {
		if sel := f.c.selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
			recv := methodRecv(sel.Obj().(*types2.Func))
			val := se.X
			if implicitAddr(f.c, se, recv) {
				val = &ast.UnaryExpr{OpPos: se.X.Pos(), Op: token.AND, X: se.X}
			}
			fn(se.X, val, recv)
		}
	} else {
		// A call through a func value: a method value holds its receiver.
		fn(call.Fun, call.Fun, nil)
	}
	sig, _ := f.c.typeOf(call.Fun).Underlying().(*types2.Signature)
	for i, a := range call.Args {
		var T types2.Type
		packed := sig != nil && sig.Variadic() && i >= sig.Params().Len()-1 && !call.Ellipsis.IsValid()
		if sig != nil && i < sig.Params().Len() && !packed {
			T = sig.Params().At(i).Type()
		}
		fn(a, a, T)
	}
}

// ----------------------------------------------------------------------------
// Storing views

// storeViews records that the views vs were stored into lhs, which an
// assignment has just given a new value.
func (f *fn) storeViews(lhs ast.Expr, vs []view) {
	if f.env == nil {
		return
	}
	if id, ok := ast.Unparen(lhs).(*ast.Ident); ok && id.Name == "_" {
		return
	}
	v, path, box, ok := f.placePath(lhs)
	if !ok || box || f.captured(v) || f.kept[v] {
		f.pin(vs, lhs.Pos(), "stored in "+exprText(lhs))
		return
	}
	s := f.env[v]
	var keep []view
	for _, w := range s.views {
		if len(w.at) >= len(path) && slices.Equal(w.at[:len(path)], path) && !slices.Contains(path, "[*]") {
			continue // overwritten
		}
		keep = append(keep, w)
	}
	for _, w := range vs {
		w.at = append(slices.Clone(path), w.at...)
		keep = append(keep, w)
	}
	s.views = sortViews(keep)
	f.setState(v, s)
}

// addViews records that the views vs may have been stored by call
// somewhere in the memory that its argument dst points to.
func (f *fn) addViews(call *ast.CallExpr, dst ast.Expr, direct bool, vs []view) {
	if len(vs) == 0 || f.env == nil {
		return
	}
	var place ast.Expr
	if direct && dst != nil {
		switch x := ast.Unparen(dst).(type) {
		case *ast.UnaryExpr:
			if x.Op == token.AND {
				place = x.X
			}
		case *ast.SliceExpr:
			if _, isArr := f.c.typeOf(x.X).Underlying().(*types2.Array); isArr {
				place = x.X
			}
		}
		if se := funSelector(call.Fun); place == nil && se != nil && se.X == dst {
			// The receiver of x.M(...), whose address the call takes.
			if sel := f.c.selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
				if fun, ok := sel.Obj().(*types2.Func); ok && implicitAddr(f.c, se, methodRecv(fun)) {
					place = se.X
				}
			}
		}
	}
	how := "stored by " + exprText(call.Fun) + " in memory the compiler does not follow"
	if dst != nil {
		how = "stored by " + exprText(call.Fun) + " in what " + exprText(dst) + " points to"
	}
	if place == nil {
		f.pin(vs, call.Pos(), how)
		return
	}
	v, _, box, ok := f.placePath(place)
	if !ok || box || f.captured(v) || f.kept[v] {
		f.pin(vs, call.Pos(), how)
		return
	}
	s := f.env[v]
	s.views = sortViews(append(slices.Clone(s.views), whole(vs)...))
	f.setState(v, s)
}

// pin records that the views vs went where the walk cannot follow them, at
// where, as how says ("stored in *q").
func (f *fn) pin(vs []view, where token.Pos, how string) {
	for _, w := range vs {
		s, ok := f.env[w.of]
		if ok && s.pin != nil || w.inHeap {
			continue
		}
		s.pin = &pinMark{pos: w.pos, text: w.text, where: where, how: how}
		f.setState(w.of, s)
	}
}

// An argView is the views a call argument holds that the callee may store
// into the memory of other arguments.
type argView struct {
	call  *ast.CallExpr
	key   ast.Expr
	views []view
}

// callStores follows the views a call's arguments hold into the memory of
// other arguments the callee stores them in. args were computed before the
// call was walked.
func (f *fn) callStores(args []argView) {
	for _, a := range args {
		af := f.c.calls.Arg(a.key)
		if af == nil {
			f.pin(a.views, a.call.Pos(), "passed to "+exprText(a.call.Fun)+", which may keep it")
			continue
		}
		for _, in := range af.Into {
			if in.Arg == a.key {
				continue // stored back into what it came from, which holds it already
			}
			f.addViews(a.call, in.Arg, in.Direct, a.views)
		}
	}
}

// argViews returns the views each argument of a call holds that the callee
// may store, before the call is walked.
func (f *fn) argViews(call *ast.CallExpr) []argView {
	if f.env == nil {
		return nil
	}
	tv := f.c.types[call.Fun]
	if tv.IsType() || tv.IsBuiltin() || f.c.scoped.Tasks[call] != nil || f.isImplicitDrop(call) {
		return nil
	}
	var out []argView
	f.eachArg(call, func(key, val ast.Expr, T types2.Type) {
		af := f.c.calls.Arg(key)
		if af == nil && key == call.Fun {
			return // a func value no method value reaches: it stores nothing
		}
		if af != nil && len(af.Into) == 0 {
			return
		}
		if vs := f.viewsTo(val, T); len(vs) > 0 {
			out = append(out, argView{call: call, key: key, views: vs})
		}
	})
	return out
}

// ----------------------------------------------------------------------------
// Making views stale, and using them

// invalidate records that the owner v lost its value at pos, as mv says:
// every view of it is stale from here on. If a view of it went where the
// walk cannot follow, that is the error.
func (f *fn) invalidate(v *types2.Var, mv move) {
	if f.env == nil || !isOwner(v.Type()) || !f.vars[v] {
		return
	}
	if p := f.env[v].pin; p != nil && f.report {
		f.c.report(f.c.pinnedDiag(v, p, mv))
	}
	for h, s := range f.env {
		var out []view // the views slice is shared with other paths' states
		for i, w := range s.views {
			if w.of == v && (w.stale == nil || w.maybe) {
				if out == nil {
					out = slices.Clone(s.views)
				}
				m := mv
				out[i].stale, out[i].maybe = &m, false
			}
		}
		if out != nil {
			s.views = out
			f.env[h] = s
		}
	}
}

// checkView reports a read of e, part of a local variable, where it may
// hold a stale view. Each place is checked once, where the walk meets the
// largest expression naming it, at the path that expression names.
func (f *fn) checkView(e ast.Expr) {
	if f.env == nil {
		return
	}
	root, path := f.viewPlace(e)
	if root == nil {
		return
	}
	if first, ok := f.checkedAt[root]; ok && first != e {
		return
	}
	f.checkedAt[root] = e
	if f.writing == e || !f.report {
		return
	}
	v := f.varOf(root)
	for _, w := range f.env[v].views {
		if w.stale != nil && overlap(w.at, path) {
			f.c.report(f.c.staleDiag(e, v, w))
			return
		}
	}
}

// viewPlace returns the variable identifier e is part of, by field
// selections and array indexing alone, and the path to the part.
func (f *fn) viewPlace(e ast.Expr) (*ast.Ident, Path) {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		if f.varOf(x) == nil || f.captured(f.varOf(x)) {
			return nil, nil
		}
		return x, nil
	case *ast.SelectorExpr:
		sel := f.c.selections[x]
		if sel == nil || sel.Kind() != types2.FieldVal || sel.Indirect() {
			return nil, nil
		}
		root, p := f.viewPlace(x.X)
		if root == nil {
			return nil, nil
		}
		for _, idx := range sel.Index() {
			p = append(p, "."+strconv.Itoa(idx))
		}
		return root, p
	case *ast.IndexExpr:
		if _, isArr := f.c.typeOf(x.X).Underlying().(*types2.Array); !isArr {
			return nil, nil
		}
		root, p := f.viewPlace(x.X)
		if root == nil {
			return nil, nil
		}
		step := "[*]"
		if tv := f.c.types[x.Index]; !tv.Value.IsNil() {
			step = "[" + tv.Value.ExactString() + "]"
		}
		return root, append(p, step)
	}
	return nil, nil
}

// litPins pins the owners of the function that a function literal makes a
// view of and may keep: the literal runs whenever it is called, so where
// it stores the view is beyond the walk.
func (f *fn) litPins(lit *ast.FuncLit) {
	if f.env == nil {
		return
	}
	called := map[*ast.SelectorExpr]bool{}
	taken := map[*ast.UnaryExpr]bool{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		var x, made ast.Expr
		switch n := n.(type) {
		case *ast.CallExpr:
			if amp := f.takeArg(n); amp != nil {
				taken[amp] = true
			}
			if se := funSelector(n.Fun); se != nil {
				called[se] = true
				if f.keepsReceiver(n, se) {
					x, made = se.X, se
				}
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND && !taken[n] && !f.transient(n) {
				x, made = n.X, n
			}
		case *ast.SliceExpr:
			if _, isArr := f.c.typeOf(n.X).Underlying().(*types2.Array); isArr && !f.transient(n) {
				x, made = n.X, n
			}
		case *ast.SelectorExpr:
			if sel := f.c.selections[n]; sel != nil && sel.Kind() == types2.MethodVal && !called[n] {
				if fun, ok := sel.Obj().(*types2.Func); ok && implicitAddr(f.c, n, methodRecv(fun)) {
					x, made = n.X, n
				}
			}
		}
		if x == nil {
			return true
		}
		if v, _, _, ok := f.placePath(x); ok {
			if o := f.ownerVar(v); o != nil {
				f.pin([]view{{of: o, pos: made.Pos(), text: exprText(made)}}, made.Pos(), "kept by the function literal at line "+strconv.Itoa(f.c.line(lit.Pos())))
			}
		}
		return true
	})
}

// isImplicitDrop reports whether call is x.Drop() on an owner whose type
// declares no Drop: the early drop of language spec section 18.6, which has
// no body and keeps nothing.
func (f *fn) isImplicitDrop(call *ast.CallExpr) bool {
	se := funSelector(call.Fun)
	if se == nil || se.Sel.Name != "Drop" || len(call.Args) != 0 {
		return false
	}
	sel := f.c.selections[se]
	if sel == nil || sel.Kind() != types2.MethodVal {
		return false
	}
	fun, _ := sel.Obj().(*types2.Func)
	return fun != nil && fun.Pkg() == nil && types2.BoxMethodOf(fun) == nil
}
