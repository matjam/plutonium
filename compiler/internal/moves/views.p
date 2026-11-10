// Translated by go2pu from internal/moves/views.go at the go-cutover tag; edited by hand since.

package moves

import (
	"compiler/compat"
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
	return compat.Concat(strconv.Itoa(int(w.pos)), "|", w.of.Name(), strconv.Itoa(int(w.of.Pos())), "|", pathKey(w.at))
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
		if r := f.countedVar(x); r != nil {
			out = compat.Append(out, view{of: r, pos: made.Pos(), text: exprText(made), inHeap: true})
		}
		return out
	}
	var out []view
	if box {
		path = nil
	}
	out = compat.Append(out, whole(f.held(v, path))...)
	if o := f.ownerVar(v); o != nil {
		out = compat.Append(out, view{of: o, pos: made.Pos(), text: exprText(made), inHeap: box, part: path, itself: !box && len(path) == 0})
	} else if o := f.scopeVar(v); o != nil {
		out = compat.Append(out, view{of: o, pos: made.Pos(), text: exprText(made), part: path})
	}
	return out
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
// through every event that ends their views (ownerVar, countedVar); a
// variable of the function's top block lasts as long as the frame.
func (f *fn) scopeVar(v *types2.Var) *types2.Var {
	if v == nil || !f.vars[v] || bounds(v.Type()) || !cleans(v.Type()) {
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
// A type parameter is left out here (roadmap item 208).
func cleans(T types2.Type) bool { return cleansSeen(T, map[types2.Type]bool{}) }

func cleansSeen(T types2.Type, seen map[types2.Type]bool) bool {
	if T == nil || seen[T] {
		return false
	}
	seen[T] = true
	if isOwner(T) {
		return true
	}
	if _, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		return false
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
		return f.viewsOf(x.X)
	case *ast.StarExpr:
		if !mayView(f.c.typeOf(x)) {
			return nil
		}
		return f.viewsOf(x.X)
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
// variables it uses hold where it is made. Calling it reads them.
func (f *fn) funcLitViews(lit *ast.FuncLit) []view {
	var out []view
	seen := map[*types2.Var]bool{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v := f.varOf(id)
		if v == nil || seen[v] || !f.vars[v] || !mayView(v.Type()) {
			return true
		}
		seen[v] = true
		out = compat.Append(out, whole(f.held(v, nil))...)
		return true
	})
	return out
}

// countedVar returns the local variable holding a counted reference
// (shared[T], synced[T]) that the place x is reached through by its first
// dereference, or nil. A view obtained that way is bounded by the
// reference as a view of an owner is (section 18.3).
func (f *fn) countedVar(x ast.Expr) *types2.Var {
	for {
		switch e := ast.Unparen(x).(type) {
		case *ast.SelectorExpr:
			sel := f.c.selections[e]
			if sel == nil || sel.Kind() != types2.FieldVal {
				return nil
			}
			if sel.Indirect() {
				id, ok := ast.Unparen(e.X).(*ast.Ident)
				if !ok || !isCountedBox(f.c.typeOf(e.X)) {
					return nil
				}
				if v := f.varOf(id); v != nil && f.vars[v] {
					return v
				}
				return nil
			}
			x = e.X
		case *ast.IndexExpr:
			if _, isArr := f.c.typeOf(e.X).Underlying().(*types2.Array); !isArr {
				return nil
			}
			x = e.X
		default:
			return nil
		}
	}
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
func bounds(T types2.Type) bool { return isOwner(T) || isCountedBox(T) }

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

// arenaAlloc reports whether call allocates from the local arena a: a
// call of its method New, Slice or Alloc. What it returns is a view of
// the arena's memory, which Reset and Release reclaim.
func (f *fn) arenaAlloc(call *ast.CallExpr, a *types2.Var) bool {
	se := funSelector(call.Fun)
	if se == nil || !isCheckedArena(a.Type()) {
		return false
	}
	switch se.Sel.Name {
	case "New", "Slice", "Alloc":
	default:
		return false
	}
	id, ok := ast.Unparen(se.X).(*ast.Ident)
	return ok && f.varOf(id) == a
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
			out := f.viewsOf(se.X)
			if r := f.countedVar(x); r != nil {
				out = compat.Append(out, view{of: r, pos: x.Pos(), text: exprText(x), inHeap: true})
			}
			return out
		}
		return f.viewsOf(baseOf(x))
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
	if T != nil && types2.IsInterface(T) && !isOwner(T) && isOwner(f.c.typeOf(e)) {
		if from := f.c.typeOf(e); lentWord(from) || types2.IsInterface(from) {
			out = compat.Append(out, f.lentViews(e)...)
		} else {
			out = compat.Append(out, f.selfView(e, e)...)
		}
	}
	return out
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
	heap := T != nil && !types2.IsInterface(T) && lentWord(T)
	for i := range vs {
		if vs[i].itself && vs[i].pos == x.Pos() {
			vs[i].itself = false
			vs[i].inHeap = vs[i].inHeap || heap
		}
	}
	return vs
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
	f.eachArg(call, func(key, val ast.Expr, T types2.Type) {
		af := f.c.calls.Arg(key)
		for _, w := range whole(f.viewsTo(val, T)) {
			w.itself = false // a view obtained from the variable
			if af != nil && !af.Returned {
				if !f.loadsOwned(call, w, T) {
					continue
				}
				w.inHeap = true
			}
			if call.Pos() <= w.pos && w.pos < call.End() {
				// Made by this call (c.Slice() takes &c): name the call. A
				// result loaded through the argument looks at what the
				// owner holds rather than at the owner.
				w.pos, w.text = call.Pos(), exprText(call)
				w.inHeap = w.inHeap || af != nil && !af.Self
			}
			out = compat.Append(out, w)
		}
	})
	return out
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
	if f.arenaAlloc(call, w.of) {
		return true
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
	if se := funSelector(call.Fun); se != nil {
		if sel := f.c.selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
			recv := methodRecv(sel.Obj().(*types2.Func))
			val := se.X
			// A pointer method of what a counted box points to gets the
			// box's object, not the box variable's address (boxView).
			if implicitAddr(f.c, se, recv) && !isCountedBox(f.c.typeOf(se.X)) {
				val = pu.New(ast.UnaryExpr{OpPos: se.X.Pos(), Op: token.AND, X: se.X})
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
		f.pin(vs, lhs.Pos(), compat.Concat("stored in ", exprText(lhs)))
		return
	}
	if f.kept[v] {
		// Something the walk does not follow may use v, which pins the
		// owners of the views of their own storage. The views of what an
		// owner holds on the heap pin nothing, so they are followed here
		// as far as the walk sees v: a function literal that uses v holds
		// them (funcLitViews).
		f.pin(vs, lhs.Pos(), compat.Concat("stored in ", exprText(lhs)))
		var heap []view
		for _, w := range vs {
			if w.inHeap {
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
	how := compat.Concat("stored by ", exprText(call.Fun), " in memory the compiler does not follow")
	if dst != nil {
		how = compat.Concat("stored by ", exprText(call.Fun), " in what ", exprText(dst), " points to")
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
	s.views = sortViews(compat.Append(compat.Clone(s.views), whole(vs)...))
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
		s.pin = pu.New(pinMark{pos: w.pos, text: w.text, where: where, how: how})
		f.setState(w.of, s)
	}
}

// heapPin records that a call may keep the views vs of what owners hold on
// the heap. Moving such an owner is no error, since a move does not
// release that memory, but moving it to another task is: the kept view
// would let this task use memory the other task now owns (section 8.8).
func (f *fn) heapPin(vs []view, where token.Pos, how string) {
	for _, w := range vs {
		s, ok := f.env[w.of]
		if ok && s.hpin != nil || !w.inHeap {
			continue
		}
		s.hpin = pu.New(pinMark{pos: w.pos, text: w.text, where: where, how: how})
		f.setState(w.of, s)
	}
}

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
			var held []view
			for _, w := range a.views {
				w.inHeap = true
				held = compat.Append(held, w)
			}
			f.heapPin(held, a.call.Pos(), f.c.keepHow(a.call, site))
		}
		if af == nil {
			f.pin(a.views, a.call.Pos(), how)
			f.heapPin(a.views, a.call.Pos(), how)
			continue
		}
		if af.Self {
			f.heapPin(a.views, a.call.Pos(), how)
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
	if !bounds(v.Type()) && (mv.kind != mvScopeEnd || !cleans(v.Type())) {
		return
	}
	if p := f.env[v].pin; p != nil && f.report {
		f.c.report(f.c.pinnedDiag(v, p, mv))
	} else if p := f.env[v].hpin; p != nil && crosses(mv) && f.report {
		f.c.report(f.c.heapPinnedDiag(v, p, mv))
	}
	f.staleViews(v, nil, mv)
}

// invalidatePart records that the part of the owner v at path lost its
// value, as take(&v.part) does: the views of that part are stale.
func (f *fn) invalidatePart(v *types2.Var, path Path, mv move) {
	if f.env == nil || !isOwner(v.Type()) || !f.vars[v] {
		return
	}
	f.staleViews(v, path, mv)
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
func (f *fn) reclaim(call *ast.CallExpr) {
	if f.env == nil {
		return
	}
	se := funSelector(call.Fun)
	if se == nil || se.Sel.Name != "Reset" && se.Sel.Name != "Release" {
		return
	}
	id, ok := ast.Unparen(se.X).(*ast.Ident)
	if !ok {
		return
	}
	a := f.varOf(id)
	if a == nil || !f.vars[a] || !isCheckedArena(a.Type()) {
		return
	}
	since := token.NoPos
	if se.Sel.Name == "Release" && len(call.Args) == 1 {
		if mid, ok := ast.Unparen(call.Args[0]).(*ast.Ident); ok {
			since = f.markPos(f.varOf(mid), a)
		}
	}
	mv := move{kind: mvReclaim, pos: call.Pos(), to: exprText(call)}
	f.staleWhere(a, mv, func(w view) bool { return w.inHeap && w.pos > since })
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
