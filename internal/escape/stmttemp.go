package escape

import (
	"fmt"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// Owner temporaries as receivers (language spec section 11.14).
//
// A pointer-receiver method may be called on a temporary of owner type,
// such as the result of a call: the temporary itself is the receiver, and
// it is dropped at the end of the statement (section 18.6; a temporary in
// the header of an if or switch statement at the end of the whole
// statement). A view of it may be passed to calls in the statement, which
// the call rules check as for any view, but it may not be kept: storing it
// in a variable, in memory, or in a temporary that is itself kept (an
// interface's copy, a literal) is an error, unless the variable is declared
// in the same if or switch header as the temporary, and so ends with it.

// stmtTempOf returns the owner temporary that l is, or that a temporary of
// the statement holding a view in l (an interface's copy, a literal,
// variadic arguments) looks at, or nil.
func stmtTempOf(l *loc) *loc {
	return stmtTempIn(l, map[*loc]bool{})
}

func stmtTempIn(l *loc, seen map[*loc]bool) *loc {
	if l.kind == lkStmtTemp {
		return l
	}
	if !stmtLocal(l) || seen[l] {
		return nil
	}
	seen[l] = true
	var found *loc
	l.each(func(ts tset) bool {
		for _, t := range ts {
			if t.kind == tFrame {
				if st := stmtTempIn(t.loc, seen); st != nil {
					found = st
					return false
				}
			}
		}
		return true
	})
	return found
}

// stmtLocal reports whether l is a temporary the compiler makes for an
// operand of a statement, which a view of an owner temporary may be stored
// in while the statement runs.
func stmtLocal(l *loc) bool {
	switch l.kind {
	case lkTemp, lkRecv, lkLit, lkSliceLit, lkVarargs, lkBox, lkStmtTemp:
		return true
	}
	return false
}

// stmtTempKept returns the owner temporary that a view of v, stored in the
// frame storage h, would keep past its statement, or nil.
func (w *walker) stmtTempKept(v, h *loc) *loc {
	st := stmtTempOf(v)
	if st == nil || stmtLocal(h) || w.sameHeader(h, st) {
		return nil
	}
	return st
}

// checkStmtTemps reports the views of owner temporaries in val that the
// tracked local h would keep past their statements, and returns val
// without them.
func (w *walker) checkStmtTemps(h *loc, val tset, pos token.Pos, d dest) tset {
	var out tset
	for _, t := range val {
		if t.kind == tFrame {
			if st := w.stmtTempKept(t.loc, h); st != nil {
				w.report(&viewErr{sink: sinkStore, pos: pos, expr: d.src, l: st, fn: w.scopeName(w.sc), dst: d.text, where: storeStmt}, fmt.Sprintf("store|%p", st))
				continue
			}
		}
		out.add(t)
	}
	return out
}

// sameHeader reports whether the variable h is declared in the init
// statement of an if or switch statement that also makes the temporary st:
// st lives until that statement ends, and so does h.
func (w *walker) sameHeader(h, st *loc) bool {
	if h.kind != lkVar || st.node == nil {
		return false
	}
	if w.headers == nil && w.f.decl != nil {
		w.headers = [][2]token.Pos{}
		ast.Inspect(w.f.decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.IfStmt:
				if n.Init != nil {
					w.headers = append(w.headers, span(n.Init))
				}
			case *ast.SwitchStmt:
				if n.Init != nil {
					w.headers = append(w.headers, span(n.Init))
				}
			case *ast.TypeSwitchStmt:
				if n.Init != nil {
					w.headers = append(w.headers, span(n.Init))
				}
			}
			return true
		})
	}
	in := func(p token.Pos, s [2]token.Pos) bool { return s[0] <= p && p < s[1] }
	for _, s := range w.headers {
		if in(h.v.Pos(), s) && in(st.node.Pos(), s) {
			return true
		}
	}
	return false
}
