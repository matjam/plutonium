package types2

import (
	"compiler/internal/syntax/ast"
	. "compiler/internal/types2/errors"
	"compiler/pu"
)

// The address of a packed field (spec section 7.14, decision D131) can be
// taken only when the field is whole bytes of plain memory: a field
// declared with a type (not a bit field, an enum or a flag group) that
// starts on a byte, is not a bool or an array of bools, lies in a value
// that is not volatile, and holds its bytes as a variable of its type
// would. That rules out a field of several bytes in a big-endian type laid
// out by byte, and every field of a big-endian integer-backed type, whose
// bytes are swapped as a whole. Taking the address covers &x.f, slicing an
// array field and calling a pointer method on a field (E2112).
//
// The rule follows how fields are stored (lower/packed.p): a field is a
// part of the storage of the outermost packed value it lies in, except
// that a big-endian integer-backed field on a byte of a type laid out by
// byte, and an element of an array field, are storage of their own.

// A packedPart describes a selector expression that denotes a field of a
// packed value.
type packedPart struct {
	plain bool // whole bytes of plain memory
	// fresh: the field is storage of its own (see above); storage is the
	// packed type whose storage holds the field otherwise.
	fresh   bool
	storage *Packed
}

// recordPackedField records the part that e, a selector of the field
// index of a value x, denotes when the field is a field of a packed type.
func (check *Checker) recordPackedField(x *operand, e *ast.SelectorExpr, index []int, indirect bool) {
	T := x.typ()
	var outer *packedPart // the part x denotes, if x is a field of a packed value
	if len(index) == 1 {
		if part, ok := check.packedPartOf(e.X); ok {
			outer = &part
		}
	}
	for _, i := range index[:len(index)-1] {
		T, _ = derefBox(T)
		s, ok := unwrapTransparent(T).Underlying().(*Struct)
		if !ok {
			return
		}
		T = s.fields[i].typ
		outer = nil
	}
	if d, isPtr := derefBox(T); isPtr {
		T = d
		outer = nil
	}
	vol := check.volatileExprs[ast.Unparen(e.X)]
	for {
		w, ok := Unalias(T).(*Wrapper)
		if !ok || !w.isTransparent() {
			break
		}
		vol = vol || w.kind == WrapVolatile
		T = w.elem
	}
	p, ok := T.Underlying().(*Packed)
	if !ok || index[len(index)-1] >= len(p.fields) {
		return
	}
	f := p.fields[index[len(index)-1]]
	storage := p
	if outer != nil && !outer.fresh {
		storage = outer.storage
	}
	part := packedPart{storage: storage}
	part.plain = (outer == nil || outer.plain) && !vol && plainPackedField(p, f, storage)
	if part.plain && isBigEndianInt(f.obj.typ) && !isIntBacked(storage) {
		part.fresh = true
	}
	if check.packedParts == nil {
		check.packedParts = make(map[ast.Expr]packedPart)
	}
	check.packedParts[e] = part
}

// plainPackedField reports whether field f of the packed type p, whose
// value lies in the storage of the packed type storage at a byte and is
// plain memory, is whole bytes of plain memory.
func plainPackedField(p *Packed, f *PackedField, storage *Packed) bool {
	if f.isBits || f.offset%8 != 0 || holdsBool(f.obj.typ) {
		return false
	}
	FT := f.obj.typ
	if isIntBacked(storage) && (storage.order == ast.BigEndian || isBigEndianInt(FT)) {
		// The storage is an integer: big-endian storage is swapped as a
		// whole, and a big-endian integer field in it is not.
		return false
	}
	if p.order != ast.BigEndian || isIntBacked(p) {
		return true
	}
	// A big-endian type laid out by byte stores a field of several bytes
	// most significant byte first, except an array (of bytes, E2111) and a
	// big-endian integer-backed type, which is stored as it is in memory.
	if _, isArray := FT.Underlying().(*Array); isArray || isBigEndianInt(FT) {
		return true
	}
	return f.width == 8
}

// holdsBool reports whether T is bool or an array of bools: a variable of
// T holds only 0 or 1 in each bool, and packed storage may hold any byte.
func holdsBool(T Type) bool {
	for {
		a, ok := T.Underlying().(*Array)
		if !ok {
			break
		}
		T = a.elem
	}
	return isBoolean(T)
}

// isIntBacked reports whether p is stored in an integer.
func isIntBacked(p *Packed) bool {
	return p.backing != nil && isInteger(p.backing)
}

// isBigEndianInt reports whether T is a packed type with a big-endian
// integer backing.
func isBigEndianInt(T Type) bool {
	p, ok := T.Underlying().(*Packed)
	return ok && p.order == ast.BigEndian && isIntBacked(p)
}

// noteVolatile records that e denotes a variable of a volatile type, whose
// wrapper the operand x is about to lose.
func (check *Checker) noteVolatile(e ast.Expr, x *operand) {
	if x.mode() != variable {
		return
	}
	for T := x.typ(); ; {
		w, ok := T.Underlying().(*Wrapper)
		if !ok || !w.isTransparent() {
			return
		}
		if w.kind == WrapVolatile {
			if check.volatileExprs == nil {
				check.volatileExprs = make(map[ast.Expr]bool)
			}
			check.volatileExprs[e] = true
			return
		}
		T = w.elem
	}
}

// unwrapTransparent returns T without its volatile and aligned wrappers.
func unwrapTransparent(T Type) Type {
	for {
		w, ok := Unalias(T).(*Wrapper)
		if !ok || !w.isTransparent() {
			return T
		}
		T = w.elem
	}
}

// packedPartOf returns the part e denotes when e is a field of a packed
// value, or an element of an array field.
func (check *Checker) packedPartOf(e ast.Expr) (packedPart, bool) {
	switch e := ast.Unparen(e).(type) {
	case *ast.SelectorExpr:
		part, ok := check.packedParts[e]
		return part, ok
	case *ast.IndexExpr:
		// A packed field indexed is an array field. An element of an array
		// field that is plain memory is storage of its own.
		part, ok := check.packedPartOf(e.X)
		if ok && part.plain {
			part = packedPart{plain: true, fresh: true}
		}
		return part, ok
	}
	return packedPart{}, false
}

// packedAddress reports E2112 when e, whose address is taken in the way
// how says, is a packed field (or a part of one) that is not whole bytes
// of plain memory, and reports whether it did.
func (check *Checker) packedAddress(at positioner, e ast.Expr, how string) bool {
	part, ok := check.packedPartOf(e)
	if !ok || part.plain {
		return false
	}
	check.errorf(at, PackedFieldAddress, "cannot %s %s: it is a packed field that is not whole bytes of plain memory", pu.Box(how), e)
	return true
}
