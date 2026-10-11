package types2

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	. "github.com/matjam/plutonium/internal/types2/errors"
)

// Or statements (language spec 11.27): a call, standing alone or on the
// right of = or :=, followed by a clause that runs when the call's last
// result, an error, is not nil.

// orStmt checks the or statement s. ctxt is the context of the statement
// itself: break and continue in the clause refer to the statements that
// enclose s, including when s is the init statement of an if, switch or
// for statement.
func (check *Checker) orStmt(ctxt stmtContext, s *ast.OrStmt) {
	// 1. The call, evaluated completely.
	values, ok := check.orCall(s)

	// The values, given to the left side (checked after the clause).
	if ok && len(s.Lhs) > 0 && len(s.Lhs) != len(values) {
		check.orCountError(s, len(values))
		ok = false
	}

	// 2. The clause, in an implicit block that declares err. The
	// variables of a := come into scope after the whole statement, so
	// the clause does not see them.
	check.orClause(ctxt, s)

	// 3. The left side.
	switch s.Tok {
	case token.DEFINE:
		var rhs []*operand
		if ok {
			rhs = values
		}
		check.orDecl(s, rhs)
	case token.ASSIGN:
		if !ok {
			check.useLHS(s.Lhs...)
			return
		}
		context := "assignment"
		if len(s.Lhs) > 1 {
			context = "multiple assignment"
		}
		for i, lhs := range s.Lhs {
			check.assignVar(lhs, nil, values[i], context)
		}
	}
}

// orCall checks the call of an or statement and returns the operands of
// its values: its results other than the last. ok is false if the call is
// invalid or not one that or accepts.
func (check *Checker) orCall(s *ast.OrStmt) (values []*operand, ok bool) {
	call := s.Call
	builtin := check.builtinCallee(call)
	var x operand
	kind := check.rawExpr(nil, &x, call, nil, false)
	switch {
	case kind == conversion:
		check.errorf(call, OrNotFunctionCall, "%s is a conversion, not a call of a function; or tests the error that a function returns", ExprString(call))
		return nil, false
	case builtin != nil:
		check.errorf(call, OrNotFunctionCall, "%s is a call of the built-in function %s, which returns no error for or to test", ExprString(call), builtin.name)
		return nil, false
	case !x.isValid():
		return nil, false
	case x.mode() == novalue:
		check.errorf(call, OrNoError, "%s returns no result; or tests the error that a call returns as its last result", ExprString(call))
		return nil, false
	}
	var results []Type
	if t, isTuple := x.typ().(*Tuple); isTuple {
		for _, v := range t.vars {
			results = append(results, v.typ)
		}
	} else {
		results = []Type{x.typ()}
	}
	last := results[len(results)-1]
	if !Identical(last, universeError) {
		check.errorf(call, OrNoError, "the last result of %s has type %s, not error; or tests a last result of type error", ExprString(call), last)
		return nil, false
	}
	values = make([]*operand, len(results)-1)
	for i, T := range results[:len(results)-1] {
		// a dummy expression (in place of the call) for error messages
		dummy := ast.NewIdent(nth(i+1, "function result"))
		dummy.NamePos = call.Pos()
		values[i] = &operand{mode_: value, expr: dummy, typ_: T}
	}
	return values, true
}

// builtinCallee returns the built-in function that call calls, or nil.
// It records nothing: the call is checked afterwards as usual.
func (check *Checker) builtinCallee(call *ast.CallExpr) *Builtin {
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		b, _ := check.lookup(f.Name).(*Builtin)
		return b
	case *ast.SelectorExpr:
		id, _ := f.X.(*ast.Ident)
		if id == nil {
			return nil
		}
		if pkg, _ := check.lookup(id.Name).(*PkgName); pkg != nil && pkg.imported != nil && pkg.imported.scope != nil {
			b, _ := pkg.imported.scope.Lookup(f.Sel.Name).(*Builtin)
			return b
		}
	}
	return nil
}

// orCountError reports a left side whose number of operands differs from
// the number of values of the call (E0017).
func (check *Checker) orCountError(s *ast.OrStmt, n int) {
	vars := "variables"
	if len(s.Lhs) == 1 {
		vars = "variable"
	}
	switch n {
	case 0:
		check.errorf(s.Lhs[0], WrongAssignCount, "assignment mismatch: %d %s but %s returns only an error; write it without a left side", len(s.Lhs), vars, ExprString(s.Call))
	case 1:
		check.errorf(s.Lhs[0], WrongAssignCount, "assignment mismatch: %d %s but %s returns 1 value besides the error", len(s.Lhs), vars, ExprString(s.Call))
	default:
		check.errorf(s.Lhs[0], WrongAssignCount, "assignment mismatch: %d %s but %s returns %d values besides the error", len(s.Lhs), vars, ExprString(s.Call), n)
	}
}

// orDecl declares or redeclares the variables on the left of an or
// statement with :=. rhs holds the values, or is nil after an error. The
// new variables come into scope at the end of the statement.
func (check *Checker) orDecl(s *ast.OrStmt, rhs []*operand) {
	check.shortVarDeclWith(inNode(s, s.TokPos), s.Lhs, s.End(), func(lhs []*Var) {
		for i, v := range lhs {
			if rhs == nil {
				if v.typ == nil {
					v.typ = Typ[Invalid]
				}
				continue
			}
			check.initVar(v, rhs[i], "assignment")
		}
	})
}

// orClause checks the clause of s in the implicit block that declares err,
// and that it leaves (E2463).
func (check *Checker) orClause(ctxt stmtContext, s *ast.OrStmt) {
	scope := NewScope(check.scope, s.Or, s.End(), "or")
	check.recordScope(s, scope)
	check.scope = scope
	defer check.closeScope()

	errVar := newVar(LocalVar, s.Or, check.pkg, "err", universeError)
	check.declare(scope, nil, errVar, s.Or)
	check.recordImplicit(s, errVar)
	check.usedVars[errVar] = true // err is never reported unused

	switch c := s.Clause.(type) {
	case *ast.ReturnStmt:
		check.orReturn(ctxt, s, c)
	default:
		check.stmt(ctxt, c)
	}

	if !check.orLeaves(s.Clause) {
		check.errorf(s.Clause, OrClauseCompletes, "the or clause can complete: it must leave with return, break, continue, goto or panic, so that the code after it does not run when %s fails", ExprString(s.Call))
	}
}

// orLeaves reports whether the clause c of an or statement leaves: a return,
// break, continue or goto statement, a call of panic, or a block whose
// statement list ends in a statement that leaves.
func (check *Checker) orLeaves(c ast.Stmt) bool {
	switch c := c.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return c.Tok != token.FALLTHROUGH
	case *ast.ExprStmt:
		return check.isTerminating(c, "")
	case *ast.BlockStmt:
		return check.leaves(c)
	}
	return false
}

// leaves reports whether s leaves (language spec 11.27): s is a terminating
// statement, a break or continue statement, a block whose statement list
// ends in a statement that leaves, or an if statement with an else branch
// in which both branches leave. A call of a function that never returns
// does not leave.
func (check *Checker) leaves(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.BranchStmt:
		if s.Tok == token.BREAK || s.Tok == token.CONTINUE {
			return true
		}
	case *ast.BlockStmt:
		for i := len(s.List) - 1; i >= 0; i-- {
			if _, empty := s.List[i].(*ast.EmptyStmt); !empty {
				return check.leaves(s.List[i])
			}
		}
		return false
	case *ast.IfStmt:
		if s.Else != nil && check.leaves(s.Body) && check.leaves(s.Else) {
			return true
		}
	}
	return check.isTerminating(s, "")
}

// orReturn checks a return statement that is the whole clause of s. It has
// two short forms (language spec 11.27): return, in a function whose last
// result is error, returns err as that result; and return E, in a function
// with more than one result whose last is error, where E is one
// single-valued expression, returns E as that result. In both, the other
// results are zero if unnamed and keep their values if named. Any other
// return is an ordinary return statement.
func (check *Checker) orReturn(ctxt stmtContext, s *ast.OrStmt, r *ast.ReturnStmt) {
	res := check.sig.results
	n := res.Len()
	lastErr := n > 0 && Identical(res.vars[n-1].typ, universeError)
	switch {
	case len(r.Results) == 0:
		if !lastErr {
			what := "has no results"
			if n > 0 {
				what = "has no result of type error last"
			}
			check.errorf(r, OrNoErrorResult, "the function %s to carry err; return with no operands would drop the error", what)
			return
		}
		check.orNamedResultsInScope(r, res.vars[:n-1])
	case len(r.Results) == 1 && n > 1 && lastErr:
		var x operand
		check.rawExpr(nil, &x, r.Results[0], nil, false)
		if t, isTuple := x.typ().(*Tuple); isTuple && x.isValid() {
			// return f() with several results: an ordinary return.
			rhs := make([]*operand, t.Len())
			for i, v := range t.vars {
				dummy := ast.NewIdent(nth(i+1, "function result"))
				dummy.NamePos = r.Results[0].Pos()
				rhs[i] = &operand{mode_: value, expr: dummy, typ_: v.typ}
			}
			if len(rhs) != n {
				check.returnError(r, res.vars, rhs)
				return
			}
			for i, v := range res.vars {
				check.initVar(v, rhs[i], "return statement")
			}
			return
		}
		check.exclude(&x, 1<<novalue|1<<builtin|1<<typexpr)
		if x.isValid() {
			check.assignment(&x, universeError, "return statement")
		}
		check.orNamedResultsInScope(r, res.vars[:n-1])
	default:
		check.stmt(ctxt, r)
	}
}

// orNamedResultsInScope reports, for a short form of return, a named result
// among vars (the results other than the last) that a declaration in the
// clause's scope hides: the
// short form returns its current value, which the program could not name
// there (language spec 12.14). The last result is set by its position, so
// err hiding a named result err is not an error.
func (check *Checker) orNamedResultsInScope(r *ast.ReturnStmt, vars []*Var) {
	for _, obj := range vars {
		if obj.name == "" || obj.name == "_" {
			continue
		}
		if alt := check.lookup(obj.name); alt != nil && alt != obj {
			err := check.newError(OutOfScopeResult)
			err.addf(r, "result parameter %s not in scope at return", obj.name)
			err.addf(alt, "inner declaration of %s", obj)
			err.report()
		}
	}
}
