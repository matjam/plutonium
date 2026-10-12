package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// countError counts only shared and synced dynamic values. Sentinels,
// integer codes and borrowed pointers have no count (section 16.7).
func (s *fnState) countError(ptr *ssa.Value, I types2.Type, retain bool) {
	tab := s.load(ssa.TPtr, ptr)
	s.ifThen(s.newValue(ssa.OpNe, ssa.TBool, tab, s.constInt(ssa.TPtr, 0)), func() {
		desc := s.typeDescOf(tab, I)
		flags := s.loadStatic(ssa.TI8, s.offPtr(desc, 13))
		counted := s.newValue(ssa.OpAnd, ssa.TI8, flags, s.constInt(ssa.TI8, flagShared|flagSynced))
		s.ifThen(s.newValue(ssa.OpNe, ssa.TBool, counted, s.constInt(ssa.TI8, 0)), func() {
			data := s.load(ssa.TPtr, s.offPtr(ptr, 8))
			if retain {
				sync := s.newValue(ssa.OpAnd, ssa.TI8, flags, s.constInt(ssa.TI8, flagSynced))
				s.callRuntime("boxRetain", nil, []*ssa.Value{data, s.constBool(false),
					s.newValue(ssa.OpNe, ssa.TBool, sync, s.constInt(ssa.TI8, 0))})
				return
			}
			code := s.loadStatic(ssa.TPtr, s.offPtr(desc, descDrop))
			sig := adapterSignature(types2.NewSignatureType(nil, nil, nil, nil, nil, false))
			recv := scalarValue(data, types2.Typ[types2.UnsafePointer])
			// Like a deferred call, implicit cleanup finishes before a
			// Goexit is propagated. Checking here would recursively emit
			// this frame's cleanup while already generating its release.
			s.emitCall(callee{code: code, ctx: s.constInt(ssa.TPtr, 0), deferred: true}, sig, &recv, nil, token.NoPos)
		})
	})
}
