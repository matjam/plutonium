package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Owning interfaces (language spec section 4.4): type R owner interface
// {...} owns the value converted into it, and dropping it drops that value,
// whatever its dynamic type. The itab of an owning interface has one more
// slot after the methods, the drop adapter of the dynamic type, which takes
// the data word as the method adapters do (abi.md section 5); the slot is
// zero when values of the type need no drop. An owner moved in is dropped by
// its drop glue, a shared, synced or weak reference is released; a plain
// value or a view needs nothing, and a frame copy holding strings keeps its
// own references, released with the frame (dataWord).
//
// An owning interface with no methods has no itab (its tab word is the type
// descriptor, as for any empty interface), so the drop adapter is a word of
// the type descriptor of the types held by one (descDrop), set for every
// type converted into an owning interface, with or without methods, since
// an owning interface can be converted to a smaller one.

// needsIfaceDrop reports whether a T held by an owning interface must be
// dropped when the interface is.
func (l *Lowerer) needsIfaceDrop(T types2.Type) bool {
	return l.needsDrop(T) || l.isDirect(T) && l.counted(T)
}

// ifaceDropAdapter returns the drop slot's adapter for concrete type T, or
// nil if a T needs no drop.
func (l *Lowerer) ifaceDropAdapter(T types2.Type) *ssa.Sym {
	if !l.needsIfaceDrop(T) {
		return nil
	}
	name := typeString(T) + "·drop·i"
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	sig := adapterSignature(types2.NewSignatureType(nil, nil, nil, nil, nil, false))
	l.queue = append(l.queue, &funcJob{sym: sym, sig: sig, build: func(s *fnState) {
		in := s.incomingArgs(sig)
		data := in[0].v
		if s.l.isDirect(T) {
			tmp := s.temp(T, "dropped")
			s.storeTo(tmp, s.fromDataWord(data, T), T)
			s.destroyAt(tmp, T)
		} else {
			s.destroyAt(data, T)
		}
		s.returnValues(nil)
	}})
	return sym
}

// noteDrop gives T's type descriptor its drop adapter, if a T needs a drop.
func (tt *typeTable) noteDrop(T types2.Type) {
	tt.desc(T)
	if d := tt.descs[typeString(T)]; d.drop == nil {
		d.drop = tt.l.ifaceDropAdapter(T)
	}
}

// dropIface drops the owning interface value at ptr: it calls the drop slot
// of its itab, if it holds a value whose type needs one.
func (s *fnState) dropIface(ptr *ssa.Value, iface *types2.Interface) {
	tab := s.load(ssa.TPtr, ptr)
	slot := int64(itabHeader + 8*iface.NumMethods())
	if iface.Empty() {
		slot = descDrop // tab is the type descriptor
	}
	s.ifThen(s.newValue(ssa.OpNe, ssa.TBool, tab, s.constInt(ssa.TPtr, 0)), func() {
		code := s.load(ssa.TPtr, s.offPtr(tab, slot))
		s.ifThen(s.newValue(ssa.OpNe, ssa.TBool, code, s.constInt(ssa.TPtr, 0)), func() {
			sig := adapterSignature(types2.NewSignatureType(nil, nil, nil, nil, nil, false))
			recv := scalarValue(s.load(ssa.TPtr, s.offPtr(ptr, 8)), types2.Typ[types2.UnsafePointer])
			s.emitCall(callee{code: code, ctx: s.constInt(ssa.TPtr, 0)}, sig, &recv, nil, token.NoPos)
		})
	})
}
