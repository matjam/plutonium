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

// Calls of function values (language spec section 18.3, A function
// literal, and "How precise the frame check is").
//
// A function literal reads the local variables it uses when it is called,
// not where it is made. Calling it is a use of each of them, and what its
// body does to them happens at the call:
//
//	p := v.Ptr(0)
//	f := func() { v.Free() }
//	f()           // ends p, as v.Free() would here
//	println(*p)   // E3313
//
// The walk follows the literal as a view of each variable it uses
// (view.lit, funcLitViews), held by the variables the literal value is
// copied to. At a call through a func value it finds the literals the
// value may hold, by those views and by the escape analysis's candidates
// for the call, and applies what each literal's body may do to the
// variables it uses (litEffects). A method value (g := v.Free) holds a
// view of its receiver; calling it may free through the receiver as the
// method may. What the walk cannot follow counts as the worst: a func value
// it knows nothing of may free through every view it holds.

// litResultViews returns the views the result of calling a function
// literal may hold through the variable u it uses: what u holds at the
// call, and a view obtained from u itself (section 18.3). called is the
// call.
func (f *fn) litResultViews(call *ast.CallExpr, u *types2.Var) []view {
	var out []view
	for _, w := range whole(f.held(u, nil)) {
		if w.lit != nil {
			continue // a literal u holds is passed on by its own views
		}
		w.itself = false
		out = compat.Append(out, w)
	}
	at, text := call.Pos(), exprText(call)
	switch {
	case f.ownerVar(u) != nil || isCountedBox(u.Type()) && f.vars[u]:
		// What u owns on the heap, or u's own storage.
		out = compat.Append(out, view{of: u, pos: at, text: text, inHeap: true})
		out = compat.Append(out, view{of: u, pos: at, text: text})
	case f.scopeVar(u) != nil:
		out = compat.Append(out, view{of: u, pos: at, text: text})
	}
	return out
}

// plainValue reports whether a value of type T holds no reference at all:
// a number, a bool or a string, or a tuple of them.
func plainValue(T types2.Type) bool {
	if T == nil {
		return false
	}
	if tu, ok := T.(*types2.Tuple); ok {
		for i := 0; i < tu.Len(); i++ {
			if !plainValue(tu.At(i).Type()) {
				return false
			}
		}
		return true
	}
	if _, tp := types2.Unalias(T).(*types2.TypeParam); tp {
		return false
	}
	b, ok := T.Underlying().(*types2.Basic)
	return ok && b.Kind() != types2.UnsafePointer
}

// litReturns reports whether what calling lit returns may hold the
// variable u it uses, or something obtained from u. The body is followed
// from u through the literal's own variables to its return statements; a
// value obtained from u that goes anywhere else, or into a call that may
// store it, counts as returned (section 18.3).
func (f *fn) litReturns(lit *ast.FuncLit, u *types2.Var) bool {
	own := func(v *types2.Var) bool { return v != nil && v != u && lit.Pos() <= v.Pos() && v.Pos() < lit.End() }
	from := map[*types2.Var]bool{u: true}
	var carries, mentions func(e ast.Expr) bool
	carries = func(e ast.Expr) bool {
		if e == nil || plainValue(f.c.typeOf(e)) {
			return false
		}
		return mentions(e)
	}
	mentions = func(e ast.Expr) bool {
		switch x := ast.Unparen(e).(type) {
		case *ast.Ident:
			v := f.varOf(x)
			return v != nil && from[v]
		case *ast.BasicLit:
			return false
		case *ast.SelectorExpr:
			if f.c.selections[x] == nil {
				return mentions(x.Sel)
			}
			return mentions(x.X)
		case *ast.IndexExpr:
			return mentions(x.X)
		case *ast.IndexListExpr:
			return mentions(x.X)
		case *ast.SliceExpr:
			return mentions(x.X)
		case *ast.StarExpr:
			return mentions(x.X)
		case *ast.UnaryExpr:
			return mentions(x.X)
		case *ast.TypeAssertExpr:
			return mentions(x.X)
		case *ast.BinaryExpr:
			return carries(x.X) || carries(x.Y)
		case *ast.KeyValueExpr:
			return carries(x.Value)
		case *ast.CompositeLit:
			for _, el := range x.Elts {
				if carries(el) {
					return true
				}
			}
			return false
		case *ast.CallExpr:
			tv := f.c.types[x.Fun]
			if !tv.IsType() && !tv.IsBuiltin() {
				if f.c.staticCallee(x) == nil {
					return true // a func value or an interface method
				}
				if se, ok := ast.Unparen(x.Fun).(*ast.SelectorExpr); ok && f.c.selections[se] != nil && mentions(se.X) {
					return true
				}
			}
			for _, a := range x.Args {
				if carries(a) {
					return true
				}
			}
			return false
		}
		return true
	}
	// rootOwn returns the literal's own variable whose storage e is, or nil.
	rootOwn := func(e ast.Expr) *types2.Var {
		for {
			switch x := ast.Unparen(e).(type) {
			case *ast.Ident:
				if v := f.varOf(x); own(v) {
					return v
				}
				return nil
			case *ast.SelectorExpr:
				sel := f.c.selections[x]
				if sel == nil || sel.Kind() != types2.FieldVal || sel.Indirect() {
					return nil
				}
				e = x.X
			case *ast.IndexExpr:
				T := f.c.typeOf(x.X)
				if T == nil {
					return nil
				}
				if _, ok := T.Underlying().(*types2.Array); !ok {
					return nil
				}
				e = x.X
			default:
				return nil
			}
		}
	}
	lost, returned, changed := false, false, false
	mark := func(lhs ast.Expr) {
		if id, ok := ast.Unparen(lhs).(*ast.Ident); ok && id.Name == "_" || plainValue(f.c.typeOf(lhs)) {
			return
		}
		v := rootOwn(lhs)
		if v == nil {
			lost = true
			return
		}
		if !from[v] {
			from[v], changed = true, true
		}
	}
	assign := func(lhs, rhs []ast.Expr) {
		if len(lhs) == len(rhs) {
			for i := range lhs {
				if carries(rhs[i]) {
					mark(lhs[i])
				}
			}
			return
		}
		for _, r := range rhs {
			if carries(r) {
				for _, l := range lhs {
					mark(l)
				}
			}
		}
	}
	// stores follows a call given something obtained from u: it may store
	// that into memory any of its other inputs reaches.
	stores := func(call *ast.CallExpr) {
		tv := f.c.types[call.Fun]
		if tv.IsType() {
			return
		}
		if tv.IsBuiltin() {
			id, _ := ast.Unparen(call.Fun).(*ast.Ident)
			for _, a := range call.Args {
				if carries(a) && (id == nil || id.Name == "panic" || id.Name == "copy") {
					lost = true // append, len, min and the rest store into nothing
				}
			}
			return
		}
		static := f.c.staticCallee(call) != nil
		se := funSelector(call.Fun)
		f.eachArg(call, func(key, val ast.Expr, _ types2.Type) {
			if lost || !carries(val) {
				return
			}
			af := f.c.calls.Arg(key)
			if !static || af == nil || af.Kept {
				lost = true // a func value may store into anything it holds
				return
			}
			if _, kept := f.c.calleeKeeps(call, key); kept {
				lost = true
				return
			}
			for _, in := range af.Into {
				if in.Arg == nil {
					lost = true
					continue
				}
				switch ad, _ := ast.Unparen(in.Arg).(*ast.UnaryExpr); {
				case ad != nil && ad.Op == token.AND && rootOwn(ad.X) != nil:
					mark(ad.X)
				case se != nil && in.Arg == se.X && rootOwn(se.X) != nil:
					mark(se.X) // the receiver, whose address a pointer method takes
				default:
					lost = true
				}
			}
		})
	}
	bare := false
	for {
		changed = false
		ast.Inspect(lit.Body, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				assign(s.Lhs, s.Rhs)
			case *ast.ValueSpec:
				var lhs []ast.Expr
				for _, id := range s.Names {
					lhs = compat.Append(lhs, ast.Expr(id))
				}
				assign(lhs, s.Values)
			case *ast.RangeStmt:
				if carries(s.X) {
					if s.Key != nil {
						mark(s.Key)
					}
					if s.Value != nil {
						mark(s.Value)
					}
				}
			case *ast.SendStmt:
				lost = lost || carries(s.Value)
			case *ast.ReturnStmt:
				bare = bare || len(s.Results) == 0
				for _, r := range s.Results {
					returned = returned || carries(r)
				}
			case *ast.CallExpr:
				stores(s)
			}
			return true
		})
		if lost || returned {
			return true
		}
		if !changed {
			break
		}
	}
	if bare && lit.Type.Results != nil {
		for _, fl := range lit.Type.Results.List {
			for _, id := range fl.Names {
				if v := f.varOf(id); v != nil && from[v] {
					return true
				}
			}
		}
	}
	return false
}

// A litEffect is what calling a function value may do to a local variable
// of this function: free memory or move an owner out of the part at part
// (nil for all of it), or reset or release the checked arena there.
type litEffect struct {
	v       *types2.Var
	part    Path
	reclaim bool
	site    keepSite
}

// funcValueFrees returns what calling the func values among call's callee
// and arguments may do to the local variables of this function, before
// the call is walked: a literal's body, by litEffects; a method value's
// method, through its receiver. An argument of function type counts as
// called, since the callee may call it.
func (f *fn) funcValueFrees(call *ast.CallExpr) []litEffect {
	if f.env == nil {
		return nil
	}
	tv := f.c.types[call.Fun]
	if tv.IsType() || tv.IsBuiltin() || f.c.scoped.Tasks[call] != nil || f.isImplicitDrop(call) {
		return nil
	}
	var out []litEffect
	seen := map[*ast.FuncLit]bool{}
	static := f.c.staticCallee(call) != nil
	f.eachArg(call, func(key, val ast.Expr, T types2.Type) {
		if key == call.Fun && static {
			return
		}
		if key != call.Fun {
			// A receiver whose address the call takes is a new &x with no
			// recorded type: a pointer, not a func value.
			VT := f.c.typeOf(val)
			if VT == nil {
				return
			}
			if _, isFunc := VT.Underlying().(*types2.Signature); !isFunc {
				return
			}
		}
		out = compat.Append(out, f.funcValueEffects(call, val, key == call.Fun, seen)...)
	})
	return out
}

// funcValueEffects returns what calling the func value val may do, at
// call: val is call's callee when fun is set, an argument otherwise.
func (f *fn) funcValueEffects(call *ast.CallExpr, val ast.Expr, fun bool, seen map[*ast.FuncLit]bool) []litEffect {
	site := keepSite{pos: call.Pos(), text: exprText(call)}
	vs := f.viewsOf(val)
	var out []litEffect
	lits := false
	for _, w := range vs {
		if w.lit != nil {
			lits = true
			out = compat.Append(out, f.litEffects(w.lit, seen)...)
		}
	}
	kept := false
	if id, ok := ast.Unparen(val).(*ast.Ident); ok && fun {
		if v, ok := f.c.uses[id].(*types2.Var); ok && f.kept[v] {
			// Something the walk does not follow, such as another
			// literal, may store a different literal in v.
			kept = true
		}
	}
	if fun && (!lits || kept) {
		// The value's views were lost (stored through a pointer): the
		// escape analysis's candidates name the literals of this function
		// it may hold.
		for _, lit := range f.candidateLits(call) {
			out = compat.Append(out, f.litEffects(lit, seen)...)
		}
	}
	if lits {
		// The other views are what the literal's variables hold, which
		// its body uses as litEffects says.
		return out
	}
	// A method value holds a view of its receiver, and the method may free
	// or reclaim through it.
	frees, reclaims := f.methodValueEffects(call, val, fun)
	if !frees && !reclaims {
		return out
	}
	for _, w := range vs {
		if w.of == nil || !f.vars[w.of] || !bounds(w.of.Type()) {
			continue
		}
		if frees && !isCheckedArena(w.of.Type()) && (w.inHeap || !isArenaPart(w.of.Type(), w.part)) {
			out = compat.Append(out, litEffect{v: w.of, part: w.part, site: site})
		}
		if reclaims && (isCheckedArena(w.of.Type()) || (!w.inHeap || w.obj || isOwnedBox(w.of.Type())) && arenaAt(w.of.Type(), w.part)) {
			out = compat.Append(out, litEffect{v: w.of, part: w.part, reclaim: true, site: site})
		}
	}
	return out
}

// methodValueEffects reports whether calling val, a method value or a
// func value of which nothing is known, may free or reclaim through the
// receiver it holds: by the method's summary when val names the method
// (x.M passed as an argument) or the candidates of call name it, and
// otherwise both.
func (f *fn) methodValueEffects(call *ast.CallExpr, val ast.Expr, fun bool) (bool, bool) {
	if se, ok := ast.Unparen(val).(*ast.SelectorExpr); ok && !fun {
		if sel := f.c.selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
			m, _ := sel.Obj().(*types2.Func)
			return f.methodEffects(m, se)
		}
	}
	if !fun {
		return true, true
	}
	cs, ok := f.c.calls.Callees(call)
	if !ok || len(cs) == 0 {
		return true, true
	}
	frees, reclaims := false, false
	for _, cd := range cs {
		switch {
		case cd.Unknown:
			return true, true
		case cd.Fn != nil:
			if cd.Shift < 1 {
				continue // not a method value: it holds no receiver
			}
			a, b := f.methodEffects(cd.Fn, nil)
			frees, reclaims = frees || a, reclaims || b
		case cd.Lit == nil && cd.Range == nil:
			return true, true // a function without a body or a name
		}
	}
	return frees, reclaims
}

// methodEffects reports whether the method m may free or reclaim through
// its receiver. se is the selector naming it, if known.
func (f *fn) methodEffects(m *types2.Func, se *ast.SelectorExpr) (bool, bool) {
	if m == nil {
		return true, true
	}
	if o := m.Origin(); o != nil {
		m = o
	}
	d := f.c.keepDecls[m]
	if d == nil {
		// A method without a body: a checked arena's Reset and Release
		// reclaim; of the others only the contracts say (bodyless).
		if m.Name() == "Reset" || m.Name() == "Release" {
			if r := methodRecv(m); r != nil {
				T := r
				if p := pointerElem(T); p != nil {
					T = p
				}
				if isCheckedArena(T) {
					return false, true
				}
			}
		}
		return true, true
	}
	frees := len(f.c.frees[d]) > 0 && f.c.frees[d][0].valid()
	reclaims := len(f.c.reclaims[d]) > 0 && f.c.reclaims[d][0].valid()
	if !reclaims && (m.Name() == "Reset" || m.Name() == "Release") {
		if r := methodRecv(m); r != nil {
			T := r
			if p := pointerElem(T); p != nil {
				T = p
			}
			reclaims = isCheckedArena(T)
		}
	}
	return frees, reclaims
}

// candidateLits returns the function literals of this function that the
// escape analysis says call may reach.
func (f *fn) candidateLits(call *ast.CallExpr) []*ast.FuncLit {
	cs, ok := f.c.calls.Callees(call)
	if !ok || f.topBody == nil {
		return nil
	}
	var out []*ast.FuncLit
	for _, cd := range cs {
		if cd.Lit != nil && f.topBody.Pos() <= cd.Lit.Pos() && cd.Lit.End() <= f.topBody.End() && f.c.scoped.Lits[cd.Lit] == nil {
			out = compat.Append(out, cd.Lit)
		}
	}
	return out
}

// litEffects returns what calling the function literal lit may do to the
// local variables of this function it uses, directly or through the
// literals and method values they hold: the frees, resets and releases of
// its body through them (calleeFrees, calleeReclaims), the owners it gives
// a new value or empties (an assignment, take, clear, delete), and what
// the func values it calls or passes on may do. A nested literal counts as
// called. A variable of the literal's own that may hold one of the
// function's counts as that variable.
func (f *fn) litEffects(lit *ast.FuncLit, seen map[*ast.FuncLit]bool) []litEffect {
	if seen[lit] || f.c.scoped.Lits[lit] != nil {
		return nil
	}
	seen[lit] = true
	// What the literal's own variables may hold of the function's, by
	// what is assigned to them, to a fixpoint.
	alias := map[*types2.Var][]*types2.Var{}
	addAlias := func(to *types2.Var, from ast.Expr) bool {
		changed := false
		for _, v := range f.litMentions(from, alias) {
			if !containsVar(alias[to], v) {
				alias[to] = compat.Append(alias[to], v)
				changed = true
			}
		}
		return changed
	}
	for changed := true; changed; {
		changed = false
		ast.Inspect(lit.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for i, l := range x.Lhs {
					id, ok := ast.Unparen(l).(*ast.Ident)
					if !ok {
						continue
					}
					to, _ := f.c.objOf(id).(*types2.Var)
					if to == nil || f.vars[to] {
						continue
					}
					if len(x.Lhs) == len(x.Rhs) {
						changed = addAlias(to, x.Rhs[i]) || changed
					} else {
						for _, r := range x.Rhs {
							changed = addAlias(to, r) || changed
						}
					}
				}
			case *ast.ValueSpec:
				for _, name := range x.Names {
					to, _ := f.c.objOf(name).(*types2.Var)
					if to == nil {
						continue
					}
					for _, r := range x.Values {
						changed = addAlias(to, r) || changed
					}
				}
			case *ast.RangeStmt:
				for _, l := range []ast.Expr{x.Key, x.Value} {
					if id, ok := ast.Unparen(l).(*ast.Ident); ok && l != nil {
						if to, _ := f.c.objOf(id).(*types2.Var); to != nil && !f.vars[to] {
							changed = addAlias(to, x.X) || changed
						}
					}
				}
			}
			return true
		})
	}
	var out []litEffect
	// effect adds what an action of kind k on the variables e mentions
	// does: a free or a reclaim through them, or a call of the func values
	// they hold.
	const (
		kFree = iota
		kReclaim
		kCall
	)
	effect := func(e ast.Expr, k int, at ast.Node) {
		site := keepSite{pos: at.Pos(), text: exprText(e)}
		if c, ok := at.(*ast.CallExpr); ok {
			site.text = exprText(c)
		}
		for _, x := range f.litMentions(e, alias) {
			out = compat.Append(out, f.varEffects(x, f.mentionPart(e, x), k == kReclaim, k == kCall, site, seen)...)
		}
	}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			tv := f.c.types[x.Fun]
			if tv.IsType() {
				return true
			}
			if tv.IsBuiltin() {
				id, _ := ast.Unparen(x.Fun).(*ast.Ident)
				if id != nil && (id.Name == "take" || id.Name == "clear" || id.Name == "delete") && len(x.Args) > 0 {
					effect(x.Args[0], kFree, x)
				}
				return true
			}
			if f.c.scoped.Tasks[x] != nil {
				return true
			}
			f.c.callArgs(x, func(key, val ast.Expr, j int) {
				if _, ok := f.c.calleeFrees(x, key); ok {
					effect(val, kFree, x)
				}
				if _, ok := f.c.calleeReclaims(x, key); ok {
					effect(val, kReclaim, x)
				}
				if _, isFunc := f.c.typeOf(val).Underlying().(*types2.Signature); isFunc {
					effect(val, kCall, x)
				}
			})
			if se := funSelector(x.Fun); se != nil {
				if sel := f.c.selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
					return true
				}
			}
			if f.c.staticCallee(x) == nil {
				effect(x.Fun, kCall, x)
			}
		case *ast.AssignStmt:
			if x.Tok != token.DEFINE {
				for _, l := range x.Lhs {
					if f.newValueFrees(l) {
						effect(l, kFree, x)
					}
				}
			}
		case *ast.RangeStmt:
			if x.Tok == token.ASSIGN {
				for _, l := range []ast.Expr{x.Key, x.Value} {
					if l != nil && f.newValueFrees(l) {
						effect(l, kFree, x)
					}
				}
			}
		}
		return true
	})
	return out
}

// newValueFrees reports whether giving the place l, in a function
// literal, a new value may free what the function's variables hold: l has
// drop glue and is not a variable of the literal's own, which a new value
// only rebinds.
func (f *fn) newValueFrees(l ast.Expr) bool {
	T := f.c.typeOf(l)
	if T == nil || !bounds(T) && !cleans(T) {
		return false
	}
	if id, ok := ast.Unparen(l).(*ast.Ident); ok {
		v, _ := f.c.objOf(id).(*types2.Var)
		return v != nil && f.vars[v]
	}
	return true
}

// varEffects returns what an action on the local variable x of this
// function at the part part does: through x itself if it bounds what it
// holds, and through each view x holds. A literal x holds counts by what
// its body does when call is set or the view is used to call it.
func (f *fn) varEffects(x *types2.Var, part Path, reclaim, call bool, site keepSite, seen map[*ast.FuncLit]bool) []litEffect {
	var out []litEffect
	held := f.held(x, nil)
	lits := false
	for _, w := range held {
		if w.lit != nil {
			lits = true
			out = compat.Append(out, f.litEffects(w.lit, seen)...)
		}
	}
	if call && lits {
		return out
	}
	if !call && bounds(x.Type()) {
		out = compat.Append(out, litEffect{v: x, part: part, reclaim: reclaim, site: site})
	}
	for _, w := range held {
		if w.lit != nil || w.of == nil {
			continue
		}
		out = compat.Append(out, litEffect{v: w.of, part: w.part, reclaim: reclaim, site: site})
		if call {
			// A method value of which nothing is known may also reclaim.
			out = compat.Append(out, litEffect{v: w.of, part: w.part, reclaim: true, site: site})
		}
	}
	if f.kept[x] && !bounds(x.Type()) && f.env != nil {
		// The walk keeps only some of the views of a variable that
		// something it does not follow uses (storeViews): x may point at
		// any owner whose views went where the walk cannot follow them,
		// each of which is pinned (section 18.3).
		var keys []*types2.Var
		for k, s := range f.env {
			if s.pin != nil && f.vars[k] && bounds(k.Type()) {
				keys = compat.Append(keys, k)
			}
		}
		slices.SortFunc(keys, func(a, b *types2.Var) int { return int(a.Pos()) - int(b.Pos()) })
		for _, k := range keys {
			out = compat.Append(out, litEffect{v: k, reclaim: reclaim, site: site})
			if call {
				out = compat.Append(out, litEffect{v: k, reclaim: true, site: site})
			}
		}
	}
	return out
}

// mentionPart returns the part of the variable x that the place e is, when
// e is x or a part of it by field selections and array indexing, or nil.
func (f *fn) mentionPart(e ast.Expr, x *types2.Var) Path {
	if u, ok := ast.Unparen(e).(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	v, path, box, ok := f.placePath(e)
	if !ok || box || v != x {
		return nil
	}
	return path
}

// litMentions returns the local variables of this function that e names,
// directly or through the variables of a literal that may hold them
// (alias).
func (f *fn) litMentions(e ast.Expr, alias map[*types2.Var][]*types2.Var) []*types2.Var {
	if e == nil {
		return nil
	}
	var out []*types2.Var
	ast.Inspect(e, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false // its body is scanned as called (litEffects)
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, _ := f.c.objOf(id).(*types2.Var)
		if v == nil {
			return true
		}
		if f.vars[v] {
			if !containsVar(out, v) {
				out = compat.Append(out, v)
			}
			return true
		}
		for _, u := range alias[v] {
			if !containsVar(out, u) {
				out = compat.Append(out, u)
			}
		}
		return true
	})
	return out
}

func containsVar(vs []*types2.Var, v *types2.Var) bool {
	for _, u := range vs {
		if u == v {
			return true
		}
	}
	return false
}

// funcValueViews applies what calls of func values did (funcValueFrees),
// once call has been walked: the views of what they freed or reclaimed are
// stale.
func (f *fn) funcValueViews(call *ast.CallExpr, effects []litEffect) {
	if f.env == nil {
		return
	}
	for _, e := range effects {
		if e.reclaim {
			mv := move{kind: mvReclaim, pos: call.Pos(), to: f.c.reclaimHow(call, e.site)}
			part := e.part
			f.staleWhere(e.v, mv, func(w view) bool { return w.inHeap && overlap(w.part, part) })
			continue
		}
		f.freedPart(e.v, e.part, move{kind: mvFree, pos: call.Pos(), to: f.c.freeHow(call, e.site)})
	}
}

// candidateUses checks, at a call through a func value whose literals the
// walk lost track of, that the owners each literal the escape analysis
// says it may reach uses still hold a value: calling it uses them
// (section 18.3).
func (f *fn) candidateUses(call *ast.CallExpr) {
	if f.env == nil || !f.report {
		return
	}
	tv := f.c.types[call.Fun]
	if tv.IsType() || tv.IsBuiltin() || f.c.staticCallee(call) != nil {
		return
	}
	if se := funSelector(call.Fun); se != nil {
		if sel := f.c.selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
			return
		}
	}
	lits := false
	for _, w := range f.viewsOf(call.Fun) {
		if w.lit == nil {
			continue
		}
		lits = true
		// A stale hold read from a variable or a part of one is reported
		// where it is read (checkView); one loaded through a pointer,
		// (*pf)(), is reported here.
		if root, _ := f.viewPlace(call.Fun); root == nil && w.stale != nil {
			holder := w.of
			if id, ok := ast.Unparen(baseOf(call.Fun)).(*ast.Ident); ok && f.varOf(id) != nil {
				holder = f.varOf(id)
			}
			f.c.report(f.c.staleDiag(call.Fun, holder, w))
			return
		}
	}
	if lits {
		return
	}
	for _, lit := range f.candidateLits(call) {
		for _, u := range f.litOwners(lit) {
			if s, ok := f.env[u.v]; ok && s.st != live {
				f.c.report(f.c.useDiag(pu.New(ast.Ident{NamePos: call.Pos(), Name: u.v.Name()}), u.v, s))
				return
			}
		}
	}
}

// reclaimHow says how a call may reset or release a checked arena, for
// messages: "wipe(&a) (a.Reset(), line 4)".
func (c *checker) reclaimHow(call *ast.CallExpr, site keepSite) string {
	how := exprText(call)
	if site.pos == call.Pos() {
		return how
	}
	at := c.fset.Position(site.pos)
	where := compat.Concat("line ", strconv.Itoa(at.Line))
	if at.Filename != c.fset.Position(call.Pos()).Filename {
		where = compat.Concat(strings.SliceFrom(at.Filename, strings.LastIndexByte(at.Filename, '/')+1), ":", strconv.Itoa(at.Line))
	}
	return compat.Concat(how, " (", site.text, ", ", where, ")")
}
