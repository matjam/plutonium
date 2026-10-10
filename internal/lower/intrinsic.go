package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// Intrinsics: standard library functions the compiler implements directly
// with one SSA operation. Each library function also has a Plutonium body,
// used when its address is taken.
type intrinsicFn func(s *fnState, args []*ssa.Value, results []types2.Type) []*ssa.Value

func unary(op ssa.Op) intrinsicFn {
	return func(s *fnState, args []*ssa.Value, results []types2.Type) []*ssa.Value {
		t := s.l.scalarType(results[0])
		return []*ssa.Value{s.newValue(op, t, args[0])}
	}
}

// bitScan returns an intrinsic for TrailingZeros / LeadingZeros, whose
// result is an int while the operation works at the argument's width.
func bitScan(op ssa.Op) intrinsicFn {
	return func(s *fnState, args []*ssa.Value, results []types2.Type) []*ssa.Value {
		v := s.newValue(op, args[0].Type, args[0])
		if v.Type.Size < 8 {
			v = s.newValue(ssa.OpZeroExt, ssa.TI64, v)
		}
		return []*ssa.Value{v}
	}
}

var intrinsics = map[string]intrinsicFn{
	"pluto/math.Sqrt":            unary(ssa.OpFSqrt),
	"pluto/math.Abs":             unary(ssa.OpFAbs),
	"pluto/math.Float64bits":     unary(ssa.OpBitcast),
	"pluto/math.Float64frombits": unary(ssa.OpBitcast),
	"pluto/math.Float32bits":     unary(ssa.OpBitcast),
	"pluto/math.Float32frombits": unary(ssa.OpBitcast),

	"pluto/bits.TrailingZeros64": bitScan(ssa.OpCtz),
	"pluto/bits.TrailingZeros32": bitScan(ssa.OpCtz),
	"pluto/bits.TrailingZeros":   bitScan(ssa.OpCtz),
	"pluto/bits.LeadingZeros64":  bitScan(ssa.OpClz),
	"pluto/bits.LeadingZeros32":  bitScan(ssa.OpClz),
	"pluto/bits.LeadingZeros":    bitScan(ssa.OpClz),
	"pluto/bits.ReverseBytes64":  unary(ssa.OpBswap),
	"pluto/bits.ReverseBytes32":  unary(ssa.OpBswap),
	"pluto/bits.ReverseBytes16":  unary(ssa.OpBswap),
	"pluto/bits.RotateLeft64": func(s *fnState, args []*ssa.Value, _ []types2.Type) []*ssa.Value {
		return []*ssa.Value{s.newValue(ssa.OpRotl, ssa.TI64, args[0], args[1])}
	},
	"pluto/bits.RotateLeft32": func(s *fnState, args []*ssa.Value, _ []types2.Type) []*ssa.Value {
		return []*ssa.Value{s.newValue(ssa.OpRotl, ssa.TI32, args[0], args[1])}
	},
	"pluto/bits.Mul64": func(s *fnState, args []*ssa.Value, _ []types2.Type) []*ssa.Value {
		hi := s.newValue(ssa.OpMulHiu, ssa.TI64, args[0], args[1])
		lo := s.newValue(ssa.OpMul, ssa.TI64, args[0], args[1])
		return []*ssa.Value{hi, lo}
	},
}

// intrinsic lowers a call to an intrinsic function, if fn is one.
func (s *fnState) intrinsic(fn *types2.Func, e *ast.CallExpr) ([]value, bool) {
	if fn.Pkg() == nil {
		return nil, false
	}
	impl := intrinsics[fn.Pkg().Path()+"."+fn.Name()]
	if impl == nil {
		return nil, false
	}
	sig := fn.Type().(*types2.Signature)
	args := make([]*ssa.Value, len(e.Args))
	for i, a := range e.Args {
		args[i] = s.exprAs(a, sig.Params().At(i).Type()).v
	}
	var resultTypes []types2.Type
	for i := 0; i < sig.Results().Len(); i++ {
		resultTypes = append(resultTypes, sig.Results().At(i).Type())
	}
	vals := impl(s, args, resultTypes)
	out := make([]value, len(vals))
	for i, v := range vals {
		out[i] = scalarValue(v, resultTypes[i])
	}
	return out, true
}
