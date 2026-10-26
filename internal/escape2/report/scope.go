package report

import (
	"fmt"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/scoped"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// scopeTaskDiag writes E3401 for a loop finding at a scoped s.Task(lit)
// call (section 19.4, "Views of storage that ends before the join"): the
// task takes a variable of the loop body, which holds a view of storage
// that a later iteration reuses before s waits for the task. It returns
// nil when the finding is not at such a call.
func (r *reporter) scopeTaskDiag(f *finding) *diag.Diagnostic {
	if f.info == nil {
		return nil
	}
	ce, ok := innermost[*ast.CallExpr](f.path)
	if !ok || ce.Pos() != f.fd.Pos || len(ce.Args) != 1 {
		return nil
	}
	lit, ok := ast.Unparen(ce.Args[0]).(*ast.FuncLit)
	if !ok {
		return nil
	}
	name, recv := scoped.ScopeMethod(f.info.Selections, ce)
	sv := scoped.LocalScopeVar(f.info.Uses, recv)
	if name != "Task" || sv == nil {
		return nil
	}
	loop := r.loopAround(f.l.pos)
	if loop == nil || !contains(loop, ce.Pos()) || contains(loop, sv.Pos()) {
		return nil
	}
	// The variable the task takes: one the literal uses that is declared
	// in the loop, a carrier of the view first.
	var v, other *types2.Var
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		u, ok := f.info.Uses[id].(*types2.Var)
		if !ok || u.IsField() || isGlobal(u) || contains(lit, u.Pos()) || !contains(loop, u.Pos()) {
			return true
		}
		if f.carr[u] && u != f.l.v && v == nil {
			v = u
		}
		if other == nil {
			other = u
		}
		return true
	})
	if v == nil {
		v = other
	}
	if v == nil {
		return nil
	}
	what, held := "a view of storage of the loop", "it"
	if f.l.v != nil {
		what, held = "a view of "+f.l.v.Name(), f.l.v.Name()
	}
	sname := sv.Name()
	at := r.basePos(loop.Pos())
	d := diag.New(r.fset.Position(ce.Pos()), diag.ScopeOutlivesVar,
		"the task started here takes %s, %s, which the loop at %s reuses before %s waits for the task", v.Name(), what, at, sname)
	d.Text = fmt.Sprintf("%s's block ends with each iteration, so the task takes its own copy of %s when %s.Task runs. But %s holds %s, and a copy of a view still looks at the same memory. %s is declared in the loop at %s, which reuses %s's memory in every iteration, and %s does not wait for the task until after the loop, so a later iteration could change the memory while the task uses it.",
		v.Name(), v.Name(), sname, v.Name(), what, held, at, held, sname)
	d.Fix = &diag.Fix{Intro: fmt.Sprintf("Use %s itself in the function literal, so that the task gets its own copy of it, or wait for the task inside the loop", held), Code: sname + ".Wait() // at the end of the loop body"}
	return d
}
