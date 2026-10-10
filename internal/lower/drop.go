package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Destroying values (language spec section 6.4).
//
// A value is destroyed when the variable holding it goes out of scope, when
// a place holding it is given a new value, and at the end of the statement
// that made it if nothing stored it (a temporary). What destroying runs is
// the type's drop glue: for an owner, its Drop method, then the drops of its
// fields in declaration order or of its elements in index order. Values that
// need nothing are destroyed by doing nothing.
//
// This file has the glue; scope.go decides where values are destroyed.

// needsDrop reports whether destroying a value of T runs drop glue: T is
// an owner with a Drop method, or with parts that need destroying (owners,
// or strings to release).
func (l *Lowerer) needsDrop(T types2.Type) bool {
	key := typeString(T)
	if v, ok := l.dropNeeds[key]; ok {
		return v
	}
	l.dropNeeds[key] = false // a recursive type needs a pointer, which needs nothing
	need := false
	if types2.IsOwner(T) {
		switch u := unwrap(T).Underlying().(type) {
		case *types2.Struct:
			need = dropMethod(T) != nil
			for i := 0; i < u.NumFields() && !need; i++ {
				need = l.needsDrop(u.Field(i).Type()) || l.counted(u.Field(i).Type())
			}
		case *types2.Array:
			need = dropMethod(T) != nil || (u.Len() > 0 && (l.needsDrop(u.Elem()) || l.counted(u.Elem())))
		case *types2.Wrapper:
			need = true // owned[T]: reported as unsupported where it is dropped
		default:
			need = dropMethod(T) != nil || types2.IsInterface(T) || types2.UnionVariants(T) != nil
		}
	}
	l.dropNeeds[key] = need
	return need
}

// dropMethod returns T's own Drop method, or nil. Drop is never promoted
// from an embedded field: the field is dropped as a field.
func dropMethod(T types2.Type) *types2.Func {
	if _, ok := types2.Unalias(T).(*types2.Named); !ok {
		return nil
	}
	sel := types2.NewMethodSet(types2.NewPointer(T)).Lookup(nil, "Drop")
	if sel == nil || len(sel.Index()) != 1 {
		return nil
	}
	fn, _ := sel.Obj().(*types2.Func)
	return fn
}

// dropGlue returns the function that destroys the T its argument points to.
func (l *Lowerer) dropGlue(T types2.Type) *ssa.Sym {
	name := typeString(T) + "·drop"
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	p := types2.NewVar(token.NoPos, nil, "p", types2.NewPointer(T))
	sig := types2.NewSignatureType(nil, nil, nil, types2.NewTuple(p), nil, false)
	l.queue = append(l.queue, &funcJob{sym: sym, sig: sig, build: func(s *fnState) {
		in := s.incomingArgs(sig)
		s.dropFields(in[0].v, T)
		s.returnValues(nil)
	}})
	return sym
}

// dropAt destroys the T at ptr.
func (s *fnState) dropAt(ptr *ssa.Value, T types2.Type) {
	if !s.l.needsDrop(T) {
		return
	}
	if why := s.l.cannotDrop(T, map[string]bool{}); why != "" {
		s.l.unsupportedf(s.pos, "cannot drop a %s here: dropping %s is not implemented yet (language spec section 6.4)", typeString(T), why)
		return
	}
	glue := s.l.dropGlue(T)
	p := types2.NewVar(token.NoPos, nil, "p", types2.NewPointer(T))
	sig := types2.NewSignatureType(nil, nil, nil, types2.NewTuple(p), nil, false)
	s.emitCall(callee{sym: glue}, sig, nil, []value{scalarValue(ptr, p.Type())}, token.NoPos)
}

// dropFields is the body of T's drop glue: Drop, then the parts.
func (s *fnState) dropFields(ptr *ssa.Value, T types2.Type) {
	if s.l.cannotDrop(T, map[string]bool{}) != "" {
		return // reported where the value is dropped (dropAt)
	}
	if m := dropMethod(T); m != nil {
		targs := s.recvTypeArgs(T)
		target, full := s.l.methodFunc(m, targs, nil, token.NoPos, "")
		var recv value
		if _, byPtr := full.Recv().Type().(*types2.Pointer); byPtr {
			recv = scalarValue(ptr, types2.NewPointer(T))
		} else {
			recv = s.loadFrom(ptr, T)
		}
		s.emitCall(callee{sym: target}, full, &recv, nil, token.NoPos)
	}
	switch u := unwrap(T).Underlying().(type) {
	case *types2.Struct:
		fields := make([]*types2.Var, u.NumFields())
		for i := range fields {
			fields[i] = u.Field(i)
		}
		offs := s.l.sizes.Offsetsof(fields)
		for i, f := range fields {
			s.destroyAt(s.offPtr(ptr, offs[i]), f.Type())
		}
	case *types2.Array:
		if !s.l.needsDrop(u.Elem()) {
			if s.l.counted(u.Elem()) {
				s.releaseAt(ptr, types2.NewArray(u.Elem(), u.Len())) // its strings
			}
			return
		}
		size := s.l.sizeof(u.Elem())
		s.countedLoop(s.constInt(ssa.TI64, u.Len()), nil, func(i *ssa.Value) {
			off := s.newValue(ssa.OpMul, ssa.TI64, i, s.constInt(ssa.TI64, size))
			s.dropAt(s.newValue(ssa.OpAddPtr, ssa.TPtr, ptr, off), u.Elem())
		})
	}
}

// cannotDrop returns what the compiler cannot drop yet in a value of type
// T, or "": owned, shared and synced references, owning interfaces and
// owner unions.
func (l *Lowerer) cannotDrop(T types2.Type, seen map[string]bool) string {
	key := typeString(T)
	if seen[key] || !l.needsDrop(T) {
		return ""
	}
	seen[key] = true
	if types2.UnionVariants(T) != nil {
		return "owner unions"
	}
	switch u := unwrap(T).Underlying().(type) {
	case *types2.Wrapper:
		return "owned, shared and synced references"
	case *types2.Interface:
		return "owning interfaces"
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if why := l.cannotDrop(u.Field(i).Type(), seen); why != "" {
				return why
			}
		}
	case *types2.Array:
		return l.cannotDrop(u.Elem(), seen)
	}
	return ""
}
