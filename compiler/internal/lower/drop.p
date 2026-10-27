// Translated by go2pu from internal/lower/drop.go at the go-cutover tag; edited by hand since.

package lower

import (
	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
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
	key := l.typeStr(T)
	if v, ok := l.dropNeeds[key]; ok {
		return v
	}
	l.dropNeeds[key] = false // a recursive type needs a pointer, which needs nothing
	need := false
	if types2.IsOwner(T) {
		if A := types2.OwnerBase(T); A != nil {
			// type B owner A: B's Drop, then the drop of A.
			need = dropMethod(T) != nil || l.needsDrop(A)
			l.dropNeeds[key] = need
			return need
		}
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

// dropsWhole reports whether destroying a T runs a Drop method on the whole
// value: its own, or that of the owner it is defined from (type B owner A).
func dropsWhole(T types2.Type) bool {
	if dropMethod(T) != nil {
		return true
	}
	A := types2.OwnerBase(T)
	return A != nil && dropsWhole(A)
}

// dropGlue returns the function that destroys the T its argument points to.
func (l *Lowerer) dropGlue(T types2.Type) *ssa.Sym {
	name := compat.Concat(typeString(T), "·drop")
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	p := types2.NewVar(token.NoPos, nil, "p", types2.NewPointer(T))
	sig := types2.NewSignatureType(nil, nil, nil, types2.NewTuple(compat.Clone([]*types2.Var{p})...), nil, false)
	l.queue = compat.Append(l.queue, pu.New(funcJob{sym: sym, sig: sig, build: pu.New(goClosure_dropGlue_1{sig: sig, T: T})}))
	return sym
}

// dropAt destroys the T at ptr.
func (s *fnState) dropAt(ptr *ssa.Value, T types2.Type) {
	if !s.l.needsDrop(T) {
		return
	}
	glue := s.l.dropGlue(T)
	p := types2.NewVar(token.NoPos, nil, "p", types2.NewPointer(T))
	sig := types2.NewSignatureType(nil, nil, nil, types2.NewTuple(compat.Clone([]*types2.Var{p})...), nil, false)
	s.emitCall(callee{sym: glue}, sig, nil, []value{scalarValue(ptr, p.Type())}, token.NoPos)
	s.noteImplicitGoexit()
}

// dropFields is the body of T's drop glue: Drop, then the parts.
func (s *fnState) dropFields(ptr *ssa.Value, T types2.Type) {
	if m := dropMethod(T); m != nil {
		// A user callback may replace still-pending fields. Its ordinary
		// old-value destruction is not progress in mandatory traversal.
		var finalizing *ssa.Value
		if s.l.rtFuncOrNil("staticCleanupPause") != nil {
			finalizing = s.callRuntime("staticCleanupPause", compat.Clone([]*ssa.Type{ssa.TI64}))[0]
		}
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
		if finalizing != nil {
			s.callRuntime("staticCleanupResume", nil, []*ssa.Value{finalizing})
		}
	}
	if A := types2.OwnerBase(T); A != nil {
		// type B owner A holds an A: after B's own Drop it is dropped as one
		// (language spec section 7.18, Owner types).
		s.dropFields(ptr, A)
		return
	}
	switch u := unwrap(T).Underlying().(type) {
	case *types2.Wrapper:
		switch u.Kind() {
		case types2.WrapOwned:
			s.dropOwned(ptr, u)
		case types2.WrapRing:
			// Dropping a ring of owners drops the elements it holds,
			// oldest first (language spec section 7.17).
			et := u.Elem()
			if !s.l.needsDrop(et) {
				return
			}
			tail := s.load(ssa.TI64, s.offPtr(ptr, 8))
			n := s.newValue(ssa.OpSub, ssa.TI64, s.load(ssa.TI64, ptr), tail)
			s.countedLoop(n, nil, func(i *ssa.Value) {
				s.destroyAt(s.ringSlot(ptr, u, s.newValue(ssa.OpAdd, ssa.TI64, tail, i)), et)
			})
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
		fields := compat.Make[*types2.Var](u.NumFields())
		for i := range fields {
			fields[i] = u.Field(i)
		}
		offs := s.l.sizes.Offsetsof(fields)
		for i, f := range fields {
			s.destroyAt(s.offPtr(ptr, offs[i]), f.Type())
		}
	case *types2.Array:
		if !s.l.needsDrop(u.Elem()) && !s.l.counted(u.Elem()) {
			return
		}
		size := s.l.sizeof(u.Elem())
		s.countedLoop(s.constInt(ssa.TI64, u.Len()), nil, func(i *ssa.Value) {
			off := s.newValue(ssa.OpMul, ssa.TI64, i, s.constInt(ssa.TI64, size))
			s.destroyAt(s.newValue(ssa.OpAddPtr, ssa.TPtr, ptr, off), u.Elem())
		})
	}
}

// goClosure_dropGlue_1 is the function literal at drop.go:89, converted to a struct
// that holds the variables it captures (go2pu closure conversion).
type goClosure_dropGlue_1 struct {
	sig *types2.Signature
	T   types2.Type
}

func (goSelf *goClosure_dropGlue_1) Call(s *fnState) {
	in := s.incomingArgs(goSelf.sig)
	s.beginCleanupGoexit()
	// A drop that has begun completes before the program exits
	// (rt.dropBegin, language spec section 15.4).
	s.callRuntime("dropBegin", nil)
	s.dropFields(in[0].v, goSelf.T)
	s.callRuntime("dropEnd", nil)
	s.finishCleanupGoexit()
	s.returnValues(nil)
}
