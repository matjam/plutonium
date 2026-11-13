// Translated by go2pu from internal/moves/views.go at the go-cutover tag; edited by hand since.

package moves

import (
	"compiler/compat"
	"compiler/internal/constant"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/slices"
	"pluto/strconv"
	"pluto/strings"
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
// escape analysis says derive from an argument or that may hold memory an
// owner passed by address holds (loadsOwned), and stores a callee makes
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
	// part is the part of the owner the view looks at or into (h.l for
	// h.l.head), nil for all of it: take(&h.l) makes only the views of
	// that part stale.
	part Path
	// itself: the view is &v of the whole variable, which a call that
	// frees through a pointer to v does not end (section 18.3).
	itself bool
	// obj: the view is a pointer to the object of a box (boxView), which
	// holds what the box owns as an owner's storage does.
	obj bool
	// lent: the view is an ordinary interface holding the word of a map or
	// channel variable (refView). Only such a view of a map or channel
	// ends when the variable is given a new value; &m stays valid.
	lent bool
	// loaded: the value holding the view was loaded through a view (an
	// element of a slice, what a pointer points to), so it may point to
	// what the owner holds but is not the view itself. Storing it does
	// not keep the view (heapPin's stored).
	loaded bool
	// lowns: the value loaded last on the way to this view frees memory
	// when it is dropped (an owner, a box, a map), so what was loaded may
	// look at memory the owner frees through it, and storing it keeps a
	// view as storing the view itself does.
	lowns bool
	// lit: the view is a function literal's hold on a variable it uses,
	// which it reads by reference when it is called (section 11.4): a
	// view of the variable itself, which a move, drop or the end of its
	// scope ends, and which marks what holds it as holding the literal
	// (funcvals.p).
	lit *ast.FuncLit
	// litSets: the literal only gives the variable new values (v = x), so
	// it does not read it and no event of the variable ends the hold.
	litSets bool
	// into: the view points into the object of an owner or box that was
	// loaded through a view (&s[0].x for a slice of boxes). A new value
	// given to the place holding that owner, or clear of the slice,
	// drops it and frees what the view points into (section 18.3).
	into bool
	// slot, slotAt: for a view into, the element of a slice it points
	// into, when that is known: element slotAt ("[2]") of the slice in
	// the local slot, which no statement but its declaration gives a
	// value (intoSlot). A new value given to another element of that
	// slice does not drop the object the view points into.
	slot   *types2.Var
	slotAt string
	// addr: the view is the address of the owner's own storage at part
	// (&b.v, &b.arr[1]), or a copy of that address. A call that frees
	// through a pointer to that part frees what the part holds, not the
	// part's storage, so it does not end the view (section 18.3). What
	// is loaded through such a view, or a call returns from it, is not.
	addr bool
}

// A pinMark records that a view of an owner went where the walk cannot
// follow it.
type pinMark struct {
	pos   token.Pos // where the view was made
	text  string    // the expression that made it
	where token.Pos // where it went
	how   string    // how: "stored in *q"
	// stored is set when the view itself went there, not only memory a
	// callee loads through it. Only such a view makes a later free an
	// error (section 18.3).
	stored bool
	lent   bool // the view is lent (view.lent)
	// inOwn says the view went into memory the same owner holds on the
	// heap (n.next = a.New[node]() for n from a.New): dropping the owner
	// frees the view with that memory, and a transfer moves it along.
	inOwn bool
}

// pinRank orders pins by what they forbid, for the join of two paths.
func pinRank(p *pinMark) int {
	switch {
	case p == nil:
		return 0
	case !p.stored:
		return 1
	case p.inOwn:
		return 2
	}
	return 3
}

func (w view) key() string {
	k := compat.Concat(strconv.Itoa(int(w.pos)), "|", w.of.Name(), strconv.Itoa(int(w.of.Pos())), "|", pathKey(w.at))
	if w.loaded {
		k = compat.Concat(k, "|loaded")
	}
	if w.lowns {
		k = compat.Concat(k, "|lowns")
	}
	if w.into {
		k = compat.Concat(k, "|into")
	}
	if w.slot != nil {
		k = compat.Concat(k, "|", w.slot.Name(), strconv.Itoa(int(w.slot.Pos())), w.slotAt)
	}
	if w.addr {
		k = compat.Concat(k, "|addr")
	}
	if w.lit != nil {
		k = compat.Concat(k, "|lit")
	}
	return k
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
	out := compat.MakeCap[view](0, len(byKey))
	for _, w := range byKey {
		out = compat.Append(out, w)
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
			w.at = compat.Clone(w.at[len(p):])
		} else {
			w.at = nil
		}
		out = compat.Append(out, w)
	}
	return out
}

// whole returns vs with their paths forgotten: they may be anywhere in the
// value.
func whole(vs []view) []view {
	out := compat.Make[view](len(vs))
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
	if st, isStar := ast.Unparen(x).(*ast.StarExpr); isStar {
		if vs := f.boxView(st.X, made); vs != nil {
			return vs
		}
	}
	v, path, box, ok := f.placePath(x)
	if !ok || v == nil {
		out := f.viewsOf(baseOf(x))
		into, keep := f.intoLoaded(x), f.storageFrom(x, baseOf(x))
		var steps Path
		if keep {
			// The address of a part of what the pointer points to: the
			// same storage, at that part.
			_, steps, _ = f.throughPlace(x)
		}
		if into || !keep || len(steps) > 0 {
			out = compat.Clone(out)
			var slot *types2.Var
			var slotAt string
			if into {
				slot, slotAt = f.intoSlot(x)
			}
			for i := range out {
				if into && !out[i].into {
					out[i].slot, out[i].slotAt = slot, slotAt
				}
				out[i].into = out[i].into || into
				out[i].addr = out[i].addr && keep
				if out[i].addr && len(steps) > 0 {
					out[i].part = Path(compat.Append(compat.Clone(out[i].part), steps...))
					out[i].itself = false
				}
			}
		}
		if r, part := f.countedPath(x); r != nil {
			out = compat.Append(out, view{of: r, pos: made.Pos(), text: exprText(made), inHeap: true, part: part})
		}
		out = compat.Append(out, f.boxFieldView(x, made)...)
		// Part of a temporary owner, dropped at the end of its statement
		// (temps.p).
		return compat.Append(out, f.tempView(x, made)...)
	}
	var out []view
	if box {
		path = nil
	}
	out = compat.Append(out, whole(f.held(v, path))...)
	if o := f.ownerVar(v); o != nil {
		out = compat.Append(out, view{of: o, pos: made.Pos(), text: exprText(made), inHeap: box, part: path, itself: !box && len(path) == 0, addr: !box})
	} else if o := f.scopeVar(v); o != nil {
		out = compat.Append(out, view{of: o, pos: made.Pos(), text: exprText(made), part: path, addr: true})
	}
	return out
}

// boxFieldView returns, for a place x in the object of an owned box that
// is a part of a local owner (n.c.x, with n.c an owned[C] field of n), a
// view of what the owner holds at the box: a new value of the box, or a
// move or free of the owner, drops that object (section 18.3). The view's
// part goes on past the box ("*") to the place in its object, so a call
// that frees through another field of the object (n.c.v.Push(1)) frees
// what that field holds and leaves the view of n.c.x, as for the fields of
// the owner's own storage.
func (f *fn) boxFieldView(x ast.Expr, made ast.Expr) []view {
	e := x
	var inner Path // the steps from the box's object to x, outermost last
	exact := true  // inner names the place: no step it cannot name
	for {
		switch s := ast.Unparen(e).(type) {
		case *ast.SelectorExpr:
			sel := f.c.selections[s]
			if sel == nil || sel.Kind() != types2.FieldVal {
				return nil
			}
			if len(sel.Index()) != 1 {
				exact = false
			}
			inner = compat.Append(inner, compat.Concat(".", strconv.Itoa(sel.Index()[len(sel.Index())-1])))
			if !sel.Indirect() {
				e = s.X
				continue
			}
			if !isOwnedBox(f.c.typeOf(s.X)) {
				return nil
			}
			v, path, box, ok := f.placePath(s.X)
			if !ok || len(path) == 0 {
				return nil
			}
			if box {
				path = nil
			}
			o := f.ownerVar(v)
			if o == nil {
				return nil
			}
			if exact && path != nil {
				part := Path(compat.Append(compat.Clone(path), "*"))
				for i := len(inner) - 1; i >= 0; i-- {
					part = Path(compat.Append(part, inner[i]))
				}
				path = part
			}
			return compat.Clone([]view{{of: o, pos: made.Pos(), text: exprText(made), inHeap: true, part: path}})
		case *ast.IndexExpr:
			if _, isArr := f.c.typeOf(s.X).Underlying().(*types2.Array); !isArr {
				return nil
			}
			step := "[*]"
			if tv := f.c.types[s.Index]; !tv.Value.IsNil() {
				if i, ok := constant.Int64Val(constant.ToInt(tv.Value)); ok {
					step = compat.Concat("[", strconv.FormatInt(i, 10), "]")
				}
			}
			inner = compat.Append(inner, step)
			e = s.X
		default:
			return nil
		}
	}
}

// intoLoaded reports whether the place x is in the object of an owner or
// box loaded through a view: a part of x, other than x itself, is an
// element of a slice or what a pointer points to, of a type that frees
// what it holds, and is not in a local's own storage (s[0].x, p.c.x).
func (f *fn) intoLoaded(x ast.Expr) bool {
	e := ast.Unparen(x)
	for {
		var next ast.Expr
		switch y := e.(type) {
		case *ast.SelectorExpr:
			next = y.X
		case *ast.IndexExpr:
			next = y.X
		case *ast.StarExpr:
			next = y.X
		default:
			return false
		}
		e = ast.Unparen(next)
		if e != ast.Unparen(x) && freesHeld(f.c.typeOf(e)) && f.loadedPlace(e) {
			if _, _, _, ok := f.placePath(e); !ok {
				return true
			}
		}
	}
}

// storageFrom reports whether the place x is in the storage that the
// pointer base points to: reached from *base by fields and array
// elements alone (p.n, p.arr[1], (*p).in.n), so its address is an
// address in the same storage.
func (f *fn) storageFrom(x, base ast.Expr) bool {
	base = ast.Unparen(base)
	e := ast.Unparen(x)
	for {
		switch y := e.(type) {
		case *ast.SelectorExpr:
			sel := f.c.selections[y]
			if sel == nil || sel.Kind() != types2.FieldVal {
				return false
			}
			if sel.Indirect() {
				return ast.Unparen(y.X) == base && len(sel.Index()) == 1
			}
			e = ast.Unparen(y.X)
		case *ast.IndexExpr:
			T := f.c.typeOf(y.X)
			if T == nil {
				return false
			}
			switch T.Underlying().(type) {
			case *types2.Array:
				e = ast.Unparen(y.X)
			case *types2.Pointer:
				return ast.Unparen(y.X) == base
			default:
				return false
			}
		case *ast.StarExpr:
			return ast.Unparen(y.X) == base
		default:
			return e == base
		}
	}
}

// loadedPlace reports whether the place x is reached through a view: an
// element of a slice, what a pointer points to, or a field through one.
func (f *fn) loadedPlace(x ast.Expr) bool {
	switch y := ast.Unparen(x).(type) {
	case *ast.StarExpr:
		return true
	case *ast.IndexExpr:
		T := f.c.typeOf(y.X)
		if T == nil {
			return false
		}
		switch T.Underlying().(type) {
		case *types2.Slice, *types2.Pointer:
			return true
		case *types2.Array:
			return f.loadedPlace(y.X)
		}
		return false
	case *ast.SelectorExpr:
		sel := f.c.selections[y]
		if sel == nil || sel.Kind() != types2.FieldVal {
			return false
		}
		return sel.Indirect() || f.loadedPlace(y.X)
	}
	return false
}

// dropLoaded records that the owner or box held at a place reached
// through base (an element of the slice base, what the pointer base
// points to) is dropped, by a new value or by clear: the views into its
// object, which point into memory an owner holds through base's views,
// are stale (section 18.3).
//
// at is the place given a new value, nil for clear: when it is a known
// element of a slice (intoSlot), the views into the objects of the other
// elements of that slice stay valid.
func (f *fn) dropLoaded(base, at ast.Expr, mv move) {
	if f.env == nil || base == nil {
		return
	}
	var slot *types2.Var
	var slotAt string
	if at != nil {
		slot, slotAt = f.intoSlot(at)
	}
	f.dropThrough(f.viewsOf(base), slot, slotAt, mv)
}

// dropThrough is dropLoaded for a base whose views are vs: the views into
// the objects reached through them are stale, but for those into another
// element of the slice in slot than slotAt.
func (f *fn) dropThrough(vs []view, slot *types2.Var, slotAt string, mv move) {
	for _, w := range vs {
		if w.of == nil || w.lit != nil || w.lent || f.captured(w.of) {
			continue
		}
		part := w.part
		f.staleWhere(w.of, mv, func(u view) bool {
			if slot != nil && u.slot == slot && u.slotAt != slotAt {
				return false // another element of the same slice
			}
			return u.into && overlap(u.part, part)
		})
	}
}

// intoSlot returns, for a place x in an element of a slice or in the
// objects that element owns (s[2], s[2].f, s[2].b.x with b an owned box),
// the local variable holding the slice and the element's index, when the
// index is a constant and the variable keeps the value its declaration
// gives it (stableLocal). The objects two elements own are disjoint, so
// what one element drops is not in the other's. Otherwise it returns nil.
func (f *fn) intoSlot(x ast.Expr) (*types2.Var, string) {
	e := ast.Unparen(x)
	for {
		switch y := e.(type) {
		case *ast.SelectorExpr:
			sel := f.c.selections[y]
			if sel == nil || sel.Kind() != types2.FieldVal {
				return nil, ""
			}
			if sel.Indirect() && (len(sel.Index()) != 1 || !isOwnedBox(f.c.typeOf(y.X))) {
				return nil, ""
			}
			e = ast.Unparen(y.X)
		case *ast.StarExpr:
			if !isOwnedBox(f.c.typeOf(y.X)) {
				return nil, ""
			}
			e = ast.Unparen(y.X)
		case *ast.IndexExpr:
			T := f.c.typeOf(y.X)
			if T == nil {
				return nil, ""
			}
			switch T.Underlying().(type) {
			case *types2.Array:
				e = ast.Unparen(y.X)
				continue
			case *types2.Slice:
			default:
				return nil, ""
			}
			id, ok := ast.Unparen(y.X).(*ast.Ident)
			if !ok {
				return nil, ""
			}
			v, ok := f.c.objOf(id).(*types2.Var)
			if !ok || v.IsField() || isGlobal(v) || !f.vars[v] || !f.stableLocal(v) {
				return nil, ""
			}
			at := f.c.indexStep(y)
			if at == "[*]" {
				return nil, ""
			}
			return v, at
		default:
			return nil, ""
		}
	}
}

// stableLocal reports whether the local variable v keeps the value its
// declaration gives it while the function body runs: the body has no
// goto, v is not declared inside a loop of the body, and nothing else
// gives it a value, takes its address, calls a pointer method on it or
// uses it in a function literal.
func (f *fn) stableLocal(v *types2.Var) bool {
	if r, ok := f.stable[v]; ok {
		return r
	}
	ok := true
	decls := 0
	// The function literals met so far: the walk is in order, so a node
	// inside one comes after it.
	var lits []*ast.FuncLit
	inLit := func(pos token.Pos) bool {
		for _, l := range lits {
			if l.Pos() <= pos && pos < l.End() {
				return true
			}
		}
		return false
	}
	ast.Inspect(f.topBody, func(m ast.Node) bool {
		if !ok || m == nil {
			return false
		}
		switch x := m.(type) {
		case *ast.FuncLit:
			lits = compat.Append(lits, x)
		case *ast.BranchStmt:
			if x.Tok == token.GOTO {
				ok = false
			}
		case *ast.ForStmt:
			if v.Pos() >= x.Pos() && v.Pos() < x.End() {
				ok = false
			}
		case *ast.Ident:
			if f.c.defs[x] == v {
				decls++
			} else if f.c.uses[x] == v && inLit(x.Pos()) {
				ok = false
			}
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				if f.namesVar(l, v) && f.c.defs[ast.Unparen(l).(*ast.Ident)] != v {
					ok = false
				}
			}
		case *ast.IncDecStmt:
			if f.namesVar(x.X, v) {
				ok = false
			}
		case *ast.RangeStmt:
			if v.Pos() >= x.Pos() && v.Pos() < x.End() {
				ok = false
			}
			if x.Tok != token.DEFINE && (f.namesVar(x.Key, v) || f.namesVar(x.Value, v)) {
				ok = false
			}
		case *ast.UnaryExpr:
			if x.Op == token.AND && f.namesVar(x.X, v) {
				ok = false
			}
		case *ast.SelectorExpr:
			if sel := f.c.selections[x]; sel != nil && sel.Kind() != types2.FieldVal && f.namesVar(x.X, v) {
				if sig, isSig := sel.Obj().Type().(*types2.Signature); !isSig || sig.Recv() == nil {
					ok = false
				} else if _, ptr := sig.Recv().Type().Underlying().(*types2.Pointer); ptr {
					ok = false
				}
			}
		}
		return ok
	})
	ok = ok && decls <= 1
	if f.stable == nil {
		f.stable = map[*types2.Var]bool{}
	}
	f.stable[v] = ok
	return ok
}

// namesVar reports whether e, without parentheses, is an identifier
// naming v.
func (f *fn) namesVar(e ast.Expr, v *types2.Var) bool {
	if e == nil {
		return false
	}
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && (f.c.uses[id] == v || f.c.defs[id] == v)
}

// loadBase returns the expression through which the place x is loaded:
// the slice of an element, the pointer of what it points to, or nil
// when x is in a local's own storage.
func (f *fn) loadBase(x ast.Expr) ast.Expr {
	switch y := ast.Unparen(x).(type) {
	case *ast.StarExpr:
		return y.X
	case *ast.IndexExpr:
		T := f.c.typeOf(y.X)
		if T == nil {
			return nil
		}
		switch T.Underlying().(type) {
		case *types2.Slice, *types2.Pointer:
			return y.X
		case *types2.Array:
			return f.loadBase(y.X)
		}
	case *ast.SelectorExpr:
		sel := f.c.selections[y]
		if sel == nil || sel.Kind() != types2.FieldVal {
			return nil
		}
		if sel.Indirect() {
			return y.X
		}
		return f.loadBase(y.X)
	}
	return nil
}

// boxView returns the view that a pointer to the object of the box at the
// place x makes, with the views that object holds: &*b, b passed where a
// *T is wanted, or a method with a pointer receiver called on b. It looks
// at what the box holds on the heap, so the local owner x is part of
// bounds it, or x itself for a counted box (sections 7.18, 7.19, 18.3).
func (f *fn) boxView(x ast.Expr, made ast.Expr) []view {
	T := f.c.typeOf(x)
	if !isOwnedBox(T) && !isCountedBox(T) {
		return nil
	}
	v, path, box, ok := f.placePath(x)
	if !ok || box || v == nil || !f.vars[v] || !bounds(v.Type()) {
		return nil
	}
	out := whole(f.held(v, path))
	return compat.Append(out, view{of: v, pos: made.Pos(), text: exprText(made), inHeap: true, part: path, obj: true})
}

// scopeVar returns v if it is a local variable of this function declared
// in an inner scope whose type has drop glue but is neither an owner nor a
// counted reference: a view of it is bounded by its scope alone (section
// 18.3, A local's scope). Owners and counted references are followed
// through every event that ends their views (ownerVar, countedVar), and a
// type parameter's value also by the end of its block; a variable of the
// function's top block lasts as long as the frame.
func (f *fn) scopeVar(v *types2.Var) *types2.Var {
	if v == nil || !f.vars[v] || (bounds(v.Type()) && !tpBounds(v.Type())) || !cleans(v.Type()) {
		return nil
	}
	if in := f.scopeIn[v]; in == nil || in == ast.Node(f.topBody) {
		return nil
	}
	return v
}

// cleans reports whether a value of type T has drop glue (sections 18.6 and
// 18.8): it is an owner, or holds a counted value (a string, a box, a map,
// a channel, an allocated error), directly or in a struct, array or union.
// A type parameter has drop glue unless no type of its type set has it
// (section 18.3, D81).
func cleans(T types2.Type) bool { return cleansSeen(T, map[types2.Type]bool{}) }

func cleansSeen(T types2.Type, seen map[types2.Type]bool) bool {
	if T == nil || seen[T] {
		return false
	}
	seen[T] = true
	if isOwner(T) {
		return true
	}
	if tp, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		return tpMay(tp, func(t types2.Type) bool { return cleansSeen(t, seen) })
	}
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		return u.Info()&types2.IsString != 0
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if cleansSeen(u.Field(i).Type(), seen) {
				return true
			}
		}
	case *types2.Array:
		return u.Len() > 0 && cleansSeen(u.Elem(), seen)
	case *types2.Wrapper:
		switch u.Kind() {
		case types2.WrapAligned, types2.WrapVolatile, types2.WrapRing:
			return cleansSeen(u.Elem(), seen)
		}
		return u.IsBox()
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants(); i++ {
			if v := u.Variant(i); v.HasPayload() && cleansSeen(v.Payload(), seen) {
				return true
			}
		}
	case *types2.Chan, *types2.Map:
		return true
	case *types2.Interface:
		return types2.IsCountedError(T)
	}
	return false
}

// partOf returns the part of an owner a view made at path looks at: none
// in particular (all of it) when the view looks into an owned box.
func partOf(path Path, box bool) Path {
	if box {
		return nil
	}
	return path
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
		return loadedViews(f.viewsOf(x.X), f.c.typeOf(x))
	case *ast.StarExpr:
		if !mayView(f.c.typeOf(x)) {
			return nil
		}
		return loadedViews(f.viewsOf(x.X), f.c.typeOf(x))
	case *ast.TypeAssertExpr:
		if from, to := f.c.typeOf(x.X), f.c.typeOf(x); types2.IsInterface(from) && isOwner(from) && to != nil && types2.IsInterface(to) && !isOwner(to) {
			return compat.Append(f.viewsOf(x.X), f.lentViews(x.X)...)
		}
		return f.viewsOf(x.X)
	case *ast.CompositeLit:
		return f.litViews(x)
	case *ast.CallExpr:
		return f.callViews(x)
	case *ast.KeyValueExpr:
		return f.viewsOf(x.Value)
	case *ast.FuncLit:
		return f.funcLitViews(x)
	}
	return nil
}

// funcLitViews returns the views a function literal holds: those the local
// variables it uses hold where it is made, which calling it reads, and its
// hold on each of those variables that may lead to an owner (view.lit).
func (f *fn) funcLitViews(lit *ast.FuncLit) []view {
	var out []view
	// A variable the literal only gives a new value, v = x, is not read
	// by it: calling it after v was moved is valid (section 18.5).
	assigned := map[*ast.Ident]bool{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && as.Tok == token.ASSIGN {
			for _, l := range as.Lhs {
				if id, ok := ast.Unparen(l).(*ast.Ident); ok {
					assigned[id] = true
				}
			}
		}
		return true
	})
	var order []*types2.Var
	read := map[*types2.Var]bool{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v := f.varOf(id)
		if v == nil || !f.vars[v] {
			return true
		}
		if _, seen := read[v]; !seen {
			order = compat.Append(order, v)
			read[v] = false
			if mayView(v.Type()) {
				out = compat.Append(out, whole(f.held(v, nil))...)
			}
		}
		read[v] = read[v] || !assigned[id]
		return true
	})
	for _, v := range order {
		if mayView(v.Type()) || bounds(v.Type()) || cleans(v.Type()) || lendsRef(v.Type()) {
			out = compat.Append(out, view{of: v, pos: lit.Pos(), text: exprText(lit), itself: true, lit: lit, litSets: !read[v]})
		}
	}
	return out
}

// countedVar returns the local variable holding a counted reference
// (shared[T], synced[T]) that the place x is reached through by its first
// dereference, or nil. A view obtained that way is bounded by the
// reference as a view of an owner is (section 18.3).
func (f *fn) countedVar(x ast.Expr) *types2.Var {
	v, _ := f.countedPath(x)
	return v
}

// countedPath is countedVar, with the path of x in the object the
// reference points to. When the first dereference is not the only one on
// the way (an embedded pointer field), the path is nil: the whole object.
func (f *fn) countedPath(x ast.Expr) (*types2.Var, Path) {
	X, path, ok := countedPlace(f.c, x)
	if !ok {
		return nil, nil
	}
	id, isID := ast.Unparen(X).(*ast.Ident)
	if !isID {
		return nil, nil
	}
	if v := f.varOf(id); v != nil && f.vars[v] {
		return v, path
	}
	return nil, nil
}

// isCountedBox reports whether T is shared[E] or synced[E].
func isCountedBox(T types2.Type) bool {
	if T == nil {
		return false
	}
	w, ok := T.Underlying().(*types2.Wrapper)
	return ok && (w.Kind() == types2.WrapShared || w.Kind() == types2.WrapSynced)
}

// bounds reports whether the views of a local variable of type T are
// bounded by it: an owner, or a counted reference.
func bounds(T types2.Type) bool { return isOwner(T) || isCountedBox(T) || tpBounds(T) }

// tpBounds reports whether T is a type parameter whose views are bounded
// by the variable holding it: unless every type of its type set is a value
// or a view, an instantiation may make it an owner, a counted value, a map
// or a channel (section 18.3, D81).
func tpBounds(T types2.Type) bool {
	tp, ok := types2.Unalias(T).(*types2.TypeParam)
	return ok && tpMay(tp, func(t types2.Type) bool { return cleans(t) || bounds(t) || lendsRef(t) })
}

// isTypeParam reports whether T is a type parameter, whose underlying type
// is its constraint's interface: a value passed as one is not converted.
func isTypeParam(T types2.Type) bool {
	_, ok := types2.Unalias(T).(*types2.TypeParam)
	return ok
}

// tpMay reports whether some type of the type set of tp may satisfy is:
// true when the constraint does not limit the set to listed types.
func tpMay(tp *types2.TypeParam, is func(types2.Type) bool) bool {
	if isOwner(tp) {
		return true
	}
	ts, limited := tpTerms(tp)
	if !limited {
		return true
	}
	for _, t := range ts {
		if is(t) {
			return true
		}
	}
	return false
}

// tpTerms returns types the type set of tp is limited to, by the terms of
// its constraint, and false when the set holds every type with its
// methods. A set given by several embedded elements is the intersection,
// so the terms of one of them cover it.
func tpTerms(tp *types2.TypeParam) ([]types2.Type, bool) {
	it, _ := tp.Underlying().(*types2.Interface)
	return ifaceTerms(it, 0)
}

func ifaceTerms(it *types2.Interface, depth int) ([]types2.Type, bool) {
	if it == nil || depth > 8 {
		return nil, false
	}
	for i := 0; i < it.NumEmbeddeds(); i++ {
		e := it.EmbeddedType(i)
		if u, ok := e.(*types2.Union); ok {
			var out []types2.Type
			all := true
			for j := 0; j < u.Len(); j++ {
				t := u.Term(j).Type()
				if sub, isIface := t.Underlying().(*types2.Interface); isIface {
					ts, k := ifaceTerms(sub, depth+1)
					if !k {
						all = false
						break
					}
					out = compat.Append(out, ts...)
					continue
				}
				out = compat.Append(out, t)
			}
			if all {
				return out, true
			}
			continue
		}
		if sub, isIface := e.Underlying().(*types2.Interface); isIface {
			if ts, k := ifaceTerms(sub, depth+1); k {
				return ts, true
			}
			continue
		}
		return compat.Clone([]types2.Type{e}), true
	}
	return nil, false
}

// isCheckedArena reports whether T is one of the checked arenas of
// pluto/arena, Fixed and Chunked (section 18.9).
func isCheckedArena(T types2.Type) bool {
	n, ok := types2.Unalias(T).(*types2.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != "pluto/arena" {
		return false
	}
	switch n.Obj().Name() {
	case "Fixed", "Chunked":
		return true
	}
	return false
}

// isArenaMark reports whether T is the mark of a checked arena, ChunkMark
// or FixedMark. A mark is a fill level, not memory of the arena: Release
// of a stale mark is a run time check (section 18.9), so a mark holds no
// view that Reset or Release ends.
func isArenaMark(T types2.Type) bool {
	n, ok := types2.Unalias(T).(*types2.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != "pluto/arena" {
		return false
	}
	switch n.Obj().Name() {
	case "ChunkMark", "FixedMark":
		return true
	}
	return false
}

// arenaAlloc reports whether call allocates from a checked arena: a call
// of its method New, Slice or Alloc. What it returns is a view of the
// arena's memory, which Reset and Release reclaim, whichever way the
// receiver reaches the arena: a local arena, a field of a local owner, or
// a pointer that holds a view of one (section 18.9).
func (f *fn) arenaAlloc(call *ast.CallExpr) bool {
	se := funSelector(call.Fun)
	if se == nil {
		return false
	}
	switch se.Sel.Name {
	case "New", "Slice", "Alloc":
	default:
		return false
	}
	sel := f.c.selections[se]
	if sel == nil || sel.Kind() != types2.MethodVal {
		return false
	}
	T := sel.Recv()
	if p := pointerElem(T); p != nil {
		T = p
	}
	return isCheckedArena(T)
}

// arenaAt reports whether the part at path of a value of type T may hold
// a checked arena: one, or something that leads to one.
func arenaAt(T types2.Type, path Path) bool {
	P, ok := typeAt(T, path)
	return !ok || P == nil || reachesArena(P)
}

// isArenaPart reports whether the part at path of a value of type T is a
// checked arena.
func isArenaPart(T types2.Type, path Path) bool {
	P, ok := typeAt(T, path)
	return ok && P != nil && isCheckedArena(P)
}

// typeAt returns the type of the part at path of a value of type T, and
// false when the path leaves the value's own storage or the types do not
// follow it.
func typeAt(T types2.Type, path Path) (types2.Type, bool) {
	for _, step := range path {
		if T == nil {
			return nil, false
		}
		switch u := T.Underlying().(type) {
		case *types2.Struct:
			k, err := strconv.Atoi(strings.SliceFrom(step, 1))
			if !strings.HasPrefix(step, ".") || err != nil || k < 0 || k >= u.NumFields() {
				return nil, false
			}
			T = u.Field(k).Type()
		case *types2.Array:
			T = u.Elem()
		default:
			return nil, false
		}
	}
	return T, true
}

// partViews returns the views a field or array element read from a local
// variable holds, and for a part of a local owner that may hold a view,
// a view of the owner: a slice or pointer field of an owner looks at what
// the owner holds.
func (f *fn) partViews(x ast.Expr) []view {
	v, path, box, ok := f.placePath(x)
	if !ok {
		if se, isSel := ast.Unparen(x).(*ast.SelectorExpr); isSel {
			// A field read through a pointer (l.head.next) looks at
			// memory that pointer reaches: the views it holds. Through a
			// local counted reference, it looks at what the reference
			// keeps alive.
			out := notAddr(f.viewsOf(se.X))
			if r, part := f.countedPath(x); r != nil {
				out = compat.Append(out, view{of: r, pos: x.Pos(), text: exprText(x), inHeap: true, part: part})
			}
			return out
		}
		return notAddr(f.viewsOf(baseOf(x)))
	}
	var out []view
	if box {
		out = whole(f.held(v, nil))
	} else {
		out = f.held(v, path)
	}
	if o := f.ownerVar(v); o != nil {
		out = compat.Append(out, view{of: o, pos: x.Pos(), text: exprText(x), inHeap: true, part: partOf(path, box)})
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
				w.at = Path(compat.Append(compat.Clone(Path{step}), w.at...))
			} else {
				w.at = nil
			}
			out = compat.Append(out, w)
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
				ft, step = u.Field(idx).Type(), compat.Concat(".", strconv.Itoa(idx))
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
	if T != nil && !isOwner(T) {
		if _, ok := T.Underlying().(*types2.Pointer); ok {
			out = compat.Append(out, f.boxView(e, e)...)
		}
	}
	if T != nil && types2.IsInterface(T) && !isOwner(T) && (isOwner(f.c.typeOf(e)) || isTypeParam(f.c.typeOf(e)) && !isTypeParam(T)) {
		// A type parameter's value converted to an ordinary interface may
		// be lent its word (a map, a channel, a box), so the interface is
		// a view the events of its variable end, for every type parameter
		// and every instantiation, [T ~int] included (section 18.3, D81).
		if from := f.c.typeOf(e); isTypeParam(from) && !isOwner(from) {
			out = compat.Append(out, f.tpView(e)...)
		} else if lentWord(from) || types2.IsInterface(from) {
			out = compat.Append(out, f.lentViews(e)...)
		} else {
			out = compat.Append(out, f.selfView(e, e)...)
		}
	}
	// An error interface counts a synced reference rather than lending it
	// (sections 7.9 and 16.7).
	if from := f.c.typeOf(e); lentIface(T) && lendsRef(from) && !(isCountedBox(from) && types2.IsCountedError(T)) {
		out = compat.Append(out, f.refView(e)...)
	}
	return out
}

// loadedViews returns vs marked as reaching a value of type T loaded
// through them.
func loadedViews(vs []view, T types2.Type) []view {
	out := compat.Clone(vs)
	owns := freesHeld(T)
	for i := range out {
		out[i].loaded = true
		out[i].lowns = owns
		out[i].addr = false
	}
	return out
}

// notAddr returns vs as views held by a value loaded through them, not
// the address of an owner's storage (view.addr).
func notAddr(vs []view) []view {
	out := compat.Clone(vs)
	for i := range out {
		out[i].addr = false
	}
	return out
}

// dropFrees reports whether dropping a value of type T may free memory it
// holds: it has a Drop method, or a part of it frees what it holds
// (freesHeld). An owner struct whose fields hold only plain pointers and
// slices frees nothing they point to.
func dropFrees(T types2.Type) bool {
	if T == nil {
		return false
	}
	if hasDrop(T) {
		return true
	}
	if _, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		return true
	}
	switch u := T.Underlying().(type) {
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if freesHeld(u.Field(i).Type()) {
				return true
			}
		}
		return false
	case *types2.Pointer, *types2.Slice, *types2.Basic, *types2.Signature:
		return false
	}
	return freesHeld(T)
}

// freesHeld reports whether a value of type T frees memory it holds when
// it is dropped, so a view loaded from it may look at memory freed with
// it: an owner, a box, a map or channel, or a value holding one. A string
// holds its own count and frees nothing a view of it sees. A type
// parameter may be any of these (section 18.3, the strictest rule).
func freesHeld(T types2.Type) bool { return freesHeldSeen(T, map[types2.Type]bool{}) }

func freesHeldSeen(T types2.Type, seen map[types2.Type]bool) bool {
	if T == nil || seen[T] {
		return false
	}
	seen[T] = true
	if isOwner(T) {
		return true
	}
	if _, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		return true
	}
	switch u := T.Underlying().(type) {
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if freesHeldSeen(u.Field(i).Type(), seen) {
				return true
			}
		}
	case *types2.Array:
		return u.Len() > 0 && freesHeldSeen(u.Elem(), seen)
	case *types2.Wrapper:
		switch u.Kind() {
		case types2.WrapAligned, types2.WrapVolatile, types2.WrapRing:
			return freesHeldSeen(u.Elem(), seen)
		}
		return u.IsBox()
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants(); i++ {
			if v := u.Variant(i); v.HasPayload() && freesHeldSeen(v.Payload(), seen) {
				return true
			}
		}
	case *types2.Chan, *types2.Map:
		return true
	case *types2.Interface:
		return isOwner(T)
	}
	return false
}

// lendsRef reports whether a value of type T, put in the data word of an
// ordinary interface, is lent rather than counted (section 7.9): a map, a
// channel or a counted box that is not an owner.
func lendsRef(T types2.Type) bool {
	if T == nil || isOwner(T) {
		return false
	}
	if isCountedBox(T) {
		return true
	}
	switch T.Underlying().(type) {
	case *types2.Map, *types2.Chan:
		return true
	}
	return false
}

// refView returns the view an ordinary interface makes of the local
// variable whose map, channel or counted box x is, or of the local owner
// x is part of: the interface holds the variable's word without a count,
// so it may be used only while the variable keeps that value (section
// 7.9: after x := any(m), m = nil drops the map and x is stale).
func (f *fn) refView(x ast.Expr) []view {
	v, path, box, ok := f.placePath(x)
	if !ok || box || v == nil || !f.vars[v] {
		return nil
	}
	if len(path) == 0 || isOwner(v.Type()) {
		return compat.Clone([]view{{of: v, pos: x.Pos(), text: exprText(x), part: path, lent: !isOwner(v.Type())}})
	}
	return nil
}

// lentWord reports whether an ordinary interface that an owner of type T is
// lent to holds a copy of its one word rather than the variable's address
// (section 7.9): a box, a map, a channel, a weak reference, an owner
// defined from a pointer type, or a struct with one field or an array of
// length 1 that holds one of these.
func lentWord(T types2.Type) bool {
	if types2.PointerShaped(T) {
		return true
	}
	switch u := T.Underlying().(type) {
	case *types2.Map, *types2.Chan:
		return true
	case *types2.Struct:
		return u.NumFields() == 1 && lentWord(u.Field(0).Type())
	case *types2.Array:
		return u.Len() == 1 && lentWord(u.Elem())
	}
	return false
}

// lentViews is selfView for an ordinary interface that holds the word of
// the owner x (lentWord), or that an assertion or type switch clause from
// the owning interface x makes (section 11.13): the view it makes is
// obtained from the variable, not the variable itself, so a call that may
// free what x holds ends it (section 18.3). The word of a box or of an
// owner defined from a pointer looks at what the owner holds on the heap,
// so a variable that a function literal uses follows it (storeViews).
func (f *fn) lentViews(x ast.Expr) []view {
	vs := f.selfView(x, x)
	T := f.c.typeOf(x)
	heap := T != nil && !types2.IsInterface(T) && (lentWord(T) || tpBounds(T))
	for i := range vs {
		// The word lent, not the address of x's storage (view.addr).
		vs[i].addr = false
		if vs[i].itself && vs[i].pos == x.Pos() {
			vs[i].itself = false
			vs[i].inHeap = vs[i].inHeap || heap
		}
	}
	return vs
}

// tpView returns the views an interface made from the type parameter value
// x holds: what x holds, and a view of the local variable x is part of,
// since an instantiation may lend the interface its word without a count
// or copy the value in the frame (section 18.3, D81). Like refView's, the
// view ends when the variable is given a new value, moved or dropped; a
// call that frees through x leaves the word itself in place.
func (f *fn) tpView(x ast.Expr) []view {
	out := f.viewsOf(x)
	v, path, box, ok := f.placePath(x)
	if !ok || v == nil || box || !f.vars[v] {
		return out
	}
	return compat.Append(out, view{of: v, pos: x.Pos(), text: exprText(x), part: path, lent: true})
}

// callViews returns the views a call's results may hold: the views of the
// arguments the escape analysis says a result derives from.
func (f *fn) callViews(call *ast.CallExpr) []view {
	tv := f.c.types[call.Fun]
	if tv.IsType() {
		// A conversion to a type that holds no reference, such as a
		// uintptr, is no longer followed (section 18.3).
		if !mayView(tv.Type) {
			return nil
		}
		var out []view
		for _, a := range call.Args {
			out = compat.Append(out, f.viewsTo(a, tv.Type)...)
			out = compat.Append(out, f.ownerPtrView(a, tv.Type, call)...)
		}
		return out
	}
	if !mayView(f.c.typeOf(call)) || isArenaMark(f.c.typeOf(call)) {
		return nil
	}
	var out []view
	if tv.IsBuiltin() {
		id, _ := ast.Unparen(call.Fun).(*ast.Ident)
		if id != nil && (id.Name == "append" || id.Name == "min" || id.Name == "max") {
			for _, a := range call.Args {
				out = compat.Append(out, whole(f.viewsOf(a))...)
			}
		}
		return out
	}
	alloc := f.arenaAlloc(call)
	f.eachArg(call, func(key, val ast.Expr, T types2.Type) {
		if alloc && f.isRecv(call, key) {
			// An allocation from a checked arena looks at the arena's
			// memory, whichever local the receiver reaches it in: the
			// arena itself, a field of an owner, or through a pointer
			// that holds a view of either (section 18.9).
			for _, w := range whole(f.viewsTo(val, T)) {
				if w.of != nil && (!w.inHeap || w.obj || isOwnedBox(w.of.Type())) && arenaAt(w.of.Type(), w.part) {
					out = compat.Append(out, view{of: w.of, pos: call.Pos(), text: exprText(call), inHeap: true, part: w.part})
				}
			}
			return
		}
		af := f.c.calls.Arg(key)
		if af != nil && af.Returned && !af.Self && !loadsMayView(T) {
			// The result holds only what was loaded through the argument,
			// and that holds no pointer: bytes copied out of a []byte.
			return
		}
		if u, ok := ast.Unparen(val).(*ast.UnaryExpr); ok && u.Op == token.AND && af != nil && af.Returned && !af.Self && len(af.Into) == 0 && f.c.takesOut(call) {
			// Every callee returns an owner it moves out of memory reached
			// through its inputs (return take(&q.v)): the result holds what
			// that owner held, not a view of the place it was in (section
			// 18.5).
			out = compat.Append(out, notAddr(whole(f.viewsOf(u.X)))...)
			return
		}
		ws := whole(f.viewsTo(val, T))
		// The one literal the callee may be, when its views name one: the
		// holds of a literal it uses name another (funcLitViews).
		var only *ast.FuncLit
		several := false
		for _, w := range ws {
			if w.lit != nil && only == nil && !several {
				only = w.lit
			}
			if w.lit != nil && w.lit != only {
				only, several = nil, true
			}
		}
		for _, w := range ws {
			if w.lit != nil {
				if key != call.Fun || T != nil {
					if af == nil || af.Returned {
						out = compat.Append(out, w) // the literal itself, passed on
					}
					continue
				}
				if w.lit == only && !f.litReturns(only, w.of) {
					continue // nothing obtained from w.of reaches a return
				}
				// What calling the literal returns may be a view of a variable
				// it uses, or of what that variable holds (section 18.3).
				out = compat.Append(out, f.litResultViews(call, w.of)...)
				continue
			}
			w.itself = false // a view obtained from the variable
			// A result the callee derives from the argument is the same
			// address only when it has the argument's type: a pointer of
			// another type may point anywhere inside (a ring's Peek).
			w.addr = w.addr && af != nil && af.Self && types2.Identical(f.c.typeOf(call), f.c.typeOf(val))
			if af != nil && !af.Returned {
				switch {
				case f.loadsOwned(call, w, T):
					w.inHeap = true
				case f.assertsLent(call, key, val, w, T):
				default:
					continue
				}
			}
			if call.Pos() <= w.pos && w.pos < call.End() {
				// Made by this call (c.Slice() takes &c): name the call. A
				// result loaded through the argument looks at what the
				// owner holds rather than at the owner.
				w.pos, w.text = call.Pos(), exprText(call)
				// Of a temporary, with no facts for the receiver: the
				// result may look at what the temporary holds (temps.p).
				w.inHeap = w.inHeap || af != nil && !af.Self || af == nil && f.isTemp(w.of)
			}
			out = compat.Append(out, w)
		}
	})
	return compat.Append(out, f.tpRecvViews(call, out)...)
}

// takesOut reports whether the call returns one owner that each function
// it may reach moves out with take: every return statement of each body
// is return take(&place). Such a result holds the owner taken, which owns
// its memory, and no pointer to where it was.
func (c *checker) takesOut(call *ast.CallExpr) bool {
	if !isOwner(c.typeOf(call)) {
		return false
	}
	targets, known := c.keepTargets(call)
	if !known || len(targets) == 0 {
		return false
	}
	for _, t := range targets {
		var body *ast.BlockStmt
		switch n := t.node.(type) {
		case *ast.FuncDecl:
			body = n.Body
		case *ast.FuncLit:
			body = n.Body
		}
		if body == nil || !c.returnsTaken(body) {
			return false
		}
	}
	return true
}

// returnsTaken reports whether body has a return statement and each one
// returns take(&place), with the builtin take.
func (c *checker) returnsTaken(body *ast.BlockStmt) bool {
	if r, ok := c.taken[body]; ok {
		return r
	}
	if c.taken == nil {
		c.taken = map[*ast.BlockStmt]bool{}
	}
	ok, seen := true, false
	ast.Inspect(body, func(n ast.Node) bool {
		if !ok {
			return false
		}
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			seen = true
			if len(x.Results) != 1 {
				ok = false
				return false
			}
			call, isCall := ast.Unparen(x.Results[0]).(*ast.CallExpr)
			if !isCall || len(call.Args) != 1 || !c.types[call.Fun].IsBuiltin() {
				ok = false
				return false
			}
			id, isID := ast.Unparen(call.Fun).(*ast.Ident)
			u, isAmp := ast.Unparen(call.Args[0]).(*ast.UnaryExpr)
			if !isID || id.Name != "take" || !isAmp || u.Op != token.AND {
				ok = false
			}
		}
		return true
	})
	c.taken[body] = ok && seen
	return ok && seen
}

// tpRecvViews returns, for a method called on the value of a type parameter
// whose type set is not limited to values and views, the view of the local
// variable the receiver is part of that the result may hold: some
// instantiation's method may return what the value holds, as a counted box's
// Slice does, and generic code takes the strictest rule any type argument
// needs (section 18.3, D81). have is what callViews found already.
func (f *fn) tpRecvViews(call *ast.CallExpr, have []view) []view {
	se := funSelector(call.Fun)
	if se == nil {
		return nil
	}
	if sel := f.c.selections[se]; sel == nil || sel.Kind() != types2.MethodVal {
		return nil
	}
	X := f.c.typeOf(se.X)
	if X == nil || !isTypeParam(X) || !tpBounds(X) || isOwner(X) {
		return nil
	}
	var out []view
	for _, w := range whole(f.tpView(se.X)) {
		if w.pos != se.X.Pos() {
			continue
		}
		w.itself = false
		w.inHeap = true
		w.pos, w.text = call.Pos(), exprText(call)
		dup := false
		for _, h := range have {
			if h.of == w.of && h.pos == w.pos && pathKey(h.part) == pathKey(w.part) {
				dup = true
			}
		}
		if !dup {
			out = compat.Append(out, w)
		}
	}
	return out
}

// loadsMayView reports whether what is loaded through a value of type T
// (the elements of a slice, what a pointer points to) may hold a view.
func loadsMayView(T types2.Type) bool {
	if T == nil {
		return true
	}
	if _, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		return true
	}
	switch u := T.Underlying().(type) {
	case *types2.Slice:
		return mayView(u.Elem())
	case *types2.Pointer:
		return mayView(u.Elem())
	}
	return true
}

// ownerPtrView returns the view that the conversion made makes of a, an
// owner defined from a pointer type (type H owner *T), converted to a
// pointer type that is not an owner: (*T)(h) looks at the object h owns,
// so it is bounded by the local variable a is part of, like &*b for a box
// b (boxView; sections 7.18, 7.19 and 18.3).
func (f *fn) ownerPtrView(a ast.Expr, T types2.Type, made ast.Expr) []view {
	from := f.c.typeOf(a)
	if from == nil || T == nil || !bounds(from) || isOwner(T) || isOwnedBox(from) || isCountedBox(from) {
		return nil // a box: viewsTo
	}
	if _, ok := T.Underlying().(*types2.Pointer); !ok {
		return nil
	}
	if v, _, _, ok := f.placePath(a); ok && v != nil && f.vars[v] && bounds(v.Type()) {
		return compat.Clone([]view{{of: v, pos: made.Pos(), text: exprText(made), inHeap: true}})
	}
	return nil
}

// loadsOwned reports whether the result of call may look at memory that
// the owner of view w holds, though the escape analysis says no result
// derives from the argument that passes w. The escape analysis follows
// pointers for the frame rules only: a load of a field that holds nothing
// but heap memory yields heap memory, whoever owns it (section 18.3, the
// heap-only fields). For an owner that memory is its own, released when it
// is moved or dropped, so v.Slice() is a view of v even when v's buffer
// field is heap-only. The call passes the owner's own storage (w is not of
// its heap) or the object of a box, and the result holds a pointer or slice of a type the owner
// may hold, other than an owner (a new value, not a view). A parameter of
// type T that is an ordinary interface lends the owner (section 7.9): the
// callee reaches what the owner holds only through the results of T's
// methods, so only the types those results hold count.
func (f *fn) loadsOwned(call *ast.CallExpr, w view, T types2.Type) bool {
	if w.inHeap && !w.obj || w.of == nil || !bounds(w.of.Type()) {
		return false
	}
	res := resultLeaves(f.c.typeOf(call), 0)
	if _, tp := types2.Unalias(T).(*types2.TypeParam); T != nil && !tp && types2.IsInterface(T) && !isOwner(T) {
		if it, ok := T.Underlying().(*types2.Interface); ok {
			res = methodLeaves(res, it)
		}
	}
	if len(res) == 0 {
		return false
	}
	owned := f.c.ownedLeaves(w.of.Type())
	for _, r := range res {
		if b, ok := r.Underlying().(*types2.Basic); ok && b.Kind() == types2.UnsafePointer && len(owned) > 0 {
			return true
		}
		for _, o := range owned {
			if types2.Identical(r, o) {
				return true
			}
		}
	}
	return false
}

// assertsLent reports whether the result of call may hold what the
// ordinary interface parameter T was lent by val, view w, through a type
// assertion or type switch in the callee (calleeAsserts): the C2 ruling
// (docs/verification/2026-11-09-spec-rulings.md) bounds what an assertion
// on a lent interface yields by what was lent. The result must hold a
// pointer or slice of a type the lent word or the owner may hold.
func (f *fn) assertsLent(call *ast.CallExpr, key, val ast.Expr, w view, T types2.Type) bool {
	if !lentIface(T) || w.of == nil || !bounds(w.of.Type()) || !f.c.calleeAsserts(call, key) {
		return false
	}
	res := resultLeaves(f.c.typeOf(call), 0)
	if len(res) == 0 {
		return false
	}
	var words []types2.Type
	if from := f.c.typeOf(val); from != nil && !types2.IsInterface(from) {
		switch {
		case isOwnedBox(from) || isCountedBox(from):
			if wr, ok := from.Underlying().(*types2.Wrapper); ok {
				words = compat.Append(words, types2.Type(types2.NewPointer(wr.Elem())))
			}
		case lentWord(from):
			words = compat.Append(words, resultLeaves(from, 0)...)
			if _, ok := from.Underlying().(*types2.Pointer); ok {
				words = compat.Append(words, from)
			}
		case isOwner(from):
			words = compat.Append(words, types2.Type(types2.NewPointer(from)))
		default:
			words = compat.Append(words, resultLeaves(from, 0)...)
		}
	}
	words = compat.Append(words, f.c.ownedLeaves(w.of.Type())...)
	for _, r := range res {
		if _, tp := types2.Unalias(r).(*types2.TypeParam); tp {
			return true
		}
		for _, o := range words {
			if types2.Identical(r, o) {
				return true
			}
		}
	}
	return false
}

// methodLeaves returns the types in res that a result of a method of it
// may hold.
func methodLeaves(res []types2.Type, it *types2.Interface) []types2.Type {
	var out []types2.Type
	for _, r := range res {
		for i := 0; i < it.NumMethods(); i++ {
			sig, _ := it.Method(i).Type().(*types2.Signature)
			if sig == nil {
				continue
			}
			found := false
			for _, m := range resultLeaves(sig.Results(), 0) {
				if types2.Identical(r, m) {
					found = true
					break
				}
			}
			if found {
				out = compat.Append(out, r)
				break
			}
		}
	}
	return out
}

// resultLeaves returns the pointer, slice and unsafe.Pointer types a value
// of type T holds by value (in its fields, elements or results), leaving
// out owners, interfaces, func values, boxes and counted values.
func resultLeaves(T types2.Type, depth int) []types2.Type {
	T = types2.Unalias(T)
	if T == nil || depth > 8 || isOwner(T) {
		return nil
	}
	if _, ok := T.(*types2.TypeParam); ok {
		return compat.Clone([]types2.Type{T})
	}
	var out []types2.Type
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		if u.Kind() == types2.UnsafePointer {
			out = compat.Append(out, T)
		}
	case *types2.Pointer, *types2.Slice:
		out = compat.Append(out, T)
	case *types2.Array:
		out = resultLeaves(u.Elem(), depth+1)
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			out = compat.Append(out, resultLeaves(u.Field(i).Type(), depth+1)...)
		}
	case *types2.Tuple:
		for i := 0; i < u.Len(); i++ {
			out = compat.Append(out, resultLeaves(u.At(i).Type(), depth+1)...)
		}
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants(); i++ {
			if v := u.Variant(i); v.HasPayload() {
				out = compat.Append(out, resultLeaves(v.Payload(), depth+1)...)
			}
		}
	}
	return out
}

// ownedLeaves returns the pointer and slice types by which a value can
// point into memory reached from an owner of type T: the pointer and slice
// fields it holds, at any depth through what they point to, and pointers to
// every part of the objects reached (&v.data[i].val), slices of their
// arrays, and the type parameters they hold.
func (c *checker) ownedLeaves(T types2.Type) []types2.Type {
	key := types2.TypeString(T, nil)
	if ls, ok := c.owned[key]; ok {
		return ls
	}
	var out []types2.Type
	seen := map[string]bool{}
	var obj func(X types2.Type)
	obj = func(X types2.Type) {
		X = types2.Unalias(X)
		if X == nil {
			return
		}
		k := types2.TypeString(X, nil)
		if seen[k] {
			return
		}
		seen[k] = true
		out = compat.Append(out, types2.Type(types2.NewPointer(X)))
		if _, ok := X.(*types2.TypeParam); ok {
			out = compat.Append(out, X)
			return
		}
		switch u := X.Underlying().(type) {
		case *types2.Pointer:
			out = compat.Append(out, X)
			obj(u.Elem())
		case *types2.Slice:
			out = compat.Append(out, X)
			obj(u.Elem())
		case *types2.Array:
			out = compat.Append(out, types2.Type(types2.NewSlice(u.Elem())))
			obj(u.Elem())
		case *types2.Struct:
			for i := 0; i < u.NumFields(); i++ {
				obj(u.Field(i).Type())
			}
		case *types2.Wrapper:
			obj(u.Elem())
		case *types2.TaggedUnion:
			for i := 0; i < u.NumVariants(); i++ {
				if v := u.Variant(i); v.HasPayload() {
					obj(v.Payload())
				}
			}
		}
	}
	obj(T)
	if c.owned == nil {
		c.owned = map[string][]types2.Type{}
	}
	c.owned[key] = out
	return out
}

// eachArg calls fn for each argument of a call to a function or method,
// receiver first: key is the expression the escape analysis records facts
// under, val the value passed (&x for a receiver whose address the call
// takes) and T the parameter's type, if known.
func (f *fn) eachArg(call *ast.CallExpr, fn func(key, val ast.Expr, T types2.Type)) {
	if se := funSelector(call.Fun); se != nil && f.c.selections[se] != nil && f.c.selections[se].Kind() == types2.MethodVal {
		if sel := f.c.selections[se]; sel != nil {
			recv := methodRecv(sel.Obj().(*types2.Func))
			val := se.X
			// A pointer method of what a counted box points to gets the
			// box's object, not the box variable's address (boxView).
			if implicitAddr(f.c, se, recv) && !isCountedBox(f.c.typeOf(se.X)) {
				val = pu.New(ast.UnaryExpr{OpPos: se.X.Pos(), Op: token.AND, X: se.X})
			}
			// A method called on a type parameter's value is the method of
			// the type argument: the value is not converted to the
			// constraint's interface (section 18.3, D81).
			if X := f.c.typeOf(se.X); X != nil && isTypeParam(X) {
				recv = X
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
		packed_ := sig != nil && sig.Variadic() && i >= sig.Params().Len()-1 && !call.Ellipsis.IsValid()
		if sig != nil && i < sig.Params().Len() && !packed_ {
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
	if !ok || box || f.captured(v) {
		// Through a pointer, a box or a captured variable: the walk
		// cannot follow the views, of the owner's storage or of what it
		// holds on the heap, so a later free of that memory is E3314 too
		// (section 18.3).
		how := compat.Concat("stored in ", exprText(lhs))
		f.pin(vs, lhs.Pos(), how)
		f.heapPinIn(vs, lhs.Pos(), how, true, f.heapOwnersOf(storeBase(f.c, lhs)))
		return
	}
	if f.kept[v] {
		// Something the walk does not follow may use v, which pins the
		// owners of the views of their own storage. The views of what an
		// owner holds on the heap pin nothing, so they are followed here
		// as far as the walk sees v: a function literal that uses v holds
		// them (funcLitViews). So are the literals v holds, which a call
		// of v runs (funcValueEffects).
		how := compat.Concat("stored in ", exprText(lhs))
		f.pin(vs, lhs.Pos(), how)
		if at, ok := f.keptAt[v]; !ok || at < lhs.Pos() || f.loopAround(v, lhs) {
			// A pointer to v taken before (ps := &s) holds the views v
			// held then, not these: a later free of the heap memory they
			// look at is E3314 (section 18.3).
			f.heapPinIn(vs, lhs.Pos(), how, true, nil)
		}
		var heap []view
		for _, w := range vs {
			if w.inHeap || w.lit != nil {
				heap = compat.Append(heap, w)
			}
		}
		vs = heap
	}
	s := f.env[v]
	var keep []view
	for _, w := range s.views {
		if len(w.at) >= len(path) && slices.Equal(w.at[:len(path)], path) && !slices.Contains(path, "[*]") {
			continue // overwritten
		}
		keep = compat.Append(keep, w)
	}
	for _, w := range vs {
		w.at = Path(compat.Append(compat.Clone(path), w.at...))
		keep = compat.Append(keep, w)
	}
	s.views = sortViews(keep)
	f.setState(v, s)
}

// loopAround reports whether x may run again after code that follows it,
// within the life of v: a loop around x that v is declared outside of, or
// any goto.
func (f *fn) loopAround(v *types2.Var, x ast.Node) bool {
	found := false
	ast.Inspect(f.topBody, func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ForStmt, *ast.RangeStmt:
			found = n.Pos() > v.Pos() && n.Pos() <= x.Pos() && x.Pos() < n.End()
		case *ast.BranchStmt:
			found = n.Tok == token.GOTO
		}
		return !found
	})
	return found
}

// addViews records that the views vs may have been stored by call
// somewhere in the memory that its argument dst points to.
//
// stored says the views themselves may be stored, not only what was
// loaded through them (heapPin).
func (f *fn) addViews(call *ast.CallExpr, dst ast.Expr, direct, stored bool, vs []view) {
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
	how := compat.Concat("stored by ", exprText(call.Fun), " in memory the compiler does not follow")
	if dst != nil {
		how = compat.Concat("stored by ", exprText(call.Fun), " in what ", exprText(dst), " points to")
	}
	if place == nil {
		f.pin(vs, call.Pos(), how)
		f.heapPin(vs, call.Pos(), how, stored)
		return
	}
	v, _, box, ok := f.placePath(place)
	if !ok || box || f.captured(v) || f.kept[v] {
		f.pin(vs, call.Pos(), how)
		f.heapPin(vs, call.Pos(), how, stored)
		return
	}
	s := f.env[v]
	s.views = sortViews(compat.Append(compat.Clone(s.views), whole(vs)...))
	f.setState(v, s)
}

// pin records that the views vs went where the walk cannot follow them, at
// where, as how says ("stored in *q").
func (f *fn) pin(vs []view, where token.Pos, how string) {
	for _, w := range vs {
		s, ok := f.env[w.of]
		if ok && s.pin != nil || w.inHeap || w.lit != nil {
			// A function literal's hold on a variable is a view of the
			// frame, which the type rules keep out of memory the walk does
			// not follow (E3002); a call of it that the walk does not see
			// is found by the escape analysis's candidates (funcvals.p).
			continue
		}
		s.pin = pu.New(pinMark{pos: w.pos, text: w.text, where: where, how: how, lent: w.lent})
		f.setState(w.of, s)
	}
}

// heapPin records that a call may keep the views vs of what owners hold on
// the heap. Moving such an owner is no error, since a move does not
// release that memory, but moving it to another task is: the kept view
// would let this task use memory the other task now owns (section 8.8).
//
// stored says the view itself was kept, not only memory loaded through
// it; a later free of such an owner is E3314 too (frees.p, freedPart).
func (f *fn) heapPin(vs []view, where token.Pos, how string, stored bool) {
	f.heapPinIn(vs, where, how, stored, nil)
}

// heapPinIn is heapPin for views stored into memory that the owners own
// hold on the heap (pinMark.inOwn).
func (f *fn) heapPinIn(vs []view, where token.Pos, how string, stored bool, own []*types2.Var) {
	for _, w := range vs {
		if !w.inHeap {
			continue
		}
		s, ok := f.env[w.of]
		// What was loaded through a view is kept as the view itself when
		// it may look at memory the owner frees through it: an element
		// that is itself an owner of a buffer (lowns).
		kept := stored && (!w.loaded || w.lowns)
		p := pu.New(pinMark{pos: w.pos, text: w.text, where: where, how: how, stored: kept, inOwn: kept && slices.Contains(own, w.of)})
		if ok && pinRank(s.hpin) >= pinRank(p) {
			continue
		}
		s.hpin = p
		f.setState(w.of, s)
	}
}

// storeBase returns the pointer, slice or box that an assignment to lhs
// stores through (n for n.next or n.a[i], p for *p, s for s[i]), or nil.
func storeBase(c *checker, lhs ast.Expr) ast.Expr {
	e := ast.Unparen(lhs)
	for {
		switch x := e.(type) {
		case *ast.SelectorExpr:
			if T := c.typeOf(x.X); T != nil {
				if _, ok := T.Underlying().(*types2.Struct); !ok {
					return x.X
				}
			}
			e = ast.Unparen(x.X)
		case *ast.IndexExpr:
			if T := c.typeOf(x.X); T != nil {
				if _, ok := T.Underlying().(*types2.Array); !ok {
					return x.X
				}
			}
			e = ast.Unparen(x.X)
		case *ast.StarExpr:
			return x.X
		default:
			return nil
		}
	}
}

// heapOwnersOf returns the owners whose heap memory the value of e may
// point into.
func (f *fn) heapOwnersOf(e ast.Expr) []*types2.Var {
	if e == nil {
		return nil
	}
	var out []*types2.Var
	for _, w := range f.viewsOf(e) {
		if w.inHeap && !slices.Contains(out, w.of) {
			out = compat.Append(out, w.of)
		}
	}
	return out
}

// loadFrees reports whether a value loaded through a value of type T may
// itself free memory when the owner of T's memory is dropped: an element
// or pointee held inline that is an owner, a box, a map or a channel, as
// the vectors in the buffer of a vector of vectors. Keeping such a loaded
// value, or a view through it, keeps a view of the owner's memory. A
// pointer element points to memory the owner does not free.
func loadFrees(T types2.Type) bool { return loadFreesSeen(T, map[types2.Type]bool{}) }

func loadFreesSeen(T types2.Type, seen map[types2.Type]bool) bool {
	if T == nil || seen[T] {
		return false
	}
	seen[T] = true
	if _, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		return true
	}
	switch u := T.Underlying().(type) {
	case *types2.Pointer:
		return freesHeld(u.Elem())
	case *types2.Slice:
		return freesHeld(u.Elem())
	case *types2.Chan:
		return freesHeld(u.Elem())
	case *types2.Map:
		return freesHeld(u.Key()) || freesHeld(u.Elem())
	case *types2.Array:
		return loadFreesSeen(u.Elem(), seen)
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if loadFreesSeen(u.Field(i).Type(), seen) {
				return true
			}
		}
	case *types2.Wrapper:
		if u.IsBox() {
			return freesHeld(u.Elem())
		}
		return loadFreesSeen(u.Elem(), seen)
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants(); i++ {
			if v := u.Variant(i); v.HasPayload() && loadFreesSeen(v.Payload(), seen) {
				return true
			}
		}
	case *types2.Interface:
		// An ordinary interface lends its value: the callee reaches what
		// it holds only through the results of its methods (section 7.9),
		// which loadsOwned follows.
		return isOwner(T)
	case *types2.Basic:
		return u.Kind() == types2.UnsafePointer
	}
	return false
}

// loadsHeld reports whether a value loaded through the view w may be an
// owner, a box, a map or a channel that the owner of w holds in its heap
// memory, which it frees with that memory: w looks at that memory and is
// not lent.
func loadsHeld(w view) bool { return w.inHeap && !w.lent && !w.obj }

// crosses reports whether a move gives the value to another task.
func crosses(mv move) bool {
	switch mv.kind {
	case mvSend, mvGo, mvGoRecv, mvTask:
		return true
	}
	return false
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
		how := compat.Concat("passed to ", exprText(a.call.Fun), ", which may keep it")
		if site, ok := f.c.calleeKeeps(a.call, a.key); ok && af != nil {
			// The callee may keep memory loaded through the argument
			// (keeps.p): what the views point to is then kept as if it
			// were the heap memory of the owner.
			frees := loadFrees(f.c.typeOf(a.key))
			for _, w := range a.views {
				if w.lit != nil {
					continue // a hold of the frame, which no callee keeps (E3002)
				}
				// What is loaded through a view of what the owner holds on
				// the heap may be an owner the buffer holds, freed with it.
				// The owner's own storage holds its fields, which the
				// callee may only borrow (section 18.3).
				stored := frees && loadsHeld(w)
				w.inHeap = true
				f.heapPin(compat.Clone([]view{w}), a.call.Pos(), f.c.keepHow(a.call, site), stored)
			}
		}
		// A view kept through an unsafe.Pointer is the unsafe code's to
		// check: it does not make a later free an error.
		b, viaUnsafe := f.c.typeOf(a.key).Underlying().(*types2.Basic)
		viaUnsafe = viaUnsafe && b.Kind() == types2.UnsafePointer
		if af == nil {
			f.pin(a.views, a.call.Pos(), how)
			f.heapPin(a.views, a.call.Pos(), how, !viaUnsafe)
			continue
		}
		if af.Self {
			// Self covers a callee that only returns the argument: the
			// result's views are followed where it goes (callViews), so
			// only one that keeps it elsewhere stores the view.
			f.heapPin(a.views, a.call.Pos(), how, af.Kept && !viaUnsafe)
		}
		for _, in := range af.Into {
			if in.Arg == a.key {
				continue // stored back into what it came from, which holds it already
			}
			if !in.Loaded || !loadFrees(f.c.typeOf(a.key)) {
				f.addViews(a.call, in.Arg, in.Direct, !in.Loaded && !viaUnsafe, a.views)
				continue
			}
			for _, w := range a.views {
				f.addViews(a.call, in.Arg, in.Direct, loadsHeld(w) && !viaUnsafe, compat.Clone([]view{w}))
			}
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
		if af != nil && len(af.Into) == 0 && !af.Self {
			if _, keeps := f.c.calleeKeeps(call, key); !keeps {
				return
			}
		}
		if vs := f.viewsTo(val, T); len(vs) > 0 {
			out = compat.Append(out, argView{call: call, key: key, views: vs})
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
	if f.env == nil || !f.vars[v] {
		return
	}
	lends := lendsRef(v.Type()) || isTypeParam(v.Type())
	if !bounds(v.Type()) && !lends && (mv.kind != mvScopeEnd || !cleans(v.Type())) {
		return
	}
	// A map or channel variable that is not released here ends only the
	// interfaces lent its word: a pointer to the variable stays valid.
	onlyLent := !bounds(v.Type()) && (mv.kind != mvScopeEnd || !cleans(v.Type()))
	// A view of what v holds on the heap that a call stored where the walk
	// cannot follow can no longer be checked once v frees that memory: at
	// its drop, at the end of its block while it holds a value, and when
	// it is given a new value (section 18.3). The pin then ends with the
	// memory it looked at.
	hp := f.env[v].hpin
	frees := hp != nil && hp.stored && !onlyLent && dropFrees(v.Type()) && (mv.kind == mvDrop || mv.kind == mvNewValue || mv.kind == mvScopeEnd && f.env[v].st != moved)
	if p := f.env[v].pin; p != nil && (!onlyLent || p.lent) && f.report {
		f.c.report(f.c.pinnedDiag(v, p, mv))
	} else if hp != nil && !hp.inOwn && (crosses(mv) || frees) && f.report {
		f.c.report(f.c.heapPinnedDiag(v, hp, mv))
	}
	if frees {
		s := f.env[v]
		s.hpin = nil
		f.setState(v, s)
	}
	f.staleWhere(v, mv, func(w view) bool {
		if w.lit != nil {
			// The literal reads the variable when it is called: a new value
			// or take leaves the variable where it was (section 11.4).
			return (!w.litSets || mv.kind == mvScopeEnd) && !onlyLent && mv.kind != mvNewValue && mv.kind != mvTake
		}
		return !onlyLent || w.lent
	})
}

// invalidatePart records that the part of the owner v at path lost its
// value, as take(&v.part) does: the views of that part are stale.
func (f *fn) invalidatePart(v *types2.Var, path Path, mv move) {
	if f.env == nil || !isOwner(v.Type()) || !f.vars[v] {
		return
	}
	f.staleWhere(v, mv, func(w view) bool { return partEnds(w, path) })
}

// partEnds reports whether a new value in the part at path of an owner
// ends the view w of it: w looks at or into that part. The owner's own
// storage stays, so &v itself, and a pointer to the storage of a part
// that holds the one at path, still look at a live place (section 18.3:
// an owner field on the view's path).
func partEnds(w view, path Path) bool {
	if w.itself || !overlap(w.part, path) {
		return false
	}
	return w.inHeap || w.obj || len(w.part) == 0 || len(w.part) >= len(path)
}

// invalidateThrough records that an assignment gives a new value to the
// owner or counted box lhs, a place reached through a pointer (pn.child,
// (*pn).child, *pn): where the pointer holds a view of the storage of a
// local owner, the views of that owner that look at or into the place
// are stale, as for an assignment to the place by the owner's name
// (section 18.3). A pointer into memory an owner holds on the heap is
// left to the generation checks (section 18.11).
func (f *fn) invalidateThrough(lhs ast.Expr, mv move) {
	if f.env == nil {
		return
	}
	base, steps, ok := f.throughPlace(lhs)
	if !ok {
		return
	}
	for _, w := range f.viewsOf(base) {
		if w.of == nil || w.inHeap || w.obj || w.lent || w.stale != nil && !w.maybe || f.captured(w.of) {
			continue
		}
		f.invalidatePart(w.of, Path(compat.Append(compat.Clone(w.part), steps...)), mv)
	}
}

// throughPlace splits the place x, reached through a pointer, into the
// pointer and the path from what it points to down to x. ok is false when
// x is not reached through a pointer.
func (f *fn) throughPlace(x ast.Expr) (ast.Expr, Path, bool) {
	switch e := ast.Unparen(x).(type) {
	case *ast.StarExpr:
		if pointerElem(f.c.typeOf(e.X)) == nil {
			return nil, nil, false
		}
		return e.X, nil, true
	case *ast.SelectorExpr:
		sel := f.c.selections[e]
		if sel == nil || sel.Kind() != types2.FieldVal {
			return nil, nil, false
		}
		idx := sel.Index()
		if sel.Indirect() {
			if len(idx) != 1 || pointerElem(f.c.typeOf(e.X)) == nil {
				// A promoted field reached through an embedded pointer: the
				// part is not known, so all of what e.X points to.
				return e.X, nil, pointerElem(f.c.typeOf(e.X)) != nil
			}
			var one Path
			return e.X, compat.Append(one, compat.Concat(".", strconv.Itoa(idx[0]))), true
		}
		base, path, ok := f.throughPlace(e.X)
		if !ok {
			return nil, nil, false
		}
		for _, i := range idx {
			path = compat.Append(path, compat.Concat(".", strconv.Itoa(i)))
		}
		return base, path, true
	case *ast.IndexExpr:
		if _, isArr := f.c.typeOf(e.X).Underlying().(*types2.Array); !isArr {
			return nil, nil, false
		}
		base, path, ok := f.throughPlace(e.X)
		if !ok {
			return nil, nil, false
		}
		step := "[*]"
		if tv := f.c.types[e.Index]; !tv.Value.IsNil() {
			if i, exact := constant.Int64Val(constant.ToInt(tv.Value)); exact {
				step = compat.Concat("[", strconv.FormatInt(i, 10), "]")
			}
		}
		return base, Path(compat.Append(path, step)), true
	}
	return nil, nil, false
}

// staleViews makes the views of v that look at or into the part at path
// (all of v for a nil path) stale.
func (f *fn) staleViews(v *types2.Var, path Path, mv move) {
	f.staleWhere(v, mv, func(w view) bool { return overlap(w.part, path) })
}

// staleWhere makes the views of v for which which holds stale.
func (f *fn) staleWhere(v *types2.Var, mv move, which func(w view) bool) {
	for h, s := range f.env {
		var out []view // the views slice is shared with other paths' states
		for i, w := range s.views {
			if w.of == v && (w.stale == nil || w.maybe) && which(w) {
				if out == nil {
					out = compat.Clone(s.views)
				}
				m := pu.New(mv)
				out[i].stale, out[i].maybe = m, false
			}
		}
		if out != nil {
			s.views = out
			f.env[h] = s
		}
	}
}

// reclaim records a call of Reset or Release on a local checked arena: the
// views of what it reclaims are stale (section 18.9). Reset reclaims every
// allocation. Release(m) reclaims those made after the Mark that gave m
// its value: when m is a local variable that only a := of a.Mark() gives a
// value, in a function without goto, those are the views made after that
// statement in the text; otherwise every allocation is taken as reclaimed.
// The arena's own storage stays, so views of it (&a) do not go stale.
//
// A call given a local arena some other way (a pointer to it, pa.Reset(),
// or a function that resets or releases what it is given, wipe(&a)) is
// found by reclaimArgs before the call is walked: every allocation of
// those arenas is taken as reclaimed.
func (f *fn) reclaim(call *ast.CallExpr, others []reclaimArg) {
	if f.env == nil {
		return
	}
	for _, r := range others {
		mv := move{kind: mvReclaim, pos: call.Pos(), to: r.how}
		f.staleWhere(r.a, mv, func(w view) bool { return w.inHeap && overlap(w.part, r.part) })
	}
	a, part := f.reclaimDirect(call)
	if a == nil {
		return
	}
	se := funSelector(call.Fun)
	since := token.NoPos
	if se.Sel.Name == "Release" && len(call.Args) == 1 && len(part) == 0 {
		if mid, ok := ast.Unparen(call.Args[0]).(*ast.Ident); ok {
			since = f.markPos(f.varOf(mid), a)
		}
	}
	mv := move{kind: mvReclaim, pos: call.Pos(), to: exprText(call)}
	f.staleWhere(a, mv, func(w view) bool { return w.inHeap && w.pos > since && overlap(w.part, part) })
}

// reclaimDirect returns the local variable and the part of it that is the
// checked arena when call is a.Reset() or a.Release(m) on a local arena or
// a checked arena field of a local owner (s.a.Reset()), or nil.
func (f *fn) reclaimDirect(call *ast.CallExpr) (*types2.Var, Path) {
	se := funSelector(call.Fun)
	if se == nil || se.Sel.Name != "Reset" && se.Sel.Name != "Release" || !isCheckedArena(f.c.typeOf(se.X)) {
		return nil, nil
	}
	a, path, box, ok := f.placePath(se.X)
	if !ok || box || a == nil || !f.vars[a] || !bounds(a.Type()) {
		return nil, nil
	}
	return a, path
}

// A reclaimArg is a local variable holding a checked arena that a call may
// reset or release through one of its arguments, the part of it that leads
// to the arena (nil for all of it), and how, for messages.
type reclaimArg struct {
	a    *types2.Var
	part Path
	how  string
}

// reclaimArgs returns the local checked arenas that the arguments of call
// lead to and that the callee may reset or release through them
// (c.reclaims), other than a.Reset() and a.Release(m) themselves.
func (f *fn) reclaimArgs(call *ast.CallExpr) []reclaimArg {
	if f.env == nil {
		return nil
	}
	if a, _ := f.reclaimDirect(call); a != nil {
		return nil
	}
	tv := f.c.types[call.Fun]
	if tv.IsType() || tv.IsBuiltin() || f.c.scoped.Tasks[call] != nil || f.isImplicitDrop(call) {
		return nil
	}
	var out []reclaimArg
	f.eachArg(call, func(key, val ast.Expr, T types2.Type) {
		site, ok := f.c.calleeReclaims(call, key)
		if !ok {
			return
		}
		how := f.c.reclaimHow(call, site)
		// The arena may be the variable a view looks at, a part of it, or
		// held in the object of a box it holds: every allocation of the
		// part the view reaches is taken as reclaimed.
		for _, w := range f.viewsTo(val, T) {
			if w.of == nil || !f.vars[w.of] || !bounds(w.of.Type()) {
				continue
			}
			if isCheckedArena(w.of.Type()) {
				out = compat.Append(out, reclaimArg{a: w.of, how: how})
			} else if (!w.inHeap || w.obj || isOwnedBox(w.of.Type())) && arenaAt(w.of.Type(), w.part) {
				out = compat.Append(out, reclaimArg{a: w.of, part: w.part, how: how})
			}
		}
	})
	return out
}

// markPos returns where the a.Mark() call is that alone gives the local
// variable m its value, by m := a.Mark(), or NoPos if m may hold another
// value.
func (f *fn) markPos(m *types2.Var, a *types2.Var) token.Pos {
	if m == nil || !f.vars[m] || f.topBody == nil {
		return token.NoPos
	}
	at, n, bad := token.NoPos, 0, false
	isM := func(e ast.Expr) bool {
		id, ok := ast.Unparen(e).(*ast.Ident)
		return ok && f.c.objOf(id) == m
	}
	ast.Inspect(f.topBody, func(nd ast.Node) bool {
		switch x := nd.(type) {
		case *ast.BranchStmt:
			bad = bad || x.Tok == token.GOTO
		case *ast.UnaryExpr:
			bad = bad || x.Op == token.AND && isM(x.X)
		case *ast.RangeStmt:
			bad = bad || isM(x.Key) || isM(x.Value)
		case *ast.ValueSpec:
			for _, name := range x.Names {
				if isM(name) {
					bad = true
				}
			}
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				if !isM(l) {
					continue
				}
				n++
				if x.Tok != token.DEFINE || len(x.Lhs) != len(x.Rhs) {
					bad = true
					continue
				}
				call, ok := ast.Unparen(x.Rhs[i]).(*ast.CallExpr)
				se := (*ast.SelectorExpr)(nil)
				if ok {
					se = funSelector(call.Fun)
				}
				if se == nil || se.Sel.Name != "Mark" || len(call.Args) != 0 {
					bad = true
					continue
				}
				if id, ok := ast.Unparen(se.X).(*ast.Ident); !ok || f.varOf(id) != a {
					bad = true
					continue
				}
				at = x.Pos()
			}
		}
		return true
	})
	if bad || n != 1 {
		return token.NoPos
	}
	return at
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
			p = Path(compat.Append(p, compat.Concat(".", strconv.Itoa(idx))))
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
			step = compat.Concat("[", tv.Value.ExactString(), "]")
		}
		return root, Path(compat.Append(p, step))
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
				f.pin([]view{{of: o, pos: made.Pos(), text: exprText(made)}}, made.Pos(), compat.Concat("kept by the function literal at line ", strconv.Itoa(f.c.line(lit.Pos()))))
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
