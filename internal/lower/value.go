package lower

import (
	"go/constant"
	"math"

	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// A value is the result of lowering an expression. Scalars are SSA values;
// aggregates are the address of memory holding them. An aggregate value may
// be a variable's own storage, so a consumer that keeps it past another write
// must copy it first (see materialize).
type value struct {
	v    *ssa.Value
	addr bool // v is the address of an aggregate
	T    types2.Type
}

func scalarValue(v *ssa.Value, T types2.Type) value { return value{v: v, T: T} }
func aggValue(ptr *ssa.Value, T types2.Type) value  { return value{v: ptr, addr: true, T: T} }

// An lvalue is a storage location.
type lvalue struct {
	ptr *ssa.Value  // address of the storage (of the packed container for bit fields)
	T   types2.Type // declared type of the storage; may be volatile[T] or aligned[T, N]

	bits *bitRef // non-nil for a packed field
}

// A bitRef locates a packed field inside its container's storage.
type bitRef struct {
	packed *types2.Packed // the outermost packed type, which defines the storage
	offset int64          // bit offset of the field from bit 0 of the storage
	width  int64
	T      types2.Type // type a read produces
	swap   bool        // the field's bytes are big-endian
}

// isVolatile reports whether storage of type T must be accessed volatilely.
func isVolatile(T types2.Type) bool {
	w, ok := T.Underlying().(*types2.Wrapper)
	return ok && w.Kind() == types2.WrapVolatile
}

// unwrap strips volatile and aligned wrappers.
func unwrap(T types2.Type) types2.Type {
	for {
		w, ok := T.Underlying().(*types2.Wrapper)
		if !ok || (w.Kind() != types2.WrapVolatile && w.Kind() != types2.WrapAligned) {
			return T
		}
		T = w.Elem()
	}
}

// markVolatileLoad makes ld volatile and threads it into the memory chain,
// so that no later access merges with it or moves above it.
func (s *fnState) markVolatileLoad(ld *ssa.Value) {
	ld.AuxInt |= ssa.MemVolatile
	s.setMem(s.newValue(ssa.OpVolatileRead, ssa.TMem, ld, s.mem()))
}

// loadLV reads an lvalue.
func (s *fnState) loadLV(lv lvalue) value {
	if lv.bits != nil {
		return scalarValue(s.loadBits(lv), lv.bits.T)
	}
	T := unwrap(lv.T)
	if t := s.l.scalarType(T); t != nil {
		ld := s.load(t, lv.ptr)
		if isVolatile(lv.T) {
			s.markVolatileLoad(ld)
		}
		if p, ok := T.Underlying().(*types2.Packed); ok && needsSwap(p) {
			ld = s.newValue(ssa.OpBswap, t, ld)
		}
		return scalarValue(ld, T)
	}
	return aggValue(lv.ptr, T)
}

// storeLV writes val to an lvalue.
func (s *fnState) storeLV(lv lvalue, val value) {
	if lv.bits != nil {
		s.storeBits(lv, val.v)
		return
	}
	T := unwrap(lv.T)
	if t := s.l.scalarType(T); t != nil {
		v := val.v
		if p, ok := T.Underlying().(*types2.Packed); ok && needsSwap(p) {
			v = s.newValue(ssa.OpBswap, t, v)
		}
		s.store(t, lv.ptr, v)
		if isVolatile(lv.T) {
			s.mem().AuxInt |= ssa.MemVolatile
		}
		return
	}
	s.move(lv.ptr, val.v, s.l.sizeof(T))
}

// storeTo writes val, of type T, to memory at ptr.
func (s *fnState) storeTo(ptr *ssa.Value, val value, T types2.Type) {
	s.storeLV(lvalue{ptr: ptr, T: T}, val)
}

// loadFrom reads a T from memory at ptr.
func (s *fnState) loadFrom(ptr *ssa.Value, T types2.Type) value {
	return s.loadLV(lvalue{ptr: ptr, T: T})
}

// materialize copies an aggregate value into a fresh temporary so later
// writes to its source cannot change it.
func (s *fnState) materialize(v value) value {
	if !v.addr {
		return v
	}
	tmp := s.temp(v.T, "tmp")
	s.move(tmp, v.v, s.l.sizeof(v.T))
	return aggValue(tmp, v.T)
}

// zeroValue returns T's zero value.
func (s *fnState) zeroValue(T types2.Type) value {
	if t := s.l.scalarType(T); t != nil {
		return scalarValue(s.zeroScalar(t), T)
	}
	tmp := s.temp(T, "zero")
	s.zero(tmp, s.l.sizeof(T))
	return aggValue(tmp, T)
}

func (s *fnState) zeroScalar(t *ssa.Type) *ssa.Value {
	if t.IsFloat() {
		return s.constFloat(t, 0)
	}
	return s.constInt(t, 0)
}

func (s *fnState) constFloat(t *ssa.Type, f float64) *ssa.Value {
	v := s.f.ConstInt(t, 0)
	v.Op = ssa.OpConstFloat
	v.AuxInt = int64(math.Float64bits(f))
	return v
}

// leafValues splits a value into the scalars of its leaves.
func (s *fnState) leafValues(val value, leaves []leaf) []*ssa.Value {
	if !val.addr {
		return []*ssa.Value{val.v}
	}
	out := make([]*ssa.Value, len(leaves))
	for i, lf := range leaves {
		out[i] = s.load(lf.t, s.offPtr(val.v, lf.off))
	}
	return out
}

// ----------------------------------------------------------------------------
// Constants

// constValue materializes a constant of type T.
func (s *fnState) constValue(c constant.Value, T types2.Type, pos ast.Node) value {
	T = unwrap(T)
	u := T.Underlying()
	if b, ok := u.(*types2.Basic); ok && b.Info()&types2.IsString != 0 {
		return s.stringConst(constant.StringVal(c), T)
	}
	if _, ok := u.(*types2.Interface); ok {
		// A constant converted to an interface: box its default type.
		dt := defaultType(c)
		return s.toInterface(s.constValue(c, dt, pos), dt, T)
	}
	t := s.l.scalarType(T)
	if t == nil {
		s.l.internalf(pos.Pos(), "constant of non-scalar type %s", T)
		return s.zeroValue(T)
	}
	if t.IsFloat() && s.f.NoSSE {
		s.floatErrorAt(pos.Pos(), t, "constant")
	}
	switch c.Kind() {
	case constant.Bool:
		return scalarValue(s.constBool(constant.BoolVal(c)), T)
	case constant.Int:
		if t.IsFloat() {
			f, _ := constant.Float64Val(c)
			return scalarValue(s.constFloat(t, f), T)
		}
		if i, ok := constant.Int64Val(c); ok {
			return scalarValue(s.constInt(t, i), T)
		}
		u, _ := constant.Uint64Val(c)
		return scalarValue(s.constInt(t, int64(u)), T)
	case constant.Float:
		if t.IsFloat() {
			f, _ := constant.Float64Val(c)
			if t.Size == 4 {
				f = float64(float32(f))
			}
			return scalarValue(s.constFloat(t, f), T)
		}
		// A float constant with an integral value in an integer context.
		i, _ := constant.Int64Val(constant.ToInt(c))
		return scalarValue(s.constInt(t, i), T)
	}
	s.l.internalf(pos.Pos(), "unsupported constant %s", c)
	return s.zeroValue(T)
}

func defaultType(c constant.Value) types2.Type {
	switch c.Kind() {
	case constant.Bool:
		return types2.Typ[types2.Bool]
	case constant.String:
		return types2.Typ[types2.String]
	case constant.Float:
		return types2.Typ[types2.Float64]
	}
	return types2.Typ[types2.Int]
}

// stringConst builds a string value pointing at read-only data.
func (s *fnState) stringConst(str string, T types2.Type) value {
	tmp := s.temp(types2.Typ[types2.String], "str")
	var ptr *ssa.Value
	if str == "" {
		ptr = s.constInt(ssa.TPtr, 0)
	} else {
		ptr = s.f.Entry.NewValue(ssa.OpSymAddr, ssa.TPtr)
		ptr.Aux = s.l.stringData(str)
		s.hoistEntry(ptr)
	}
	s.store(ssa.TPtr, tmp, ptr)
	s.store(ssa.TI64, s.offPtr(tmp, 8), s.constInt(ssa.TI64, int64(len(str))))
	return aggValue(tmp, T)
}

// hoistEntry moves v, just appended to the entry block, to the front so that
// it dominates every use.
func (s *fnState) hoistEntry(v *ssa.Value) {
	vs := s.f.Entry.Values
	i := 0
	for i < len(vs)-1 && vs[i].Op == ssa.OpPhi {
		i++
	}
	copy(vs[i+1:], vs[i:len(vs)-1])
	vs[i] = v
}

// symAddr returns the address of a symbol.
func (s *fnState) symAddr(sym *ssa.Sym) *ssa.Value {
	v := s.f.Entry.NewValue(ssa.OpSymAddr, ssa.TPtr)
	v.Aux = sym
	s.hoistEntry(v)
	return v
}

// ----------------------------------------------------------------------------
// Conversions between scalar representations

// convertScalar converts v from scalar type `from` to `to`, following Go's
// conversion rules (truncation, sign or zero extension, float conversions).
func (s *fnState) convertScalar(v *ssa.Value, from, to types2.Type) *ssa.Value {
	ft := s.l.scalarType(from)
	tt := s.l.scalarType(to)
	if ft == nil || tt == nil {
		return v
	}
	fu := isUnsigned(from)
	switch {
	case (ft.IsInt() || ft.IsBool()) && tt.IsInt():
		switch {
		case ft.Size == tt.Size:
			if ft.IsBool() {
				return s.newValue(ssa.OpZeroExt, tt, v)
			}
			return v
		case ft.Size > tt.Size:
			return s.newValue(ssa.OpTrunc, tt, v)
		case fu || ft.IsBool():
			return s.newValue(ssa.OpZeroExt, tt, v)
		default:
			return s.newValue(ssa.OpSignExt, tt, v)
		}
	case ft.IsInt() && tt.IsFloat():
		return s.intToFloat(v, fu, tt)
	case ft.IsFloat() && tt.IsInt():
		return s.floatToInt(v, isUnsigned(to), tt)
	case ft.IsFloat() && tt.IsFloat():
		if ft.Size == tt.Size {
			return v
		}
		return s.newValue(ssa.OpFloatConv, tt, v)
	case ft.Kind != tt.Kind || ft.Size != tt.Size:
		// pointer <-> uintptr and similar same-size reinterpretations
		return s.newValue(ssa.OpBitcast, tt, v)
	}
	return v
}

// intToFloat converts an integer to a float using only conversions from
// signed 32- and 64-bit integers, which is what the hardware provides.
func (s *fnState) intToFloat(v *ssa.Value, unsigned bool, tt *ssa.Type) *ssa.Value {
	switch {
	case v.Type.Size < 4 && !unsigned:
		v = s.newValue(ssa.OpSignExt, ssa.TI32, v)
	case v.Type.Size < 8 && unsigned:
		v = s.newValue(ssa.OpZeroExt, ssa.TI64, v)
	case v.Type.Size == 8 && unsigned:
		// Values with the top bit set: halve (keeping the low bit so the
		// rounding is right), convert, double.
		res := s.tempSize(tt.Size, tt.Size, "u2f")
		neg := s.newValue(ssa.OpLess, ssa.TBool, v, s.constInt(ssa.TI64, 0))
		big := s.newBlock(ssa.BlockInvalid)
		small := s.newBlock(ssa.BlockInvalid)
		done := s.newBlock(ssa.BlockInvalid)
		s.endIf(neg, big, small)
		s.seal(big)
		s.startBlock(big)
		half := s.newValue(ssa.OpShrL, ssa.TI64, v, s.constInt(ssa.TI64, 1))
		low := s.newValue(ssa.OpAnd, ssa.TI64, v, s.constInt(ssa.TI64, 1))
		f := s.newValue(ssa.OpIntToFloat, tt, s.newValue(ssa.OpOr, ssa.TI64, half, low))
		s.store(tt, res, s.newValue(ssa.OpFAdd, tt, f, f))
		s.endPlain(done)
		s.seal(small)
		s.startBlock(small)
		s.store(tt, res, s.newValue(ssa.OpIntToFloat, tt, v))
		s.endPlain(done)
		s.seal(done)
		s.startBlock(done)
		return s.load(tt, res)
	}
	return s.newValue(ssa.OpIntToFloat, tt, v)
}

// floatToInt converts a float to an integer, truncating, using only the
// hardware's conversions to signed 32- and 64-bit integers.
func (s *fnState) floatToInt(v *ssa.Value, unsigned bool, tt *ssa.Type) *ssa.Value {
	switch {
	case tt.Size == 8 && unsigned:
		// Values at or above 2^63 convert after subtracting 2^63.
		limit := s.constFloat(v.Type, 1<<63)
		lo := s.newValue(ssa.OpFloatToInt, ssa.TI64, v)
		shifted := s.newValue(ssa.OpFloatToInt, ssa.TI64, s.newValue(ssa.OpFSub, v.Type, v, limit))
		hi := s.newValue(ssa.OpXor, ssa.TI64, shifted, s.constInt(ssa.TI64, -1<<63))
		small := s.newValue(ssa.OpFLess, ssa.TBool, v, limit)
		return s.newValue(ssa.OpSelect, ssa.TI64, small, lo, hi)
	case tt.Size == 8:
		return s.newValue(ssa.OpFloatToInt, ssa.TI64, v)
	case unsigned && tt.Size == 4:
		return s.newValue(ssa.OpTrunc, tt, s.newValue(ssa.OpFloatToInt, ssa.TI64, v))
	case tt.Size == 4:
		return s.newValue(ssa.OpFloatToInt, ssa.TI32, v)
	}
	return s.newValue(ssa.OpTrunc, tt, s.newValue(ssa.OpFloatToInt, ssa.TI32, v))
}

func isUnsigned(T types2.Type) bool {
	if b, ok := unwrap(T).Underlying().(*types2.Basic); ok {
		return b.Info()&types2.IsUnsigned != 0
	}
	if _, ok := unwrap(T).Underlying().(*types2.Packed); ok {
		return true
	}
	return false
}

func isSigned(T types2.Type) bool {
	if b, ok := unwrap(T).Underlying().(*types2.Basic); ok {
		return b.Info()&types2.IsInteger != 0 && b.Info()&types2.IsUnsigned == 0
	}
	return false
}

func isString(T types2.Type) bool {
	if b, ok := unwrap(T).Underlying().(*types2.Basic); ok {
		return b.Info()&types2.IsString != 0
	}
	return false
}

func isInterface(T types2.Type) bool {
	_, ok := T.Underlying().(*types2.Interface)
	return ok
}
