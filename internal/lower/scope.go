package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Where values are destroyed (language spec section 6.4).
//
// Lowering keeps a stack of scopes: one for each statement list (a block,
// a case clause), one for each if, for, switch and select statement (the
// variables of its header), and one for each statement (the temporaries it
// makes). An owner variable is registered in the innermost scope that
// holds variables when its declaration is lowered, a parameter in the
// function's outermost scope, and a temporary in the innermost scope.
// Leaving a scope, by falling off its end, by return, break, continue or
// goto, destroys what it holds in reverse order and clears it.
//
// A move clears the variable it moved out of (moves.Info.Moves), so that
// destroying it at the end of its scope destroys the zero value, which
// does nothing; the spec's drop flags are these cleared variables. Every
// registered variable is cleared when it is declared, so a scope exit
// never destroys memory that was not given a value on the path to it.
//
// What destroying a value of a type runs is its drop glue (drop.go); the
// destruction of string references (counted strings) is another kind of
// glue that can share these scopes.

// A dropScope is a scope's owners, in the order they were registered.
type dropScope struct {
	vars     []dropVar
	holdsVar bool         // a block or statement header, rather than one statement
	stmt     bool         // one statement's scope, rather than a list's
	span     [2]token.Pos // the source the scope covers, for goto
}

type dropVar struct {
	addr *ssa.Value
	T    types2.Type
	pos  token.Pos // where it is declared (a variable) or made
}

// pushScope starts a scope covering n.
func (s *fnState) pushScope(holdsVar bool, from, to token.Pos) {
	s.scopes = append(s.scopes, &dropScope{holdsVar: holdsVar, span: [2]token.Pos{from, to}})
}

// popScope ends the innermost scope, destroying what it holds if the end
// is reachable.
func (s *fnState) popScope() {
	sc := s.scopes[len(s.scopes)-1]
	s.scopes = s.scopes[:len(s.scopes)-1]
	if s.b != nil {
		s.dropScopeVars(sc)
	}
}

// dropScopes destroys what the scopes from the innermost out to index depth
// hold, without ending them: a jump out of them.
func (s *fnState) dropScopes(depth int) {
	for i := len(s.scopes) - 1; i >= depth && s.b != nil; i-- {
		s.dropScopeVars(s.scopes[i])
	}
}

func (s *fnState) dropScopeVars(sc *dropScope) {
	for i := len(sc.vars) - 1; i >= 0 && s.b != nil; i-- {
		s.dropVar(sc.vars[i])
	}
}

// dropVar destroys and clears a registered value. Errors about it name
// where it was declared or made.
func (s *fnState) dropVar(v dropVar) {
	defer s.at(v.pos)()
	s.dropAt(v.addr, v.T)
	s.zero(v.addr, s.l.sizeof(v.T))
}

// registerVar records an owner variable's storage in the innermost scope
// that holds variables.
func (s *fnState) registerVar(addr *ssa.Value, T types2.Type) {
	if !s.l.needsDrop(T) {
		return
	}
	for i := len(s.scopes) - 1; i >= 0; i-- {
		if s.scopes[i].holdsVar {
			s.scopes[i].vars = append(s.scopes[i].vars, dropVar{addr, T, s.pos})
			return
		}
	}
}

// gotoDrops destroys, for a goto to the label at p, what the jump leaves:
// the scopes that do not contain the label, and in the innermost one that
// does, the variables declared after the label, whose declarations run
// again.
func (s *fnState) gotoDrops(p token.Pos) {
	depth := s.labelDepth(p)
	s.dropScopes(depth)
	if depth == 0 {
		return
	}
	sc := s.scopes[depth-1]
	for i := len(sc.vars) - 1; i >= 0 && s.b != nil; i-- {
		if v := sc.vars[i]; v.pos >= p {
			s.dropVar(v)
		}
	}
}

// registerTemp records a temporary owner in the innermost scope. Outside
// any scope (a package variable's initialiser) it is never destroyed.
func (s *fnState) registerTemp(addr *ssa.Value, T types2.Type) {
	if !s.l.needsDrop(T) || len(s.scopes) == 0 {
		return
	}
	sc := s.scopes[len(s.scopes)-1]
	sc.vars = append(sc.vars, dropVar{addr, T, s.pos})
}

// isTemp reports whether e makes an owner value that nothing takes.
func (s *fnState) isTemp(e ast.Expr) bool {
	return s.l.moves != nil && s.l.moves.Temps[e] && e != s.inTemp
}

// isMove reports whether the identifier moves its variable's value out.
func (s *fnState) isMove(e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && s.l.moves != nil && s.l.moves.Moves[id]
}

// tempSlot evaluates the temporary e into a slot of the statement and
// returns the slot's address.
func (s *fnState) tempSlot(e ast.Expr, T types2.Type) *ssa.Value {
	saved := s.inTemp
	s.inTemp = e
	v := s.expr(e)
	s.inTemp = saved
	slot := s.temp(T, "drop")
	s.storeTo(slot, v, T)
	s.registerTemp(slot, T)
	return slot
}

// movedOut reads a variable whose value moves out at id, and clears it.
func (s *fnState) movedOut(lv lvalue) value {
	v := s.materialize(s.loadLV(lv))
	s.zero(lv.ptr, s.l.sizeof(unwrap(lv.T)))
	return v
}

// labelDepth returns how many of the current scopes a goto to the labeled
// statement at p stays in: those containing it, but not the labeled
// statement's own, which the jump enters afresh.
func (s *fnState) labelDepth(p token.Pos) int {
	d := 0
	for i, sc := range s.scopes {
		own := sc.stmt && sc.span[0] == p // the labeled statement itself
		if sc.span[0] <= p && p < sc.span[1] && !own {
			d = i + 1
		} else {
			break
		}
	}
	return d
}
