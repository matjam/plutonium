package moves

import (
	"compiler/compat"
	"compiler/internal/constant"
	"compiler/internal/load"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/slices"
	"pluto/strconv"
)

// Counted copies (section 18.3). A view obtained through a counted
// reference (shared[T], synced[T]) held in a local variable ends at a call
// that may free memory of the object through any reference that may point
// to it: a copy in another local, a field, a parameter of the same box
// type, or a package variable. The compiler does not know which references
// share an object, so a reference of the same type anywhere may.
//
// A copy passed by value is an input of the callee's summaries (keeps.p),
// so a callee that frees through it frees through the caller's reference
// (freeArgs). A copy in a package variable is followed here: findGlobalFrees
// records, for each function, the counted reference types it may free
// through a reference reached from a package variable, by itself or
// through the functions it calls, and freeArgs ends the views of every
// local reference of such a type at a call of it. A new value given to an
// owner in the object, through any reference, ends the views of the
// objects of the same type that look at or into that part
// (countedNewValue).

// A globalFree is what a function may free through package variables.
type globalFree struct {
	types []types2.Type // the counted reference types
	all   bool          // a reference of any type
	site  keepSite      // a statement or call by which it may
	ret   bool          // a result may be reached from a package variable
}

// add merges types and all into g, and reports whether g grew.
func (g *globalFree) add(c *checker, types []types2.Type, all bool, site keepSite) bool {
	grew := false
	if all && !g.all {
		g.all, grew = true, true
	}
	for _, t := range types {
		if !slices.ContainsFunc(g.types, func(u types2.Type) bool { return c.sameCounted(t, u) }) {
			g.types = compat.Append(g.types, t)
			grew = true
		}
	}
	if grew && !g.site.valid() {
		g.site = site
	}
	return grew
}

// globalHits reports whether g may free through a reference of type T.
func (c *checker) globalHits(g *globalFree, T types2.Type) bool {
	if g == nil {
		return false
	}
	if g.all {
		return true
	}
	return slices.ContainsFunc(g.types, func(u types2.Type) bool { return c.countedHit(u, T) })
}

// anyValue is the key, in c.gfrees, of the union over the functions whose
// value the program takes: what a call through a func value whose
// candidates are not known may reach.
var anyValue ast.Node = &ast.BadExpr{}

// A gfScan holds what one function body's locals may hold of package
// variables.
type gfScan struct {
	c    *checker
	from map[*types2.Var]bool       // locals that may hold a reference reached from a package variable
	part map[*types2.Var][]partFrom // fields of locals that may, when the local as a whole does not
}

// A partFrom is a field of a local, given a value that may be reached from
// a package variable (v.a = alloc.Default()): the field path, as the
// indices of the selections, and the type of the field.
type partFrom struct {
	path []int
	T    types2.Type
}

// fieldPath returns, for a local variable v or a field of it reached by
// field selections only (v.a, v.s.b, through pointers too), the variable
// and the path of the field, as the indices of the selections.
func (c *checker) fieldPath(x ast.Expr) (*types2.Var, []int, bool) {
	var path []int
	for {
		switch e := ast.Unparen(x).(type) {
		case *ast.SelectorExpr:
			sel := c.selections[e]
			if sel == nil || sel.Kind() != types2.FieldVal {
				return nil, nil, false
			}
			path = compat.Append(compat.Clone(sel.Index()), path...)
			x = e.X
		case *ast.Ident:
			v, ok := c.objOf(e).(*types2.Var)
			if !ok || v.IsField() || isGlobal(v) {
				return nil, nil, false
			}
			return v, path, true
		default:
			return nil, nil, false
		}
	}
}

// pathPrefix reports whether p is a prefix of q.
func pathPrefix(p, q []int) bool {
	return len(p) <= len(q) && slices.Equal(p, q[:len(p)])
}

// findGlobalFrees computes c.gfrees, after findFrees.
func (c *checker) findGlobalFrees(prog *load.Program) {
	c.gfrees = map[ast.Node]*globalFree{}
	c.taken = map[*ast.BlockStmt]bool{}
	type body struct {
		node ast.Node
		body *ast.BlockStmt
	}
	var bodies []body
	values := map[ast.Node]bool{}
	called := map[*ast.Ident]bool{}
	for _, p := range prog.Packages {
		for _, file := range p.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.FuncDecl:
					if x.Body != nil {
						bodies = compat.Append(bodies, body{node: x, body: x.Body})
					}
				case *ast.FuncLit:
					bodies = compat.Append(bodies, body{node: x, body: x.Body})
					values[x] = true
				case *ast.RangeStmt:
					if c.rangeBody(x) {
						bodies = compat.Append(bodies, body{node: x, body: x.Body})
					}
				case *ast.CallExpr:
					// The function called by name is not a value taken.
					if id := calledName(x.Fun); id != nil {
						called[id] = true
					}
				case *ast.Ident:
					if fn, ok := c.objOf(x).(*types2.Func); ok && !called[x] {
						if d := c.keepDecls[fn.Origin()]; d != nil {
							values[d] = true
						}
					}
				}
				return true
			})
		}
	}
	for _, b := range bodies {
		c.gfrees[b.node] = pu.New(globalFree{})
	}
	c.gfrees[anyValue] = pu.New(globalFree{})
	// What the functions return of package variables, to a fixed point. A
	// fixed point not reached in the bound is the worst case: every result.
	done := false
	for i := 0; i < 20 && !done; i++ {
		done = true
		for _, b := range bodies {
			g := c.gfrees[b.node]
			if g.ret {
				continue
			}
			s := c.gfScan(b.body)
			ast.Inspect(b.body, func(n ast.Node) bool {
				if r, ok := n.(*ast.ReturnStmt); ok {
					for _, e := range r.Results {
						if s.reached(e) {
							g.ret = true
						}
					}
				}
				return true
			})
			if g.ret {
				done = false
			}
		}
	}
	if !done {
		for _, b := range bodies {
			c.gfrees[b.node].ret = true
		}
	}
	// What each function frees by itself, and the edges to the functions
	// it calls.
	into := map[ast.Node][]ast.Node{}
	for _, b := range bodies {
		s := c.gfScan(b.body)
		g := c.gfrees[b.node]
		s.direct(b.body, func(types []types2.Type, all bool, site keepSite) {
			g.add(c, types, all, site)
		}, func(to ast.Node) {
			into[to] = compat.Append(into[to], b.node)
		})
	}
	for n := range values {
		into[n] = compat.Append(into[n], anyValue)
	}
	// Up the edges, to a fixed point: the sets only grow.
	var work []ast.Node
	for _, b := range bodies {
		if g := c.gfrees[b.node]; g.all || len(g.types) > 0 {
			work = compat.Append(work, b.node)
		}
	}
	for len(work) > 0 {
		n := work[len(work)-1]
		work = work[:len(work)-1]
		g := c.gfrees[n]
		for _, from := range into[n] {
			if c.gfrees[from].add(c, g.types, g.all, g.site) {
				work = compat.Append(work, from)
			}
		}
	}
}

// calledName returns the identifier naming the function or method fun
// calls, or nil.
func calledName(fun ast.Expr) *ast.Ident {
	switch x := ast.Unparen(fun).(type) {
	case *ast.Ident:
		return x
	case *ast.SelectorExpr:
		return x.Sel
	case *ast.IndexExpr:
		return calledName(x.X)
	case *ast.IndexListExpr:
		return calledName(x.X)
	}
	return nil
}

// gfScan finds the locals of body that may hold a reference reached from a
// package variable, to a fixed point.
func (c *checker) gfScan(body *ast.BlockStmt) *gfScan {
	s := pu.New(gfScan{c: c, from: map[*types2.Var]bool{}, part: map[*types2.Var][]partFrom{}})
	mark := func(l ast.Expr) bool {
		// A field of a local: only that field may hold what is reached.
		if v, path, ok := c.fieldPath(l); ok && len(path) > 0 {
			if s.from[v] || slices.ContainsFunc(s.part[v], func(p partFrom) bool { return pathPrefix(p.path, path) }) {
				return false
			}
			s.part[v] = compat.Append(s.part[v], partFrom{path: path, T: c.typeOf(l)})
			return true
		}
		id, ok := ast.Unparen(baseOf(l)).(*ast.Ident)
		if !ok {
			return false
		}
		v, ok := c.objOf(id).(*types2.Var)
		if !ok || v.IsField() || isGlobal(v) || s.from[v] {
			return false
		}
		s.from[v] = true
		return true
	}
	for i := 0; i < 20; i++ {
		change := false
		ast.Inspect(body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				if x.Tok != token.ASSIGN && x.Tok != token.DEFINE {
					return true
				}
				hit := false
				for _, r := range x.Rhs {
					hit = hit || s.reached(r)
				}
				if hit {
					for _, l := range x.Lhs {
						change = mark(l) || change
					}
				}
			case *ast.ValueSpec:
				hit := false
				for _, r := range x.Values {
					hit = hit || s.reached(r)
				}
				if hit {
					for _, l := range x.Names {
						change = mark(l) || change
					}
				}
			case *ast.RangeStmt:
				if s.reached(x.X) {
					for _, l := range []ast.Expr{x.Key, x.Value} {
						if l != nil {
							change = mark(l) || change
						}
					}
				}
			}
			return true
		})
		if !change {
			return s
		}
	}
	// Not settled in the bound: every local may.
	ast.Inspect(body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := c.defs[id].(*types2.Var); ok && !v.IsField() {
				s.from[v] = true
			}
		}
		return true
	})
	return s
}

// reached reports whether the value of e may be, or be reached from, a
// package variable whose type may lead to a counted reference.
func (s *gfScan) reached(e ast.Expr) bool {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		v, ok := s.c.objOf(x).(*types2.Var)
		if !ok || v.IsField() {
			return false
		}
		if isGlobal(v) {
			types, all := s.c.countedReached(v.Type())
			return all || len(types) > 0
		}
		return s.from[v] || len(s.part[v]) > 0
	case *ast.SelectorExpr:
		if s.c.selections[x] == nil {
			return s.reached(x.Sel) // a qualified identifier
		}
		if v, path, ok := s.c.fieldPath(x); ok && !s.from[v] {
			// A field of a local: reached when it is in, or holds, a
			// field given such a value.
			return slices.ContainsFunc(s.part[v], func(p partFrom) bool {
				return pathPrefix(p.path, path) || pathPrefix(path, p.path)
			})
		}
		return s.reached(x.X)
	case *ast.StarExpr:
		return s.reached(x.X)
	case *ast.IndexExpr:
		return s.reached(x.X)
	case *ast.SliceExpr:
		return s.reached(x.X)
	case *ast.TypeAssertExpr:
		return s.reached(x.X)
	case *ast.UnaryExpr:
		return x.Op == token.AND && s.reached(x.X)
	case *ast.CompositeLit:
		for _, el := range x.Elts {
			if s.reached(el) {
				return true
			}
		}
	case *ast.KeyValueExpr:
		return s.reached(x.Value)
	case *ast.CallExpr:
		tv := s.c.types[x.Fun]
		if tv.IsType() {
			for _, a := range x.Args {
				if s.reached(a) {
					return true
				}
			}
			return false
		}
		if tv.IsBuiltin() {
			if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok && id.Name == "append" {
				for _, a := range x.Args {
					if s.reached(a) {
						return true
					}
				}
			}
			return false
		}
		targets, known := s.c.keepTargets(x)
		if !known {
			return true
		}
		for _, t := range targets {
			if g := s.c.gfrees[t.node]; g != nil && g.ret {
				return true
			}
		}
		hit := false
		s.c.callArgs(x, func(key, val ast.Expr, j int) {
			if af := s.c.calls.Arg(key); (af == nil || af.Returned) && s.reached(val) {
				hit = true
			}
		})
		return hit
	}
	return false
}

// prefixTypes returns the counted reference types reached from the
// prefixes of the place x (x itself, then what it is part of or loaded
// through) that are reached from a package variable, by reached; all is
// true when a prefix may lead to a reference of any type.
func (s *gfScan) prefixTypes(x ast.Expr) ([]types2.Type, bool) {
	// A local or field that holds fields given such a value, without
	// being in one, leads only to what those fields lead to.
	var out []types2.Type
	all := false
	types, a := countedPrefixes(s.c, x, func(P ast.Expr) bool {
		v, path, ok := s.c.fieldPath(P)
		if !ok || s.from[v] || slices.ContainsFunc(s.part[v], func(p partFrom) bool { return pathPrefix(p.path, path) }) {
			return s.reached(P)
		}
		for _, p := range s.part[v] {
			if pathPrefix(path, p.path) {
				ts, pa := s.c.countedReached(p.T)
				out, all = compat.Append(out, ts...), all || pa
			}
		}
		return false
	})
	return compat.Append(out, types...), all || a
}

// countedPrefixes returns the counted reference types reached from the
// prefixes of the place x for which want holds.
func countedPrefixes(c *checker, x ast.Expr, want func(ast.Expr) bool) ([]types2.Type, bool) {
	var out []types2.Type
	all := false
	for x != nil {
		if want(x) {
			types, a := c.countedReached(c.typeOf(x))
			out, all = compat.Append(out, types...), all || a
		}
		switch e := ast.Unparen(x).(type) {
		case *ast.SelectorExpr:
			if c.selections[e] == nil {
				return out, all
			}
			x = e.X
		case *ast.StarExpr:
			x = e.X
		case *ast.IndexExpr:
			x = e.X
		case *ast.SliceExpr:
			x = e.X
		case *ast.UnaryExpr:
			if e.Op != token.AND {
				return out, all
			}
			x = e.X
		default:
			return out, all
		}
	}
	return out, all
}

// direct finds, in body, the calls and assignments that may free through a
// reference reached from a package variable (found), and the functions
// body calls (edge). A call through a func value whose candidates are not
// known reaches every function whose value the program takes (anyValue).
// A function without a body names no package variable of the program: it
// reaches only what it is given, which the summaries of frees.p follow.
func (s *gfScan) direct(body *ast.BlockStmt, found func(types []types2.Type, all bool, site keepSite), edge func(to ast.Node)) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if x.Tok != token.ASSIGN {
				return true
			}
			for _, l := range x.Lhs {
				X, _, ok := countedPlace(s.c, l)
				if ok && s.reached(X) && freeing(s.c.typeOf(l)) {
					types, all := s.c.countedReached(s.c.typeOf(X))
					found(types, all, keepSite{pos: x.Pos(), text: compat.Concat(exprText(l), " = ...")})
				}
			}
		case *ast.CallExpr:
			s.call(x, found, edge)
		}
		return true
	})
}

// call handles one call for direct.
func (s *gfScan) call(call *ast.CallExpr, found func(types []types2.Type, all bool, site keepSite), edge func(to ast.Node)) {
	site := keepSite{pos: call.Pos(), text: exprText(call)}
	tv := s.c.types[call.Fun]
	if tv.IsType() {
		return
	}
	if tv.IsBuiltin() {
		id, ok := ast.Unparen(call.Fun).(*ast.Ident)
		if !ok || len(call.Args) == 0 {
			return
		}
		switch id.Name {
		case "take", "clear", "delete":
			if types, all := s.prefixTypes(call.Args[0]); all || len(types) > 0 {
				found(types, all, site)
			}
		}
		return
	}
	targets, known := s.c.keepTargets(call)
	if !known {
		edge(anyValue)
	}
	for _, t := range targets {
		edge(t.node)
	}
	prim := (&keepScan{c: s.c}).freePrimitive(call)
	worst := !known || s.c.bodyless(call)
	s.c.callArgs(call, func(key, val ast.Expr, j int) {
		types, all := s.prefixTypes(val)
		if !all && len(types) == 0 {
			return
		}
		frees := prim(j) || worst && reachesFreeing(s.c.typeOf(val))
		for _, t := range targets {
			fs := s.c.frees[t.node]
			frees = frees || j+t.shift < len(fs) && fs[j+t.shift].valid()
		}
		if frees {
			found(types, all, site)
		}
	})
}

// countedPlace returns, for a place x reached through a counted reference
// (r.f, r.a[1].g), the reference r and the path of x in its object; the
// path is nil when another dereference follows the first (the whole
// object).
func countedPlace(c *checker, x ast.Expr) (ast.Expr, Path, bool) {
	var rev Path // the steps below the current expression, innermost first
	for {
		switch e := ast.Unparen(x).(type) {
		case *ast.SelectorExpr:
			sel := c.selections[e]
			if sel == nil || sel.Kind() != types2.FieldVal {
				return nil, nil, false
			}
			steps := fieldSteps(sel)
			if sel.Indirect() {
				if isOwnedBox(c.typeOf(e.X)) {
					// In the object of an owned box: somewhere in the
					// box at e.X, which the steps above it reach.
					rev = nil
					x = e.X
					continue
				}
				if !isCountedBox(c.typeOf(e.X)) {
					return nil, nil, false
				}
				if len(steps) != 1 {
					return e.X, nil, true
				}
				for i := len(rev) - 1; i >= 0; i-- {
					steps = compat.Append(steps, rev[i])
				}
				return e.X, steps, true
			}
			for i := len(steps) - 1; i >= 0; i-- {
				rev = compat.Append(rev, steps[i])
			}
			x = e.X
		case *ast.IndexExpr:
			if _, isArr := c.typeOf(e.X).Underlying().(*types2.Array); !isArr {
				return nil, nil, false
			}
			rev = compat.Append(rev, c.indexStep(e))
			x = e.X
		default:
			return nil, nil, false
		}
	}
}

// globalFreesOf returns what call may free through package variables: the
// union over the functions it may reach.
func (c *checker) globalFreesOf(call *ast.CallExpr) *globalFree {
	if c.gfrees == nil {
		return nil
	}
	targets, known := c.keepTargets(call)
	out := pu.New(globalFree{})
	if !known {
		g := c.gfrees[anyValue]
		out.add(c, g.types, g.all, g.site)
	}
	for _, t := range targets {
		if g := c.gfrees[t.node]; g != nil {
			out.add(c, g.types, g.all, g.site)
		}
	}
	return out
}

// countedLocals returns the local counted references of this function
// whose type hit accepts, in the order they were declared.
func (f *fn) countedLocals(hit func(T types2.Type) bool) []*types2.Var {
	var out []*types2.Var
	for u := range f.vars {
		if isCountedBox(u.Type()) && hit(u.Type()) {
			out = compat.Append(out, u)
		}
	}
	slices.SortFunc(out, func(a, b *types2.Var) int { return int(a.Pos()) - int(b.Pos()) })
	return out
}

// countedNewValue records an assignment to the owner or counted box lhs, a
// place in the object of a counted reference: the old value is dropped,
// so the views that look at or into that part of the object are stale,
// through the reference named and through every local one of the same
// type, which may point to the same object (section 18.3).
func (f *fn) countedNewValue(lhs ast.Expr) {
	if f.env == nil {
		return
	}
	f.countedEmptied(lhs, move{kind: mvNewValue, pos: lhs.Pos(), to: compat.Concat(exprText(lhs), " = ..., which drops the old value of ", exprText(lhs))})
}

// countedEmptied is countedNewValue for the move mv out of the place x:
// take(&r.c) leaves the zero value there (section 18.5).
func (f *fn) countedEmptied(x ast.Expr, mv move) {
	if f.env == nil {
		return
	}
	X, path, ok := countedPlace(f.c, x)
	if !ok {
		return
	}
	T := f.c.typeOf(X)
	for _, u := range f.countedLocals(func(U types2.Type) bool { return types2.Identical(U, T) }) {
		if !f.captured(u) {
			f.freedPart(u, path, mv)
		}
	}
}

// fieldSteps returns the path steps of the field selection sel.
func fieldSteps(sel *types2.Selection) Path {
	steps := Path(compat.Make[string](len(sel.Index())))
	for i, idx := range sel.Index() {
		steps[i] = compat.Concat(".", strconv.Itoa(idx))
	}
	return steps
}

// indexStep returns the path step of the index of an array e: [k] for a
// constant index, [*] otherwise.
func (c *checker) indexStep(e *ast.IndexExpr) string {
	if tv := c.types[e.Index]; !tv.Value.IsNil() {
		if i, exact := constant.Int64Val(constant.ToInt(tv.Value)); exact {
			return compat.Concat("[", strconv.FormatInt(i, 10), "]")
		}
	}
	return "[*]"
}
