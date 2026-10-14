package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Destroying values (language spec section 18.6).
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
			need = true // owned[T] (box.go)
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
		s.beginCleanupGoexit()
		// A drop that has begun completes before the program exits
		// (rt.dropBegin, language spec section 15.4).
		s.callRuntime("dropBegin", nil)
		s.dropFields(in[0].v, T)
		s.callRuntime("dropEnd", nil)
		s.finishCleanupGoexit()
		s.returnValues(nil)
	}})
	return sym
}

// dropAt destroys the T at ptr.
func (s *fnState) dropAt(ptr *ssa.Value, T types2.Type) {
	if !s.l.needsDrop(T) {
		return
	}
	glue := s.l.dropGlue(T)
	p := types2.NewVar(token.NoPos, nil, "p", types2.NewPointer(T))
	sig := types2.NewSignatureType(nil, nil, nil, types2.NewTuple(p), nil, false)
	s.emitCall(callee{sym: glue}, sig, nil, []value{scalarValue(ptr, p.Type())}, token.NoPos)
	s.noteImplicitGoexit()
}

// dropFields is the body of T's drop glue: Drop, then the parts.
func (s *fnState) dropFields(ptr *ssa.Value, T types2.Type) {
	if m := dropMethod(T); m != nil {
		targs := s.recvTypeArgs(T)
		target, full := s.l.methodFunc(m, targs, nil, token.NoPos, "")
		var recv value
		if _, byPtr := full.Recv().Type().(*types2.Pointer); byPtr {
			recv = scalarValue(ptr, types2.NewPointer(T))
		} else {
			recv = s.loadFrom(ptr, T)
		}
		s.emitCall(callee{sym: target, deferred: true}, full, &recv, nil, token.NoPos)
		s.noteImplicitGoexit()
	}
	switch u := unwrap(T).Underlying().(type) {
	case *types2.Wrapper:
		if u.Kind() == types2.WrapOwned {
			s.dropOwned(ptr, u)
		}
	case *types2.Interface:
		s.dropIface(ptr, u)
	case *types2.TaggedUnion:
		// An owner union drops the payload of the variant it holds.
		ul := s.l.unionLayoutOf(T, s.pos)
		tag := s.unionTag(ul, aggValue(ptr, T))
		for i := 0; i < u.NumVariants(); i++ {
			v := u.Variant(i)
			P := s.subst(v.Payload())
			if !v.HasPayload() || !s.l.needsDrop(P) && !s.l.counted(P) {
				continue
			}
			s.ifThen(s.newValue(ssa.OpEq, ssa.TBool, tag, s.constInt(ul.tag, int64(i))), func() {
				s.destroyAt(s.offPtr(ptr, ul.payload), P)
			})
		}
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
