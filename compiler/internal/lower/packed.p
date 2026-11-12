// Translated by go2pu from internal/lower/packed.go at the go-cutover tag; edited by hand since.

package lower

import (
	"compiler/internal/ssa"
	"compiler/internal/syntax/ast"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/slices"
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

// comparedBits returns the bits of packed type T that comparison and map
// hashing look at (spec section 7.14), numbered as in a register holding a
// value of T, or nil when T is not packed or has no reserved bits.
func comparedBits(T types2.Type) []byte {
	p, ok := unwrap(T).Underlying().(*types2.Packed)
	if !ok {
		return nil
	}
	return p.ComparedBits()
}

// comparedBitsInMemory is comparedBits laid out as the bytes of a value of T
// in memory, for the type descriptor.
func comparedBitsInMemory(T types2.Type) []byte {
	m := comparedBits(T)
	if m != nil && needsSwap(unwrap(T).Underlying().(*types2.Packed)) {
		slices.Reverse(m)
	}
	return m
}

// leWord reads up to eight bytes as a little-endian word.
func leWord(b []byte) uint64 {
	var v uint64
	for i := 0; i < len(b) && i < 8; i++ {
		v |= uint64(b[i]) << (8 * i)
	}
	return v
}

// packedEqual compares two packed values of type T whose reserved bits are
// given by mask (comparedBits): equal when the bits outside the reserved
// bits are equal.
func (s *fnState) packedEqual(x, y value, T types2.Type, mask []byte) *ssa.Value {
	if st := s.l.scalarType(unwrap(T)); st != nil {
		xv, yv := x.v, y.v
		if x.addr {
			xv = s.loadFrom(xv, T).v
		}
		if y.addr {
			yv = s.loadFrom(yv, T).v
		}
		m := leWord(mask)
		xm := s.newValue(ssa.OpAnd, st, xv, s.constInt(st, int64(m)))
		ym := s.newValue(ssa.OpAnd, st, yv, s.constInt(st, int64(m)))
		return s.newValue(ssa.OpEq, ssa.TBool, xm, ym)
	}
	// Stored as bytes: compare them in chunks of 8, 4, 2 and 1 bytes,
	// leaving out the chunks with no compared bit.
	result := s.constBool(true)
	for off := int64(0); off < int64(len(mask)); {
		n := int64(8)
		for n > int64(len(mask))-off {
			n /= 2
		}
		m := leWord(mask[off : off+n])
		if m != 0 {
			t := ssa.IntType(n)
			xv := s.load(t, s.offPtr(x.v, off))
			yv := s.load(t, s.offPtr(y.v, off))
			if m != ^uint64(0)>>(64-8*n) {
				xv = s.newValue(ssa.OpAnd, t, xv, s.constInt(t, int64(m)))
				yv = s.newValue(ssa.OpAnd, t, yv, s.constInt(t, int64(m)))
			}
			result = s.newValue(ssa.OpAnd, ssa.TBool, result, s.newValue(ssa.OpEq, ssa.TBool, xv, yv))
		}
		off += n
	}
	return result
}

func isIntegerType(T types2.Type) bool {
	b, ok := T.Underlying().(*types2.Basic)
	return ok && b.Info()&types2.IsInteger != 0
}

// packedField extends a container lvalue (or bit reference) with a field of
// the packed type p.
func (s *fnState) packedField(base lvalue, p *types2.Packed, idx int) lvalue {
	f := p.Field(idx)
	if base.bits != nil && base.bits.swap {
		// A big-endian integer packed type that does not start on a byte,
		// inside a type laid out by byte: its value is the swap of its
		// bytes, and its own fields are defined on that value by the
		// integer rule (section 7.14, Byte order). Its fields are read and
		// written in a copy of the value.
		W := base.bits.T
		tmp := s.temp(W, "packed")
		base = lvalue{ptr: tmp, T: W, stable: true, win: pu.New(packedWindow{src: base, tmp: tmp})}
	}
	return s.subField(base, p, f.Offset(), f.Width(), f.Var().Type(), f.Var().Name(), !f.IsBits())
}

// subField extends a container lvalue (or bit reference) with the part of
// the packed type p that starts at bit rel of p and is width bits wide, of
// type ft: a field, or an element of an array field. byteLevel says the
// part is a byte-level field of p, whose bytes follow p's byte order.
func (s *fnState) subField(base lvalue, p *types2.Packed, rel, width int64, ft types2.Type, name string, byteLevel bool) lvalue {
	ref := pu.New(bitRef{packed_: p, offset: rel, width: width, T: ft})
	if base.bits != nil {
		// A field of a flag group or nested packed type: offsets add up in
		// the outer storage.
		ref.packed_ = base.bits.packed_
		ref.offset += base.bits.offset
	}
	inner, nested := ft.Underlying().(*types2.Packed)
	if nested && needsSwap(inner) && !isIntegerBacked(ref.packed_) {
		// A packed type with a big-endian integer backing inside a type
		// laid out by byte (section 7.14, Byte order): the field is the
		// integer stored in the bytes that start at its first bit, and its
		// own fields are defined by the integer rule.
		if ref.offset%8 == 0 {
			return lvalue{ptr: s.offPtr(base.ptr, ref.offset/8), T: ft, stable: base.stable, win: base.win}
		}
		// Off a byte, its bytes are the groups of eight bits from its first
		// bit: the value is their swap.
		ref.swap = width > 8
		return lvalue{ptr: base.ptr, T: base.T, bits: ref, win: base.win}
	}
	if _, isArray := unwrap(ft).Underlying().(*types2.Array); isArray {
		if byteLevel && p.Order() == ast.BigEndian && !s.singleBytes(ft) {
			// The type checker rejects an array of elements wider than a
			// byte in a big-endian packed type (E2111, decision D131).
			s.l.internalf(s.pos, "packed field %s is an array of elements wider than a byte in a big-endian packed type", pu.Box(name))
		}
		// An array field (section 7.14, Fields) is plain memory when it
		// starts on a byte, unless the storage is a big-endian integer,
		// whose bytes are swapped as a whole: element i is then the byte
		// at bit 8i of the field in the swapped value (decision D131). A
		// volatile packed value is read and written whole (section 7.15),
		// so its array fields are reached as part of the value, like one
		// that does not start on a byte.
		if ref.offset%8 == 0 && !isVolatile(base.T) && !needsSwap(ref.packed_) {
			return lvalue{ptr: s.offPtr(base.ptr, ref.offset/8), T: ft, stable: base.stable, win: base.win}
		}
		return lvalue{ptr: base.ptr, T: base.T, bits: ref, win: base.win}
	}
	if b, ok := ft.Underlying().(*types2.Basic); ok && b.Info()&(types2.IsInteger|types2.IsBoolean|types2.IsFloat) != 0 &&
		p.Order() == ast.BigEndian && !isIntegerBacked(p) && byteLevel && width > 8 && rel%8 == 0 {
		// A field of several bytes in a big-endian type laid out by byte
		// is stored most significant byte first.
		switch width {
		case 16, 32, 64:
			ref.swap = true
		default:
			// A byte-level integer, bool or float field is 8, 16, 32 or 64
			// bits wide, and a bits field never gets here.
			s.l.internalf(s.pos, "packed field %s of %d bits in a big-endian packed type", pu.Box(name), width)
		}
	}
	return lvalue{ptr: base.ptr, T: base.T, bits: ref, win: base.win}
}

// placeAddr returns the address of the storage lv denotes. A packed field
// has an address only when it is whole bytes of plain memory (section 7.14,
// decision D131), which the type checker enforces (E2112): a field read
// with shifts and masks then starts on a byte of storage that is not
// swapped as a whole, and its bytes are those of a variable of its type.
func (s *fnState) placeAddr(lv lvalue) *ssa.Value {
	if lv.win != nil {
		s.l.unsupportedf(s.pos, "the address of a packed field that is not plain memory cannot be taken")
		return s.absAddr(lv.ptr)
	}
	if ref := lv.bits; ref != nil {
		if ref.swap || ref.offset%8 != 0 || ref.width != 8*s.l.sizeof(ref.T) || needsSwap(ref.packed_) || isVolatile(lv.T) {
			s.l.unsupportedf(s.pos, "the address of a packed field that is not plain memory cannot be taken")
		}
		return s.absAddr(s.offPtr(lv.ptr, ref.offset/8))
	}
	return s.absAddr(lv.ptr)
}

// packedIndex returns element e.Index of the array field arr, which has no
// place of its own in its storage (subField).
func (s *fnState) packedIndex(arr lvalue, u *types2.Array, e *ast.IndexExpr) lvalue {
	elem := s.subst(u.Elem())
	eb := s.l.sizeof(elem) * 8
	if c, isConst := constIndex(s.info, e.Index); isConst && c >= 0 && c < u.Len() {
		// Element c is the part of the field at bit c*eb.
		return s.subField(arr, arr.bits.packed_, c*eb, eb, elem, "", false)
	}
	i := s.indexValue(e.Index)
	arr = s.nilGuard(arr)
	s.boundsCheck(i, s.constInt(ssa.TI64, u.Len()), e.Index.Pos())
	tmp := s.temp(arr.bits.T, "packed")
	return lvalue{ptr: s.elemAddr(tmp, i, u.Elem()), T: elem, stable: true, win: pu.New(packedWindow{src: arr, tmp: tmp})}
}

// volatileRoot returns the volatile packed storage that lv, a part of a
// packed value, lies in, or false when it does not lie in volatile storage.
func volatileRoot(lv lvalue) (lvalue, bool) {
	for lv.win != nil {
		lv = lv.win.src
	}
	if lv.bits == nil || !isVolatile(lv.T) {
		return lvalue{}, false
	}
	lv.bits = nil
	return lv, true
}

// rebaseRoot returns lv with the storage it lies in replaced by snap, a
// copy of type T.
func rebaseRoot(lv lvalue, snap *ssa.Value, T types2.Type) lvalue {
	if lv.win != nil {
		lv.win = pu.New(packedWindow{src: rebaseRoot(lv.win.src, snap, T), tmp: lv.win.tmp})
		return lv
	}
	lv.ptr, lv.T = snap, T
	lv.nilBase = nil
	return lv
}

// loadPackedPart reads a part of a packed value that is not one field of
// at most eight bytes in its storage: an array field or large packed field
// that is not plain memory, or a part of a window. A volatile value is
// read with one load (section 7.15).
func (s *fnState) loadPackedPart(lv lvalue) value {
	if root, ok := volatileRoot(lv); ok {
		T := unwrap(root.T)
		snap := s.temp(T, "volatile")
		s.storeTo(snap, s.loadLV(root), T)
		lv = rebaseRoot(lv, snap, T)
	}
	var v value
	if lv.win != nil {
		w := lv.win
		s.storeTo(w.tmp, s.loadLV(w.src), w.src.bits.T)
		in := lv
		in.win = nil
		v = s.loadLV(in)
		if v.addr {
			// The window is filled again by the next access.
			v = s.materialize(v)
			v.cnt = cntStable
		}
	} else {
		T := lv.bits.T
		tmp := s.temp(T, "packed")
		for off, size := int64(0), s.l.sizeof(T); off < size; {
			n := chunkSize(size - off)
			s.store(ssa.IntType(n), s.offPtr(tmp, off), s.loadBits(s.packedChunk(lv, off, n)))
			off += n
		}
		v = aggValue(tmp, T)
		v.cnt = cntStable
	}
	return v
}

// storePackedPart writes a part of a packed value that loadPackedPart
// reads. A volatile value is written with one load and one store
// (section 7.15).
func (s *fnState) storePackedPart(lv lvalue, val value) {
	val = s.materialize(val)
	root, vol := volatileRoot(lv)
	var snap *ssa.Value
	var T types2.Type
	if vol {
		T = unwrap(root.T)
		snap = s.temp(T, "volatile")
		s.storeTo(snap, s.loadLV(root), T)
		lv = rebaseRoot(lv, snap, T)
	}
	if lv.win != nil {
		w := lv.win
		WT := w.src.bits.T
		s.storeTo(w.tmp, s.loadLV(w.src), WT)
		in := lv
		in.win = nil
		s.storeLV(in, val)
		s.storeLV(w.src, s.loadFrom(w.tmp, WT))
	} else {
		for off, size := int64(0), s.l.sizeof(lv.bits.T); off < size; {
			n := chunkSize(size - off)
			s.storeBits(s.packedChunk(lv, off, n), s.load(ssa.IntType(n), s.offPtr(val.v, off)))
			off += n
		}
	}
	if vol {
		s.storeLV(root, s.loadFrom(snap, T))
	}
}

// chunkSize returns the size of the first chunk when n bytes are covered by
// chunks of 8, 4, 2 and 1 bytes, largest first.
func chunkSize(n int64) int64 {
	c := int64(8)
	for c > n {
		c /= 2
	}
	return c
}

// packedChunk returns the n bytes at byte off of the bit field lv as a bit
// field of an unsigned integer: as bits, numbered from the field's first
// bit, they are the bytes of the field's value in memory.
func (s *fnState) packedChunk(lv lvalue, off, n int64) lvalue {
	var T types2.Type
	switch n {
	case 1:
		T = types2.Typ[types2.Uint8]
	case 2:
		T = types2.Typ[types2.Uint16]
	case 4:
		T = types2.Typ[types2.Uint32]
	default:
		T = types2.Typ[types2.Uint64]
	}
	out := lv
	out.bits = pu.New(bitRef{packed_: lv.bits.packed_, offset: lv.bits.offset + 8*off, width: 8 * n, T: T})
	return out
}

// singleBytes reports whether the elements of the array type ft, and of
// the arrays it holds, are single bytes.
func (s *fnState) singleBytes(ft types2.Type) bool {
	elem := unwrap(ft)
	for {
		a, ok := elem.Underlying().(*types2.Array)
		if !ok {
			break
		}
		elem = unwrap(a.Elem())
	}
	return s.l.sizeof(elem) == 1
}

// isIntegerBacked reports whether p is stored in an integer, whose bit
// positions count from the least significant bit of the value.
func isIntegerBacked(p *types2.Packed) bool {
	return p.Backing() != nil && isIntegerType(p.Backing())
}

// storageWord loads the whole storage of an integer-held packed container,
// or the bytes covering [offset, offset+width) of a byte-array one. It
// returns the loaded word, its type and the shift of the field within it.
func (s *fnState) storageWord(lv lvalue) (word *ssa.Value, t *ssa.Type, shift int64, ptr *ssa.Value) {
	ref := lv.bits
	if st := s.l.scalarType(ref.packed_); st != nil {
		ld := s.load(st, lv.ptr)
		if isVolatile(lv.T) {
			s.markVolatileLoad(ld)
		}
		if needsSwap(ref.packed_) {
			ld = s.newValue(ssa.OpBswap, st, ld)
		}
		return ld, st, ref.offset, lv.ptr
	}
	// Byte array storage: load the smallest power-of-two window covering the
	// field that stays inside the storage.
	size := s.l.sizeof(ref.packed_)
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
	if rt.IsFloat() {
		// A float field is read as the integer of its bits.
		it := lv
		it.bits = pu.New(*ref)
		it.bits.T = types2.Typ[types2.Uint32]
		if rt.Size == 8 {
			it.bits.T = types2.Typ[types2.Uint64]
		}
		return s.newValue(ssa.OpBitcast, rt, s.loadBits(it))
	}
	word, wt, shift, _ := s.storageWord(lv)
	if word == nil {
		word, wt, shift = s.wideWord(lv), ssa.IntType(8), 0
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
	// Bring the new value to the word's width. A float is written as the
	// integer of its bits.
	v := val
	vt := val.Type
	if vt.IsFloat() {
		vt = ssa.IntType(vt.Size)
		v = s.newValue(ssa.OpBitcast, vt, v)
	}
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
	if word == nil {
		s.storeWide(lv, v, vt)
		return
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
	if st := s.l.scalarType(ref.packed_); st != nil && needsSwap(ref.packed_) {
		out = s.newValue(ssa.OpBswap, wt, out)
	}
	s.store(wt, ptr, out)
	if isVolatile(lv.T) {
		s.mem().AuxInt |= ssa.MemVolatile
	}
}

// wideWord loads a field of byte array storage that no power-of-two window
// of at most eight bytes inside the storage covers, as a 64-bit word that
// holds the field at bit 0 and other bits above it. Such a field spans at
// most nine bytes.
func (s *fnState) wideWord(lv lvalue) *ssa.Value {
	ref := lv.bits
	first, r := ref.offset/8, ref.offset%8
	span := (ref.offset+ref.width-1)/8 - first + 1
	t64 := ssa.IntType(8)
	if span <= 8 {
		v := s.loadRun(s.offPtr(lv.ptr, first), span)
		if r > 0 {
			v = s.newValue(ssa.OpShrL, t64, v, s.constInt(ssa.TI64, r))
		}
		return v
	}
	if span != 9 || r == 0 {
		s.l.internalf(s.pos, "packed field of %d bits at bit %d", ref.width, ref.offset)
	}
	t8 := ssa.IntType(1)
	lo := s.load(t64, s.offPtr(lv.ptr, first))
	hi := s.load(t8, s.offPtr(lv.ptr, first+8))
	lo = s.newValue(ssa.OpShrL, t64, lo, s.constInt(ssa.TI64, r))
	hi = s.newValue(ssa.OpShl, t64, s.newValue(ssa.OpZeroExt, t64, hi), s.constInt(ssa.TI64, 64-r))
	return s.newValue(ssa.OpOr, t64, lo, hi)
}

// storeWide writes v, of type vt, the integer of a field's bits, to a field
// that wideWord reads.
func (s *fnState) storeWide(lv lvalue, v *ssa.Value, vt *ssa.Type) {
	ref := lv.bits
	first, r, w := ref.offset/8, ref.offset%8, ref.width
	span := (ref.offset+w-1)/8 - first + 1
	t64, t8 := ssa.IntType(8), ssa.IntType(1)
	if vt.IsBool() || vt.Size < 8 {
		v = s.newValue(ssa.OpZeroExt, t64, v)
	}
	mask := ^uint64(0)
	if w < 64 {
		mask = uint64(1)<<uint(w) - 1
		v = s.newValue(ssa.OpAnd, t64, v, s.constInt(t64, int64(mask)))
	}
	if span <= 8 {
		ptr := s.offPtr(lv.ptr, first)
		word := s.loadRun(ptr, span)
		word = s.newValue(ssa.OpAnd, t64, word, s.constInt(t64, int64(^(mask<<uint(r)))))
		if r > 0 {
			v = s.newValue(ssa.OpShl, t64, v, s.constInt(ssa.TI64, r))
		}
		s.storeRun(ptr, span, s.newValue(ssa.OpOr, t64, word, v))
		return
	}
	if span != 9 || r == 0 {
		s.l.internalf(s.pos, "packed field of %d bits at bit %d", w, ref.offset)
	}
	// The low bits of the field go in the top of the first eight bytes.
	loPtr := s.offPtr(lv.ptr, first)
	lo := s.load(t64, loPtr)
	lo = s.newValue(ssa.OpAnd, t64, lo, s.constInt(t64, int64(^(mask<<uint(r)))))
	lo = s.newValue(ssa.OpOr, t64, lo, s.newValue(ssa.OpShl, t64, v, s.constInt(ssa.TI64, r)))
	// The rest go in the bottom of the ninth.
	hiMask := int64(1)<<uint(r+w-64) - 1
	hiPtr := s.offPtr(lv.ptr, first+8)
	hi := s.load(t8, hiPtr)
	hi = s.newValue(ssa.OpAnd, t8, hi, s.constInt(t8, ^hiMask))
	top := s.newValue(ssa.OpTrunc, t8, s.newValue(ssa.OpShrL, t64, v, s.constInt(ssa.TI64, 64-r)))
	hi = s.newValue(ssa.OpOr, t8, hi, s.newValue(ssa.OpAnd, t8, top, s.constInt(t8, hiMask)))
	s.store(t64, loPtr, lo)
	s.store(t8, hiPtr, hi)
}

// loadRun loads the n bytes at ptr, at most eight, as a little-endian
// 64-bit word.
func (s *fnState) loadRun(ptr *ssa.Value, n int64) *ssa.Value {
	t64 := ssa.IntType(8)
	var word *ssa.Value
	for off := int64(0); off < n; {
		size := chunkSize(n - off)
		v := s.load(ssa.IntType(size), s.offPtr(ptr, off))
		if size < 8 {
			v = s.newValue(ssa.OpZeroExt, t64, v)
		}
		if off > 0 {
			v = s.newValue(ssa.OpShl, t64, v, s.constInt(ssa.TI64, 8*off))
		}
		if word == nil {
			word = v
		} else {
			word = s.newValue(ssa.OpOr, t64, word, v)
		}
		off += size
	}
	return word
}

// storeRun stores the low n bytes of the 64-bit word v at ptr.
func (s *fnState) storeRun(ptr *ssa.Value, n int64, v *ssa.Value) {
	t64 := ssa.IntType(8)
	for off := int64(0); off < n; {
		size := chunkSize(n - off)
		c := v
		if off > 0 {
			c = s.newValue(ssa.OpShrL, t64, c, s.constInt(ssa.TI64, 8*off))
		}
		if size < 8 {
			c = s.newValue(ssa.OpTrunc, ssa.IntType(size), c)
		}
		s.store(ssa.IntType(size), s.offPtr(ptr, off), c)
		off += size
	}
}

// bitsOfValue reads a scalar packed field from a packed value held in a
// register.
func (s *fnState) bitsOfValue(v *ssa.Value, p *types2.Packed, idx int) *ssa.Value {
	return s.fieldOfValue(v, p, idx).v
}

// fieldOfValue reads a packed field, scalar or array, from a packed value
// held in a register.
func (s *fnState) fieldOfValue(v *ssa.Value, p *types2.Packed, idx int) value {
	tmp := s.temp(p, "packed")
	t := s.l.scalarType(p)
	if needsSwap(p) {
		v = s.newValue(ssa.OpBswap, t, v)
	}
	s.store(t, tmp, v)
	return s.loadLV(s.packedField(lvalue{ptr: tmp, T: p}, p, idx))
}
