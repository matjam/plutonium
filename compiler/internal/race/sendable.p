// Translated by go2pu from internal/race/sendable.go at the go-cutover tag; edited by hand since.

package race

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strings"
)

// The sendable rule for views (language spec section 19.2). A view crossing
// to another task must point to memory that cannot change while both may
// use it. The analysis knows that of:
//
//   - memory of a synchronised type (sync-only types below): every use is
//     a method that synchronises;
//   - a package variable that is never written once tasks run;
//   - the object of a synced[T], whose fields the guard rule covers;
//   - a struct type that no code in the program changes through a view
//     after making it (immutable types below). Writes through a pointer to
//     memory the function itself has just allocated, before the pointer is
//     stored, passed or returned anywhere, initialise it and do not count.

// A writeSite is where memory of some type is changed through a view.
type writeSite struct {
	pos  token.Pos
	fn   string
	expr string
}

// typeID numbers the referent types of synced[T] for paths.
func (a *analyzer) typeID(T types2.Type) int {
	k := typeKey(T)
	for i, t := range a.typeIDs {
		if typeKey(t) == k {
			return i
		}
	}
	a.typeIDs = compat.Append(a.typeIDs, T)
	return len(a.typeIDs) - 1
}

// mapTypeID numbers a map type for paths: the contents of every map of
// the type are one place of that number (checkMaps). Maps whose types
// mention type parameters share one number, which the check counts with
// every map type.
func (a *analyzer) mapTypeID(m *types2.Map) int {
	if mentionsTypeParam(m) {
		return a.typeID(genericMap)
	}
	return a.typeID(m)
}

// genericMap stands for the map types of generic code.
var genericMap = types2.NewMap(types2.Typ[types2.Invalid], types2.Typ[types2.Invalid])

// mentionsTypeParam reports whether T mentions a type parameter.
func mentionsTypeParam(T types2.Type) bool {
	found := false
	var walk func(T types2.Type, depth int)
	walk = func(T types2.Type, depth int) {
		if found || T == nil || depth > 8 {
			return
		}
		switch t := types2.Unalias(T).(type) {
		case *types2.TypeParam:
			found = true
		case *types2.Named:
			if ta := t.TypeArgs(); ta != nil {
				for i := 0; i < ta.Len(); i++ {
					walk(ta.At(i), depth+1)
				}
			}
		case *types2.Map:
			walk(t.Key(), depth+1)
			walk(t.Elem(), depth+1)
		case *types2.Pointer:
			walk(t.Elem(), depth+1)
		case *types2.Slice:
			walk(t.Elem(), depth+1)
		case *types2.Array:
			walk(t.Elem(), depth+1)
		case *types2.Chan:
			walk(t.Elem(), depth+1)
		case *types2.Struct:
			for i := 0; i < t.NumFields(); i++ {
				walk(t.Field(i).Type(), depth+1)
			}
		case *types2.Wrapper:
			walk(t.Elem(), depth+1)
		}
	}
	walk(T, 0)
	return found
}

// noteMapsIn records the map types a value of type T reaches, through
// fields, elements, pointers, boxes and the types converted to the
// interfaces in it: a value of type T crosses to another task, so maps of
// those types may be shared (checkMaps).
func (a *analyzer) noteMapsIn(T types2.Type, seen map[types2.Type]bool) {
	if T == nil || seen[T] {
		return
	}
	seen[T] = true
	if tp, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		for _, arg := range a.tpArgs[tp] {
			a.noteMapsIn(arg, seen)
		}
		return
	}
	switch u := underlying(T).(type) {
	case *types2.Map:
		a.crossMaps[a.mapTypeID(u)] = true
		a.noteMapsIn(u.Key(), seen)
		a.noteMapsIn(u.Elem(), seen)
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			a.noteMapsIn(u.Field(i).Type(), seen)
		}
	case *types2.Array:
		a.noteMapsIn(u.Elem(), seen)
	case *types2.Slice:
		a.noteMapsIn(u.Elem(), seen)
	case *types2.Pointer:
		a.noteMapsIn(u.Elem(), seen)
	case *types2.Chan:
		a.noteMapsIn(u.Elem(), seen)
	case *types2.Wrapper:
		a.noteMapsIn(u.Elem(), seen)
	case *types2.Interface:
		for _, C := range a.convertedTo(u) {
			a.noteMapsIn(C, seen)
		}
	}
}

// typeKey names a type for the tables of writes: a generic type by its
// origin, so that a write in a method of Vec[T] counts for every Vec.
func typeKey(T types2.Type) string {
	if T == nil {
		return ""
	}
	if n, ok := types2.Unalias(T).(*types2.Named); ok {
		o := n.Origin().Obj()
		if o.Pkg() != nil {
			return compat.Concat(o.Pkg().Path(), ".", o.Name())
		}
		return o.Name()
	}
	return types2.TypeString(T, nil)
}

// noteViewWrite records a write of lhs, at places ps, of type T, when it
// changes memory through a view: memory a pointer or slice reaches, or a
// package variable whose address is taken.
func (w *walker) noteViewWrite(lhs ast.Expr, ps []vpath, T types2.Type) {
	if w.quiet > 0 {
		return
	}
	through := false
	var globals []*types2.Var
	for _, p := range ps {
		loads := strings.ContainsAny(p.path, "*%$#")
		switch p.root.kind {
		case rLocal:
			through = through || loads
		case rFresh:
			if !loads && w.created[p.root.node] && !w.st.pub[p.root.node] && !w.st.pubAll {
				continue // initialising memory this function just allocated
			}
			through = true
		case rGlobal:
			if loads {
				through = true
			} else {
				globals = compat.Append(globals, p.root.v)
			}
		case rParam:
			through = true
		}
	}
	if !through && len(globals) == 0 {
		return
	}
	site := pu.New(writeSite{pos: lhs.Pos(), fn: w.fn.name, expr: exprText(lhs)})
	// A write of a field is the type rule's business (typerule.go); here
	// whole values written through a view are recorded.
	if se, ok := ast.Unparen(lhs).(*ast.SelectorExpr); ok {
		if sel := w.a.selections[se]; sel != nil && sel.Kind() == types2.FieldVal {
			if !isAggregate(T) {
				return
			}
		}
	}
	keys := wholeKeys(T, map[types2.Type]bool{})
	for _, k := range keys {
		if through {
			w.a.noteWrite(k, site)
		}
		for _, g := range globals {
			if w.a.globalWrites[g] == nil {
				w.a.globalWrites[g] = map[string]*writeSite{}
			}
			if w.a.globalWrites[g][k] == nil {
				w.a.globalWrites[g][k] = site
			}
		}
	}
}

func (a *analyzer) noteWrite(k string, site *writeSite) {
	if a.viewWrites[k] == nil {
		a.viewWrites[k] = site
	}
}

// wholeKeys returns the keys of overwriting a whole value of type T: T
// itself and every struct type it holds by value.
func wholeKeys(T types2.Type, seen map[types2.Type]bool) []string {
	if T == nil || seen[T] {
		return nil
	}
	seen[T] = true
	out := compat.Clone([]string{compat.Concat("whole:", typeKey(T))})
	switch u := underlying(T).(type) {
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			out = compat.Append(out, wholeKeys(u.Field(i).Type(), seen)...)
		}
	case *types2.Array:
		out = compat.Append(out, wholeKeys(u.Elem(), seen)...)
	}
	return out
}

// finishWrites adds the writes of package variables whose address is
// taken: a view may reach them.
func (a *analyzer) finishWrites() {
	for g, ks := range a.globalWrites {
		if !a.addrGlobals[g] {
			continue
		}
		for k, site := range ks {
			a.noteWrite(k, site)
		}
	}
}

// isSyncOnly reports whether every use of memory of type T synchronises:
// a synchronised type, a channel, or a struct or array of only those.
func isSyncOnly(T types2.Type) bool {
	return syncOnly(T, map[types2.Type]bool{})
}

func syncOnly(T types2.Type, seen map[types2.Type]bool) bool {
	if T == nil {
		return false
	}
	if isSyncType(T) {
		return true
	}
	if seen[T] {
		return true
	}
	seen[T] = true
	switch u := underlying(T).(type) {
	case *types2.Chan:
		return true
	case *types2.Struct:
		if u.NumFields() == 0 {
			return true
		}
		for i := 0; i < u.NumFields(); i++ {
			if !syncOnly(u.Field(i).Type(), seen) {
				return false
			}
		}
		return true
	case *types2.Array:
		return syncOnly(u.Elem(), seen)
	}
	return false
}

// A problem says why a value is not sendable: the part at fault and why.
type problem struct {
	part  string      // "p", "the field next of p"
	typ   types2.Type // the view's type
	elem  types2.Type // what it points to
	write *writeSite  // a write that changes it, if known
	why   string      // when the use at fault is not a write: what is wrong
	glob  *types2.Var // a package variable it points to that tasks write
	gpos  token.Pos   // where that variable is written
	// shared is a shared[T] that typ, a type converted to the interface
	// part is, holds: its count is not atomic. The type checker cannot see
	// what an interface holds.
	shared types2.Type
	site   *convSite // a conversion of typ to an interface
}

// sendable checks a value of type T with origins vals that crosses to
// another task, and returns the first part that may not cross.
func (a *analyzer) sendable(T types2.Type, vals origins, part string, written func(vpath) token.Pos) *problem {
	a.noteMapsIn(T, map[types2.Type]bool{})
	return a.sendableIn(T, vals, part, written, map[types2.Type]bool{}, false)
}

func (a *analyzer) sendableIn(T types2.Type, vals origins, part string, written func(vpath) token.Pos, seen map[types2.Type]bool, typeOnly bool) *problem {
	if T == nil {
		return nil
	}
	if tp, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		// Generic code: each type the program instantiates it with.
		if seen[T] {
			return nil
		}
		seen[T] = true
		defer delete(seen, T)
		for _, arg := range a.tpArgs[tp] {
			if p := a.sendableIn(arg, nil, compat.Concat(part, " (a ", types2.TypeString(arg, a.qualifier), ")"), written, seen, true); p != nil {
				return p
			}
		}
		return nil
	}
	switch u := underlying(T).(type) {
	case *types2.Basic, *types2.Chan, *types2.Signature:
		return nil
	case *types2.Map:
		// A map crosses as a counted reference; its contents are shared
		// and guarded by the map rule (checkMaps). The views stored in it
		// cross with it.
		if p := a.sendableIn(u.Key(), nil, compat.Concat("a key of ", part), written, seen, true); p != nil {
			return p
		}
		return a.sendableIn(u.Elem(), nil, compat.Concat("an element of ", part), written, seen, true)
	case *types2.Struct:
		if seen[T] {
			return nil
		}
		seen[T] = true
		defer delete(seen, T)
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			sub := compat.Concat("the field ", f.Name(), " of ", part)
			if a.ownsField(T, f) {
				// Memory the owner owns moves with it: only what that
				// memory holds is checked.
				if p := a.sendableIn(viewElem(f.Type()), nil, compat.Concat("an element of ", sub), written, seen, true); p != nil {
					return p
				}
				continue
			}
			if p := a.sendableIn(f.Type(), vals.field(f.Name()), sub, written, seen, typeOnly); p != nil {
				return p
			}
		}
		return nil
	case *types2.Array:
		return a.sendableIn(u.Elem(), vals, compat.Concat("an element of ", part), written, seen, typeOnly)
	case *types2.Wrapper:
		switch u.Kind() {
		case types2.WrapOwned:
			// The object moves with the owner: its own fields are values.
			return a.sendableIn(u.Elem(), nil, compat.Concat("the object of ", part), written, seen, true)
		case types2.WrapSynced, types2.WrapShared:
			// synced crosses; a shared is the type checker's error, or
			// the interface case's below.
			return nil
		}
		return a.sendableIn(u.Elem(), vals, part, written, seen, typeOnly)
	case *types2.Interface:
		for _, C := range a.convertedTo(u) {
			if seen[C] {
				continue
			}
			if bad := types2.Unsendable(C); bad != nil {
				// The type checker cannot see what an interface holds.
				site := a.convSites[types2.TypeString(C, nil)]
				if site == nil {
					continue // only lent from the frame: it never crosses
				}
				return pu.New(problem{part: part, typ: C, shared: bad, site: site})
			}
			seen[C] = true
			p := a.sendableIn(C, nil, compat.Concat(part, " (holding a ", types2.TypeString(C, a.qualifier), ")"), written, seen, true)
			delete(seen, C)
			if p != nil {
				return p
			}
		}
		return nil
	case *types2.Pointer, *types2.Slice:
		E := viewElem(T)
		if isSyncOnly(E) {
			return nil
		}
		// What it points to is safe by the type rule, or by where it
		// points: either way, the views in it must point to safe memory too.
		inner := func() *problem {
			if seen[E] {
				return nil
			}
			seen[E] = true
			p := a.sendableIn(E, nil, compat.Concat("the memory ", part, " points to"), written, seen, true)
			delete(seen, E)
			return p
		}
		var typeSite *writeSite
		var typeWhy string
		if isAggregate(E) {
			if seen[E] {
				return nil
			}
			if typeSite, typeWhy = a.typeProblem(E); typeSite == nil {
				return inner()
			}
		}
		if !typeOnly && len(vals) > 0 {
			if p := a.byOrigin(T, E, vals, part, written); p != nil {
				p.write, p.why = typeSite, typeWhy
				return p
			} else if a.allKnown(vals) {
				return inner()
			}
		}
		if typeSite != nil {
			return pu.New(problem{part: part, typ: T, elem: E, write: typeSite, why: typeWhy})
		}
		return pu.New(problem{part: part, typ: T, elem: E, write: a.viewWrites[compat.Concat("whole:", typeKey(E))]})
	}
	return nil
}

// byOrigin judges a view by where it points: a package variable that tasks
// write is a problem; it returns nil when the origins settle nothing or
// show it is fine.
func (a *analyzer) byOrigin(T, E types2.Type, vals origins, part string, written func(vpath) token.Pos) *problem {
	for _, o := range vals {
		if o.vp.root.kind != rGlobal || strings.ContainsAny(o.vp.path, "*%#$") {
			continue
		}
		// A pointer to the variable (or part of it), or a view the
		// variable holds: what it points to is reached through it.
		place := o.vp
		if place.isAddr() {
			place.path = strings.Slice(place.path, 0, len(place.path)-1)
		} else {
			step := "*"
			if _, ok := underlying(T).(*types2.Slice); ok {
				step = "%"
			}
			place = place.extend(step)
		}
		if pos := written(place); pos.IsValid() {
			return pu.New(problem{part: part, typ: T, elem: E, glob: o.vp.root.v, gpos: pos})
		}
	}
	return nil
}

// allKnown reports whether every origin shows a view that may cross: a
// package variable no task writes (byOrigin has checked), a synced
// object's memory, the frame (the escape analysis's business), or a task's
// own parameter (checked where the task started).
func (a *analyzer) allKnown(vals origins) bool {
	for _, o := range vals {
		switch {
		case strings.Contains(o.vp.path, "$"):
		case o.vp.root.kind == rGlobal && !strings.ContainsAny(o.vp.path, "*%#"):
		case o.vp.root.kind == rLocal, o.vp.root.kind == rParam:
		default:
			return false
		}
	}
	return true
}

// viewElem returns what a pointer or slice type points to.
func viewElem(T types2.Type) types2.Type {
	switch u := underlying(T).(type) {
	case *types2.Pointer:
		return u.Elem()
	case *types2.Slice:
		return u.Elem()
	}
	return nil
}

// ownsField reports whether f, a field of T, is a view field of an owner
// that owns what it points to (vec.Vec's array, hash.Map's table,
// strings.Builder's buffer): moving the owner moves that memory too
// (ownedview.go). Sendability depends only on the types and the memory an
// owner holds (language spec section 8.8).
func (a *analyzer) ownsField(T types2.Type, f *types2.Var) bool {
	return T != nil && f != nil && types2.IsOwner(T) && isView(f.Type()) && a.ownedView(f)
}

// convertedTo returns the types converted to interfaces that implement
// iface.
func (a *analyzer) convertedTo(iface *types2.Interface) []types2.Type {
	var out []types2.Type
	for _, k := range sortedKeys(a.converted) {
		C := a.converted[k]
		if types2.Implements(C, iface) {
			out = compat.Append(out, C)
		}
	}
	return out
}

// conv records a conversion of e's value to type to.
func (w *walker) conv(e ast.Expr, to types2.Type) {
	if e == nil || to == nil || !isInterface(to) {
		return
	}
	from := w.typeOf(e)
	if from == nil || isInterface(from) {
		return
	}
	if b, ok := from.Underlying().(*types2.Basic); ok && b.Info()&types2.IsUntyped != 0 {
		return
	}
	if _, ok := types2.Unalias(from).(*types2.TypeParam); ok {
		return
	}
	if w.a.converted == nil {
		w.a.converted = map[string]types2.Type{}
	}
	if w.a.convSites == nil {
		w.a.convSites = map[string]*convSite{}
	}
	key := types2.TypeString(from, nil)
	w.a.converted[key] = from
	if w.lendsFrame(e, from) || w.a.convSites[key] != nil {
		return
	}
	w.a.convSites[key] = pu.New(convSite{pos: e.Pos(), pkg: w.fn.pkg.Types.Name()})
}

// lendsFrame reports whether converting e, of type from, to an interface
// lends a box that only the frame of the function holds: a box in a local
// variable (or a field of one), or the result of a call, which is a
// temporary. The interface then points at the object without a reference
// of its own, and it can never outlive the statement or the variable's
// scope, so it can never cross to another task (language spec sections 18.3
// and 8.8): fmt.Println(sh) of a shared[T] in a local variable is such a
// conversion.
func (w *walker) lendsFrame(e ast.Expr, from types2.Type) bool {
	if !isBox(from) {
		return false
	}
	if call, ok := ast.Unparen(e).(*ast.CallExpr); ok {
		return !w.a.types[call.Fun].IsType() && !w.a.types[call.Fun].IsBuiltin()
	}
	return w.localPlace(e)
}

// localPlace reports whether the place e is a local variable or a field of
// one that is held by value.
func (w *walker) localPlace(e ast.Expr) bool {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		v, ok := w.a.uses[e].(*types2.Var)
		return ok && !v.IsField() && v.Parent() != nil && v.Pkg() != nil && v.Parent() != v.Pkg().Scope()
	case *ast.SelectorExpr:
		sel := w.a.selections[e]
		return sel != nil && sel.Kind() == types2.FieldVal && !sel.Indirect() && w.localPlace(e.X)
	}
	return false
}

// qualifier writes a type's package as the program would: none for the
// main package's own types.
func (a *analyzer) qualifier(p *types2.Package) string {
	if a.prog.Main != nil && p == a.prog.Main.Types {
		return ""
	}
	return p.Name()
}

// isAggregate reports whether T is a struct or an array: memory whose
// writes the analysis records by type and field.
func isAggregate(T types2.Type) bool {
	switch underlying(T).(type) {
	case *types2.Struct, *types2.Array:
		return true
	}
	return false
}
