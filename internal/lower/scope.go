package lower

import (
	"github.com/matjam/plutonium/internal/scoped"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// Where owners are destroyed (language spec section 18.6).
//
// Owners share the cleanup stack of counted strings (strscope.go): an owner
// variable is a slot of the innermost entry that holds variables, a
// parameter is one of the function's owned parameters, and a temporary is
// a slot of the innermost entry. Leaving an entry, by falling off its end,
// by return, break, continue or goto, destroys its slots in reverse order
// and clears them; for an owner, destroying runs its drop glue (drop.go).
//
// A move clears the variable it moved out of (moves.Info.Moves), so that
// destroying it at the end of its scope destroys the zero value, which
// does nothing; the spec's drop flags are these cleared variables. Every
// slot is cleared when the function is entered, so a scope exit never
// destroys memory that was not given a value on the path to it.

// registerVar records an owner variable's storage in the innermost scope
// that holds variables.
func (s *fnState) registerVar(addr *ssa.Value, T types2.Type) {
	if !s.l.needsDrop(T) {
		return
	}
	s.addVar(addr, T)
}

// registerTemp records a temporary owner in the innermost entry. Outside
// any entry (a package variable's initialiser) it is never destroyed.
func (s *fnState) registerTemp(addr *ssa.Value, T types2.Type) {
	if !s.l.needsDrop(T) || len(s.cleanups) == 0 {
		return
	}
	c := countedSlot{base: addr, T: T, pos: s.pos}
	s.zeroAtEntry(c)
	e := s.cleanups[len(s.cleanups)-1]
	e.slots = append(e.slots, c)
}

// destroyAt destroys the value at ptr, of type T, that a slot holds: an
// owner's drop glue, or the release of the strings in it.
func (s *fnState) destroyAt(ptr *ssa.Value, T types2.Type) {
	switch {
	case s.l.needsDrop(T):
		s.dropAt(ptr, T)
	case s.l.counted(T):
		s.releaseAt(ptr, T)
	}
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
	s.checkScopeMove(lv.ptr, lv.T)
	v := s.materialize(s.loadLV(lv))
	s.zero(lv.ptr, s.l.sizeof(unwrap(lv.T)))
	return v
}

// checkScopeMove checks, before a value of type T at ptr moves, that no
// sync.Scope in it has tasks running (rt.joinMoved): they would report to
// the address it leaves.
func (s *fnState) checkScopeMove(ptr *ssa.Value, T types2.Type) {
	for _, off := range s.l.scopeOffsets(unwrap(T), 0) {
		s.callRuntime("joinMoved", nil, []*ssa.Value{s.offPtr(ptr, off)})
	}
}

// scopeOffsets returns the offsets of the sync.Scopes a value of type T
// holds by value: T itself, its fields, and the elements of short arrays.
func (l *Lowerer) scopeOffsets(T types2.Type, depth int) []int64 {
	if scoped.IsScopeType(T) {
		return []int64{0}
	}
	if depth > 4 || !types2.IsOwner(T) {
		return nil
	}
	var out []int64
	switch u := T.Underlying().(type) {
	case *types2.Struct:
		fields := make([]*types2.Var, u.NumFields())
		for i := range fields {
			fields[i] = u.Field(i)
		}
		offs := l.sizes.Offsetsof(fields)
		for i, f := range fields {
			for _, o := range l.scopeOffsets(unwrap(f.Type()), depth+1) {
				out = append(out, offs[i]+o)
			}
		}
	case *types2.Array:
		inner := l.scopeOffsets(unwrap(u.Elem()), depth+1)
		if len(inner) == 0 || u.Len() > 16 {
			return nil
		}
		size := l.sizeof(u.Elem())
		for i := int64(0); i < u.Len(); i++ {
			for _, o := range inner {
				out = append(out, i*size+o)
			}
		}
	}
	return out
}
