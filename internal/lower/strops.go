package lower

import (
	"go/constant"

	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// String operations that build new strings: concatenation and the
// allocating conversions. Each calls the runtime (pluto/rt/str.p), which
// returns the new string's three words holding one reference; lowering
// makes it a statement temporary.

// concat lowers a chain of string additions, a + b + c, as one runtime call
// that copies every operand into one new buffer, as Go does.
func (s *fnState) concat(e *ast.BinaryExpr, T types2.Type) value {
	var operands []ast.Expr
	var collect func(x ast.Expr)
	collect = func(x ast.Expr) {
		x = ast.Unparen(x)
		if b, ok := x.(*ast.BinaryExpr); ok && b.Op == token.ADD && s.info.Types[b].Value == nil && isString(unwrap(s.typeOf(b))) {
			collect(b.X)
			collect(b.Y)
			return
		}
		operands = append(operands, x)
	}
	collect(e)
	return s.concatValues(s.evalOperands(operands, T), T)
}

// evalOperands evaluates string operands left to right, holding each one
// that a later operand's calls could change.
func (s *fnState) evalOperands(operands []ast.Expr, T types2.Type) []value {
	vals := make([]value, len(operands))
	for i, x := range operands {
		vals[i] = s.holdAcross(s.exprAs(x, unwrap(T)), operands[i+1:]...)
	}
	return vals
}

// concatValues concatenates string values already evaluated.
func (s *fnState) concatValues(vals []value, T types2.Type) value {
	strT := types2.Typ[types2.String]
	arrT := types2.NewArray(strT, int64(len(vals)))
	arr := s.temp(arrT, "concat")
	for i, v := range vals {
		// The operands are borrowed for the call: copies of their words.
		s.move(s.offPtr(arr, int64(i)*strSize), v.v, strSize)
	}
	n := s.constInt(ssa.TI64, int64(len(vals)))
	res := s.callRuntime("concatStrings", []*ssa.Type{ssa.TPtr, ssa.TI64, ssa.TI64}, []*ssa.Value{arr, n, n})
	return s.newString(res, T)
}

// newString makes a statement temporary of a string the runtime built and
// returned as its words, holding one reference.
func (s *fnState) newString(words []*ssa.Value, T types2.Type) value {
	v := s.makeString(words[0], words[1], words[2], T)
	return s.ownTemp(v.v, T)
}

// concatAssign lowers s += t on a string place.
func (s *fnState) concatAssign(lv lvalue, rhs ast.Expr, pos token.Pos) {
	T := unwrap(lv.T)
	old := s.holdAcross(s.loadLV(lv), rhs)
	y := s.exprAs(rhs, T)
	s.storeCounted(lv, s.concatValues([]value{old, y}, T), false)
}

// stringConversion lowers a conversion to a string that copies, and so
// allocates: string(runes), and string(r) for a rune that is not a constant.
// It reports false for any other conversion. (The checker rejects the
// conversions of a string to a byte or rune slice, and of a byte slice or a
// non-rune integer to a string; language spec section 11.24.)
func (s *fnState) stringConversion(e *ast.CallExpr, to, from types2.Type) (value, bool) {
	if !isString(to) || isString(from) {
		return value{}, false
	}
	fu := unwrap(from).Underlying()
	if sl, ok := fu.(*types2.Slice); ok {
		if b, ok := unwrap(sl.Elem()).Underlying().(*types2.Basic); ok && b.Kind() == types2.Rune {
			p, n, _ := s.sliceParts(s.expr(e.Args[0]))
			res := s.callRuntime("stringFromRunes", []*ssa.Type{ssa.TPtr, ssa.TI64, ssa.TI64}, []*ssa.Value{p, n})
			return s.newString(res, to), true
		}
	}
	if b, ok := fu.(*types2.Basic); ok && b.Kind() == types2.Int32 {
		r := s.convertScalar(s.scalar(e.Args[0]), from, types2.Typ[types2.Rune])
		res := s.callRuntime("stringFromRune", []*ssa.Type{ssa.TPtr, ssa.TI64, ssa.TI64}, []*ssa.Value{r})
		return s.newString(res, to), true
	}
	return value{}, false
}

// runeBoundaryCheck makes s[lo:hi] panic unless both bounds are at the
// start of a rune or at the end (language spec section 18.8). A string
// whose flags say it is ASCII needs no check, so the test of the bytes is a
// call made only for others. A bound that is the constant 0, or left out,
// needs none either.
func (s *fnState) runeBoundaryCheck(e *ast.SliceExpr, base, length, lo, hi, ctl *ssa.Value) {
	if tv := s.info.Types[e.X]; tv.Value != nil && strFlags(constant.StringVal(tv.Value)) == 0 {
		return // an ASCII constant
	}
	if e.Low == nil || isZeroConst(s.info, e.Low) {
		lo = length
	}
	if e.High == nil {
		hi = length
	}
	if lo == length && hi == length {
		return
	}
	nonASCII := s.newValue(ssa.OpNe, ssa.TBool, s.newValue(ssa.OpAnd, ssa.TI64, ctl, s.constInt(ssa.TI64, strNonASCII)), s.constInt(ssa.TI64, 0))
	s.ifThen(nonASCII, func() {
		s.callRuntime("checkRuneBounds", nil, []*ssa.Value{base, length, lo, hi})
	})
}

func isZeroConst(info *types2.Info, e ast.Expr) bool {
	c, ok := constIndex(info, e)
	return ok && c == 0
}

// stringsSlice expands a call of strings.Slice or strings.SliceFrom as the
// slice of a string it stands for: bounds checked against the length, rune
// boundaries checked unless the string is known to be ASCII, and a result
// that shares the buffer and its count (language spec sections 7.3, 11.12
// and 18.8). The type checker rejects the slice expression itself (E2019),
// so this is the only way a program cuts a string.
func (s *fnState) stringsSlice(fn *types2.Func, e *ast.CallExpr) ([]value, bool) {
	if fn.Pkg() == nil || fn.Pkg().Path() != "pluto/strings" || fn.Type().(*types2.Signature).Recv() != nil {
		return nil, false
	}
	var se *ast.SliceExpr
	switch {
	case fn.Name() == "Slice" && len(e.Args) == 3:
		se = &ast.SliceExpr{X: e.Args[0], Low: e.Args[1], High: e.Args[2]}
	case fn.Name() == "SliceFrom" && len(e.Args) == 2:
		se = &ast.SliceExpr{X: e.Args[0], Low: e.Args[1]}
	default:
		return nil, false
	}
	return []value{s.sliceExpr(se, s.typeOf(e))}, true
}

// countIntrinsics are the generic functions of pluto/rt that give library
// containers the compiler's string drop glue (pluto/rt/str.p).
var countIntrinsics = map[string]bool{
	"ReleaseValues": true, "RetainValues": true, "MoveValues": true, "HoldsStrings": true,
}

// countIntrinsic expands a call of one of the countIntrinsics.
func (s *fnState) countIntrinsic(fn *types2.Func, e *ast.CallExpr, id *ast.Ident) ([]value, bool) {
	if fn.Pkg() == nil || fn.Pkg().Path() != "pluto/rt" {
		return nil, false
	}
	switch fn.Name() {
	case "SliceBytewise":
		// s[lo:hi] with its bounds checked but not that they are rune
		// boundaries.
		se := &ast.SliceExpr{X: e.Args[0], Low: e.Args[1], High: e.Args[2]}
		was := s.bytewise
		s.bytewise = true
		v := s.sliceExpr(se, types2.Typ[types2.String])
		s.bytewise = was
		return []value{v}, true
	case "StringControl", "KnownASCII":
		// Read the control word without taking the argument's address.
		ctl := s.stringCtl(s.exprAs(e.Args[0], types2.Typ[types2.String]))
		if fn.Name() == "StringControl" {
			return []value{scalarValue(ctl, types2.Typ[types2.Uintptr])}, true
		}
		bit := s.newValue(ssa.OpAnd, ssa.TI64, ctl, s.constInt(ssa.TI64, strNonASCII))
		return []value{scalarValue(s.newValue(ssa.OpEq, ssa.TBool, bit, s.constInt(ssa.TI64, 0)), types2.Typ[types2.Bool])}, true
	}
	if !countIntrinsics[fn.Name()] {
		return nil, false
	}
	targs := s.instanceArgs(id)
	if len(targs) != 1 {
		s.l.internalf(e.Pos(), "rt.%s is not instantiated", fn.Name())
		return nil, true
	}
	T := targs[0]
	intT := types2.Typ[types2.Int]
	switch fn.Name() {
	case "HoldsStrings":
		return []value{scalarValue(s.constBool(s.l.counted(T)), types2.Typ[types2.Bool])}, true
	case "MoveValues":
		dst := s.scalar(e.Args[0])
		src := s.scalar(e.Args[1])
		n := s.exprAs(e.Args[2], intT).v
		bytes := s.newValue(ssa.OpMul, ssa.TI64, n, s.constInt(ssa.TI64, s.l.sizeof(T)))
		s.callRuntime("memmove", nil, []*ssa.Value{dst, src, bytes})
		return nil, true
	}
	p := s.scalar(e.Args[0])
	n := s.exprAs(e.Args[1], intT).v
	if s.l.counted(T) {
		if fn.Name() == "ReleaseValues" {
			s.releaseRange(p, n, T)
		} else {
			s.retainRange(p, n, T)
		}
	}
	return nil, true
}
