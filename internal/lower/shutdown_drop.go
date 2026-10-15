package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/types2"
)

// destroyCountedAt is mandatory destruction, unlike releaseAt used by
// assignments and container refills. Complete subobjects in layout order so
// a later reference's Drop cannot revive an already-cleaned earlier field.
func (s *fnState) destroyCountedAt(ptr *ssa.Value, T types2.Type) {
	switch u := T.Underlying().(type) {
	case *types2.Struct:
		fields := make([]*types2.Var, u.NumFields())
		for i := range fields {
			fields[i] = u.Field(i)
		}
		offs := s.l.sizes.Offsetsof(fields)
		for i, f := range fields {
			s.destroyAt(s.offPtr(ptr, offs[i]), s.subst(f.Type()))
		}
	case *types2.Array:
		et := s.subst(u.Elem())
		s.countedLoop(s.constInt(ssa.TI64, u.Len()), nil, func(i *ssa.Value) {
			s.destroyAt(s.elemAddr(ptr, i, et), et)
		})
	case *types2.Wrapper:
		switch u.Kind() {
		case types2.WrapAligned, types2.WrapVolatile:
			s.destroyAt(ptr, s.subst(u.Elem()))
		case types2.WrapRing:
			et := s.subst(u.Elem())
			align := s.l.alignof(et)
			s.destroyAt(s.offPtr(ptr, (16+align-1)&^(align-1)), types2.NewArray(et, u.N()))
		default:
			s.releaseAt(ptr, T)
		}
	case *types2.TaggedUnion:
		ul := s.l.unionLayoutOf(T, s.pos)
		tag := s.unionTag(ul, aggValue(ptr, T))
		for i := 0; i < u.NumVariants(); i++ {
			v := u.Variant(i)
			P := s.subst(v.Payload())
			if v.HasPayload() && s.l.counted(P) {
				s.ifThen(s.newValue(ssa.OpEq, ssa.TBool, tag, s.constInt(ul.tag, int64(i))), func() {
					s.destroyAt(s.offPtr(ptr, ul.payload), P)
				})
			}
		}
	default:
		s.releaseAt(ptr, T)
	}
}
