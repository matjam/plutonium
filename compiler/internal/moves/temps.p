package moves

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/slices"
	"pluto/strconv"
	"pluto/strings"
)

// Temporaries (section 18.6). An owner that an expression makes and
// nothing stores is dropped at the end of the statement that made it; one
// in the header of an if, switch or for ... range statement at the end of
// the whole statement. A pointer method called on such a temporary takes
// its address, and the views made that way are views of a stand-in variable
// (tempVar) that no code names. A view of a temporary may be passed to the
// calls of its statement and may not outlive it (E3009): endTemps reports
// one that a local variable, a deferred call or memory the walk does not
// follow still holds where the statement ends, and returnTemps one that a
// return statement hands to the caller.
//
// The escape analysis reports a kept address into the temporary's own
// storage (readAll().Slice() of an inline array), so only the views of
// memory the temporary holds on the heap (inHeap) are reported here.

// tempRoot returns the temporary of owner type that the place x is part
// of (mk() for mk().v.Slice()), or nil.
func (f *fn) tempRoot(x ast.Expr) ast.Expr {
	r := ast.Unparen(baseOf(x))
	switch e := r.(type) {
	case *ast.CallExpr:
		if tv := f.c.types[e.Fun]; tv.IsBuiltin() && !isOwner(f.c.typeOf(e)) {
			return nil
		}
	case *ast.CompositeLit:
	case *ast.UnaryExpr:
		if e.Op != token.ARROW {
			return nil
		}
	default:
		return nil
	}
	if !isOwner(f.c.typeOf(r)) {
		return nil
	}
	return r
}

// tempVar returns the stand-in for the temporary root, the same from walk
// to walk.
func (f *fn) tempVar(root ast.Expr) *types2.Var {
	if f.tempVars == nil {
		f.tempVars = map[ast.Node]*types2.Var{}
		f.tempOf = map[*types2.Var]ast.Expr{}
	}
	v := f.tempVars[root]
	if v == nil {
		// Of the temporary's type, so that what it holds on the heap is
		// known (loadsOwned).
		v = types2.NewVar(root.Pos(), nil, exprText(root), f.c.typeOf(root))
		f.tempVars[root] = v
		f.tempOf[v] = root
	}
	return v
}

// tempView returns the view that taking the address of the place x, part
// of a temporary owner, makes (made), or nil when x is not.
func (f *fn) tempView(x ast.Expr, made ast.Expr) []view {
	root := f.tempRoot(x)
	if root == nil {
		return nil
	}
	return compat.Clone([]view{{of: f.tempVar(root), pos: made.Pos(), text: exprText(made)}})
}

// isTemp reports whether v is the stand-in of a temporary.
func (f *fn) isTemp(v *types2.Var) bool {
	_, ok := f.tempOf[v]
	return ok
}

// headerStmt reports whether st is the init or assign statement of an if,
// switch or type switch, whose temporaries live until that statement ends.
func (f *fn) headerStmt(st ast.Stmt) bool {
	return f.headers[st]
}

// markHeader records st as the init statement of a statement whose
// temporaries live until it ends.
func (f *fn) markHeader(st ast.Stmt) {
	if st == nil {
		return
	}
	if f.headers == nil {
		f.headers = map[ast.Stmt]bool{}
	}
	f.headers[st] = true
}

// endTemps ends the temporaries made in st, where st ends: a view of one
// that a variable declared outside st still holds, or that went where the
// walk does not follow, is E3009.
func (f *fn) endTemps(st ast.Stmt) {
	if len(f.tempOf) == 0 || f.headerStmt(st) {
		return
	}
	in := func(t *types2.Var) bool {
		r := f.tempOf[t]
		return r != nil && st.Pos() <= r.Pos() && r.Pos() < st.End()
	}
	if f.env == nil {
		return
	}
	var keys []*types2.Var
	for k := range f.env {
		keys = compat.Append(keys, k)
	}
	slices.SortFunc(keys, func(a, b *types2.Var) int { return int(a.Pos()) - int(b.Pos()) })
	for _, t := range keys {
		if !f.isTemp(t) || !in(t) {
			continue
		}
		s := f.env[t]
		if s.hpin != nil && s.hpin.stored && f.report {
			f.c.report(f.c.tempKeptDiag(f.tempOf[t], s.hpin.where, s.hpin.text, s.hpin.how))
		}
		delete(f.env, t)
	}
	for _, k := range keys {
		s, ok := f.env[k]
		if !ok || f.isTemp(k) {
			continue
		}
		var keep []view
		reported := false
		for _, w := range s.views {
			if w.of == nil || !f.isTemp(w.of) || !in(w.of) {
				keep = compat.Append(keep, w)
				continue
			}
			if reported || w.stale != nil || !w.inHeap || w.lit != nil || f.scopedIn(k, st) || !f.report {
				continue
			}
			reported = true
			f.c.report(f.c.tempKeptDiag(f.tempOf[w.of], st.Pos(), w.text, compat.Concat("kept in ", f.holderName(k))))
		}
		if len(keep) != len(s.views) {
			s.views = keep
			f.setState(k, s)
		}
	}
}

// scopedIn reports whether the scope of the local k ends within st: a
// variable the header of an if declares, which the statement drops.
func (f *fn) scopedIn(k *types2.Var, st ast.Stmt) bool {
	n := f.scopeIn[k]
	return n != nil && st.Pos() <= n.Pos() && n.End() <= st.End()
}

// holderName names the variable k for a diagnostic: a deferred call's
// stand-in by what the call keeps.
func (f *fn) holderName(k *types2.Var) string {
	for key, v := range f.deferVars {
		if v == k {
			return compat.Concat("the deferred call's ", exprText(key.(ast.Expr)))
		}
	}
	return k.Name()
}

// returnTemps reports a result of the return statement st that holds a
// view of what a temporary of the statement holds on the heap: the
// temporary is dropped before the caller can use it (E3009).
func (f *fn) returnTemps(st *ast.ReturnStmt) {
	if !f.report || f.env == nil {
		return
	}
	for _, r := range st.Results {
		for _, w := range f.viewsOf(r) {
			if w.of != nil && f.isTemp(w.of) && w.inHeap && w.stale == nil && w.lit == nil {
				f.c.report(f.c.tempKeptDiag(f.tempOf[w.of], r.Pos(), w.text, "returned"))
				return
			}
		}
	}
}

// tempKeptDiag explains a view, made by text, of what the temporary root
// holds, kept past its statement at where, as how says.
func (c *checker) tempKeptDiag(root ast.Expr, where token.Pos, text, how string) *diag.Diagnostic {
	tmp := exprText(root)
	d := c.newDiag(where, diag.TempViewKept, "%s is %s, but it points into memory the temporary %s owns, which is dropped at the end of the statement", pu.Box(text), pu.Box(how), pu.Box(tmp))
	d.Text = compat.Sprintfb("%s looks at memory that the %s returned by %s holds, without owning it. Nothing keeps that temporary after its statement: it is dropped there, and its memory is freed. A view of a temporary owner may be passed to calls in its statement but may not outlive it (section 18.3, Views; section 18.6, Drops).", compat.ArgS(text), compat.ArgS(c.typeName(c.typeOf(root))), compat.ArgS(tmp))
	d.Fix = pu.New(diag.Fix{Intro: "Keep the owner in a variable, so that it lives until the end of its scope, and make the view from the variable", Code: compat.Concat("v := ", tmp)})
	return d
}

// dropOrder reports, where the variables in vs are dropped, a variable
// declared before one of them whose own drop would read or write through
// a view of it: the drops run in reverse order of declaration, so the
// holder's drop would come after (section 18.3, E3313 at the holder's
// declaration).
func (f *fn) dropOrder(vs []*types2.Var) {
	if !f.report || f.env == nil || len(vs) == 0 {
		return
	}
	var hs []*types2.Var
	for h := range f.env {
		hs = compat.Append(hs, h)
	}
	slices.SortFunc(hs, func(a, b *types2.Var) int { return int(a.Pos()) - int(b.Pos()) })
	for _, h := range hs {
		s := f.env[h]
		if !f.vars[h] || s.st != live || s.dd || slices.Contains(f.results, h) {
			continue
		}
		for _, w := range s.views {
			if w.stale != nil || w.lit != nil || w.of == nil || w.of == h || h.Pos() >= w.of.Pos() || !slices.Contains(vs, w.of) {
				continue
			}
			viewed, ok := typeAt(w.of.Type(), w.part)
			if !ok {
				viewed = nil
			}
			if !dropTouches(h.Type(), w.at, viewed) {
				continue
			}
			f.c.report(f.c.dropOrderDiag(h, w))
			break
		}
	}
}

// exitDropOrder is dropOrder for the variables of the function, which a
// return drops.
func (f *fn) exitDropOrder() {
	var vs []*types2.Var
	for v := range f.vars {
		if !slices.Contains(f.results, v) {
			vs = compat.Append(vs, v)
		}
	}
	f.dropOrder(vs)
}

// dropOrderDiag explains the holder h, declared before the owner of the
// view w it holds, whose drop uses w.
func (c *checker) dropOrderDiag(h *types2.Var, w view) *diag.Diagnostic {
	d := c.newDiag(h.Pos(), diag.StaleView, "%s is declared before %s, but it holds a view of %s (%s, line %d) that its drop uses, and %s is dropped first", pu.Box(h.Name()), pu.Box(w.of.Name()), pu.Box(w.of.Name()), pu.Box(w.text), c.line(w.pos), pu.Box(w.of.Name()))
	d.Text = compat.Sprintfb("Variables are dropped in reverse order of declaration, so %s is dropped before %s. Dropping %s reads or releases what it holds, and the view made by %s at line %d would then look at memory %s has freed. A variable whose drop releases or writes through a view of another local variable must be declared after that variable (section 18.3).", compat.ArgS(w.of.Name()), compat.ArgS(h.Name()), compat.ArgS(h.Name()), compat.ArgS(w.text), compat.ArgI(int64(c.line(w.pos))), compat.ArgS(w.of.Name()))
	d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Declare %s after %s", compat.ArgS(h.Name()), compat.ArgS(w.of.Name()))})
	return d
}

// dropTouches reports whether dropping a value of type H may read or write
// through a view it holds at the path at, of a value of type viewed (nil
// when not known). The drop glue of a type with a Drop method runs that
// method on the whole value; a generic type's Drop reaches its type
// arguments' values only by their own drop glue (section 18.3, D81).
func dropTouches(H types2.Type, at Path, viewed types2.Type) bool {
	T := H
	for _, step := range at {
		if T == nil {
			return true
		}
		if hasDrop(T) {
			return dropWhole(T, viewed, map[types2.Type]bool{})
		}
		switch u := T.Underlying().(type) {
		case *types2.Struct:
			k, ok := fieldIndex(step)
			if !ok || k >= u.NumFields() {
				return dropWhole(T, viewed, map[types2.Type]bool{})
			}
			T = u.Field(k).Type()
		case *types2.Array:
			T = u.Elem()
		default:
			return dropWhole(T, viewed, map[types2.Type]bool{})
		}
	}
	return dropWhole(T, viewed, map[types2.Type]bool{})
}

// fieldIndex parses a path step ".k".
func fieldIndex(step string) (int, bool) {
	k, err := strconv.Atoi(strings.SliceFrom(step, 1))
	if !strings.HasPrefix(step, ".") || err != nil || k < 0 {
		return 0, false
	}
	return k, true
}

// dropWhole reports whether dropping a value of type T may read or write
// through a view it holds anywhere, of a value of type viewed.
func dropWhole(T types2.Type, viewed types2.Type, seen map[types2.Type]bool) bool {
	if T == nil {
		return true
	}
	if seen[T] {
		return false
	}
	seen[T] = true
	if tp, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		return tpMay(tp, func(t types2.Type) bool { return dropWhole(t, viewed, seen) })
	}
	if hasDrop(T) {
		return genericDrop(T, viewed, seen)
	}
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		return u.Info()&types2.IsString != 0 && releasesInto(viewed)
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if dropWhole(u.Field(i).Type(), viewed, seen) {
				return true
			}
		}
	case *types2.Array:
		return u.Len() > 0 && dropWhole(u.Elem(), viewed, seen)
	case *types2.Wrapper:
		switch u.Kind() {
		case types2.WrapAligned, types2.WrapVolatile, types2.WrapRing:
			return dropWhole(u.Elem(), viewed, seen)
		}
		if u.IsBox() {
			return releasesInto(viewed) || dropWhole(u.Elem(), viewed, seen)
		}
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants(); i++ {
			if v := u.Variant(i); v.HasPayload() && dropWhole(v.Payload(), viewed, seen) {
				return true
			}
		}
	case *types2.Map:
		return releasesInto(viewed) || dropWhole(u.Key(), viewed, seen) || dropWhole(u.Elem(), viewed, seen)
	case *types2.Chan:
		return releasesInto(viewed) || dropWhole(u.Elem(), viewed, seen)
	case *types2.Interface:
		if isOwner(T) {
			return true // the dynamic value's Drop
		}
		return types2.IsCountedError(T) && releasesInto(viewed)
	}
	return false
}

// genericDrop reports whether the Drop method of T may read or write
// through a view of a value of type viewed that T holds. A Drop of a type
// that is not generic may use any of it. A generic type's Drop is the same
// code for every type argument: it reaches the type arguments' values only
// by their drop glue, unless a constraint gives them methods, and its other
// fields as their types allow.
func genericDrop(T types2.Type, viewed types2.Type, seen map[types2.Type]bool) bool {
	n, ok := types2.Unalias(T).(*types2.Named)
	if !ok || n.TypeArgs().Len() == 0 {
		return true
	}
	orig := n.Origin()
	st, ok := orig.Underlying().(*types2.Struct)
	if !ok {
		return true
	}
	tps := orig.TypeParams()
	for i := 0; i < tps.Len(); i++ {
		if it, ok := tps.At(i).Constraint().Underlying().(*types2.Interface); !ok || it.NumMethods() > 0 {
			if mayView(n.TypeArgs().At(i)) {
				return true
			}
		}
	}
	for i := 0; i < st.NumFields(); i++ {
		F := st.Field(i).Type()
		if mentionsTypeParam(F) {
			continue
		}
		if it, ok := F.Underlying().(*types2.Interface); ok {
			if it.NumMethods() == 0 || viewed == nil || types2.Implements(viewed, it) || types2.Implements(types2.NewPointer(viewed), it) {
				return true
			}
			continue
		}
		if mayView(F) || dropWhole(F, viewed, seen) {
			return true
		}
	}
	for i := 0; i < n.TypeArgs().Len(); i++ {
		if dropWhole(n.TypeArgs().At(i), viewed, seen) {
			return true
		}
	}
	return false
}

// mentionsTypeParam reports whether the type T is written with a type
// parameter.
func mentionsTypeParam(T types2.Type) bool {
	found := false
	var walk func(T types2.Type, seen map[types2.Type]bool)
	walk = func(T types2.Type, seen map[types2.Type]bool) {
		if T == nil || found || seen[T] {
			return
		}
		seen[T] = true
		switch u := types2.Unalias(T).(type) {
		case *types2.TypeParam:
			found = true
		case *types2.Named:
			for i := 0; i < u.TypeArgs().Len(); i++ {
				walk(u.TypeArgs().At(i), seen)
			}
		case *types2.Pointer:
			walk(u.Elem(), seen)
		case *types2.Slice:
			walk(u.Elem(), seen)
		case *types2.Array:
			walk(u.Elem(), seen)
		case *types2.Map:
			walk(u.Key(), seen)
			walk(u.Elem(), seen)
		case *types2.Chan:
			walk(u.Elem(), seen)
		case *types2.Wrapper:
			walk(u.Elem(), seen)
		case *types2.Struct:
			for i := 0; i < u.NumFields(); i++ {
				walk(u.Field(i).Type(), seen)
			}
		}
	}
	walk(T, map[types2.Type]bool{})
	return found
}

// releasesInto reports whether releasing a counted value may write into
// the memory of a value of type viewed: a checked arena or another value
// with the methods of an allocator, in whose memory the count may live.
// Unknown is yes.
func releasesInto(viewed types2.Type) bool {
	if viewed == nil || isCheckedArena(viewed) {
		return true
	}
	ms := types2.NewMethodSet(types2.NewPointer(viewed))
	return ms.Lookup(nil, "Alloc") != nil && ms.Lookup(nil, "Free") != nil
}
