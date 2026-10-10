package opt

import (
	"math"
	"math/bits"

	"github.com/matjam/plutonium/internal/ssa"
)

// Simplify folds constants, applies algebraic identities and strength
// reductions, and rewrites division by constants as multiplication. It runs
// to a fixed point.
func Simplify(f *ssa.Func) {
	// Division by a constant is expanded only once the other rules have
	// settled, so that rules matching a division (x % 2^k == 0) see it
	// before it turns into shifts and multiplies.
	for _, expand := range []bool{false, true} {
		for round := 0; round < 20; round++ {
			changed := false
			for _, b := range f.Blocks {
				for i := 0; i < len(b.Values); i++ {
					v := b.Values[i]
					if simplifyValue(f, v, expand) {
						changed = true
					}
				}
			}
			if !changed {
				break
			}
			ssa.CopyElim(f)
		}
	}
	ssa.DeadCode(f)
}

func isConst(v *ssa.Value) bool { return v.Op == ssa.OpConst }

func constEq(v *ssa.Value, c int64) bool { return v.Op == ssa.OpConst && v.AuxInt == ssa.Canon(v.Type, c) }

// replaceConst turns v into the constant c of its own type.
func replaceConst(v *ssa.Value, c int64) bool {
	v.Reset(ssa.OpConst)
	v.AuxInt = ssa.Canon(v.Type, c)
	return true
}

func replaceFloat(v *ssa.Value, f float64) bool {
	v.Reset(ssa.OpConstFloat)
	if v.Type.Size == 4 {
		f = float64(float32(f))
	}
	v.AuxInt = int64(math.Float64bits(f))
	return true
}

func floatOf(v *ssa.Value) float64 { return math.Float64frombits(uint64(v.AuxInt)) }

// rotate rewrites v = x<<c | x>>(w-c) as a left rotation by c. The two
// shifted parts share no bits, so Xor and Add combine them as Or does.
func rotate(v, a, b *ssa.Value) bool {
	if a.Op == ssa.OpShrL {
		a, b = b, a
	}
	if a.Op != ssa.OpShl || b.Op != ssa.OpShrL || a.Args[0] != b.Args[0] || !isConst(a.Args[1]) || !isConst(b.Args[1]) {
		return false
	}
	w := uint64(v.Type.Size * 8)
	l, r := ssa.ZeroExt(a.Args[1].Type, a.Args[1].AuxInt), ssa.ZeroExt(b.Args[1].Type, b.Args[1].AuxInt)
	if l == 0 || r == 0 || l+r != w {
		return false
	}
	x, n := a.Args[0], a.Args[1]
	v.Reset(ssa.OpRotl)
	v.AddArg(x)
	v.AddArg(n)
	return true
}

// stepOf reports whether y is x+1 (d = 1) or x-1 (d = -1).
func stepOf(y, x *ssa.Value) (d int, ok bool) {
	if len(y.Args) != 2 || y.Args[0] != x {
		return 0, false
	}
	switch {
	case y.Op == ssa.OpAdd && constEq(y.Args[1], 1), y.Op == ssa.OpSub && constEq(y.Args[1], -1):
		return 1, true
	case y.Op == ssa.OpAdd && constEq(y.Args[1], -1), y.Op == ssa.OpSub && constEq(y.Args[1], 1):
		return -1, true
	}
	return 0, false
}

// stepOp is the operation that applies a step of direction d.
func stepOp(d int) ssa.Op {
	if d > 0 {
		return ssa.OpAdd
	}
	return ssa.OpSub
}

// newBefore inserts a value before v in v's block.
func newBefore(v *ssa.Value, op ssa.Op, t *ssa.Type, args ...*ssa.Value) *ssa.Value {
	return v.Block.NewValueBefore(v, op, t, args...)
}

func constBefore(f *ssa.Func, t *ssa.Type, c int64) *ssa.Value { return f.ConstInt(t, c) }

// simplifyValue applies one rewrite to v, reporting whether it did.
// expand enables the expansion of division by constants.
func simplifyValue(f *ssa.Func, v *ssa.Value, expand bool) bool {
	if v.Op == ssa.OpCopy {
		return false
	}
	// Canonical order: constants on the right of commutative operations.
	if v.Op.IsCommutative() && len(v.Args) == 2 && isConst(v.Args[0]) && !isConst(v.Args[1]) {
		a, b := v.Args[0], v.Args[1]
		v.SetArg(0, b)
		v.SetArg(1, a)
		return true
	}
	t := v.Type
	switch v.Op {
	case ssa.OpAdd, ssa.OpSub, ssa.OpMul, ssa.OpAnd, ssa.OpOr, ssa.OpXor, ssa.OpAndNot:
		return simplifyIntBinary(f, v)
	case ssa.OpShl, ssa.OpShrL, ssa.OpShrA, ssa.OpRotl:
		return simplifyShift(f, v)
	case ssa.OpDiv, ssa.OpDivu, ssa.OpMod, ssa.OpModu:
		return simplifyDivide(f, v, expand)
	case ssa.OpNeg:
		a := v.Args[0]
		switch {
		case isConst(a):
			return replaceConst(v, -a.AuxInt)
		case a.Op == ssa.OpNeg:
			v.CopyOf(a.Args[0])
			return true
		}
	case ssa.OpCom:
		a := v.Args[0]
		switch {
		case isConst(a):
			return replaceConst(v, ^a.AuxInt)
		case a.Op == ssa.OpCom:
			v.CopyOf(a.Args[0])
			return true
		}
	case ssa.OpNot:
		a := v.Args[0]
		switch {
		case isConst(a):
			return replaceConst(v, 1-a.AuxInt)
		case a.Op == ssa.OpNot:
			v.CopyOf(a.Args[0])
			return true
		}
		if inv, ok := invertCompare(a.Op); ok && a.Uses == 1 {
			// !(a < b) is (b <= a), etc.
			x, y := a.Args[0], a.Args[1]
			if inv.swap {
				x, y = y, x
			}
			v.Reset(inv.op)
			v.AddArg(x)
			v.AddArg(y)
			return true
		}
	case ssa.OpEq, ssa.OpNe, ssa.OpLess, ssa.OpLeq, ssa.OpLessu, ssa.OpLequ:
		return simplifyCompare(f, v)
	case ssa.OpSelect:
		c, a, b := v.Args[0], v.Args[1], v.Args[2]
		switch {
		case isConst(c) && c.AuxInt != 0:
			v.CopyOf(a)
			return true
		case isConst(c):
			v.CopyOf(b)
			return true
		case a == b:
			v.CopyOf(a)
			return true
		}
		// For bools, c ? true : x is c || x and c ? x : false is c && x
		// (x is computed either way).
		if t.IsBool() {
			switch {
			case a == c || isConst(a) && a.AuxInt != 0:
				v.Reset(ssa.OpOr)
				v.AddArg(c)
				v.AddArg(b)
				return true
			case b == c || isConst(b) && b.AuxInt == 0:
				v.Reset(ssa.OpAnd)
				v.AddArg(c)
				v.AddArg(a)
				return true
			}
		}
		// c ? x+1 : x is x + zext(c), and c ? x : x+1 is x + zext(!c);
		// likewise for x-1. A bool is 0 or 1.
		if t.IsInteger() && c.Type.IsBool() {
			if d, ok := stepOf(a, b); ok {
				v.Reset(stepOp(d))
				v.AddArg(b)
				v.AddArg(newBefore(v, ssa.OpZeroExt, t, c))
				return true
			}
			if d, ok := stepOf(b, a); ok {
				v.Reset(stepOp(d))
				v.AddArg(a)
				v.AddArg(newBefore(v, ssa.OpZeroExt, t, newBefore(v, ssa.OpNot, ssa.TBool, c)))
				return true
			}
		}
	case ssa.OpSignExt, ssa.OpZeroExt:
		a := v.Args[0]
		if isConst(a) {
			if v.Op == ssa.OpZeroExt {
				return replaceConst(v, int64(ssa.ZeroExt(a.Type, a.AuxInt)))
			}
			return replaceConst(v, a.AuxInt) // canonical form is sign-extended
		}
		if a.Op == v.Op {
			// ext(ext(x)) = ext(x) to the outer width.
			v.SetArg(0, a.Args[0])
			return true
		}
	case ssa.OpTrunc:
		a := v.Args[0]
		switch {
		case isConst(a):
			return replaceConst(v, a.AuxInt)
		case (a.Op == ssa.OpSignExt || a.Op == ssa.OpZeroExt || a.Op == ssa.OpTrunc) && a.Args[0].Type.Same(t):
			v.CopyOf(a.Args[0])
			return true
		case a.Op == ssa.OpSignExt || a.Op == ssa.OpZeroExt:
			if a.Args[0].Type.Size > t.Size {
				v.SetArg(0, a.Args[0]) // trunc(ext(x)) = trunc(x)
				return true
			}
		case a.Op == ssa.OpTrunc:
			v.SetArg(0, a.Args[0])
			return true
		}
	case ssa.OpBitcast:
		a := v.Args[0]
		if a.Op == ssa.OpBitcast && a.Args[0].Type.Same(t) {
			v.CopyOf(a.Args[0])
			return true
		}
		if isConst(a) && t.IsInteger() {
			return replaceConst(v, a.AuxInt)
		}
	case ssa.OpFAdd, ssa.OpFSub, ssa.OpFMul, ssa.OpFDiv:
		a, b := v.Args[0], v.Args[1]
		if a.Op == ssa.OpConstFloat && b.Op == ssa.OpConstFloat {
			x, y := floatOf(a), floatOf(b)
			var r float64
			switch v.Op {
			case ssa.OpFAdd:
				r = x + y
			case ssa.OpFSub:
				r = x - y
			case ssa.OpFMul:
				r = x * y
			case ssa.OpFDiv:
				r = x / y
			}
			if t.Size == 4 {
				switch v.Op {
				case ssa.OpFAdd:
					r = float64(float32(x) + float32(y))
				case ssa.OpFSub:
					r = float64(float32(x) - float32(y))
				case ssa.OpFMul:
					r = float64(float32(x) * float32(y))
				case ssa.OpFDiv:
					r = float64(float32(x) / float32(y))
				}
			}
			return replaceFloat(v, r)
		}
		// Constants on the right of commutative operations.
		if v.Op.IsCommutative() && a.Op == ssa.OpConstFloat {
			v.SetArg(0, b)
			v.SetArg(1, a)
			return true
		}
		// x * 1.0 = x, x / 1.0 = x (exact, including NaN and signed zero).
		if (v.Op == ssa.OpFMul || v.Op == ssa.OpFDiv) && b.Op == ssa.OpConstFloat && floatOf(b) == 1 {
			v.CopyOf(a)
			return true
		}
		// x * -1.0 = x / -1.0 = -x (exact up to the sign of a NaN, which
		// Go does not specify either).
		if (v.Op == ssa.OpFMul || v.Op == ssa.OpFDiv) && b.Op == ssa.OpConstFloat && floatOf(b) == -1 {
			v.Reset(ssa.OpFNeg)
			v.AddArg(a)
			return true
		}
		// IEEE defines x - y as x + (-y), so these are exact.
		switch {
		case v.Op == ssa.OpFAdd && b.Op == ssa.OpFNeg:
			v.Op = ssa.OpFSub
			v.SetArgs(a, b.Args[0])
			return true
		case v.Op == ssa.OpFAdd && a.Op == ssa.OpFNeg:
			v.Op = ssa.OpFSub
			v.SetArgs(b, a.Args[0])
			return true
		case v.Op == ssa.OpFSub && b.Op == ssa.OpFNeg:
			v.Op = ssa.OpFAdd
			v.SetArgs(a, b.Args[0])
			return true
		}
	case ssa.OpFNeg:
		switch a := v.Args[0]; a.Op {
		case ssa.OpConstFloat:
			return replaceFloat(v, -floatOf(a))
		case ssa.OpFNeg:
			v.CopyOf(a.Args[0])
			return true
		}
	case ssa.OpIntToFloat:
		if a := v.Args[0]; isConst(a) {
			return replaceFloat(v, float64(a.AuxInt))
		}
	case ssa.OpFloatConv:
		if a := v.Args[0]; a.Op == ssa.OpConstFloat {
			return replaceFloat(v, floatOf(a))
		}
	case ssa.OpOffPtr:
		a := v.Args[0]
		switch {
		case v.AuxInt == 0:
			v.CopyOf(a)
			return true
		case a.Op == ssa.OpOffPtr:
			v.AuxInt += a.AuxInt
			v.SetArg(0, a.Args[0])
			return true
		}
	case ssa.OpAddPtr:
		if idx := v.Args[1]; isConst(idx) {
			p := v.Args[0]
			v.Reset(ssa.OpOffPtr)
			v.AuxInt = idx.AuxInt
			v.AddArg(p)
			return true
		}
	case ssa.OpBswap:
		if a := v.Args[0]; isConst(a) {
			switch t.Size {
			case 2:
				return replaceConst(v, int64(bits.ReverseBytes16(uint16(a.AuxInt))))
			case 4:
				return replaceConst(v, int64(bits.ReverseBytes32(uint32(a.AuxInt))))
			case 8:
				return replaceConst(v, int64(bits.ReverseBytes64(uint64(a.AuxInt))))
			}
		}
	}
	return false
}

// ----------------------------------------------------------------------------
// Integer arithmetic

func simplifyIntBinary(f *ssa.Func, v *ssa.Value) bool {
	t := v.Type
	a, b := v.Args[0], v.Args[1]
	if (v.Op == ssa.OpOr || v.Op == ssa.OpXor || v.Op == ssa.OpAdd) && rotate(v, a, b) {
		return true
	}
	if isConst(a) && isConst(b) {
		x, y := a.AuxInt, b.AuxInt
		var r int64
		switch v.Op {
		case ssa.OpAdd:
			r = x + y
		case ssa.OpSub:
			r = x - y
		case ssa.OpMul:
			r = x * y
		case ssa.OpAnd:
			r = x & y
		case ssa.OpOr:
			r = x | y
		case ssa.OpXor:
			r = x ^ y
		case ssa.OpAndNot:
			r = x &^ y
		}
		return replaceConst(v, r)
	}
	switch v.Op {
	case ssa.OpAdd:
		switch {
		case constEq(b, 0):
			v.CopyOf(a)
			return true
		case isConst(b) && a.Op == ssa.OpAdd && isConst(a.Args[1]):
			// (x + c1) + c2 = x + (c1+c2)
			c := constBefore(f, t, a.Args[1].AuxInt+b.AuxInt)
			v.SetArgs(a.Args[0], c)
			return true
		case isConst(b) && a.Op == ssa.OpSub && isConst(a.Args[1]):
			c := constBefore(f, t, b.AuxInt-a.Args[1].AuxInt)
			v.SetArgs(a.Args[0], c)
			return true
		case a.Op == ssa.OpNeg:
			v.Reset(ssa.OpSub)
			v.AddArg(b)
			v.AddArg(a.Args[0])
			return true
		case b.Op == ssa.OpNeg:
			v.Reset(ssa.OpSub)
			v.AddArg(a)
			v.AddArg(b.Args[0])
			return true
		}
	case ssa.OpSub:
		switch {
		case constEq(b, 0):
			v.CopyOf(a)
			return true
		case a == b:
			return replaceConst(v, 0)
		case isConst(b):
			// x - c = x + (-c)
			c := constBefore(f, t, -b.AuxInt)
			v.Reset(ssa.OpAdd)
			v.AddArg(a)
			v.AddArg(c)
			return true
		case constEq(a, 0):
			v.Reset(ssa.OpNeg)
			v.AddArg(b)
			return true
		case a.Op == ssa.OpAdd && a.Args[0] == b:
			v.CopyOf(a.Args[1]) // (x + y) - x = y
			return true
		case a.Op == ssa.OpAdd && a.Args[1] == b:
			v.CopyOf(a.Args[0])
			return true
		}
	case ssa.OpMul:
		if isConst(b) {
			c := b.AuxInt
			switch c {
			case 0:
				return replaceConst(v, 0)
			case 1:
				v.CopyOf(a)
				return true
			case -1:
				v.Reset(ssa.OpNeg)
				v.AddArg(a)
				return true
			}
			if k, ok := isPowerOfTwo(uint64(c)); ok {
				v.Reset(ssa.OpShl)
				v.AddArg(a)
				v.AddArg(constBefore(f, ssa.TI64, int64(k)))
				return true
			}
		}
	case ssa.OpAnd:
		switch {
		case constEq(b, 0):
			return replaceConst(v, 0)
		case constEq(b, -1), a == b:
			v.CopyOf(a)
			return true
		case isConst(b) && a.Op == ssa.OpAnd && isConst(a.Args[1]):
			c := constBefore(f, t, a.Args[1].AuxInt&b.AuxInt)
			v.SetArgs(a.Args[0], c)
			return true
		case isConst(b) && a.Op == ssa.OpZeroExt && t.Size > a.Args[0].Type.Size:
			// The extension already clears the high bits.
			inner := a.Args[0]
			mask := int64(1)<<(8*inner.Type.Size) - 1
			if b.AuxInt&mask == mask {
				v.CopyOf(a)
				return true
			}
		}
	case ssa.OpOr:
		switch {
		case constEq(b, 0), a == b:
			v.CopyOf(a)
			return true
		case constEq(b, -1):
			return replaceConst(v, -1)
		}
	case ssa.OpXor:
		switch {
		case constEq(b, 0):
			v.CopyOf(a)
			return true
		case a == b:
			return replaceConst(v, 0)
		case constEq(b, -1):
			v.Reset(ssa.OpCom)
			v.AddArg(a)
			return true
		}
	case ssa.OpAndNot:
		switch {
		case constEq(b, 0):
			v.CopyOf(a)
			return true
		case a == b:
			return replaceConst(v, 0)
		case isConst(b):
			c := constBefore(f, t, ^b.AuxInt)
			v.Reset(ssa.OpAnd)
			v.AddArg(a)
			v.AddArg(c)
			return true
		}
	}
	return false
}

func simplifyShift(f *ssa.Func, v *ssa.Value) bool {
	t := v.Type
	a, n := v.Args[0], v.Args[1]
	if !isConst(n) {
		return false
	}
	w := uint64(t.Size * 8)
	cnt := ssa.ZeroExt(n.Type, n.AuxInt)
	if cnt == 0 {
		v.CopyOf(a)
		return true
	}
	if isConst(a) {
		x := a.AuxInt
		switch v.Op {
		case ssa.OpShl:
			if cnt >= w {
				return replaceConst(v, 0)
			}
			return replaceConst(v, x<<cnt)
		case ssa.OpShrL:
			if cnt >= w {
				return replaceConst(v, 0)
			}
			return replaceConst(v, int64(ssa.ZeroExt(t, x)>>cnt))
		case ssa.OpShrA:
			if cnt >= w {
				cnt = w - 1
			}
			return replaceConst(v, x>>cnt)
		case ssa.OpRotl:
			k := int(cnt % w)
			switch w {
			case 64:
				return replaceConst(v, int64(bits.RotateLeft64(uint64(x), k)))
			case 32:
				return replaceConst(v, int64(bits.RotateLeft32(uint32(x), k)))
			}
		}
	}
	if (v.Op == ssa.OpShl || v.Op == ssa.OpShrL) && cnt >= w {
		return replaceConst(v, 0)
	}
	// An arithmetic shift by w or more fills with the sign bit, as a shift
	// by w-1 does.
	if v.Op == ssa.OpShrA && cnt >= w {
		v.SetArgs(a, constBefore(f, ssa.TI64, int64(w-1)))
		return true
	}
	// (x << c1) << c2 = x << (c1+c2), and the same for right shifts. Both
	// counts are below w here, so the sum cannot wrap; a sum of w or more
	// shifts every bit out (or, for an arithmetic shift, fills with the
	// sign bit).
	if a.Op == v.Op && v.Op != ssa.OpRotl && isConst(a.Args[1]) {
		c1 := ssa.ZeroExt(a.Args[1].Type, a.Args[1].AuxInt)
		if c1 < w {
			total := cnt + c1
			switch {
			case total < w:
			case v.Op == ssa.OpShrA:
				total = w - 1
			default:
				return replaceConst(v, 0)
			}
			v.SetArgs(a.Args[0], constBefore(f, ssa.TI64, int64(total)))
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------------
// Division

func simplifyDivide(f *ssa.Func, v *ssa.Value, expand bool) bool {
	t := v.Type
	a, b := v.Args[0], v.Args[1]
	if !isConst(b) || b.AuxInt == 0 {
		return false
	}
	signed := v.Op == ssa.OpDiv || v.Op == ssa.OpMod
	isDiv := v.Op == ssa.OpDiv || v.Op == ssa.OpDivu
	if isConst(a) {
		x, y := a.AuxInt, b.AuxInt
		switch v.Op {
		case ssa.OpDiv:
			if y == -1 {
				return replaceConst(v, -x)
			}
			return replaceConst(v, x/y)
		case ssa.OpMod:
			if y == -1 {
				return replaceConst(v, 0)
			}
			return replaceConst(v, x%y)
		case ssa.OpDivu:
			return replaceConst(v, int64(ssa.ZeroExt(t, x)/ssa.ZeroExt(t, y)))
		case ssa.OpModu:
			return replaceConst(v, int64(ssa.ZeroExt(t, x)%ssa.ZeroExt(t, y)))
		}
	}

	if !expand {
		return false
	}
	w := uint(t.Size * 8)
	if !signed {
		c := ssa.ZeroExt(t, b.AuxInt)
		if c == 1 {
			if isDiv {
				v.CopyOf(a)
			} else {
				replaceConst(v, 0)
			}
			return true
		}
		if k, ok := isPowerOfTwo(c); ok {
			if isDiv {
				v.Reset(ssa.OpShrL)
				v.AddArg(a)
				v.AddArg(constBefore(f, ssa.TI64, int64(k)))
			} else {
				v.Reset(ssa.OpAnd)
				v.AddArg(a)
				v.AddArg(constBefore(f, t, int64(c-1)))
			}
			return true
		}
		q := unsignedDivConst(f, v, a, c)
		if isDiv {
			v.CopyOf(q)
		} else {
			prod := newBefore(v, ssa.OpMul, t, q, constBefore(f, t, int64(c)))
			v.Reset(ssa.OpSub)
			v.AddArg(a)
			v.AddArg(prod)
		}
		return true
	}

	c := b.AuxInt
	if c == 1 {
		if isDiv {
			v.CopyOf(a)
		} else {
			replaceConst(v, 0)
		}
		return true
	}
	if c == -1 || c == math.MinInt64 || c == ssa.Canon(t, int64(1)<<(w-1)) {
		return false // leave the awkward ones to the hardware
	}
	neg := c < 0
	if neg {
		c = -c
	}
	var q *ssa.Value
	if k, ok := isPowerOfTwo(uint64(c)); ok {
		// Round toward zero: add c-1 to negative dividends before shifting.
		sign := newBefore(v, ssa.OpShrA, t, a, constBefore(f, ssa.TI64, int64(w-1)))
		bias := newBefore(v, ssa.OpShrL, t, sign, constBefore(f, ssa.TI64, int64(w-k)))
		sum := newBefore(v, ssa.OpAdd, t, a, bias)
		q = newBefore(v, ssa.OpShrA, t, sum, constBefore(f, ssa.TI64, int64(k)))
	} else {
		q = signedDivConst(f, v, a, c)
	}
	if neg {
		q = newBefore(v, ssa.OpNeg, t, q)
		c = -c
	}
	if isDiv {
		v.CopyOf(q)
	} else {
		prod := newBefore(v, ssa.OpMul, t, q, constBefore(f, t, c))
		v.Reset(ssa.OpSub)
		v.AddArg(a)
		v.AddArg(prod)
	}
	return true
}

// unsignedDivConst emits x / c for unsigned x of any width.
func unsignedDivConst(f *ssa.Func, at, x *ssa.Value, c uint64) *ssa.Value {
	t := x.Type
	x64 := x
	if t.Size < 8 {
		x64 = newBefore(at, ssa.OpZeroExt, ssa.TI64, x)
	}
	s, m := umagic(c)
	var q *ssa.Value
	if m&1 == 0 {
		hi := newBefore(at, ssa.OpMulHiu, ssa.TI64, x64, constBefore(f, ssa.TI64, int64(1<<63+m/2)))
		q = newBefore(at, ssa.OpShrL, ssa.TI64, hi, constBefore(f, ssa.TI64, int64(s-1)))
	} else {
		hi := newBefore(at, ssa.OpMulHiu, ssa.TI64, x64, constBefore(f, ssa.TI64, int64(m)))
		d := newBefore(at, ssa.OpSub, ssa.TI64, x64, hi)
		half := newBefore(at, ssa.OpShrL, ssa.TI64, d, constBefore(f, ssa.TI64, 1))
		avg := newBefore(at, ssa.OpAdd, ssa.TI64, half, hi)
		q = newBefore(at, ssa.OpShrL, ssa.TI64, avg, constBefore(f, ssa.TI64, int64(s-1)))
	}
	if t.Size < 8 {
		q = newBefore(at, ssa.OpTrunc, t, q)
	}
	return q
}

// signedDivConst emits x / c for signed x of any width and c > 1, not a
// power of two.
func signedDivConst(f *ssa.Func, at, x *ssa.Value, c int64) *ssa.Value {
	t := x.Type
	x64 := x
	if t.Size < 8 {
		x64 = newBefore(at, ssa.OpSignExt, ssa.TI64, x)
	}
	s, m := smagic(c)
	var q *ssa.Value
	if m&1 == 0 {
		hi := newBefore(at, ssa.OpMulHi, ssa.TI64, x64, constBefore(f, ssa.TI64, int64(m/2)))
		q = newBefore(at, ssa.OpShrA, ssa.TI64, hi, constBefore(f, ssa.TI64, int64(s-1)))
	} else {
		hi := newBefore(at, ssa.OpMulHi, ssa.TI64, x64, constBefore(f, ssa.TI64, int64(m)))
		sum := newBefore(at, ssa.OpAdd, ssa.TI64, hi, x64)
		q = newBefore(at, ssa.OpShrA, ssa.TI64, sum, constBefore(f, ssa.TI64, int64(s)))
	}
	sign := newBefore(at, ssa.OpShrA, ssa.TI64, x64, constBefore(f, ssa.TI64, 63))
	q = newBefore(at, ssa.OpSub, ssa.TI64, q, sign)
	if t.Size < 8 {
		q = newBefore(at, ssa.OpTrunc, t, q)
	}
	return q
}

// ----------------------------------------------------------------------------
// Comparisons

type inverted struct {
	op   ssa.Op
	swap bool
}

// invertCompare returns the comparison computing the negation of op.
func invertCompare(op ssa.Op) (inverted, bool) {
	switch op {
	case ssa.OpEq:
		return inverted{ssa.OpNe, false}, true
	case ssa.OpNe:
		return inverted{ssa.OpEq, false}, true
	case ssa.OpLess:
		return inverted{ssa.OpLeq, true}, true
	case ssa.OpLeq:
		return inverted{ssa.OpLess, true}, true
	case ssa.OpLessu:
		return inverted{ssa.OpLequ, true}, true
	case ssa.OpLequ:
		return inverted{ssa.OpLessu, true}, true
	}
	return inverted{}, false
}

func simplifyCompare(f *ssa.Func, v *ssa.Value) bool {
	a, b := v.Args[0], v.Args[1]
	t := a.Type
	if isConst(a) && isConst(b) {
		x, y := a.AuxInt, b.AuxInt
		ux, uy := ssa.ZeroExt(t, x), ssa.ZeroExt(t, y)
		var r bool
		switch v.Op {
		case ssa.OpEq:
			r = x == y
		case ssa.OpNe:
			r = x != y
		case ssa.OpLess:
			r = x < y
		case ssa.OpLeq:
			r = x <= y
		case ssa.OpLessu:
			r = ux < uy
		case ssa.OpLequ:
			r = ux <= uy
		}
		if r {
			return replaceConst(v, 1)
		}
		return replaceConst(v, 0)
	}
	if a == b && !t.IsFloat() {
		switch v.Op {
		case ssa.OpEq, ssa.OpLeq, ssa.OpLequ:
			return replaceConst(v, 1)
		default:
			return replaceConst(v, 0)
		}
	}
	// Bool comparisons with constants.
	if t.IsBool() && isConst(b) {
		switch {
		case v.Op == ssa.OpEq && b.AuxInt == 1, v.Op == ssa.OpNe && b.AuxInt == 0:
			v.CopyOf(a)
			return true
		case v.Op == ssa.OpEq && b.AuxInt == 0, v.Op == ssa.OpNe && b.AuxInt == 1:
			v.Reset(ssa.OpNot)
			v.AddArg(a)
			return true
		}
	}
	// x % 2^k == 0 tests the low bits, whatever the sign.
	if (v.Op == ssa.OpEq || v.Op == ssa.OpNe) && constEq(b, 0) && (a.Op == ssa.OpMod || a.Op == ssa.OpModu) && isConst(a.Args[1]) {
		if k, ok := isPowerOfTwo(ssa.ZeroExt(t, a.Args[1].AuxInt)); ok && k > 0 {
			mask := newBefore(v, ssa.OpAnd, t, a.Args[0], constBefore(f, t, int64(1)<<k-1))
			v.SetArg(0, mask)
			return true
		}
	}
	// x <u 0 is false; 0 <=u x is true.
	if v.Op == ssa.OpLessu && constEq(b, 0) {
		return replaceConst(v, 0)
	}
	if v.Op == ssa.OpLequ && constEq(a, 0) {
		return replaceConst(v, 1)
	}
	return false
}
