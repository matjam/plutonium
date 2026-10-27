// Translated by go2pu from internal/lower/scope.go at the go-cutover tag; edited by hand since.

package lower

import (
	"compiler/compat"
	"compiler/internal/moves"
	"compiler/internal/scoped"
	"compiler/internal/ssa"
	"compiler/internal/syntax/ast"
	"compiler/internal/types2"
	"pluto/slices"
	"pluto/strconv"
	"pluto/strings"
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
// Drop accepts; the spec's drop flags are these cleared variables. Every
// slot is cleared when the function is entered, so a scope exit never
// destroys memory that was not given a value on the path to it.
//
// Where the move checker knows a variable holds nothing (moved away on
// every path, or never given a value), it is not destroyed at all: an exit
// skips the variables in moves.Info.Empty (or AtStmt, for a call that ends
// with rt.ExitTask), and an assignment whose target is in moves.Info.Fresh
// does not destroy the old value. Only a variable that may have been moved
// on some paths, or a place emptied by take, is destroyed holding the zero
// value.

// registerVar records the storage of owner variable v in the innermost
// scope that holds variables.
func (s *fnState) registerVar(addr *ssa.Value, T types2.Type, v *types2.Var) {
	if !s.l.needsDrop(T) {
		return
	}
	s.addVar(addr, T, v)
}

// emptyAt returns what the move checker knows of the variables where
// control leaves through n (a block, clause, statement with an implicit
// block, return or branch statement): which parts of them may hold values.
func (s *fnState) emptyAt(n ast.Node) moves.Known {
	if s.l.moves == nil || n == nil {
		return nil
	}
	return s.l.moves.Empty[n]
}

// leavingThrough runs release, which releases scopes, with what is known
// of the variables there.
func (s *fnState) leavingThrough(known moves.Known, release func()) {
	saved := s.leaving
	s.leaving = known
	release()
	s.leaving = saved
}

// destroyHeld destroys the T at ptr, of which only the parts h names may
// hold values.
func (s *fnState) destroyHeld(ptr *ssa.Value, T types2.Type, h moves.Held) {
	if !h.Box {
		s.destroyParts(ptr, T, h.Parts)
		return
	}
	// An owned box whose object holds values only in h.Parts: drop those,
	// then free the object, as its drop glue would (dropOwned).
	w, ok := unwrap(T).Underlying().(*types2.Wrapper)
	if !ok || w.Kind() != types2.WrapOwned {
		s.destroyAt(ptr, T)
		return
	}
	elem := s.subst(w.Elem())
	obj := s.load(ssa.TPtr, ptr)
	nonNil := s.newValue(ssa.OpNe, ssa.TBool, obj, s.constInt(ssa.TPtr, 0))
	s.ifThen(nonNil, func() {
		s.callRuntime("dropBegin", nil)
		s.destroyParts(obj, elem, h.Parts)
		s.callRuntime("boxFree", nil, []*ssa.Value{obj, s.constInt(ssa.TI64, 16),
			s.constInt(ssa.TI64, s.l.sizeof(elem)), s.constInt(ssa.TI64, s.l.alignof(elem))})
		s.callRuntime("dropEnd", nil)
	})
}

// destroyParts destroys the parts of the T at ptr that parts name, in the
// order destroying the whole would, and nothing else: the rest is known to
// be zero. A T with a Drop method of its own is destroyed whole, since its
// Drop runs on the whole value.
func (s *fnState) destroyParts(ptr *ssa.Value, T types2.Type, parts []moves.Path) {
	if len(parts) == 0 {
		return
	}
	for _, p := range parts {
		if len(p) == 0 {
			s.destroyAt(ptr, T)
			return
		}
	}
	if dropsWhole(T) {
		s.destroyAt(ptr, T)
		return
	}
	// The parts below each step, by step.
	below := func(step string) []moves.Path {
		var out []moves.Path
		for _, p := range parts {
			if p[0] == step {
				out = compat.Append(out, p[1:])
			}
		}
		return out
	}
	switch u := unwrap(T).Underlying().(type) {
	case *types2.Struct:
		fields := compat.Make[*types2.Var](u.NumFields())
		for i := range fields {
			fields[i] = u.Field(i)
		}
		offs := s.l.sizes.Offsetsof(fields)
		for i, f := range fields {
			if sub := below(compat.Concat(".", strconv.Itoa(i))); sub != nil {
				s.destroyParts(s.offPtr(ptr, offs[i]), s.subst(f.Type()), sub)
			}
		}
	case *types2.Array:
		var idx []int64
		for _, p := range parts {
			k, err := strconv.ParseInt(strings.Trim(p[0], "[]"), 10, 64)
			if err != nil {
				s.destroyAt(ptr, T) // an element whose index is not known
				return
			}
			if !slices.Contains(idx, k) {
				idx = compat.AppendPlain(idx, k)
			}
		}
		slices.Sort(idx)
		elem := s.subst(u.Elem())
		for _, k := range idx {
			s.destroyParts(s.offPtr(ptr, k*s.l.sizeof(elem)), elem, below(compat.Concat("[", strconv.FormatInt(k, 10), "]")))
		}
	default:
		s.destroyAt(ptr, T)
	}
}

// fresh reports whether the old value of the place an assignment stores
// to is known to be nothing.
func (s *fnState) fresh(lhs ast.Expr) bool {
	return s.l.moves != nil && s.l.moves.Fresh[lhs]
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
	e.slots = compat.Append(e.slots, c)
}

// destroyAt destroys the value at ptr, of type T, that a slot holds: an
// owner's drop glue, or the release of the strings in it.
func (s *fnState) destroyAt(ptr *ssa.Value, T types2.Type) {
	switch {
	case s.l.needsDrop(T):
		s.dropAt(ptr, T)
	case s.l.counted(T):
		s.destroyCountedAt(ptr, T)
	}
	s.completeStaticCleanup(ptr, T)
}

// holdProducedOwner keeps an evaluated owner alive until its actual transfer.
// Calls evaluated ahead of a statement may be followed by a call that exits.
func (s *fnState) holdProducedOwner(v value) value {
	if !s.l.gx.active || !s.l.needsDrop(v.T) || v.tmp != nil && !v.tmp.consumed {
		return v
	}
	slot := s.temp(v.T, "produced.owner")
	s.storeTo(slot, v, v.T)
	return s.ownTemp(slot, v.T)
}

func (s *fnState) consumeProducedOwner(v value) {
	if v.tmp != nil && s.l.needsDrop(v.T) {
		v.tmp.consumed = true
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
	if slot := s.takeEvaluatedSlot(e); slot != nil {
		return slot // evaluated ahead of its expression (order.go)
	}
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
func (s *fnState) movedOut(lv lvalue, obj *types2.Var) value {
	s.checkScopeMove(lv.ptr, lv.T)
	v := s.materialize(s.loadLV(lv))
	s.zero(lv.ptr, s.l.sizeof(unwrap(lv.T)))
	if obj != nil && s.ownedVars[obj] {
		return s.heldAfterMove(v)
	}
	return v
}

// heldAfterMove makes the result of moving a value out of its place the
// holder of the references the place held. It matters when generic code
// that moves its values of an owner type parameter is instantiated with a
// counted type (a string): the place is cleared without releasing, so the
// value must keep the references and release them unless a store takes them
// over. Owners hold none of their own, and neither does a borrowed
// parameter, whose value a store counts for itself.
func (s *fnState) heldAfterMove(v value) value {
	if !s.l.counted(v.T) {
		return v
	}
	if v.addr {
		return s.ownTemp(v.v, v.T)
	}
	return s.wordTemp(v.v, v.T, "moved")
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
		return compat.Clone([]int64{0})
	}
	if depth > 4 || !types2.IsOwner(T) {
		return nil
	}
	var out []int64
	switch u := T.Underlying().(type) {
	case *types2.Struct:
		fields := compat.Make[*types2.Var](u.NumFields())
		for i := range fields {
			fields[i] = u.Field(i)
		}
		offs := l.sizes.Offsetsof(fields)
		for i, f := range fields {
			for _, o := range l.scopeOffsets(unwrap(f.Type()), depth+1) {
				out = compat.AppendPlain(out, offs[i]+o)
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
				out = compat.AppendPlain(out, i*size+o)
			}
		}
	}
	return out
}
