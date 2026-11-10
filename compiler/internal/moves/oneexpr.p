package moves

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/syntax/ast"
	"compiler/internal/types2"
	"compiler/pu"
)

// Moves and uses in one expression (language spec sections 11.14 and 18.5).
// One expression or statement may not move a variable and also use it, in
// either order. The walk reports a use that follows the move as an ordinary
// use after a move; a use that comes before the move in the same expression
// is found here, once the expression has been walked.

// A unitMove is a move of v at id made while walking the current unit.
type unitMove struct {
	id *ast.Ident
	v  *types2.Var
}

// noteMove records a move made in the unit being walked.
func (f *fn) noteMove(id *ast.Ident, v *types2.Var, kind moveKind) {
	if !f.report || kind == mvTask {
		return
	}
	f.unitMoves = compat.Append(f.unitMoves, unitMove{id: id, v: v})
}

// beginUnit starts a unit: a simple statement, or the condition, tag or
// range expression of a compound one. It returns the moves of the unit
// around it, which endUnit puts back.
func (f *fn) beginUnit() []unitMove {
	saved := f.unitMoves
	f.unitMoves = nil
	return saved
}

// endUnit checks the unit n for other uses of the variables it moved. The
// whole variables in lhs are assigned, which is not a use: v = grow(v)
// and a, b = b, a are allowed.
func (f *fn) endUnit(saved []unitMove, n ast.Node, lhs []ast.Expr) {
	moved := f.unitMoves
	f.unitMoves = saved
	if len(moved) == 0 || n == nil {
		return
	}
	assigned := map[*ast.Ident]bool{}
	for _, l := range lhs {
		if id, ok := ast.Unparen(l).(*ast.Ident); ok {
			assigned[id] = true
		}
	}
	movers := map[*ast.Ident]bool{}
	for _, m := range moved {
		movers[m.id] = true
	}
	for _, m := range moved {
		ast.Inspect(n, func(x ast.Node) bool {
			if e, ok := x.(ast.Expr); ok {
				// A constant expression such as len of an array reads
				// nothing.
				if tv, ok := f.c.types[e]; ok && !tv.Value.IsNil() {
					return false
				}
			}
			id, ok := x.(*ast.Ident)
			if !ok || id == m.id || assigned[id] || movers[id] {
				return true
			}
			if v, ok := f.c.objOf(id).(*types2.Var); !ok || v != m.v {
				return true
			}
			if f.c.reported(id) {
				return true
			}
			f.c.report(f.c.sameExprDiag(id, m, useExpr(n, id, m.id)))
			return true
		})
	}
}

// reported reports whether a use or move error is already reported at id.
func (c *checker) reported(id *ast.Ident) bool {
	p := c.fset.Position(id.Pos()).String()
	for _, code := range []diag.Code{diag.UseAfterMove, diag.UseMaybeMoved, diag.MoveCaptured} {
		if c.seen[compat.Concat(p, string(code))] {
			return true
		}
	}
	return false
}

// useExpr returns the outermost call in n around the use that does not
// contain the move, for the fix, or nil.
func useExpr(n ast.Node, use, mover *ast.Ident) ast.Expr {
	var best ast.Expr
	var stack []ast.Node
	done := false
	ast.Inspect(n, func(x ast.Node) bool {
		if done {
			return false
		}
		if x == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = compat.Append(stack, x)
		if x != ast.Node(use) {
			return true
		}
		for i := len(stack) - 2; i >= 0; i-- {
			e, ok := stack[i].(ast.Expr)
			if !ok || contains(e, mover) {
				break
			}
			if _, isCall := e.(*ast.CallExpr); isCall {
				best = e
			}
		}
		done = true
		return false
	})
	return best
}

// contains reports whether id is inside e.
func contains(e ast.Expr, id *ast.Ident) bool {
	found := false
	ast.Inspect(e, func(x ast.Node) bool {
		if x == ast.Node(id) {
			found = true
		}
		return !found
	})
	return found
}

// sameExprDiag reports the use at id of a variable that the same
// expression moves at m.id.
func (c *checker) sameExprDiag(id *ast.Ident, m unitMove, use ast.Expr) *diag.Diagnostic {
	name := m.v.Name()
	d := c.newDiag(id.Pos(), diag.UseAfterMove, "%s is used here and moved by the same expression at column %d", pu.Box(name), c.fset.Position(m.id.Pos()).Column)
	d.Text = compat.Sprintfb("%s One expression or statement may not move a variable and also use it, in either order: whether the use sees the value would depend on the order the parts are evaluated in, and a use that runs after the move would find %s holding nothing.", compat.ArgS(c.ownerIntro(m.v.Type())), compat.ArgS(name))
	d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Do the use in a statement of its own before the one that moves %s, or move a copy", compat.ArgS(name))})
	if use != nil {
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Do the use in a statement of its own before the one that moves %s, and use v in its place", compat.ArgS(name)), Code: compat.Concat("v := ", exprText(use))})
	}
	return d
}

// exprUnit walks e as a unit of its own.
func (f *fn) exprUnit(e ast.Expr) {
	u := f.beginUnit()
	f.expr(e)
	f.endUnit(u, e, nil)
}
