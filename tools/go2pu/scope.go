package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
)

// A Go program that waits for its goroutines with a sync.WaitGroup may let
// them use the variables of the function that starts them. Plutonium
// rejects that for a WaitGroup, which waits only where the program calls
// Wait (language spec section 19.4), and accepts it for a sync.Scope, which
// also waits when its block ends, so the translation rewrites a local
// WaitGroup into a Scope when it can do so exactly:
//
//	var wg sync.WaitGroup               var wg sync.Scope
//	for i := 0; i < n; i++ {            for i := 0; i < n; i++ {
//		wg.Add(1)                           wg.Task(func() {
//		go func() {              =>             out[i] = f(i)
//			defer wg.Done()                 })
//			out[i] = f(i)               }
//		}()                             wg.Wait()
//	}
//	wg.Wait()
//
// The rewrite applies when every goroutine the WaitGroup counts is started
// in one of these forms:
//
//   - wg.Go(f), with f a function literal;
//   - go func(...) {...}(...), where the literal begins with defer wg.Done()
//     or ends with wg.Done(), and a wg.Add statement precedes the go
//     statement (in its block or in a block around it). Each argument must
//     be a variable the function never assigns after its declaration, with
//     the type of its parameter; the parameter then stands for the variable.
//
// In both forms Done is the last thing the goroutine does, so Go's Wait
// already waited until the goroutine returned, and the rewrite changes
// nothing a Go program can observe. Any other use of the WaitGroup (it is
// passed to a function, a goroutine calls Done and keeps running, a Wait
// precedes a start, ...) keeps the WaitGroup and notes why.

// A scopePlan describes one local sync.WaitGroup and what the rewrite does
// to it.
type scopePlan struct {
	v      *types.Var
	decl   ast.Node // the *ast.ValueSpec or *ast.AssignStmt that declares it
	fn     ast.Node // the function that declares it
	block  *ast.BlockStmt
	reason string // why it stays a WaitGroup, or "" if it becomes a Scope

	adds    []*ast.ExprStmt
	used    map[*ast.ExprStmt]bool // the adds some start relies on
	waits   []*ast.ExprStmt
	goCalls []*ast.CallExpr
	starts  []*goStart
}

// A goStart is a go statement of a function literal that ends in Done.
type goStart struct {
	stmt   *ast.GoStmt
	lit    *ast.FuncLit
	done   ast.Stmt              // the statement calling Done
	rename map[*ast.Ident]string // parameter uses to replace by the argument variable
	endsIn bool                  // Done is the last statement (not the first, deferred)
}

type scopeKind int

const (
	scopeDecl   scopeKind = iota // the declaration of the WaitGroup
	scopeRemove                  // an Add or Done statement
	scopeGo                      // a go statement
	scopeCall                    // a wg.Go(f) call
	scopeRename                  // a use of a parameter the argument replaces
)

// A scopeEdit is the rewrite of one node.
type scopeEdit struct {
	kind  scopeKind
	plan  *scopePlan
	start *goStart
	name  string // scopeRename: the new identifier
}

// pkgType reports whether T is the named type name of the package at path.
func pkgType(T types.Type, path, name string) bool {
	n, ok := types.Unalias(T).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == path && n.Obj().Name() == name
}

func isWaitGroup(T types.Type) bool { return T != nil && pkgType(T, "sync", "WaitGroup") }

// nearestFunc returns the innermost function declaration or literal around n.
func (tc *translator) nearestFunc(n ast.Node) ast.Node {
	for p := tc.parent[n]; p != nil; p = tc.parent[p] {
		switch p.(type) {
		case *ast.FuncLit, *ast.FuncDecl:
			return p
		}
	}
	return nil
}

// indexParents records the parent of every node of the files.
func (tc *translator) indexParents(files []*ast.File) {
	tc.parent = map[ast.Node]ast.Node{}
	for _, f := range files {
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if len(stack) > 0 {
				tc.parent[n] = stack[len(stack)-1]
			}
			stack = append(stack, n)
			return true
		})
	}
	tc.usesOf = map[*types.Var][]*ast.Ident{}
	tc.defOf = map[*types.Var]*ast.Ident{}
	for id, obj := range tc.info.Defs {
		if v, ok := obj.(*types.Var); ok {
			tc.defOf[v] = id
		}
	}
	for id, obj := range tc.info.Uses {
		if v, ok := obj.(*types.Var); ok {
			tc.usesOf[v] = append(tc.usesOf[v], id)
		}
	}
	for _, ids := range tc.usesOf {
		sort.Slice(ids, func(i, j int) bool { return ids[i].Pos() < ids[j].Pos() })
	}
}

// planScopes decides, for each local WaitGroup of the files, whether it
// becomes a Scope, and records the edits of those that do.
func (tc *translator) planScopes(files []*ast.File, res *Result) {
	tc.scopeEdits = map[ast.Node]scopeEdit{}
	tc.scopedLits = map[*ast.FuncLit]bool{}
	tc.scopeVars = map[*types.Var]bool{}
	var plans []*scopePlan
	planned := map[*types.Var]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if p := tc.declaredWaitGroup(n); p != nil {
				plans = append(plans, p)
				planned[p.v] = true
			}
			return true
		})
	}
	// A local WaitGroup declared any other way (&sync.WaitGroup{},
	// new(sync.WaitGroup), several names in one var) stays.
	for id, obj := range tc.info.Defs {
		v, ok := obj.(*types.Var)
		if !ok || v.IsField() || planned[v] || v.Parent() == nil || v.Parent() == tc.pkg.Scope() {
			continue
		}
		if _, isParam := tc.parent[id].(*ast.Field); isParam {
			continue
		}
		T := v.Type()
		if p, ok := T.(*types.Pointer); ok {
			T = p.Elem()
		}
		if isWaitGroup(T) {
			tc.noteScope(res, id, "sync.WaitGroup %s stays: go2pu rewrites only a variable declared as var %s sync.WaitGroup or %s := sync.WaitGroup{}; hand-fix with a sync.Scope", v.Name(), v.Name(), v.Name())
		}
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].decl.Pos() < plans[j].decl.Pos() })
	for _, p := range plans {
		tc.analyzeScope(p)
		switch {
		case p.reason != "":
			tc.noteScope(res, p.decl, "sync.WaitGroup %s stays: %s; the goroutines it counts cannot use this function's variables in Plutonium: hand-fix with a sync.Scope", p.v.Name(), p.reason)
		case len(p.starts)+len(p.goCalls) > 0:
			tc.commitScope(p)
		}
	}
}

// noteScope records a note for the result.
func (tc *translator) noteScope(res *Result, n ast.Node, format string, args ...any) {
	pos := tc.fset.Position(n.Pos())
	res.Notes = append(res.Notes, fmt.Sprintf("%s:%d: %s", filepath.Base(pos.Filename), pos.Line, fmt.Sprintf(format, args...)))
}

// declaredWaitGroup returns the plan for n if n declares a local variable
// of type sync.WaitGroup with var or :=.
func (tc *translator) declaredWaitGroup(n ast.Node) *scopePlan {
	var id *ast.Ident
	switch n := n.(type) {
	case *ast.ValueSpec:
		if _, ok := tc.parent[tc.parent[n]].(*ast.DeclStmt); !ok || len(n.Names) != 1 {
			return nil
		}
		switch {
		case len(n.Values) == 0 && n.Type != nil && isWaitGroup(tc.typeOf(n.Type)):
		case len(n.Values) == 1 && n.Type == nil && tc.isEmptyWaitGroupLit(n.Values[0]):
		default:
			return nil
		}
		id = n.Names[0]
	case *ast.AssignStmt:
		if n.Tok != token.DEFINE || len(n.Lhs) != 1 || len(n.Rhs) != 1 || !tc.isEmptyWaitGroupLit(n.Rhs[0]) {
			return nil
		}
		id, _ = n.Lhs[0].(*ast.Ident)
	}
	if id == nil {
		return nil
	}
	v, ok := tc.info.Defs[id].(*types.Var)
	if !ok {
		return nil
	}
	p := &scopePlan{v: v, decl: n, fn: tc.nearestFunc(n), used: map[*ast.ExprStmt]bool{}}
	stmt := n
	if _, ok := n.(*ast.ValueSpec); ok {
		stmt = tc.parent[tc.parent[n]] // the DeclStmt
	}
	p.block, _ = tc.parent[stmt].(*ast.BlockStmt)
	return p
}

func (tc *translator) isEmptyWaitGroupLit(e ast.Expr) bool {
	lit, ok := ast.Unparen(e).(*ast.CompositeLit)
	return ok && len(lit.Elts) == 0 && isWaitGroup(tc.typeOf(lit))
}

// analyzeScope classifies every use of the plan's WaitGroup. It sets
// p.reason when one is not part of a form go2pu rewrites.
func (tc *translator) analyzeScope(p *scopePlan) {
	fail := func(format string, args ...any) {
		if p.reason == "" {
			p.reason = fmt.Sprintf(format, args...)
		}
	}
	if p.block == nil {
		fail("it is not declared in a block of its own")
		return
	}
	dones := map[*ast.CallExpr]bool{}
	for _, id := range tc.usesOf[p.v] {
		sel, ok := tc.parent[id].(*ast.SelectorExpr)
		if !ok || sel.X != id {
			fail("it is used other than by calling its methods (passed, stored or its address taken)")
			continue
		}
		call, ok := tc.parent[sel].(*ast.CallExpr)
		if !ok || call.Fun != sel {
			fail("a method of it is used as a value")
			continue
		}
		stmt := tc.parent[call]
		inFn := tc.nearestFunc(call) == p.fn
		switch sel.Sel.Name {
		case "Add":
			es, isStmt := stmt.(*ast.ExprStmt)
			switch {
			case !isStmt || !inFn:
				fail("Add is called in a function literal or in an expression")
			case len(call.Args) != 1 || !tc.pure(call.Args[0]):
				fail("the argument of Add has side effects")
			default:
				p.adds = append(p.adds, es)
			}
		case "Wait":
			es, isStmt := stmt.(*ast.ExprStmt)
			if !isStmt || !inFn {
				fail("Wait is deferred, or called in a function literal or in an expression")
			} else {
				p.waits = append(p.waits, es)
			}
		case "Go":
			_, isStmt := stmt.(*ast.ExprStmt)
			var lit *ast.FuncLit
			if len(call.Args) == 1 {
				lit, _ = ast.Unparen(call.Args[0]).(*ast.FuncLit)
			}
			switch {
			case !isStmt || !inFn:
				fail("Go is called in a function literal or in an expression")
			case lit == nil:
				fail("Go starts a function that is not a function literal")
			default:
				p.goCalls = append(p.goCalls, call)
			}
		case "Done":
			dones[call] = true
		default:
			fail("it calls %s", sel.Sel.Name)
		}
	}
	// Every Done must close a goroutine started by a go statement of a
	// function literal.
	var doneCalls []*ast.CallExpr
	for c := range dones {
		doneCalls = append(doneCalls, c)
	}
	sort.Slice(doneCalls, func(i, j int) bool { return doneCalls[i].Pos() < doneCalls[j].Pos() })
	byLit := map[*ast.FuncLit]*goStart{}
	for _, c := range doneCalls {
		lit, _ := tc.nearestFunc(c).(*ast.FuncLit)
		if lit == nil || lit == p.fn {
			fail("Done is called outside a goroutine")
			continue
		}
		g := tc.goOf(lit)
		if g == nil || tc.nearestFunc(g) != p.fn {
			fail("Done is called in a function literal that is not started by a go statement of this function")
			continue
		}
		if byLit[lit] != nil {
			fail("a goroutine calls Done twice")
			continue
		}
		st := &goStart{stmt: g, lit: lit}
		byLit[lit] = st
		if why := tc.doneForm(st, c); why != "" {
			fail("%s", why)
			continue
		}
		p.starts = append(p.starts, st)
	}
	if p.reason != "" {
		return
	}
	if len(p.starts)+len(p.goCalls) == 0 {
		if len(p.adds) > 0 {
			fail("Add has no goroutine")
		}
		return
	}
	for _, st := range p.starts {
		if !tc.addBefore(p, st.stmt) {
			fail("a goroutine is not preceded by a wg.Add statement")
			return
		}
		if why := tc.bindArgs(st); why != "" {
			fail("%s", why)
			return
		}
	}
	for _, a := range p.adds {
		if !p.used[a] {
			fail("Add has no goroutine")
			return
		}
	}
	// A Wait at the top level of the declaring block, after every start,
	// keeps the Scope's wait at the end of the block from changing when
	// the program waits.
	last := token.NoPos
	for _, w := range p.waits {
		if tc.parent[w] == p.block && w.Pos() > last {
			last = w.Pos()
		}
	}
	if last == token.NoPos {
		fail("no Wait call at the top level of its block")
		return
	}
	for _, st := range p.starts {
		if st.stmt.Pos() > last {
			fail("a goroutine is started after the last Wait")
			return
		}
	}
	for _, c := range p.goCalls {
		if c.Pos() > last {
			fail("a goroutine is started after the last Wait")
			return
		}
	}
}

// goOf returns the go statement that starts the function literal lit by
// calling it, or nil.
func (tc *translator) goOf(lit *ast.FuncLit) *ast.GoStmt {
	call, ok := tc.parent[lit].(*ast.CallExpr)
	if !ok || call.Fun != lit {
		return nil
	}
	g, _ := tc.parent[call].(*ast.GoStmt)
	return g
}

// doneForm checks that the Done call c is the first statement of the
// goroutine, deferred, or its last statement, and returns why not.
func (tc *translator) doneForm(st *goStart, c *ast.CallExpr) string {
	body := st.lit.Body
	if st.lit.Type.Results != nil {
		return "a goroutine's function literal has results"
	}
	switch s := tc.parent[c].(type) {
	case *ast.DeferStmt:
		if len(body.List) == 0 || body.List[0] != ast.Stmt(s) {
			return "a goroutine defers Done after other statements"
		}
		st.done = s
		return ""
	case *ast.ExprStmt:
		if tc.parent[s] != body {
			return "a goroutine calls Done in a nested statement"
		}
		if body.List[len(body.List)-1] != ast.Stmt(s) {
			return "a goroutine calls Done and keeps running (a latch)"
		}
		st.done = s
		st.endsIn = true
		if tc.hasStmt(body, func(n ast.Node) bool { _, ok := n.(*ast.ReturnStmt); return ok }) {
			return "a goroutine returns before it calls Done"
		}
		if tc.hasStmt(body, func(n ast.Node) bool { _, ok := n.(*ast.DeferStmt); return ok }) {
			return "a goroutine defers calls that run after Done"
		}
		return ""
	}
	return "a goroutine calls Done in an expression"
}

// hasStmt reports whether a node of the body (outside nested function
// literals) satisfies f.
func (tc *translator) hasStmt(body *ast.BlockStmt, f func(ast.Node) bool) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if n != nil && f(n) {
			found = true
		}
		return !found
	})
	return found
}

// addBefore reports whether an Add statement of p precedes the go statement
// g in its block or in a block around it, and marks that statement used.
func (tc *translator) addBefore(p *scopePlan, g *ast.GoStmt) bool {
	var cur ast.Node = g
	for {
		parent := tc.parent[cur]
		if parent == nil || parent == p.fn {
			return false
		}
		var list []ast.Stmt
		switch b := parent.(type) {
		case *ast.BlockStmt:
			list = b.List
		case *ast.CaseClause:
			list = b.Body
		case *ast.CommClause:
			list = b.Body
		}
		// The nearest Add before cur in this block is the one the go
		// statement relies on.
		var near *ast.ExprStmt
		for _, s := range list {
			if s.Pos() >= cur.Pos() {
				break
			}
			if es, ok := s.(*ast.ExprStmt); ok {
				for _, a := range p.adds {
					if a == es {
						near = es
					}
				}
			}
		}
		if near != nil {
			p.used[near] = true
			return true
		}
		cur = parent
	}
}

// pure reports whether evaluating e has no side effects, so dropping it
// does not change the program.
func (tc *translator) pure(e ast.Expr) bool {
	if tc.info.Types[e].Value != nil {
		return true
	}
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		_, isVar := tc.info.Uses[e].(*types.Var)
		return isVar
	case *ast.BinaryExpr:
		return e.Op != token.QUO && e.Op != token.REM && e.Op != token.SHL && e.Op != token.SHR && tc.pure(e.X) && tc.pure(e.Y)
	case *ast.UnaryExpr:
		return (e.Op == token.ADD || e.Op == token.SUB) && tc.pure(e.X)
	case *ast.SelectorExpr:
		sel := tc.info.Selections[e]
		return sel != nil && sel.Kind() == types.FieldVal && !sel.Indirect() && tc.pure(e.X)
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok && len(e.Args) == 1 {
			if b, ok := tc.info.Uses[id].(*types.Builtin); ok && (b.Name() == "len" || b.Name() == "cap") {
				return tc.pure(e.Args[0])
			}
		}
	}
	return false
}

// bindArgs matches the arguments of a go statement with the parameters of
// its literal. A parameter stands for its argument, a variable that is
// never assigned after its declaration, so the task reads the same value
// the go statement passed.
func (tc *translator) bindArgs(st *goStart) string {
	call := st.stmt.Call
	var params []*ast.Ident
	for _, f := range st.lit.Type.Params.List {
		if _, variadic := f.Type.(*ast.Ellipsis); variadic {
			return "a goroutine has a variadic parameter"
		}
		if len(f.Names) == 0 {
			return "a goroutine has an unnamed parameter"
		}
		params = append(params, f.Names...)
	}
	if len(params) != len(call.Args) || call.Ellipsis != token.NoPos {
		return "a goroutine's arguments do not match its parameters"
	}
	st.rename = map[*ast.Ident]string{}
	for i, id := range params {
		if id.Name == "_" {
			continue
		}
		pv, _ := tc.info.Defs[id].(*types.Var)
		arg, ok := ast.Unparen(call.Args[i]).(*ast.Ident)
		if pv == nil || !ok {
			return fmt.Sprintf("the argument for %s is not a plain variable", id.Name)
		}
		av, ok := tc.info.Uses[arg].(*types.Var)
		if !ok || av.IsField() || av.Parent() == nil || av.Parent() == tc.pkg.Scope() {
			return fmt.Sprintf("the argument for %s is not a local variable", id.Name)
		}
		switch av.Type().Underlying().(type) {
		case *types.Struct, *types.Array:
			return fmt.Sprintf("the argument for %s is a struct or array, which the goroutine would copy", id.Name)
		}
		if !types.Identical(pv.Type(), av.Type()) {
			return fmt.Sprintf("the argument for %s has another type than the parameter", id.Name)
		}
		if tc.isResult(av) {
			return fmt.Sprintf("the argument %s is a named result, which return assigns", arg.Name)
		}
		if !tc.finalVar(av) {
			return fmt.Sprintf("the argument %s is assigned after its declaration", arg.Name)
		}
		if !tc.finalVar(pv) {
			return fmt.Sprintf("the goroutine assigns or takes the address of its parameter %s", id.Name)
		}
		if tc.esc.has(tc.fset, id.Pos(), "moved to heap: "+id.Name, "") {
			return fmt.Sprintf("the goroutine's parameter %s moves to the heap", id.Name)
		}
		if id.Name == arg.Name {
			continue
		}
		scope := tc.info.Scopes[st.lit.Type]
		if scope == nil {
			return "scope information is missing"
		}
		for _, u := range tc.usesOf[pv] {
			_, obj := scope.Innermost(u.Pos()).LookupParent(arg.Name, u.Pos())
			if obj != types.Object(av) {
				return fmt.Sprintf("the name %s is redeclared where the goroutine uses %s", arg.Name, id.Name)
			}
			st.rename[u] = arg.Name
		}
	}
	return ""
}

// isResult reports whether v is a named result of its function.
func (tc *translator) isResult(v *types.Var) bool {
	field, ok := tc.parent[tc.defOf[v]].(*ast.Field)
	if !ok {
		return false
	}
	ft, ok := tc.parent[tc.parent[field]].(*ast.FuncType)
	return ok && ft.Results == tc.parent[field]
}

// finalVar reports whether v is never assigned after its declaration, and
// its address is never taken. The post statement of the 3-clause for loop
// that declares v may assign it: each iteration has its own copy (Go 1.22).
func (tc *translator) finalVar(v *types.Var) bool {
	for _, id := range tc.usesOf[v] {
		if tc.assigns(id) {
			return false
		}
	}
	return true
}

// assigns reports whether the use id is assigned, incremented, or has its
// address taken.
func (tc *translator) assigns(id *ast.Ident) bool {
	var cur ast.Node = id
	for {
		if _, ok := tc.parent[cur].(*ast.ParenExpr); !ok {
			break
		}
		cur = tc.parent[cur]
	}
	switch p := tc.parent[cur].(type) {
	case *ast.UnaryExpr:
		return p.Op == token.AND
	case *ast.AssignStmt:
		for _, l := range p.Lhs {
			if l == cur {
				return !tc.isLoopPost(p, id)
			}
		}
	case *ast.IncDecStmt:
		return !tc.isLoopPost(p, id)
	case *ast.RangeStmt:
		return p.Tok == token.ASSIGN && (p.Key == cur || p.Value == cur)
	}
	return false
}

// isLoopPost reports whether s is the post statement of a for loop whose
// init statement declares the variable id refers to.
func (tc *translator) isLoopPost(s ast.Stmt, id *ast.Ident) bool {
	f, ok := tc.parent[s].(*ast.ForStmt)
	if !ok || f.Post != s {
		return false
	}
	init, ok := f.Init.(*ast.AssignStmt)
	if !ok || init.Tok != token.DEFINE {
		return false
	}
	for _, l := range init.Lhs {
		if d, ok := l.(*ast.Ident); ok && tc.info.Defs[d] == tc.info.Uses[id] {
			return true
		}
	}
	return false
}

// commitScope records the edits of a plan that applies.
func (tc *translator) commitScope(p *scopePlan) {
	tc.scopeEdits[p.decl] = scopeEdit{kind: scopeDecl, plan: p}
	tc.scopeVars[p.v] = true
	for _, a := range p.adds {
		tc.scopeEdits[a] = scopeEdit{kind: scopeRemove}
	}
	for _, st := range p.starts {
		tc.scopeEdits[st.stmt] = scopeEdit{kind: scopeGo, plan: p, start: st}
		tc.scopeEdits[st.done] = scopeEdit{kind: scopeRemove}
		tc.scopedLits[st.lit] = true
		for id, name := range st.rename {
			tc.scopeEdits[id] = scopeEdit{kind: scopeRename, name: name}
		}
	}
	for _, c := range p.goCalls {
		tc.scopeEdits[c] = scopeEdit{kind: scopeCall, plan: p}
		tc.scopedLits[ast.Unparen(c.Args[0]).(*ast.FuncLit)] = true
	}
}

// scopeRewrite translates a node that a planned Scope rewrites.
func (t *fileTr) scopeRewrite(n ast.Node, e scopeEdit) string {
	switch e.kind {
	case scopeDecl:
		scope := t.use("pluto/sync", t.syncName()) + ".Scope"
		name := e.plan.v.Name()
		if _, ok := n.(*ast.AssignStmt); ok {
			return "var " + name + " " + scope
		}
		return name + " " + scope
	case scopeRename:
		return e.name
	case scopeGo:
		return e.plan.v.Name() + ".Task(func() " + t.render(e.start.lit.Body) + ")"
	case scopeCall:
		call := n.(*ast.CallExpr)
		return e.plan.v.Name() + ".Task(" + t.splice(ast.Unparen(call.Args[0]).(*ast.FuncLit)) + ")"
	}
	return "" // scopeRemove
}

// syncName is the name this file uses for package sync.
func (t *fileTr) syncName() string {
	if n, ok := t.local["sync"]; ok {
		return n
	}
	return "sync"
}

// scopeOnlyEscape reports whether v is a variable that Go moves to the
// heap only because a goroutine that becomes a task of a Scope uses it. A
// Scope waits for its tasks before the frame ends, so such a variable
// stays in the frame. It reports false whenever any use might let the
// variable outlive the frame: a closure that escapes uses it, its address
// is taken (or passed to a method that might keep it), or it is a result.
func (tc *translator) scopeOnlyEscape(v *types.Var, def *ast.Ident) bool {
	if tc.isResult(v) {
		return false
	}
	decl := tc.nearestFunc(def)
	scoped := false
	for _, u := range tc.usesOf[v] {
		for f := tc.nearestFunc(u); f != decl; f = tc.nearestFunc(f) {
			lit, ok := f.(*ast.FuncLit)
			if !ok {
				return false
			}
			if tc.scopedLits[lit] {
				scoped = true
			} else if tc.esc.has(tc.fset, lit.Pos(), "func literal escapes", "") {
				return false
			}
		}
		if !tc.plainUse(u) {
			return false
		}
	}
	return scoped
}

// plainUse reports whether the use u of a variable only reads or writes it,
// without a pointer to the variable or to a part of it leaving the
// expression. A call of a pointer-receiver method of package sync or
// sync/atomic (mu.Lock()) counts: those keep nothing.
func (tc *translator) plainUse(u *ast.Ident) bool {
	var cur ast.Node = u
	for {
		switch p := tc.parent[cur].(type) {
		case *ast.ParenExpr:
			cur = p
		case *ast.UnaryExpr:
			return p.Op != token.AND
		case *ast.SliceExpr:
			if p.X != cur {
				return true
			}
			_, isArray := tc.typeOf(p.X).Underlying().(*types.Array)
			return !isArray
		case *ast.IndexExpr:
			if p.X != cur {
				return true
			}
			if _, isArray := tc.typeOf(p.X).Underlying().(*types.Array); !isArray {
				return true
			}
			cur = p
		case *ast.SelectorExpr:
			sel := tc.info.Selections[p]
			if p.X != cur || sel == nil || sel.Indirect() {
				return true
			}
			switch sel.Kind() {
			case types.FieldVal:
				cur = p
			case types.MethodVal:
				fn, ok := sel.Obj().(*types.Func)
				if !ok {
					return false
				}
				if _, ptr := fn.Type().(*types.Signature).Recv().Type().(*types.Pointer); !ptr {
					return true
				}
				call, isCall := tc.parent[p].(*ast.CallExpr)
				path := ""
				if fn.Pkg() != nil {
					path = fn.Pkg().Path()
				}
				return isCall && call.Fun == p && (path == "sync" || path == "sync/atomic")
			default:
				return true
			}
		default:
			return true
		}
	}
}
