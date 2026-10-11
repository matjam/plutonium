package moves

import (
	"fmt"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/scoped"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// Scoped tasks (language spec section 19.4).
//
// A task started by s.Task on a sync.Scope of this function takes the
// variables it captures whose block may end before s waits for it
// (internal/scoped): an owner among them moves into the task when Task
// runs, so the starting function holds nothing in it afterwards, and the
// task owns it: its literal may move it on, and the task drops what is
// left when the literal returns. The variables it shares stay where they
// are; moving one inside the literal is still E3305, since the starting
// function drops it at the end of its block.
//
// A Scope that starts tasks may not be moved (E3405): the tasks find it at
// its address when they return.

// litOf returns the function literal whose body is body, if it is a scoped
// task's.
func (c *checker) litOf(body *ast.BlockStmt) *ast.FuncLit {
	if c.taskBodies == nil {
		c.taskBodies = map[*ast.BlockStmt]*ast.FuncLit{}
		for lit := range c.scoped.Lits {
			c.taskBodies[lit.Body] = lit
		}
	}
	return c.taskBodies[body]
}

// scopedTask walks the literal of a scoped task where Task starts it.
func (f *fn) scopedTask(t *scoped.Task) {
	lit := t.Lit
	first := map[*types2.Var]*ast.Ident{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if v := f.varOf(id); v != nil && f.vars[v] && f.c.uses[id] != nil {
			f.use(id, v)
			if first[v] == nil {
				first[v] = id
			}
		}
		return true
	})
	for _, v := range t.Captures {
		if !t.Taken[v] || !isOwner(v.Type()) || !f.vars[v] || f.env == nil {
			continue
		}
		f.env[v] = vstate{st: moved, mv: &move{pos: t.Call.Pos(), kind: mvTask, to: t.Scope.Var.Name() + ".Task"}}
	}
	if f.report {
		sig, _ := f.c.typeOf(lit).(*types2.Signature)
		f.c.checkFunc(lit.Type, nil, lit.Body, sig)
	}
}

// scopeMovedDiag explains a move of a Scope that starts tasks.
func (c *checker) scopeMovedDiag(id *ast.Ident, v *types2.Var, sc *scoped.Scope) *diag.Diagnostic {
	name := v.Name()
	at := ""
	if len(sc.Calls) > 0 {
		at = " at " + c.basePos(sc.Calls[0].Pos())
	}
	d := c.newDiag(id.Pos(), diag.ScopeMoved, "cannot move %s: it starts tasks%s, which find it at its address", name, at)
	d.Text = fmt.Sprintf("%s is a sync.Scope, and %s.Task%s starts tasks that tell %s when they return, at the address %s has in this function. Moving %s (assigning, passing or returning it) would leave them telling the old place, so nothing would wait for them where %s ends up.", name, name, at, name, name, name, name)
	d.Fix = &diag.Fix{Intro: fmt.Sprintf("Keep %s where it is and wait for its tasks before it goes", name), Code: name + ".Wait()"}
	return d
}
