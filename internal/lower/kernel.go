package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// Kernel mode: functions without //pu:fpu run where nothing saves the SSE
// and x87 registers, so they may not use floating point at all (abi.md
// section 10.2). Lowering reports every float operation such a function
// would need; copying float-typed values as bits stays allowed, and the
// backend does it with general-purpose registers.

// at makes pos the current position and returns a function that restores
// the previous one: defer s.at(e.Pos())().
func (s *fnState) at(pos token.Pos) func() {
	old := s.pos
	s.pos = pos
	return func() { s.pos = old }
}

// floatError reports a use of floating point in a function without FPU
// state, at the current position.
func (s *fnState) floatError(t *ssa.Type, what string) {
	s.floatErrorAt(s.pos, t, what)
}

// floatErrorAt reports the first float use in the function; the rest
// usually follow from it (a float variable used several times, or a
// float function called by mistake), so they are not listed.
func (s *fnState) floatErrorAt(pos token.Pos, t *ssa.Type, what string) {
	if s.floatReported {
		return
	}
	s.floatReported = true
	name := "float64"
	if t != nil && t.Size == 4 {
		name = "float32"
	}
	s.l.errorf(pos, "%s %s is not allowed in kernel mode (no FPU state is saved; mark the function //pu:fpu to use floats)", name, what)
}

// checkFloatOp reports op if it computes with floats.
func (s *fnState) checkFloatOp(op ssa.Op, t *ssa.Type, args []*ssa.Value) {
	switch op {
	case ssa.OpFAdd, ssa.OpFSub, ssa.OpFMul, ssa.OpFDiv, ssa.OpFNeg, ssa.OpFSqrt, ssa.OpFAbs:
		s.floatError(t, "arithmetic")
	case ssa.OpFEq, ssa.OpFNe, ssa.OpFLess, ssa.OpFLeq:
		s.floatError(args[0].Type, "comparison")
	case ssa.OpIntToFloat, ssa.OpUintToFloat, ssa.OpFloatConv:
		s.floatError(t, "conversion")
	case ssa.OpFloatToInt, ssa.OpFloatToUint:
		s.floatError(args[0].Type, "conversion")
	case ssa.OpSelect:
		if t.IsFloat() {
			s.floatError(t, "selection")
		}
	}
}

// checkFloatSignature reports float parameters and results of a function
// without FPU state: the ABI passes them in XMM registers.
func (s *fnState) checkFloatSignature() {
	for _, t := range s.f.Params {
		if t.IsFloat() {
			s.floatErrorAt(s.f.Pos, t, "parameter")
			break
		}
	}
	for _, t := range s.f.Results {
		if t.IsFloat() {
			s.floatErrorAt(s.f.Pos, t, "result")
			break
		}
	}
}

// checkFloatCall reports a call that passes or returns floats.
func (s *fnState) checkFloatCall(flat []*ssa.Value, results []*ssa.Type) {
	for _, a := range flat {
		if a.Type.IsFloat() {
			s.floatError(a.Type, "argument")
			break
		}
	}
	for _, t := range results {
		if t.IsFloat() {
			s.floatError(t, "result of a call")
			break
		}
	}
}
