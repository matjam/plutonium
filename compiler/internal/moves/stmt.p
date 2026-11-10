// Translated by go2pu from internal/moves/stmt.go at the go-cutover tag; edited by hand since.

package moves

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/slices"
	"pluto/sort"
)

func (f *fn) block(b *ast.BlockStmt) {
	if b == nil {
		return
	}
	f.blocks = compat.Append(f.blocks, b)
	f.scopes = compat.Append(f.scopes, ast.Node(b))
	for _, st := range b.List {
		f.stmt(st, "")
	}
	if b != f.topBody {
		// The variables declared in the block are dropped or released
		// here: views of them are stale (views.p).
		f.endScope(b, b.Rbrace, "")
	}
	f.recordExit(b)
	f.scopes = f.scopes[:len(f.scopes)-1]
	f.blocks = f.blocks[:len(f.blocks)-1]
}

// endScope records that the scope n ends at pos, or that the statement
// by (break or continue) leaves it there: the views of the variables
// declared in it are stale from here on (section 18.3, A local's scope).
func (f *fn) endScope(n ast.Node, pos token.Pos, by string) {
	for v, in := range f.scopeIn {
		if in == n {
			f.invalidate(v, move{kind: mvScopeEnd, pos: pos, to: by, typ: v.Type()})
		}
	}
}

// leaveScopes records that the break or continue st leaves the scopes
// beyond the first depth, innermost first.
func (f *fn) leaveScopes(depth int, st *ast.BranchStmt) {
	for i := len(f.scopes) - 1; i >= depth; i-- {
		if f.scopes[i] != ast.Node(f.topBody) {
			f.endScope(f.scopes[i], st.Pos(), st.Tok.String())
		}
	}
}

func (f *fn) stmt(st ast.Stmt, label string) {
	if _, isLabel := st.(*ast.LabeledStmt); !isLabel && st != nil {
		f.recordStmt(st)
	}
	switch st.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		// The variables of its implicit block are dropped where it ends.
		f.scopes = compat.Append(f.scopes, ast.Node(st))
		defer func() {
			f.endScope(st, st.End(), "")
			f.scopes = f.scopes[:len(f.scopes)-1]
			f.recordExit(st)
		}()
	}
	switch st := st.(type) {
	case nil, *ast.EmptyStmt, *ast.BadStmt:
	case *ast.ExprStmt:
		u := f.beginUnit()
		f.expr(st.X)
		f.endUnit(u, st, nil)
	case *ast.DeclStmt:
		u := f.beginUnit()
		f.declStmt(st)
		f.endUnit(u, st, nil)
	case *ast.AssignStmt:
		u := f.beginUnit()
		f.assignStmt(st)
		f.endUnit(u, st, st.Lhs)
	case *ast.OrStmt:
		f.orStmt(st)
	case *ast.IncDecStmt:
		f.expr(st.X)
		f.forget(st.X)
	case *ast.BlockStmt:
		f.block(st)
	case *ast.IfStmt:
		before := f.env.clone()
		f.stmt(st.Init, "")
		f.exprUnit(st.Cond)
		entry := f.env.clone()
		f.block(st.Body)
		after := f.env
		f.env = entry
		if v := f.assertFailed(st); v != nil && f.env != nil {
			// if x, ok := i.(T); ok {...} else {...}: the assertion moved
			// the owner out of i only where ok is true.
			if s, ok := before[v]; ok {
				f.env[v] = s
			} else {
				delete(f.env, v)
			}
		}
		f.stmt(st.Else, "")
		f.env = join(after, f.env)
	case *ast.ForStmt:
		f.stmt(st.Init, "")
		f.loop(st, label, func() {
			if st.Cond != nil {
				f.exprUnit(st.Cond)
			}
		}, func() {
			f.block(st.Body)
		}, func() { f.stmt(st.Post, "") })
	case *ast.RangeStmt:
		f.rangeStmt(st, label)
	case *ast.SwitchStmt:
		f.stmt(st.Init, "")
		if st.Tag != nil {
			f.exprUnit(st.Tag)
		}
		f.clauses(label, st.Body.List, func(c ast.Stmt) []ast.Stmt {
			cc := c.(*ast.CaseClause)
			for _, e := range cc.List {
				f.expr(e)
			}
			return cc.Body
		})
	case *ast.TypeSwitchStmt:
		f.stmt(st.Init, "")
		var x ast.Expr
		switch a := st.Assign.(type) {
		case *ast.AssignStmt:
			x = a.Rhs[0].(*ast.TypeAssertExpr).X
		case *ast.ExprStmt:
			x = a.X.(*ast.TypeAssertExpr).X
		}
		_, binds := st.Assign.(*ast.AssignStmt)
		if binds && f.assertMoves(x, f.c.typeOf(x)) {
			// The binding takes the owner in every clause.
			if f.assertOut(x, compat.Concat("switch ", exprText(x), ".(type)"), "type", true) && f.report {
				f.c.info.SwitchMoves[st] = true
			}
		} else {
			f.expr(x)
		}
		f.clauses(label, st.Body.List, func(c ast.Stmt) []ast.Stmt {
			cc := c.(*ast.CaseClause)
			for _, e := range cc.List {
				if !binds || !f.assertMoves(x, f.c.typeOf(x)) {
					f.assertTo(x, f.c.typeOf(e), e.Pos())
				}
			}
			if v, ok := f.c.implicits[cc].(*types2.Var); ok {
				f.assignVar(v)
				f.scopeIn[v] = cc
				if mayView(v.Type()) && f.env != nil {
					s := f.env[v]
					s.views = sortViews(whole(f.viewsOf(x)))
					f.setState(v, s)
				}
			}
			return cc.Body
		})
	case *ast.SelectStmt:
		f.clauses(label, st.Body.List, func(c ast.Stmt) []ast.Stmt {
			cc := c.(*ast.CommClause)
			f.stmt(cc.Comm, "")
			return cc.Body
		})
	case *ast.LabeledStmt:
		// A label may be reached by goto from anywhere in the body.
		f.env = join(f.env, f.heads[st])
		f.stmt(st.Stmt, st.Label.Name)
	case *ast.BranchStmt:
		f.branch(st)
	case *ast.ReturnStmt:
		u := f.beginUnit()
		f.returnStmt(st)
		f.endUnit(u, st, nil)
		f.env = nil
	case *ast.DeferStmt:
		u := f.beginUnit()
		f.deferStmt(st)
		f.endUnit(u, st, nil)
	case *ast.TaskStmt:
		u := f.beginUnit()
		f.call(st.Call, mvGo)
		f.endUnit(u, st, nil)
	case *ast.SendStmt:
		u := f.beginUnit()
		f.expr(st.Chan)
		var elem types2.Type
		if ch, ok := f.c.typeOf(st.Chan).Underlying().(*types2.Chan); ok {
			elem = ch.Elem()
		}
		f.consume(st.Value, elem, move{kind: mvSend, to: exprText(st.Chan)})
		f.endUnit(u, st, nil)
	}
}

func (f *fn) declStmt(st *ast.DeclStmt) {
	gd, ok := st.Decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR {
		return
	}
	for _, s := range gd.Specs {
		vs := s.(*ast.ValueSpec)
		views := compat.Make[[]view](len(vs.Names)) // views.go
		for i, name := range vs.Names {
			switch {
			case len(vs.Values) == len(vs.Names):
				views[i] = f.viewsTo(vs.Values[i], f.lhsType(name))
			case len(vs.Values) == 1:
				views[i] = whole(f.viewsOf(vs.Values[0]))
			}
		}
		switch {
		case len(vs.Values) == len(vs.Names):
			for i, val := range vs.Values {
				v, _ := f.c.defs[vs.Names[i]].(*types2.Var)
				var T types2.Type
				if v != nil {
					T = v.Type()
				}
				f.consume(val, T, move{kind: mvAssign, to: vs.Names[i].Name})
			}
		case len(vs.Values) == 1:
			f.multi(vs.Values[0])
		}
		for i, name := range vs.Names {
			if v, ok := f.c.defs[name].(*types2.Var); ok {
				f.assignVar(v)
				f.renew(name, v)
				f.storeViews(name, views[i])
				f.declared(v)
				switch {
				case len(vs.Values) == 0:
					f.declaredEmpty(v)
				case len(vs.Values) == len(vs.Names):
					f.valueFacts(v, vs.Values[i])
				}
			}
		}
	}
}

// declared records the block a variable is declared in.
func (f *fn) declared(v *types2.Var) {
	if len(f.blocks) > 0 {
		f.declIn[v] = f.blocks[len(f.blocks)-1]
	}
	if len(f.scopes) > 0 {
		f.scopeIn[v] = f.scopes[len(f.scopes)-1]
	}
}

func (f *fn) assignStmt(st *ast.AssignStmt) {
	switch st.Tok {
	case token.ASSIGN, token.DEFINE:
	default:
		// x op= y
		f.expr(st.Lhs[0])
		f.expr(st.Rhs[0])
		f.forget(st.Lhs[0])
		return
	}
	// The views each target receives, as the values are before the
	// statement moves anything (views.go).
	vs := compat.Make[[]view](len(st.Lhs))
	for i, l := range st.Lhs {
		if len(st.Lhs) == len(st.Rhs) {
			vs[i] = f.viewsTo(st.Rhs[i], f.lhsType(l))
		} else if T := f.lhsType(l); T == nil || mayView(T) {
			// A result that cannot hold a view, as the bool of
			// p, ok := m.Upsert(k), gets none of the call's.
			vs[i] = whole(f.viewsOf(st.Rhs[0]))
		}
	}
	if len(st.Lhs) == len(st.Rhs) {
		if len(st.Lhs) > 1 && f.permutes(st) {
			// a, b = b, a: nothing is duplicated or lost.
			for _, r := range st.Rhs {
				f.expr(r)
			}
			for i, l := range st.Lhs {
				f.assignLHS(l, st.Tok)
				f.storeViews(l, vs[i])
			}
			return
		}
		for i, r := range st.Rhs {
			f.consume(r, f.lhsType(st.Lhs[i]), move{kind: f.storeKind(st.Lhs[i]), to: exprText(st.Lhs[i])})
		}
	} else {
		f.multi(st.Rhs[0])
	}
	for i, l := range st.Lhs {
		f.assignTarget(l, st.Tok)
		f.storeViews(l, vs[i])
		if id, ok := ast.Unparen(l).(*ast.Ident); ok && len(st.Lhs) == len(st.Rhs) {
			if v := f.varOf(id); v != nil {
				f.valueFacts(v, st.Rhs[i])
			}
		}
	}
}

// orStmt walks an or statement (language spec section 11.27): the call,
// whose moves happen on both paths, then the clause, where nothing on the
// left is assigned, then the assignment of the other results. The clause
// leaves, so the path out of it does not reach the statement after.
func (f *fn) orStmt(st *ast.OrStmt) {
	u := f.beginUnit()
	f.expr(st.Call)
	f.endUnit(u, st.Call, nil)
	after := f.env.clone()
	r, isReturn := st.Clause.(*ast.ReturnStmt)
	form := types2.OrReturnPlain
	if isReturn && f.sig != nil {
		form = types2.OrReturnForm(f.sig.Results(), r, f.c.types)
	}
	if form != types2.OrReturnPlain {
		// A short form of return: the error result is err or E, and the
		// named results, if any, are returned as they are.
		if form == types2.OrReturnExpr {
			f.expr(r.Results[0])
		}
		f.returnStmt(pu.New(ast.ReturnStmt{Return: r.Return}))
	} else {
		f.stmt(st.Clause, "")
	}
	f.env = after
	for _, l := range st.Lhs {
		f.assignLHS(l, st.Tok)
	}
}

// storeKind is the kind of move into lhs: to a variable, or into a place.
func (f *fn) storeKind(lhs ast.Expr) moveKind {
	if id, ok := ast.Unparen(lhs).(*ast.Ident); ok && f.varOf(id) != nil {
		return mvAssign
	}
	return mvStore
}

// permutes reports whether an assignment's right-hand sides are its
// left-hand sides in another order.
func (f *fn) permutes(st *ast.AssignStmt) bool {
	left := map[string]int{}
	for _, l := range st.Lhs {
		left[exprText(l)]++
	}
	for _, r := range st.Rhs {
		k := exprText(r)
		if left[k] == 0 {
			return false
		}
		left[k]--
	}
	return true
}

func (f *fn) lhsType(lhs ast.Expr) types2.Type {
	if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
		if id.Name == "_" {
			return nil
		}
		if v, ok := f.c.objOf(id).(*types2.Var); ok {
			return v.Type()
		}
	}
	return f.c.typeOf(lhs)
}

// assignLHS walks the target of an assignment and gives a variable its
// new value.
func (f *fn) assignLHS(lhs ast.Expr, tok token.Token) {
	if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
		if id.Name == "_" {
			return
		}
		if v := f.varOf(id); v != nil {
			f.assignVar(v)
			f.renew(id, v)
			if tok == token.DEFINE {
				f.declared(v)
			}
		}
		return
	}
	// A field, element or pointer target: what it is part of must hold a
	// value. A container element is set with SetIndex.
	if ix, ok := ast.Unparen(lhs).(*ast.IndexExpr); ok && types2.IndexesByMethod(f.c.typeOf(ix.X)) {
		f.expr(ix.X)
		f.expr(ix.Index)
		return
	}
	// An owner or counted box in a part of a local owner given a new
	// value drops the old one: the views that look at or into that part
	// are stale (section 18.3).
	if T := f.c.typeOf(lhs); T != nil && bounds(T) {
		if v, path, _, ok := f.placePath(lhs); ok && v != nil && !f.captured(v) {
			f.invalidatePart(v, path, move{kind: mvNewValue, pos: lhs.Pos()})
		}
	}
	// A part of a variable is written, not read: the views it held are
	// not used (views.go).
	f.writing = ast.Unparen(lhs)
	f.expr(lhs)
	f.writing = nil
	f.forget(lhs)
}

// renew records that the owner or counted reference v, at id, is given a
// new value: the views of its old one are stale (views.go).
func (f *fn) renew(id *ast.Ident, v *types2.Var) {
	if bounds(v.Type()) && !f.captured(v) {
		f.invalidate(v, move{kind: mvNewValue, pos: id.Pos()})
	}
}

// multi walks an expression with several results.
func (f *fn) multi(e ast.Expr) {
	switch x := ast.Unparen(e).(type) {
	case *ast.IndexExpr:
		if f.containerOfOwners(x) {
			f.indexErr(x)
		}
		f.expr(x.X)
		f.expr(x.Index)
	case *ast.TypeAssertExpr:
		if f.assertMoves(x.X, f.c.typeOf(x.Type)) {
			if f.assertOut(x.X, exprText(x), exprText(x.Type), false) && f.report {
				f.c.info.AssertMoves[x] = true
			}
			return
		}
		f.expr(x.X)
		f.assertTo(x.X, f.c.typeOf(x.Type), x.Pos())
	default:
		f.expr(e)
	}
}

func (f *fn) rangeStmt(st *ast.RangeStmt, label string) {
	f.exprUnit(st.X)
	xt := f.c.typeOf(st.X)
	var elem types2.Type
	switch u := xt.Underlying().(type) {
	case *types2.Slice:
		elem = u.Elem()
	case *types2.Array:
		elem = u.Elem()
	case *types2.Pointer:
		if a, ok := u.Elem().Underlying().(*types2.Array); ok {
			elem = a.Elem()
		}
	}
	if st.Value != nil && isOwner(elem) && !isBlank(st.Value) && f.report {
		f.c.report(f.c.rangeDiag(st, elem))
	}
	f.loop(st, label, func() {
		for _, e := range []ast.Expr{st.Key, st.Value} {
			if e != nil {
				f.assignLHS(e, st.Tok)
			}
		}
		if st.Value != nil && mayView(f.c.typeOf(st.Value)) {
			f.storeViews(st.Value, whole(f.viewsOf(st.X))) // views.go
		}
	}, func() {
		f.block(st.Body)
	}, nil)
}

func isBlank(e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && id.Name == "_"
}

// loop walks a loop: head runs at the start of each iteration, then body,
// then post. The states reaching the head from the previous iteration come
// from the walk before; a variable that the loop moves without giving it a
// new value is thereby moved when the next iteration starts.
func (f *fn) loop(st ast.Node, label string, head, body, post func()) {
	entry := f.env
	back := f.heads[st]
	at := join(entry, back)
	if entry != nil && back != nil {
		// Mark what only the previous iteration moved.
		for v, s := range at {
			if es, before := entry[v]; (!before || es.st == live) && s.st != live && s.mv != nil {
				mv := pu.New(*s.mv)
				mv.loop = true
				s.mv = mv
				at[v] = s
			}
		}
	}
	f.env = at
	t := pu.New(target{label: label, isLoop: true, depth: len(f.scopes)})
	f.targets = compat.Append(f.targets, t)
	if head != nil {
		head()
	}
	afterHead := f.env.clone()
	body()
	f.env = join(f.env, t.conts)
	if post != nil {
		post()
	}
	f.note(st, f.env)
	f.targets = f.targets[:len(f.targets)-1]
	f.env = join(afterHead, t.breaks)
}

// clauses walks the clauses of a switch or select. Each starts where the
// statement does, or where the previous clause ended if that clause falls
// through.
func (f *fn) clauses(label string, list []ast.Stmt, clause func(ast.Stmt) []ast.Stmt) {
	entry := f.env.clone()
	t := pu.New(target{label: label, depth: len(f.scopes)})
	f.targets = compat.Append(f.targets, t)
	var out, prev env
	hasDefault := false
	for _, c := range list {
		switch c := c.(type) {
		case *ast.CaseClause:
			hasDefault = hasDefault || c.List == nil
		case *ast.CommClause:
			hasDefault = hasDefault || c.Comm == nil
		}
		f.env = join(entry, prev)
		if prev == nil {
			f.env = entry.clone()
		}
		f.scopes = compat.Append(f.scopes, ast.Node(c))
		body := clause(c)
		for _, s := range body {
			f.stmt(s, "")
		}
		f.endScope(c, c.End(), "")
		f.scopes = f.scopes[:len(f.scopes)-1]
		f.recordExit(c)
		prev = nil
		if n := len(body); n > 0 {
			if b, ok := body[n-1].(*ast.BranchStmt); ok && b.Tok == token.FALLTHROUGH {
				prev = f.env
			}
		}
		out = join(out, f.env)
	}
	if !hasDefault {
		out = join(out, entry)
	}
	f.env = join(out, t.breaks)
	f.targets = f.targets[:len(f.targets)-1]
}

// branch records a break, continue or goto and makes the rest of the path
// unreachable.
func (f *fn) branch(st *ast.BranchStmt) {
	var label string
	if st.Label != nil {
		label = st.Label.Name
	}
	if st.Tok != token.FALLTHROUGH {
		f.recordExit(st)
	}
	switch st.Tok {
	case token.GOTO:
		if obj, ok := f.c.uses[st.Label].(*types2.Label); ok {
			// The labeled statement collects the states of every goto.
			if ls := f.labeled(obj); ls != nil {
				f.note(ls, f.env)
			}
		}
		f.env = nil
		return
	case token.FALLTHROUGH:
		return
	}
	for i := len(f.targets) - 1; i >= 0; i-- {
		t := f.targets[i]
		if label != "" && t.label != label {
			continue
		}
		if st.Tok == token.CONTINUE && !t.isLoop {
			continue
		}
		// The scopes the branch leaves end here (section 18.4).
		f.leaveScopes(t.depth, st)
		if st.Tok == token.BREAK {
			t.breaks = join(t.breaks, f.env)
		} else {
			t.conts = join(t.conts, f.env)
		}
		f.env = nil
		return
	}
}

// labeled finds the statement a label labels.
func (f *fn) labeled(l *types2.Label) *ast.LabeledStmt {
	var out *ast.LabeledStmt
	ast.Inspect(f.topBody, func(n ast.Node) bool {
		if ls, ok := n.(*ast.LabeledStmt); ok && ls.Label.Pos() == l.Pos() {
			out = ls
			return false
		}
		_, lit := n.(*ast.FuncLit)
		return !lit && out == nil
	})
	return out
}

func (f *fn) returnStmt(st *ast.ReturnStmt) {
	defer func() {
		f.deferExit()
		f.recordResults(st)
		f.recordExit(st)
	}()
	if len(st.Results) == 0 {
		// A bare return moves the named results to the caller.
		for _, v := range f.results {
			if s, ok := f.env[v]; ok && s.st != live && isOwner(v.Type()) && f.report && f.env != nil {
				f.c.report(f.c.useDiag(&ast.Ident{NamePos: st.Pos(), Name: v.Name()}, v, s))
			}
		}
		return
	}
	sig := f.sig
	if len(st.Results) == 1 && sig != nil && sig.Results().Len() > 1 {
		f.multi(st.Results[0])
		return
	}
	for i, r := range st.Results {
		var T types2.Type
		if sig != nil && i < sig.Results().Len() {
			T = sig.Results().At(i).Type()
		}
		f.consume(r, T, move{kind: mvReturn})
	}
}

// deferredDrop returns the owner variable of a deferred call x.Drop(), or
// nil.
func (f *fn) deferredDrop(call *ast.CallExpr) (*types2.Var, *ast.Ident) {
	se, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || se.Sel.Name != "Drop" || len(call.Args) != 0 {
		return nil, nil
	}
	id, ok := ast.Unparen(se.X).(*ast.Ident)
	if !ok {
		return nil, nil
	}
	if v := f.varOf(id); v != nil && isOwner(v.Type()) && !f.captured(v) {
		return v, id
	}
	return nil, nil
}

// deferStmt walks a deferred call. Its arguments are evaluated now; a
// deferred call may not use an owner of an inner block, which is dropped
// when that block ends, before the call runs.
func (f *fn) deferStmt(st *ast.DeferStmt) {
	if v, id := f.deferredDrop(st.Call); v != nil {
		// defer x.Drop() ends x's life when the function returns, after
		// everything else has used it: here it only takes x's address.
		f.use(id, v)
		if f.report {
			f.c.info.DeferredDrops[st.Call] = true
		}
		if f.env != nil {
			s := f.env[v]
			s.dd = true // empty.go
			f.setState(v, s)
		}
	} else {
		f.call(st.Call, mvArg)
		f.deferUses(st)
	}
	if !f.report || len(f.blocks) == 0 {
		return
	}
	ast.Inspect(st.Call, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v := f.varOf(id)
		if v == nil || !isOwner(v.Type()) {
			return true
		}
		if b := f.declIn[v]; b != nil && b != f.topBody {
			f.c.report(f.c.deferDiag(st, id, v, b))
		}
		return true
	})
}

// A deferred call runs when the function returns, so what it uses of an
// owner must still hold a value then (deferExit). deferUses registers the
// owners the deferred call uses, in three shapes (decision D32, section
// 18.5): a function literal in call position, "defer func() {...}()"; a view
// of an owner among the receiver and arguments, "defer show(&o)", which the
// call keeps until it runs (section 18.3); and a func variable that may hold
// a function literal capturing an owner, "f := func() {...}; defer f()"
// (litHolders), or such a literal passed as an argument.
func (f *fn) deferUses(st *ast.DeferStmt) {
	if f.env == nil {
		return
	}
	call := st.Call
	if lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok {
		f.deferLitUses(st, lit, deferLiteral, "")
	} else if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
		f.deferHolder(st, id)
	}
	for _, a := range call.Args {
		switch x := ast.Unparen(a).(type) {
		case *ast.FuncLit:
			f.deferLitUses(st, x, deferLiteral, "")
		case *ast.Ident:
			f.deferHolder(st, x)
		}
	}
	f.eachArg(call, func(key, val ast.Expr, T types2.Type) {
		for _, w := range f.viewsTo(val, T) {
			if o := f.ownerVar(w.of); o != nil {
				f.deferMark(st, o, f.firstIdent(val, o), deferView, "")
			}
		}
	})
}

// firstIdent returns the first identifier of e that names v, or else the
// first identifier of e (the variable that holds a view of v), or else a
// stand-in at e.
func (f *fn) firstIdent(e ast.Expr, v *types2.Var) *ast.Ident {
	var first, named *ast.Ident
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if first == nil {
				first = id
			}
			if named == nil && f.varOf(id) == v {
				named = id
			}
		}
		return true
	})
	switch {
	case named != nil:
		return named
	case first != nil:
		return first
	}
	return pu.New(ast.Ident{NamePos: e.Pos(), Name: v.Name()})
}

// deferMark records on owner v that the deferred call of st uses it.
func (f *fn) deferMark(st *ast.DeferStmt, v *types2.Var, use *ast.Ident, kind deferKind, via string) {
	s := f.env[v]
	if s.dl == nil {
		s.dl = pu.New(deferMark{use: use, stmt: st.Pos(), kind: kind, via: via})
		f.setState(v, s)
	}
}

// deferLitUses registers the owners of this function that the function
// literal lit uses.
func (f *fn) deferLitUses(st *ast.DeferStmt, lit *ast.FuncLit, kind deferKind, via string) {
	for _, u := range f.litOwners(lit) {
		f.deferMark(st, u.v, u.id, kind, via)
	}
}

// deferHolder registers the owners used by the function literals that the
// variable id may hold.
func (f *fn) deferHolder(st *ast.DeferStmt, id *ast.Ident) {
	v := f.varOf(id)
	if v == nil {
		return
	}
	for _, u := range f.litHolders()[v] {
		f.deferMark(st, u.v, u.id, deferStored, v.Name())
	}
}

// An ownerUse is a use of an owner of the function by a function literal.
type ownerUse struct {
	v  *types2.Var
	id *ast.Ident
}

// litOwners returns the owners of this function that lit uses (the first
// use of each), in source order. A scoped task's literal has its own check.
func (f *fn) litOwners(lit *ast.FuncLit) []ownerUse {
	if f.c.scoped.Lits[lit] != nil {
		return nil
	}
	var out []ownerUse
	seen := map[*types2.Var]bool{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if v := f.varOf(id); v != nil && f.vars[v] && f.c.uses[id] != nil && isOwner(v.Type()) && !seen[v] {
			seen[v] = true
			out = compat.Append(out, ownerUse{v, id})
		}
		return true
	})
	return out
}

// litHolders returns, for each variable of the function that is assigned a
// function literal somewhere in the body (or another such variable), the
// owners that the literals use. It ignores the order of statements: a
// variable that may hold such a literal at a deferred call is taken to hold
// it, so the use counts on doubt (the principle of section 1.2). Literals
// that reach a deferred call through anything other than a local variable
// and one chain of copies (a field, a slice, a call result) are not
// followed.
func (f *fn) litHolders() map[*types2.Var][]ownerUse {
	if f.holders != nil {
		return f.holders
	}
	h := map[*types2.Var][]ownerUse{}
	f.holders = h
	type pair struct{ to, from ast.Expr }
	var pairs []pair
	ast.Inspect(f.topBody, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) == len(n.Rhs) {
				for i := range n.Lhs {
					pairs = compat.Append(pairs, pair{n.Lhs[i], n.Rhs[i]})
				}
			}
		case *ast.ValueSpec:
			if len(n.Names) == len(n.Values) {
				for i := range n.Names {
					pairs = compat.Append(pairs, pair{n.Names[i], n.Values[i]})
				}
			}
		}
		return true
	})
	add := func(v *types2.Var, us []ownerUse) bool {
		changed := false
		for _, u := range us {
			if !slices.ContainsFunc(h[v], func(w ownerUse) bool { return w.v == u.v }) {
				h[v] = compat.Append(h[v], u)
				changed = true
			}
		}
		return changed
	}
	for changed := true; changed; {
		changed = false
		for _, p := range pairs {
			id, ok := ast.Unparen(p.to).(*ast.Ident)
			if !ok {
				continue
			}
			to := f.varOf(id)
			if to == nil || !f.vars[to] {
				continue
			}
			switch r := ast.Unparen(p.from).(type) {
			case *ast.FuncLit:
				changed = add(to, f.litOwners(r)) || changed
			case *ast.Ident:
				if from := f.varOf(r); from != nil {
					changed = add(to, h[from]) || changed
				}
			}
		}
	}
	return h
}

// deferExit checks, where the function returns, that every owner used by a
// deferred function literal registered on the way here holds a value: the
// literal is about to run (section 18.5, Use after a move).
func (f *fn) deferExit() {
	if !f.report || f.env == nil {
		return
	}
	var vs []*types2.Var
	for v, s := range f.env {
		if s.dl != nil && s.st != live {
			vs = compat.Append(vs, v)
		}
	}
	sort.SortFunc(vs, func(a, b *types2.Var) bool { return a.Pos() < b.Pos() })
	for _, v := range vs {
		f.c.report(f.c.deferUseDiag(v, f.env[v]))
	}
}

// assertFailed returns the owning interface variable that the init of an
// if statement "if v, ok := x.(T); ok" asserts out of, or nil: on the else
// path the assertion failed, and x still holds its value.
func (f *fn) assertFailed(st *ast.IfStmt) *types2.Var {
	as, ok := st.Init.(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 2 || len(as.Rhs) != 1 {
		return nil
	}
	ta, ok := ast.Unparen(as.Rhs[0]).(*ast.TypeAssertExpr)
	if !ok || !f.c.info.AssertMoves[ta] && !f.assertMoves(ta.X, f.c.typeOf(ta.Type)) {
		return nil
	}
	okID, ok := as.Lhs[1].(*ast.Ident)
	cond, ok2 := ast.Unparen(st.Cond).(*ast.Ident)
	if !ok || !ok2 || f.c.objOf(okID) == nil || f.c.objOf(okID) != f.c.objOf(cond) {
		return nil
	}
	x, ok := ast.Unparen(ta.X).(*ast.Ident)
	if !ok {
		return nil
	}
	return f.varOf(x)
}
