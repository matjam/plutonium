// Translated by go2pu from internal/lower/or.go at the go-cutover tag; edited by hand since.

package lower

import (
	"selfhost/internal/ssa"
	"selfhost/internal/syntax/ast"
	"selfhost/internal/syntax/token"
	"selfhost/internal/types2"
	"selfhost/pu"
)

// Or statements (language spec section 11.27). An or statement is lowered
// in the four steps of the spec:
//
//  1. the calls of the left side's operands (stmtFirst, order.go), then the
//     call that or follows, completely;
//  2. a test of its last result, the error, against nil;
//  3. where the error is nil, the rest of the left side and the assignment
//     or declaration of the other results, as an assignment statement
//     whose right side is the call already made;
//  4. where it is not, err is declared with the error in an implicit block
//     of its own, the statement's temporaries and the other results are
//     dropped, and the clause runs. The clause leaves, by return, break,
//     continue, goto or panic, releasing the scopes it leaves (strscope.go)
//     as every such statement does.
//
// The error path is lowered first, while the statement's temporaries are
// all still unconsumed, so that releasing them there releases every one;
// the success path then consumes the values it assigns.

func (s *fnState) orStmt(st *ast.OrStmt) {
	stmtEntry := s.cleanups[len(s.cleanups)-1] // pushed by stmt for st
	res := s.callResults(st.Call)
	if s.b == nil || len(res) == 0 {
		// The call never returns: nothing after it runs.
		clear(s.evaluated)
		return
	}
	for i := range res {
		res[i] = s.materialize(res[i])
	}
	errV := res[len(res)-1]
	vals := res[:len(res)-1]

	ok := s.newBlock(ssa.BlockInvalid)
	fail := s.newBlock(ssa.BlockInvalid)
	tab := errV.v
	if errV.addr {
		tab = s.load(ssa.TPtr, errV.v)
	}
	s.endIf(s.newValue(ssa.OpEq, ssa.TBool, tab, s.constInt(ssa.TPtr, 0)), ok, fail)

	// The clause. The calls of the left side, evaluated ahead, are the
	// success path's: the clause's statements must not see them.
	s.seal(fail)
	s.startBlock(fail)
	saved := s.evaluated
	s.evaluated = nil
	s.orClause(st, stmtEntry, errV, vals)
	s.evaluated = saved

	// The values.
	s.seal(ok)
	s.startBlock(ok)
	if len(st.Lhs) == 0 {
		// Discarded, as the results of an expression statement are: an
		// owner is a temporary of the statement.
		for _, r := range vals {
			if r.T != nil && s.l.needsDrop(r.T) && !isTemp(r) {
				slot := s.temp(r.T, "drop")
				s.storeTo(slot, r, r.T)
				s.registerTemp(slot, r.T)
			}
		}
		return
	}
	if s.evaluated == nil {
		s.evaluated = map[ast.Expr]evaluated{}
	}
	s.evaluated[st.Call] = evaluated{vals: vals}
	s.assignStmt(&ast.AssignStmt{Lhs: st.Lhs, TokPos: st.TokPos, Tok: st.Tok, Rhs: []ast.Expr{st.Call}})
}

// orClause lowers the clause of st where the call failed, in the implicit
// block that declares err with the error errV. The statement's temporaries,
// in stmtEntry, and the other results vals are dropped first.
func (s *fnState) orClause(st *ast.OrStmt, stmtEntry *cleanupEntry, errV value, vals []value) {
	e := s.pushCleanup(st.Clause, true)
	if errVar, ok := s.info.Implicits[st].(*types2.Var); ok {
		T := s.subst(errVar.Type())
		addr := s.declareVar(errVar)
		s.assign(lvalue{ptr: addr, T: T}, errV, true) // hands the reference over
	}
	for _, r := range vals {
		if r.T != nil && s.l.needsDrop(r.T) && !isTemp(r) {
			slot := s.temp(r.T, "drop")
			s.storeTo(slot, r, r.T)
			s.destroyAt(slot, r.T)
		}
	}
	s.releaseEntry(stmtEntry)

	if r, ok := st.Clause.(*ast.ReturnStmt); ok {
		// The function is the one the statement is in, also from the
		// body of a range-over-func loop.
		res := types2.NewTuple(s.rootFn().signatureVars().results...)
		if form := types2.OrReturnForm(res, r, s.info.Types); form != types2.OrReturnPlain {
			s.orReturn(st, r, form)
			s.popCleanup(e)
			return
		}
	}
	s.stmt(st.Clause)
	s.popCleanup(e)
}

// orReturn lowers a short form of return in the clause of st: the last
// result is err, or E, and the other results are zero if unnamed and keep
// their values if named.
func (s *fnState) orReturn(st *ast.OrStmt, r *ast.ReturnStmt, form types2.OrReturn) {
	defer s.at(r.Pos())()
	root := s
	if s.rf != nil {
		root = s.rootFn()
	}
	n := len(root.results)
	T := root.abi.results[n-1].typ
	var v value
	if form == types2.OrReturnExpr {
		v = s.materialize(s.exprAs(r.Results[0], T))
	} else {
		errVar, _ := s.info.Implicits[st].(*types2.Var)
		v = s.loadLV(s.varLV(errVar, r))
	}
	named := root.namedResults()
	if s.rf == nil {
		// The last result is an error, counted and never an owner: assign
		// releases what a named one held.
		s.assign(lvalue{ptr: s.results[n-1], T: T}, v, !named)
		s.emitReturn(r.Pos())
		return
	}
	s.assign(lvalue{ptr: s.outerPtr(root.results[n-1], root), T: T}, v, !named)
	pos := r.Pos()
	s.exitTo(root, pu.Box(returnExit{}), pu.New(goClosure_orReturn_1{pos: pos}))
}

// goClosure_orReturn_1 is the function literal at or.go:142, converted to a struct
// that holds the variables it captures (go2pu closure conversion).
type goClosure_orReturn_1 struct {
	pos token.Pos
}

func (goSelf *goClosure_orReturn_1) Call(p *fnState) { p.emitReturn(goSelf.pos) }
