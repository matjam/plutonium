package escape

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Flow-sensitive locals.
//
// Most of the analysis is flow-insensitive: a location holds everything
// ever stored in it. For a local variable whose address is never taken,
// that is needlessly coarse, and Go code reuses such variables all the time
// (err above all). Their values are tracked along the control flow instead:
// an assignment replaces what the variable holds, and paths join where
// control flow merges.

// An env maps each tracked variable's location to what it holds at a point
// of the program. A nil env means the point cannot be reached.
type env map[*loc]tset

func (e env) clone() env {
	if e == nil {
		return nil
	}
	out := make(env, len(e))
	for k, v := range e {
		out[k] = v
	}
	return out
}

// join merges two envs where control flow meets.
func join(a, b env) env {
	if a == nil {
		return b.clone()
	}
	if b == nil {
		return a.clone()
	}
	out := a.clone()
	for k, v := range b {
		s := out[k]
		s = union(s, v)
		out[k] = s
	}
	return out
}

func envEqual(a, b env) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for k, v := range a {
		u, ok := b[k]
		if !ok || len(u) != len(v) {
			return false
		}
		for _, t := range v {
			if !u.has(t) {
				return false
			}
		}
	}
	return true
}

// A flowTarget is a statement that break or continue may leave: it
// collects the envs that jump to its end or to its next iteration.
type flowTarget struct {
	label  string
	isLoop bool
	breaks env
	conts  env
}

// computeTracked finds the local variables of the function and its
// literals whose address is never taken and whose values the analysis can
// therefore follow along the control flow. A function with goto tracks
// nothing.
func (w *walker) computeTracked(body ast.Node) {
	w.tracked = map[*types2.Var]bool{}
	if body == nil {
		return
	}
	taken := map[*types2.Var]bool{}
	hasGoto := false
	rootVar := func(e ast.Expr) *types2.Var {
		for {
			switch x := e.(type) {
			case *ast.ParenExpr:
				e = x.X
			case *ast.SelectorExpr:
				if pointerElem(w.typeOf(x.X)) != nil || w.a.selections[x] == nil {
					return nil
				}
				e = x.X
			case *ast.IndexExpr:
				if _, ok := underlying(w.typeOf(x.X)).(*types2.Array); !ok {
					return nil
				}
				e = x.X
			case *ast.Ident:
				v, _ := w.objOf(x).(*types2.Var)
				return v
			default:
				return nil
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BranchStmt:
			if n.Tok == token.GOTO {
				hasGoto = true
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				if v := rootVar(n.X); v != nil {
					taken[v] = true
				}
			}
		case *ast.SliceExpr:
			if v := rootVar(n.X); v != nil {
				taken[v] = true
			}
		case *ast.SelectorExpr:
			// x.M with a pointer method on an addressable x takes &x.
			if sel := w.a.selections[n]; sel != nil && sel.Kind() == types2.MethodVal && pointerElem(w.typeOf(n.X)) == nil {
				fn, _ := sel.Obj().(*types2.Func)
				var recv *types2.Var
				if fn != nil {
					recv = fn.Type().(*types2.Signature).Recv()
				}
				if recv == nil || pointerElem(recv.Type()) != nil || len(sel.Index()) > 1 {
					if v := rootVar(n.X); v != nil {
						taken[v] = true
					}
				}
			}
		case *ast.FuncLit:
			for _, v := range w.captured(n) {
				taken[v] = true
			}
		}
		return true
	})
	if hasGoto {
		return
	}
	// Results stay flow-insensitive: what they hold is checked when the
	// function returns, whichever path it takes.
	ast.Inspect(body, func(n ast.Node) bool {
		if ft, ok := n.(*ast.FuncType); ok && ft.Results != nil {
			for _, f := range ft.Results.List {
				for _, id := range f.Names {
					if v, ok := w.a.defs[id].(*types2.Var); ok {
						taken[v] = true
					}
				}
			}
		}
		return true
	})
	ast.Inspect(body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := w.a.defs[id].(*types2.Var)
		if !ok || taken[v] || isGlobal(v) || v.IsField() || !flowType(v.Type()) {
			return true
		}
		w.tracked[v] = true
		return true
	})
	for _, obj := range w.a.implicits {
		if v, ok := obj.(*types2.Var); ok && !taken[v] && flowType(v.Type()) && v.Pos() >= body.Pos() && v.Pos() < body.End() {
			w.tracked[v] = true
		}
	}
}

// flowType reports whether a variable of type T is followed along the
// control flow: anything but structs, arrays and Plutonium's storage
// wrappers, whose parts are stored separately.
func flowType(T types2.Type) bool {
	switch T.Underlying().(type) {
	case *types2.Struct, *types2.Array, *types2.Wrapper, *types2.Packed:
		return false
	}
	return true
}

// isTracked reports whether v's value is followed along the control flow.
func (w *walker) isTracked(v *types2.Var) bool { return w.tracked[v] }

// readVar returns what a tracked variable holds here.
func (w *walker) readVar(l *loc) tset {
	return append(tset(nil), w.env[l]...)
}

// setVar makes a tracked variable hold v from here on.
func (w *walker) setVar(l *loc, v tset) {
	if w.env != nil {
		w.env[l] = append(tset(nil), v...)
	}
}

// pushTarget starts a breakable statement.
func (w *walker) pushTarget(label string, isLoop bool) *flowTarget {
	t := &flowTarget{label: label, isLoop: isLoop}
	w.targets = append(w.targets, t)
	return t
}

func (w *walker) popTarget() { w.targets = w.targets[:len(w.targets)-1] }

// branch records a break or continue and makes the rest of the path
// unreachable.
func (w *walker) branch(st *ast.BranchStmt) {
	var label string
	if st.Label != nil {
		label = st.Label.Name
	}
	for i := len(w.targets) - 1; i >= 0; i-- {
		t := w.targets[i]
		if label != "" && t.label != label {
			continue
		}
		switch st.Tok {
		case token.BREAK:
			t.breaks = join(t.breaks, w.env)
		case token.CONTINUE:
			if !t.isLoop {
				continue
			}
			t.conts = join(t.conts, w.env)
		default:
			return
		}
		w.env = nil
		return
	}
}

// clauses walks the clauses of a switch or select. Each starts where the
// statement does, or where the previous clause ended if that clause falls
// through; the statement ends where any clause does, or where it started if
// there is no default clause. clause walks a clause's header and returns
// its body.
func (w *walker) clauses(label string, list []ast.Stmt, clause func(ast.Stmt) []ast.Stmt) {
	entry := w.env.clone()
	t := w.pushTarget(label, false)
	var out env
	hasDefault := false
	var prev env
	for _, c := range list {
		switch c := c.(type) {
		case *ast.CaseClause:
			hasDefault = hasDefault || c.List == nil
		case *ast.CommClause:
			hasDefault = hasDefault || c.Comm == nil
		}
		w.env = join(entry, prev)
		body := clause(c)
		for _, s := range body {
			w.stmt(s)
		}
		prev = nil
		if n := len(body); n > 0 {
			if b, ok := body[n-1].(*ast.BranchStmt); ok && b.Tok == token.FALLTHROUGH {
				prev = w.env
			}
		}
		out = join(out, w.env)
	}
	if !hasDefault {
		out = join(out, entry)
	}
	w.env = join(out, t.breaks)
	w.popTarget()
}

// loop walks the body of loop st until the envs at its head stop changing.
// body walks one iteration from the head; head, if any, runs at the head.
// Where the next iteration starts, the tracked locals may not hold views
// of the iteration's storage (loops.go).
func (w *walker) loop(label string, st ast.Stmt, head func(), body func()) {
	entry := w.env
	t := w.pushTarget(label, true)
	at := entry.clone()
	for i := 0; ; i++ {
		w.env = at.clone()
		if head != nil {
			head()
		}
		afterHead := w.env
		t.conts = nil
		body()
		back := join(w.env, t.conts)
		w.checkBackEdge(w.loopOf[st], back)
		next := join(entry, back)
		if envEqual(next, at) || i > 20 {
			w.env = join(afterHead, t.breaks)
			break
		}
		at = next
	}
	w.popTarget()
}
