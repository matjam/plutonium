package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Defer in the body of a range-over-func loop.
//
// The deferred call belongs to the enclosing function, as in Go: its
// function and arguments are saved in that function's frame, and its bit
// set in that function's defer mask, through the body's closure record.
// Like any defer, each defer statement has one slot, so a defer in a loop
// body must run at most once per call of the enclosing function: the
// statements after it must leave the loop on every path. deferLeavesLoop
// checks that.

// outermostLoop returns the range-over-func loop, among those whose bodies
// s is nested in, that is directly in the enclosing function.
func (s *fnState) outermostLoop() *rangeFunc {
	rf := s.rf
	for rf.parent.rf != nil {
		rf = rf.parent.rf
	}
	return rf
}

// deferredLit returns a func value for a function literal deferred in a
// loop body. Its closure record is built in the frame of the function that
// runs it, since the body's frame is gone by then; for the same reason it
// may not capture the body's own variables.
func (s *fnState) deferredLit(lit *ast.FuncLit) *ssa.Value {
	captures := s.capturedVars(lit)
	if len(captures) == 0 {
		return s.closure(lit)
	}
	outer := s.outermostLoop()
	for _, v := range captures {
		// The loop's own variables are declared ahead of its body.
		if p := v.Pos(); p >= outer.st.Pos() && p < outer.st.End() {
			s.l.errorf(lit.Pos(), "a function literal deferred in a range-over-func loop body cannot capture %s, a variable of the loop body: the deferred call runs after the body has returned", v.Name())
			return nil
		}
	}
	sym := s.closureFunc(lit, captures)
	root := s.rootFn()
	rec := root.tempSize(int64(8*(1+len(captures))), 8, "closure")
	recp := s.outerPtr(rec, root)
	s.store(ssa.TPtr, recp, s.funcAddr(sym))
	for i, v := range captures {
		s.store(ssa.TPtr, s.offPtr(recp, int64(8*(i+1))), s.lookupVar(v))
	}
	return recp
}

// deferLeavesLoop reports whether, after the defer statement d in the loop
// body s, every path leaves the outermost enclosing range-over-func loop
// (by return, panic, or break, continue or goto to a statement outside it)
// before the body can run again.
func (s *fnState) deferLeavesLoop(d *ast.DeferStmt) bool {
	rf := s.outermostLoop()
	c := &leaveCheck{
		info:   s.info,
		loop:   rf.st,
		label:  rf.label,
		parent: map[ast.Node]ast.Node{rf.st.Body: rf.st},
		labels: map[*types2.Label]*ast.LabeledStmt{},
		memo:   map[leaveKey]int8{},
	}
	var stack []ast.Node
	stack = append(stack, rf.st)
	ast.Inspect(rf.st.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if n != ast.Node(rf.st.Body) {
			c.parent[n] = stack[len(stack)-1]
		}
		if ls, ok := n.(*ast.LabeledStmt); ok {
			if l, ok := c.info.Defs[ls.Label].(*types2.Label); ok {
				c.labels[l] = ls
			}
		}
		stack = append(stack, n)
		return true
	})
	return c.after(d)
}

type leaveCheck struct {
	info   *types2.Info
	loop   *ast.RangeStmt
	label  *types2.Label // the loop's label, or nil
	parent map[ast.Node]ast.Node
	labels map[*types2.Label]*ast.LabeledStmt
	memo   map[leaveKey]int8
}

// A leaveKey is a position in a statement list: its owner (a block or a
// case clause) and an index.
type leaveKey struct {
	owner ast.Node
	i     int
}

func stmtList(owner ast.Node) []ast.Stmt {
	switch o := owner.(type) {
	case *ast.BlockStmt:
		return o.List
	case *ast.CaseClause:
		return o.Body
	case *ast.CommClause:
		return o.Body
	}
	return nil
}

// from reports whether every path from statement i of owner's list leaves
// the loop.
func (c *leaveCheck) from(owner ast.Node, i int) bool {
	key := leaveKey{owner, i}
	switch c.memo[key] {
	case 1:
		return true
	case 2, 3:
		return false
	}
	c.memo[key] = 3
	list := stmtList(owner)
	var ok bool
	if i >= len(list) {
		ok = c.afterList(owner)
	} else {
		ok = c.stmt(list[i], func() bool { return c.from(owner, i+1) })
	}
	c.memo[key] = 2
	if ok {
		c.memo[key] = 1
	}
	return ok
}

// afterList reports whether every path leaves the loop once owner's list
// has run to its end.
func (c *leaveCheck) afterList(owner ast.Node) bool {
	if owner == ast.Node(c.loop.Body) {
		return false // the next iteration
	}
	switch owner.(type) {
	case *ast.CaseClause, *ast.CommClause:
		return c.after(c.parent[c.parent[owner]])
	}
	switch p := c.parent[owner].(type) {
	case *ast.IfStmt, *ast.LabeledStmt:
		return c.after(p)
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
		return c.after(owner)
	}
	return false // a loop body
}

// after reports whether every path leaves the loop once n has completed.
func (c *leaveCheck) after(n ast.Node) bool {
	switch p := c.parent[n].(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
		for i, st := range stmtList(p) {
			if st == n {
				return c.from(p, i+1)
			}
		}
	case *ast.LabeledStmt, *ast.IfStmt:
		return c.after(p)
	}
	return false
}

// stmt reports whether every path through n leaves the loop, where cont
// tells for the paths that complete n.
func (c *leaveCheck) stmt(n ast.Stmt, cont func() bool) bool {
	switch n := n.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.ExprStmt:
		if c.isPanic(n.X) {
			return true
		}
	case *ast.BranchStmt:
		return c.branch(n)
	case *ast.BlockStmt:
		return c.from(n, 0)
	case *ast.LabeledStmt:
		return c.stmt(n.Stmt, cont)
	case *ast.IfStmt:
		if !c.from(n.Body, 0) {
			return false
		}
		if n.Else == nil {
			return cont()
		}
		return c.stmt(n.Else, cont)
	case *ast.SwitchStmt, *ast.TypeSwitchStmt:
		var body *ast.BlockStmt
		if sw, ok := n.(*ast.SwitchStmt); ok {
			body = sw.Body
		} else {
			body = n.(*ast.TypeSwitchStmt).Body
		}
		hasDefault := false
		for _, cl := range body.List {
			cc := cl.(*ast.CaseClause)
			if cc.List == nil {
				hasDefault = true
			}
			if !c.from(cc, 0) {
				return false
			}
		}
		return hasDefault || cont()
	}
	return c.escapes(n) && cont()
}

// escapes reports whether every branch out of n leaves the loop.
func (c *leaveCheck) escapes(n ast.Stmt) bool {
	ok := true
	ast.Inspect(n, func(m ast.Node) bool {
		switch m := m.(type) {
		case *ast.FuncLit:
			return false
		case *ast.BranchStmt:
			var t ast.Node
			switch m.Tok {
			case token.BREAK:
				t = c.breakTarget(m)
			case token.CONTINUE:
				t = c.continueTarget(m)
			case token.GOTO:
				if ls := c.labels[c.labelOf(m)]; ls != nil {
					t = ls
				}
			}
			inside := t != nil && t != ast.Node(c.loop) && t.Pos() >= n.Pos() && t.End() <= n.End()
			if !inside && !c.branch(m) {
				ok = false
			}
		}
		return ok
	})
	return ok
}

func (c *leaveCheck) labelOf(b *ast.BranchStmt) *types2.Label {
	if b.Label == nil {
		return nil
	}
	l, _ := c.info.Uses[b.Label].(*types2.Label)
	return l
}

// branch reports whether every path from the branch statement b leaves the
// loop.
func (c *leaveCheck) branch(b *ast.BranchStmt) bool {
	switch b.Tok {
	case token.GOTO:
		return c.labels[c.labelOf(b)] == nil // a label outside the loop
	case token.BREAK:
		switch t := c.breakTarget(b); t {
		case nil, ast.Node(c.loop):
			return true
		default:
			return c.after(t)
		}
	case token.CONTINUE:
		return c.continueTarget(b) == nil
	}
	return false // fallthrough
}

// breakTarget returns the statement b breaks out of: the loop itself, a
// statement in its body, or nil for a statement outside it.
func (c *leaveCheck) breakTarget(b *ast.BranchStmt) ast.Node {
	return c.target(b, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			return true
		}
		return false
	})
}

// continueTarget returns the loop b continues, as breakTarget does.
func (c *leaveCheck) continueTarget(b *ast.BranchStmt) ast.Node {
	return c.target(b, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			return true
		}
		return false
	})
}

func (c *leaveCheck) target(b *ast.BranchStmt, is func(ast.Node) bool) ast.Node {
	if l := c.labelOf(b); l != nil {
		if l == c.label {
			return c.loop
		}
		if ls := c.labels[l]; ls != nil {
			return ls.Stmt
		}
		return nil
	}
	for n := c.parent[b]; n != nil; n = c.parent[n] {
		if is(n) {
			return n
		}
	}
	return nil
}

func (c *leaveCheck) isPanic(e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := c.info.Uses[id].(*types2.Builtin)
	return ok && b.Name() == "panic"
}
