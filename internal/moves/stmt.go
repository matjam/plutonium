package moves

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

func (f *fn) block(b *ast.BlockStmt) {
	if b == nil {
		return
	}
	f.blocks = append(f.blocks, b)
	for _, st := range b.List {
		f.stmt(st, "")
	}
	f.recordExit(b)
	f.blocks = f.blocks[:len(f.blocks)-1]
}

func (f *fn) stmt(st ast.Stmt, label string) {
	if _, isLabel := st.(*ast.LabeledStmt); !isLabel && st != nil {
		f.recordStmt(st)
	}
	switch st.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		// The variables of its implicit block are dropped where it ends.
		defer f.recordExit(st)
	}
	switch st := st.(type) {
	case nil, *ast.EmptyStmt, *ast.BadStmt:
	case *ast.ExprStmt:
		f.expr(st.X)
	case *ast.DeclStmt:
		f.declStmt(st)
	case *ast.AssignStmt:
		f.assignStmt(st)
	case *ast.IncDecStmt:
		f.expr(st.X)
		f.forget(st.X)
	case *ast.BlockStmt:
		f.block(st)
	case *ast.IfStmt:
		before := f.env.clone()
		f.stmt(st.Init, "")
		f.expr(st.Cond)
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
				f.expr(st.Cond)
			}
		}, func() {
			f.block(st.Body)
		}, func() { f.stmt(st.Post, "") })
	case *ast.RangeStmt:
		f.rangeStmt(st, label)
	case *ast.SwitchStmt:
		f.stmt(st.Init, "")
		if st.Tag != nil {
			f.expr(st.Tag)
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
			if f.assertOut(x, "switch "+exprText(x)+".(type)") && f.report {
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
		f.returnStmt(st)
		f.env = nil
	case *ast.DeferStmt:
		f.deferStmt(st)
	case *ast.TaskStmt:
		f.call(st.Call, mvGo)
	case *ast.SendStmt:
		f.expr(st.Chan)
		var elem types2.Type
		if ch, ok := f.c.typeOf(st.Chan).Underlying().(*types2.Chan); ok {
			elem = ch.Elem()
		}
		f.consume(st.Value, elem, move{kind: mvSend, to: exprText(st.Chan)})
	}
}

func (f *fn) declStmt(st *ast.DeclStmt) {
	gd, ok := st.Decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR {
		return
	}
	for _, s := range gd.Specs {
		vs := s.(*ast.ValueSpec)
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
	if len(st.Lhs) == len(st.Rhs) {
		if len(st.Lhs) > 1 && f.permutes(st) {
			// a, b = b, a: nothing is duplicated or lost.
			for _, r := range st.Rhs {
				f.expr(r)
			}
			for _, l := range st.Lhs {
				f.assignLHS(l, st.Tok)
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
		if id, ok := ast.Unparen(l).(*ast.Ident); ok && len(st.Lhs) == len(st.Rhs) {
			if v := f.varOf(id); v != nil {
				f.valueFacts(v, st.Rhs[i])
			}
		}
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
	f.expr(lhs)
	f.forget(lhs)
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
			if f.assertOut(x.X, exprText(x)) && f.report {
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
	f.expr(st.X)
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
				mv := *s.mv
				mv.loop = true
				s.mv = &mv
				at[v] = s
			}
		}
	}
	f.env = at
	t := &target{label: label, isLoop: true}
	f.targets = append(f.targets, t)
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
	t := &target{label: label}
	f.targets = append(f.targets, t)
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
		body := clause(c)
		for _, s := range body {
			f.stmt(s, "")
		}
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
