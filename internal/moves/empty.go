package moves

import (
	"go/constant"
	"slices"
	"strconv"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// What holds nothing (language spec section 18.6).
//
// An owner is dropped where its variable's scope ends, and a place's old
// value where the place is assigned. A variable that holds nothing there
// is not dropped at all: one whose value moved away on every path, and one
// that was never given a value (var x T, a named result) and was not used
// since, through which it could have been given one. The same goes for the
// old value of a field or element assigned for the first time in a
// variable declared without a value, or in the object of an owned box made
// by new(T) (zeroed memory), as long as nothing else used the variable in
// between. Lowering consults Info.Empty, Info.AtStmt, Info.EmptyResults and
// Info.Fresh; a place they do not mention is dropped, which for a place
// holding the zero value runs Drop on the zero value, which Drop accepts.
//
// The facts are kept only for the function's own variables that nothing
// else can change behind the walk: not those a function literal uses, whose
// address is kept (&x or x[:] of an array anywhere but as the argument of
// a call that the escape analysis says keeps no pointer to it, a method
// value, a pointer-receiver method call whose method keeps its receiver),
// or that a deferred call other than x.Drop() uses. An address that lasts
// only for a call is a use of the variable there, after which nothing is
// known to be zero in it until it is moved or given a new value.

// A zset says what is known to be zero in a variable's storage, or in the
// object an owned box holds: everything but the paths written. A path is a
// list of steps, a field index ".2" or an element "[3]", or "[*]" for an
// element whose index is not constant. A nil *zset knows nothing.
type zset struct {
	paths []Path // sorted by key
}

var emptyZ = &zset{}

// none reports whether nothing at all has been written: the storage holds
// nothing.
func (z *zset) none() bool { return z != nil && len(z.paths) == 0 }

func pathKey(p Path) string { return strings.Join(p, "") }

// overlap reports whether two paths may name overlapping storage.
func overlap(a, b Path) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] && a[i] != "[*]" && b[i] != "[*]" {
			return false
		}
	}
	return true
}

// zeroAt reports whether the storage at path p is known to be zero.
func (z *zset) zeroAt(p Path) bool {
	if z == nil {
		return false
	}
	for _, w := range z.paths {
		if overlap(w, p) {
			return false
		}
	}
	return true
}

// with returns z with path p written.
func (z *zset) with(p Path) *zset {
	if z == nil {
		return nil
	}
	return z.add([]Path{p})
}

func (z *zset) add(ps []Path) *zset {
	out := &zset{paths: slices.Clone(z.paths)}
	for _, p := range ps {
		k := pathKey(p)
		i, found := slices.BinarySearchFunc(out.paths, k, func(q Path, k string) int { return strings.Compare(pathKey(q), k) })
		if !found {
			out.paths = slices.Insert(out.paths, i, p)
		}
	}
	return out
}

// join merges what two paths know.
func (z *zset) join(o *zset) *zset {
	if z == nil || o == nil {
		return nil
	}
	if z.equal(o) {
		return z
	}
	return z.add(o.paths)
}

func (z *zset) equal(o *zset) bool {
	if z == nil || o == nil {
		return z == o
	}
	return slices.EqualFunc(z.paths, o.paths, func(a, b Path) bool { return pathKey(a) == pathKey(b) })
}

// ----------------------------------------------------------------------------
// Which variables the facts are kept for

// findNoFacts collects the variables of body for which nothing is known to
// be empty, because something other than the walk may change them.
// It also collects the variables whose own storage something other than
// the walk may read or write (f.kept), whose views the walk cannot follow
// (views.go).
func (f *fn) findNoFacts(body *ast.BlockStmt) {
	f.noFacts = map[*types2.Var]bool{}
	f.kept = map[*types2.Var]bool{}
	all := func(e ast.Node) {
		ast.Inspect(e, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if v, ok := f.c.objOf(id).(*types2.Var); ok && f.vars[v] {
					f.noFacts[v] = true
					f.kept[v] = true
				}
			}
			return true
		})
	}
	// mark marks the variables in the place x whose address is kept.
	mark := func(x ast.Expr) {
		ast.Inspect(x, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if v, ok := f.c.objOf(id).(*types2.Var); ok && f.vars[v] {
					f.noFacts[v] = true
				}
			}
			return true
		})
		if v, _, box, ok := f.placePath(x); ok && !box && f.vars[v] {
			f.kept[v] = true
		}
	}
	called := map[*ast.SelectorExpr]bool{}
	taken := map[*ast.UnaryExpr]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			all(n)
			return false
		case *ast.UnaryExpr:
			// take(&place) uses the address only while it empties the
			// place (takeArg). An address passed to a call that keeps no
			// pointer to it lasts only for the call (transient).
			if n.Op == token.AND && !taken[n] && !f.transient(n) {
				mark(n.X)
			}
		case *ast.SliceExpr:
			if _, ok := f.c.typeOf(n.X).Underlying().(*types2.Array); ok && !f.transient(n) {
				mark(n.X)
			}
		case *ast.CallExpr:
			if se := funSelector(n.Fun); se != nil {
				called[se] = true
				if f.keepsReceiver(n, se) {
					mark(se.X)
				}
			}
			if amp := f.takeArg(n); amp != nil {
				taken[amp] = true
			}
		case *ast.SelectorExpr:
			// A method value with a pointer receiver keeps the address.
			if sel := f.c.selections[n]; sel != nil && sel.Kind() == types2.MethodVal && !called[n] {
				if fun, ok := sel.Obj().(*types2.Func); ok && pointerElem(methodRecv(fun)) != nil {
					mark(n.X)
				}
			}
		case *ast.DeferStmt:
			if v, _ := f.deferredDrop(n.Call); v == nil {
				all(n.Call)
			}
		}
		return true
	})
}

// tracked reports whether facts are kept for v.
func (f *fn) tracked(v *types2.Var) bool {
	return v != nil && f.vars[v] && !f.noFacts[v] && isOwner(v.Type())
}

// hasDrop reports whether T has a Drop method of its own, which runs on
// the whole value however little of it holds anything.
func hasDrop(T types2.Type) bool {
	if _, ok := types2.Unalias(T).(*types2.Named); !ok {
		return false
	}
	sel := types2.NewMethodSet(types2.NewPointer(T)).Lookup(nil, "Drop")
	return sel != nil && len(sel.Index()) == 1
}

// ----------------------------------------------------------------------------
// Recording

// known returns what is known of the tracked variables here: the parts that
// may hold values, if not all of them. A variable that a deferred x.Drop()
// drops on every path holds nothing once the deferred calls have run, which
// is before the function's own drops.
func (f *fn) known() Known {
	var out Known
	for v, s := range f.env {
		if !f.tracked(v) {
			continue
		}
		var h Held
		switch {
		case s.dd:
		case s.z != nil:
			h.Parts = s.z.paths
		case s.bz != nil:
			h = Held{Box: true, Parts: s.bz.paths}
		default:
			continue
		}
		if out == nil {
			out = Known{}
		}
		out[v] = h
	}
	return out
}

// recordExit records what holds nothing where control leaves through n.
func (f *fn) recordExit(n ast.Node) {
	if !f.report || f.env == nil {
		return
	}
	if k := f.known(); k != nil {
		f.c.info.Empty[n] = k
	}
}

// recordStmt records what holds nothing when st starts and is not
// mentioned in it, for a call in st that leaves the function for Goexit.
func (f *fn) recordStmt(st ast.Stmt) {
	if !f.report || f.env == nil {
		return
	}
	k := f.known()
	if k == nil {
		return
	}
	ast.Inspect(st, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := f.c.objOf(id).(*types2.Var); ok {
				delete(k, v)
			}
		}
		return len(k) > 0
	})
	if len(k) > 0 {
		f.c.info.AtStmt[st] = k
	}
}

// recordResults records the named results that hold nothing before the
// return statement st replaces them.
func (f *fn) recordResults(st *ast.ReturnStmt) {
	if !f.report || f.env == nil {
		return
	}
	var out VarSet
	for _, v := range f.results {
		if f.tracked(v) && f.env[v].z.none() {
			if out == nil {
				out = VarSet{}
			}
			out[v] = true
		}
	}
	if out != nil {
		f.c.info.EmptyResults[st] = out
	}
}

// declaredEmpty records that v was declared without a value.
func (f *fn) declaredEmpty(v *types2.Var) {
	if f.tracked(v) && f.env != nil {
		s := f.env[v]
		s.z = emptyZ
		f.setState(v, s)
	}
}

// valueFacts records what is zero in the value val just stored in v: the
// object of an owned box made by new(T), or the fields or elements a
// composite literal leaves out. A type with a Drop method of its own is
// left alone: the program made the value, and its Drop runs.
func (f *fn) valueFacts(v *types2.Var, val ast.Expr) {
	if !f.tracked(v) || f.env == nil {
		return
	}
	s := f.env[v]
	switch x := ast.Unparen(val).(type) {
	case *ast.CallExpr:
		if !isNewOfType(f.c, x) || hasDrop(f.c.typeOf(x.Args[0])) {
			return
		}
		s.bz = emptyZ
	case *ast.CompositeLit:
		z := litParts(f.c, x)
		if z == nil {
			return
		}
		s.z = z
	default:
		return
	}
	f.setState(v, s)
}

// isNewOfType reports whether e is new(T) with a type operand.
func isNewOfType(c *checker, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !c.types[call.Fun].IsBuiltin() {
		return false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	return ok && id.Name == "new" && c.types[call.Args[0]].IsType()
}

// litParts returns the parts of a struct or array composite literal that
// it gives values, or nil.
func litParts(c *checker, lit *ast.CompositeLit) *zset {
	T := c.typeOf(lit)
	if T == nil || hasDrop(T) {
		return nil
	}
	var parts []Path
	switch u := T.Underlying().(type) {
	case *types2.Struct:
		for i, el := range lit.Elts {
			idx := i
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				id, ok := kv.Key.(*ast.Ident)
				if !ok {
					return nil
				}
				idx = -1
				for j := 0; j < u.NumFields(); j++ {
					if u.Field(j).Name() == id.Name {
						idx = j
					}
				}
				if idx < 0 {
					return nil
				}
			}
			parts = append(parts, Path{"." + strconv.Itoa(idx)})
		}
	case *types2.Array:
		var k int64
		for _, el := range lit.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				tv := c.types[kv.Key]
				if tv.Value == nil {
					return nil
				}
				i, exact := constant.Int64Val(constant.ToInt(tv.Value))
				if !exact {
					return nil
				}
				k = i
			}
			parts = append(parts, Path{"[" + strconv.FormatInt(k, 10) + "]"})
			k++
		}
	default:
		return nil
	}
	return emptyZ.add(parts)
}

// takeArg returns the &place of a call take(&place), or nil.
func (f *fn) takeArg(call *ast.CallExpr) *ast.UnaryExpr {
	if len(call.Args) != 1 || !f.c.types[call.Fun].IsBuiltin() {
		return nil
	}
	if id, ok := ast.Unparen(call.Fun).(*ast.Ident); !ok || id.Name != "take" {
		return nil
	}
	amp, ok := ast.Unparen(call.Args[0]).(*ast.UnaryExpr)
	if !ok || amp.Op != token.AND {
		return nil
	}
	return amp
}

// take walks take(&place): the place holds the zero value afterwards.
func (f *fn) take(amp *ast.UnaryExpr) {
	v, path, box, ok := f.placePath(amp.X)
	if !ok || !f.tracked(v) || f.env == nil {
		f.expr(amp)
		return
	}
	before := f.env[v]
	f.expr(amp)
	if f.env == nil || mentions(f.c, amp.X, v) > 1 {
		return
	}
	s := f.env[v]
	if box {
		s.bz = before.bz.without(path)
	} else {
		s.z = before.z.without(path)
	}
	f.setState(v, s)
}

// addrOf walks &x. Through the address, the program may write the part of
// a variable that x names, and only that part: what is known to be zero in
// the rest stays known.
func (f *fn) addrOf(amp *ast.UnaryExpr) {
	v, path, box, ok := f.placePath(amp.X)
	if !ok || !f.tracked(v) || f.env == nil {
		f.expr(amp.X)
		return
	}
	before := f.env[v]
	f.expr(amp.X)
	if f.env == nil || mentions(f.c, amp.X, v) > 1 {
		return
	}
	s := f.env[v]
	if s.st != live {
		return // a use after a move, reported
	}
	if box {
		s.z, s.bz = before.z, before.bz.with(path)
	} else {
		s.z = before.z.with(path)
	}
	f.setState(v, s)
}

// without returns z with the parts within path p emptied.
func (z *zset) without(p Path) *zset {
	if z == nil || slices.Contains(p, "[*]") {
		return z
	}
	out := &zset{}
	for _, w := range z.paths {
		if len(w) >= len(p) && slices.Equal(w[:len(p)], p) {
			continue
		}
		out.paths = append(out.paths, w)
	}
	return out
}

// ----------------------------------------------------------------------------
// Assignments

// placePath returns the variable whose storage lhs names part of, the path
// to that part, and whether the path is in the object of the owned box the
// variable holds rather than in the variable. ok is false for any other
// place (through a pointer, a slice or a container).
func (f *fn) placePath(lhs ast.Expr) (v *types2.Var, path Path, box, ok bool) {
	switch x := ast.Unparen(lhs).(type) {
	case *ast.Ident:
		v = f.varOf(x)
		return v, nil, false, v != nil
	case *ast.SelectorExpr:
		sel := f.c.selections[x]
		if sel == nil || sel.Kind() != types2.FieldVal {
			return nil, nil, false, false
		}
		steps := make(Path, len(sel.Index()))
		for i, idx := range sel.Index() {
			steps[i] = "." + strconv.Itoa(idx)
		}
		if sel.Indirect() {
			// Only the first step from a variable holding an owned box.
			id, isID := ast.Unparen(x.X).(*ast.Ident)
			if !isID || len(steps) != 1 || !isOwnedBox(f.c.typeOf(x.X)) {
				return nil, nil, false, false
			}
			v = f.varOf(id)
			return v, steps, true, v != nil
		}
		v, path, box, ok = f.placePath(x.X)
		return v, append(slices.Clone(path), steps...), box, ok
	case *ast.IndexExpr:
		if _, isArr := f.c.typeOf(x.X).Underlying().(*types2.Array); !isArr {
			return nil, nil, false, false
		}
		step := "[*]"
		if tv := f.c.types[x.Index]; tv.Value != nil {
			if i, exact := constant.Int64Val(constant.ToInt(tv.Value)); exact {
				step = "[" + strconv.FormatInt(i, 10) + "]"
			}
		}
		v, path, box, ok = f.placePath(x.X)
		return v, append(slices.Clone(path), step), box, ok
	}
	return nil, nil, false, false
}

// isOwnedBox reports whether T is owned[E].
func isOwnedBox(T types2.Type) bool {
	if T == nil {
		return false
	}
	w, ok := T.Underlying().(*types2.Wrapper)
	return ok && w.Kind() == types2.WrapOwned
}

// assignTarget walks the target lhs of an assignment of an owner after its
// value is evaluated: it records whether the place's old value is known to
// be nothing (Info.Fresh), walks lhs, and records the part written.
func (f *fn) assignTarget(lhs ast.Expr, tok token.Token) {
	v, path, box, ok := f.placePath(lhs)
	if !ok || !f.tracked(v) || f.env == nil || !isOwner(f.c.typeOf(lhs)) {
		f.assignLHS(lhs, tok)
		return
	}
	s := f.env[v]
	z := s.z
	if box {
		z = s.bz
	}
	if f.report && z.zeroAt(path) {
		f.c.info.Fresh[lhs] = true
	}
	f.assignLHS(lhs, tok)
	if len(path) == 0 || z == nil || f.env == nil || mentions(f.c, lhs, v) > 1 {
		return
	}
	// The rest of the variable, or of its box's object, is still zero.
	s = f.env[v]
	if box {
		s.bz = z.with(path)
	} else {
		s.z = z.with(path)
	}
	f.setState(v, s)
}

// mentions counts the identifiers in e that denote v.
func mentions(c *checker, e ast.Expr, v *types2.Var) int {
	n := 0
	ast.Inspect(e, func(x ast.Node) bool {
		if id, ok := x.(*ast.Ident); ok && c.objOf(id) == v {
			n++
		}
		return true
	})
	return n
}

// ----------------------------------------------------------------------------
// Reads and writes of parts

// readOnly walks e if it reads a field or array element of a local
// variable (or of the object of the owned box it holds) as a value that is
// not an owner: a copy, which cannot give the variable a value, so what is
// known to be zero in it stays known. It reports false, having walked
// nothing, for any other expression. Writes through such an expression (an
// assignment, a method with a pointer receiver) forget what is known with
// forget.
func (f *fn) readOnly(e ast.Expr) bool {
	if isOwner(f.c.typeOf(e)) {
		return false
	}
	return f.readChain(e)
}

func (f *fn) readChain(e ast.Expr) bool {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		v := f.varOf(x)
		if v == nil {
			return false
		}
		f.useKeeping(x, v, true)
		return true
	case *ast.SelectorExpr:
		sel := f.c.selections[x]
		if sel == nil || sel.Kind() != types2.FieldVal {
			return false
		}
		if sel.Indirect() {
			if _, isID := ast.Unparen(x.X).(*ast.Ident); !isID || len(sel.Index()) != 1 || !isOwnedBox(f.c.typeOf(x.X)) {
				return false
			}
		}
		return f.readChain(x.X)
	case *ast.IndexExpr:
		if _, isArr := f.c.typeOf(x.X).Underlying().(*types2.Array); !isArr {
			return false
		}
		if !f.readChain(x.X) {
			return false
		}
		f.expr(x.Index)
		return true
	}
	return false
}

// forget forgets what is known to be zero in the variable whose storage, or
// whose owned box's object, e is part of: e is written.
func (f *fn) forget(e ast.Expr) {
	for f.env != nil {
		switch x := ast.Unparen(e).(type) {
		case *ast.Ident:
			if v := f.varOf(x); v != nil {
				if s, ok := f.env[v]; ok {
					s.z, s.bz = nil, nil
					f.setState(v, s)
				}
			}
			return
		case *ast.SelectorExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		default:
			return
		}
	}
}

// JoinKnown returns what is known where two paths that know a and b meet.
func JoinKnown(a, b Known) Known {
	var out Known
	for v, ha := range a {
		hb, ok := b[v]
		if !ok || ha.Box != hb.Box {
			continue
		}
		if out == nil {
			out = Known{}
		}
		out[v] = Held{Box: ha.Box, Parts: emptyZ.add(ha.Parts).add(hb.Parts).paths}
	}
	return out
}

// ----------------------------------------------------------------------------
// Views that last only for a call

// transient reports whether the view e, an argument of a call, lasts only
// for the call: the escape analysis says the callee keeps no pointer to
// what e points to, or the call is len, cap, copy or clear.
func (f *fn) transient(e ast.Expr) bool {
	key := f.argKey[e]
	if key == nil {
		return false
	}
	if key == builtinUse {
		return true
	}
	af := f.c.calls.Arg(key)
	return af != nil && !af.Self
}

// keepsReceiver reports whether the method call se(...) takes the address of
// its receiver se.X, or of the object of the owned box se.X, and may keep
// it past the call. A scoped task's call (s.Task) is checked by the rules
// for scoped tasks (section 19.4).
func (f *fn) keepsReceiver(call *ast.CallExpr, se *ast.SelectorExpr) bool {
	sel := f.c.selections[se]
	if sel == nil || sel.Kind() != types2.MethodVal || f.c.scoped.Tasks[call] != nil {
		return false
	}
	fun, _ := sel.Obj().(*types2.Func)
	if fun == nil || fun.Pkg() == nil || fun.Name() == "Drop" || types2.BoxMethodOf(fun) != nil {
		return false // built-in methods, and Drop, keep nothing
	}
	recv := methodRecv(fun)
	ownedRecv := isOwnedBox(f.c.typeOf(se.X)) && pointerElem(recv) != nil
	if !implicitAddr(f.c, se, recv) && !ownedRecv {
		return false
	}
	af := f.c.calls.Arg(se.X)
	return af == nil || af.Self
}

// builtinUse stands for the key of an argument of a built-in function
// that keeps nothing (keysOf).
var builtinUse ast.Expr = &ast.BadExpr{}

// keysOf maps the arguments and receivers of the calls in body, with their
// parentheses removed, to the expressions the escape analysis records facts
// under, or to builtinUse for the arguments of len, cap, copy and clear.
func keysOf(c *checker, body ast.Node) map[ast.Expr]ast.Expr {
	out := map[ast.Expr]ast.Expr{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok && c.types[call.Fun].IsBuiltin() {
			switch id.Name {
			case "len", "cap", "copy", "clear":
				for _, a := range call.Args {
					out[ast.Unparen(a)] = builtinUse
				}
			}
			return true
		}
		for _, a := range call.Args {
			out[ast.Unparen(a)] = a
		}
		if se := funSelector(call.Fun); se != nil {
			out[ast.Unparen(se.X)] = se.X
		}
		return true
	})
	return out
}

// implicitAddr reports whether the method call or value se takes the
// address of its receiver expression.
func implicitAddr(c *checker, se *ast.SelectorExpr, recv types2.Type) bool {
	xt := c.typeOf(se.X)
	return recv != nil && pointerElem(recv) != nil && pointerElem(xt) == nil && !types2.IsInterface(xt) && !isOwnedBox(xt)
}

// funSelector returns the selector x.M that the function of a call names,
// with any type arguments (x.M[T]) removed, or nil.
func funSelector(fun ast.Expr) *ast.SelectorExpr {
	switch ix := ast.Unparen(fun).(type) {
	case *ast.IndexExpr:
		fun = ix.X
	case *ast.IndexListExpr:
		fun = ix.X
	}
	se, _ := ast.Unparen(fun).(*ast.SelectorExpr)
	return se
}
