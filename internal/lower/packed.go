package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// Packed storage. A packed type whose size is 1, 2, 4 or 8 bytes is held in
// one integer; its fields are shifts and masks of that integer. Larger packed
// types are byte arrays; a field is read by loading the bytes that cover it.
// Bit numbering is LSB-first: bit b is bit b%8 of byte b/8.
//
// Byte order: an integer-backed "packed be" type is byte-swapped when loaded
// and stored, so its fields number bits from the least significant bit of the
// swapped value. In a byte-array packed type with "be", byte-aligned fields
// whose width is a whole number of bytes are byte-swapped individually.

// needsSwap reports whether an integer-held packed type is stored with the
// opposite byte order to the target (little-endian x86-64).
func needsSwap(p *types2.Packed) bool {
	return p.Order() == ast.BigEndian && p.Backing() != nil && isIntegerType(p.Backing())
}

func isIntegerType(T types2.Type) bool {
	b, ok := T.Underlying().(*types2.Basic)
	return ok && b.Info()&types2.IsInteger != 0
}

// packedField extends a container lvalue (or bit reference) with a field.
func (s *fnState) packedField(base lvalue, p *types2.Packed, idx int) lvalue {
	f := p.Field(idx)
	ref := &bitRef{packed: p, offset: f.Offset(), width: f.Width(), T: f.Var().Type()}
	if base.bits != nil {
		// A field of a flag group or nested packed type: offsets add up in
		// the outer storage.
		ref.packed = base.bits.packed
		ref.offset += base.bits.offset
		ref.swap = base.bits.swap
	} else {
		ref.packed = p
	}
	if ref.packed.Order() == ast.BigEndian && s.l.scalarType(ref.packed) == nil &&
		ref.offset%8 == 0 && ref.width%8 == 0 && ref.width > 8 {
		ref.swap = true
	}
	if inner, ok := f.Var().Type().Underlying().(*types2.Packed); ok && s.l.scalarType(inner) == nil {
		// A nested packed type too large for a register is accessed only
		// through its fields.
		ref.T = f.Var().Type()
	}
	return lvalue{ptr: base.ptr, T: base.T, bits: ref}
}

// storageWord loads the whole storage of an integer-held packed container,
// or the bytes covering [offset, offset+width) of a byte-array one. It
// returns the loaded word, its type and the shift of the field within it.
func (s *fnState) storageWord(lv lvalue) (word *ssa.Value, t *ssa.Type, shift int64, ptr *ssa.Value) {
	ref := lv.bits
	if st := s.l.scalarType(ref.packed); st != nil {
		ld := s.load(st, lv.ptr)
		if isVolatile(lv.T) {
			ld.AuxInt |= ssa.MemVolatile
		}
		if needsSwap(ref.packed) {
			ld = s.newValue(ssa.OpBswap, st, ld)
		}
		return ld, st, ref.offset, lv.ptr
	}
	// Byte array storage: load the smallest power-of-two window covering the
	// field that stays inside the storage.
	size := s.l.sizeof(ref.packed)
	first := ref.offset / 8
	last := (ref.offset + ref.width - 1) / 8
	span := last - first + 1
	n := int64(1)
	for n < span {
		n *= 2
	}
	start := first
	if start+n > size {
		start = size - n
	}
	if start < 0 || n > 8 {
		return nil, nil, 0, nil
	}
	t = ssa.IntType(n)
	ptr = s.offPtr(lv.ptr, start)
	ld := s.load(t, ptr)
	return ld, t, ref.offset - start*8, ptr
}

// loadBits reads a packed field.
func (s *fnState) loadBits(lv lvalue) *ssa.Value {
	ref := lv.bits
	rt := s.l.scalarType(ref.T)
	word, wt, shift, _ := s.storageWord(lv)
	if word == nil {
		s.l.errorf(0, "lower: packed field of %d bits at bit %d cannot be accessed", ref.width, ref.offset)
		return s.zeroScalar(rt)
	}
	// Move the field to the top of the word and shift it back down, which
	// both masks it and sign-extends signed fields.
	top := wt.Bits() - shift - ref.width
	v := word
	if top > 0 {
		v = s.newValue(ssa.OpShl, wt, v, s.constInt(ssa.TI64, top))
	}
	down := wt.Bits() - ref.width
	if down > 0 {
		op := ssa.OpShrL
		if isSigned(ref.T) {
			op = ssa.OpShrA
		}
		v = s.newValue(op, wt, v, s.constInt(ssa.TI64, down))
	}
	if ref.swap {
		fieldType := ssa.IntType(ref.width / 8)
		if wt.Size > fieldType.Size {
			v = s.newValue(ssa.OpTrunc, fieldType, v)
		}
		v = s.newValue(ssa.OpBswap, fieldType, v)
		wt = fieldType
	}
	switch {
	case rt.IsBool():
		return s.newValue(ssa.OpNe, ssa.TBool, v, s.constInt(wt, 0))
	case rt.Size < wt.Size:
		return s.newValue(ssa.OpTrunc, rt, v)
	case rt.Size > wt.Size:
		if isSigned(ref.T) && !ref.swap {
			return s.newValue(ssa.OpSignExt, rt, v)
		}
		return s.newValue(ssa.OpZeroExt, rt, v)
	}
	return v
}

// storeBits writes a packed field: read the storage, clear the field's bits,
// insert the new value's low bits, write it back.
func (s *fnState) storeBits(lv lvalue, val *ssa.Value) {
	ref := lv.bits
	word, wt, shift, ptr := s.storageWord(lv)
	if word == nil {
		s.l.errorf(0, "lower: packed field of %d bits at bit %d cannot be accessed", ref.width, ref.offset)
		return
	}
	// Bring the new value to the word's width.
	v := val
	vt := val.Type
	if ref.swap {
		ft := ssa.IntType(ref.width / 8)
		if vt.Size > ft.Size {
			v = s.newValue(ssa.OpTrunc, ft, v)
		} else if vt.Size < ft.Size || vt.IsBool() {
			v = s.newValue(ssa.OpZeroExt, ft, v)
		}
		v = s.newValue(ssa.OpBswap, ft, v)
		vt = ft
	}
	switch {
	case vt.IsBool() || vt.Size < wt.Size:
		v = s.newValue(ssa.OpZeroExt, wt, v)
	case vt.Size > wt.Size:
		v = s.newValue(ssa.OpTrunc, wt, v)
	}
	mask := uint64(1)<<uint(ref.width) - 1
	if ref.width >= 64 {
		mask = ^uint64(0)
	}
	fieldMask := int64(mask << uint(shift))
	if ref.width < wt.Bits() {
		v = s.newValue(ssa.OpAnd, wt, v, s.constInt(wt, int64(mask)))
	}
	if shift > 0 {
		v = s.newValue(ssa.OpShl, wt, v, s.constInt(ssa.TI64, shift))
	}
	kept := s.newValue(ssa.OpAnd, wt, word, s.constInt(wt, ^fieldMask))
	out := s.newValue(ssa.OpOr, wt, kept, v)
	if st := s.l.scalarType(ref.packed); st != nil && needsSwap(ref.packed) {
		out = s.newValue(ssa.OpBswap, wt, out)
	}
	s.store(wt, ptr, out)
	if isVolatile(lv.T) {
		s.mem().AuxInt |= ssa.MemVolatile
	}
}

// bitsOfValue reads a packed field from a packed value held in a register.
func (s *fnState) bitsOfValue(v *ssa.Value, p *types2.Packed, idx int) *ssa.Value {
	tmp := s.temp(p, "packed")
	t := s.l.scalarType(p)
	if needsSwap(p) {
		v = s.newValue(ssa.OpBswap, t, v)
	}
	s.store(t, tmp, v)
	return s.loadBits(s.packedField(lvalue{ptr: tmp, T: p}, p, idx))
}
