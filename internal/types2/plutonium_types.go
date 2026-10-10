package types2

import (
	"go/constant"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	. "github.com/matjam/plutonium/internal/types2/errors"
)

// ----------------------------------------------------------------------------
// Wrapper types: volatile[T], aligned[T, N], ring[T, N], owned[T],
// shared[T], synced[T].
//
// All of them wrap one element type and, for aligned and ring, an integer.
// They share one Go type so that every type switch in the checker needs only
// one extra case.

// A WrapperKind says which Plutonium type constructor built a Wrapper.
type WrapperKind int

const (
	WrapVolatile WrapperKind = iota // volatile[T]: every access is one load or store, in order
	WrapAligned                     // aligned[T, N]: T placed on an N-byte boundary
	WrapRing                        // ring[T, N]: fixed-capacity ring buffer
	WrapOwned                       // owned[T]: unique owning pointer
	WrapShared                      // shared[T]: reference-counted pointer, one core
	WrapSynced                      // synced[T]: atomically reference-counted pointer
)

var wrapperNames = [...]string{
	WrapVolatile: "volatile",
	WrapAligned:  "aligned",
	WrapRing:     "ring",
	WrapOwned:    "owned",
	WrapShared:   "shared",
	WrapSynced:   "synced",
}

func (k WrapperKind) String() string { return wrapperNames[k] }

// hasCount reports whether the constructor takes an integer argument.
func (k WrapperKind) hasCount() bool { return k == WrapAligned || k == WrapRing }

// A Wrapper is a Plutonium wrapper type.
type Wrapper struct {
	kind WrapperKind
	elem Type
	n    int64 // alignment for aligned, capacity for ring; 0 otherwise

	methods []*Func // ring methods, built on first lookup
}

// RingMethods lists the built-in methods of ring[T, N] in the order
// ringMethod indexes them. The compiler expands calls to them inline.
var RingMethods = [...]string{"Push", "Pop", "Peek", "Len", "Cap", "Full", "Empty", "Reset"}

// ringMethod returns the index and object of a ring method, or nil.
// The methods have pointer receivers, so calling one needs an addressable
// ring or a pointer to one.
func (w *Wrapper) ringMethod(name string, foldCase bool) (int, *Func) {
	if w.methods == nil {
		recv := NewVar(nopos, nil, "r", NewPointer(w))
		elem := func(name string) *Var { return NewParam(nopos, nil, name, w.elem) }
		boolean := func(name string) *Var { return NewParam(nopos, nil, name, Typ[Bool]) }
		integer := NewParam(nopos, nil, "", Typ[Int])
		sig := func(params, results []*Var) *Signature {
			return NewSignatureType(recv, nil, nil, NewTuple(params...), NewTuple(results...), false)
		}
		sigs := map[string]*Signature{
			"Push":  sig([]*Var{elem("v")}, []*Var{boolean("")}),
			"Pop":   sig(nil, []*Var{elem("v"), boolean("ok")}),
			"Peek":  sig(nil, []*Var{elem("v"), boolean("ok")}),
			"Len":   sig(nil, []*Var{integer}),
			"Cap":   sig(nil, []*Var{integer}),
			"Full":  sig(nil, []*Var{boolean("")}),
			"Empty": sig(nil, []*Var{boolean("")}),
			"Reset": sig(nil, nil),
		}
		for _, m := range RingMethods {
			f := NewFunc(nopos, nil, m, sigs[m])
			f.hasPtrRecv_ = true
			w.methods = append(w.methods, f)
		}
	}
	for i, m := range w.methods {
		if m.name == name || foldCase && strings.EqualFold(m.name, name) {
			return i, m
		}
	}
	return -1, nil
}

// NewWrapper returns a wrapper type. n is the alignment or capacity for
// aligned and ring and is ignored for the other kinds.
func NewWrapper(kind WrapperKind, elem Type, n int64) *Wrapper {
	if !kind.hasCount() {
		n = 0
	}
	return &Wrapper{kind: kind, elem: elem, n: n}
}

// Kind returns which constructor built w.
func (w *Wrapper) Kind() WrapperKind { return w.kind }

// Elem returns the wrapped element type.
func (w *Wrapper) Elem() Type { return w.elem }

// N returns the alignment (aligned) or capacity (ring), or 0.
func (w *Wrapper) N() int64 { return w.n }

// IsPointer reports whether w is one of the owning or counted pointer kinds.
func (w *Wrapper) IsPointer() bool {
	return w.kind == WrapOwned || w.kind == WrapShared || w.kind == WrapSynced
}

func (w *Wrapper) Underlying() Type { return w }
func (w *Wrapper) String() string   { return TypeString(w, nil) }

// isTransparent reports whether a value of w is read and written as its
// element type: volatile[T] and aligned[T, N] change how T is stored, not
// what it is.
func (w *Wrapper) isTransparent() bool {
	return w.kind == WrapVolatile || w.kind == WrapAligned
}

// ----------------------------------------------------------------------------
// Packed types

// A Packed is a packed bitfield type:
//
//	packed [le|be] [Backing] { fields }
//
// Fields are laid out LSB-first in declaration order with no padding, except
// where a field's type is aligned[T, N].
type Packed struct {
	order   ast.Endianness
	backing Type // integer or [N]byte backing type; nil when sized from fields
	fields  []*PackedField
	bits    int64 // total storage size in bits (a multiple of 8)
	align   int64 // alignment in bytes
}

// A PackedField is one field of a packed type.
type PackedField struct {
	obj    *Var  // the field object; obj.Type() is the access type
	offset int64 // bit offset from bit 0 of the storage
	width  int64 // width in bits
	isBits bool  // declared as bits[T, N] or as a flag in a bits group
}

// Var returns the field's object. Its type is the type a read produces.
func (f *PackedField) Var() *Var { return f.obj }

// Offset returns the field's bit offset from bit 0 of the packed storage.
func (f *PackedField) Offset() int64 { return f.offset }

// Width returns the field's width in bits.
func (f *PackedField) Width() int64 { return f.width }

// IsBits reports whether the field is a bit field (bits[T, N] or a flag)
// rather than a byte-level field of an ordinary type.
func (f *PackedField) IsBits() bool { return f.isBits }

// Order returns the byte-order annotation.
func (p *Packed) Order() ast.Endianness { return p.order }

// Backing returns the declared backing type, or nil.
func (p *Packed) Backing() Type { return p.backing }

// NumFields returns the number of fields.
func (p *Packed) NumFields() int { return len(p.fields) }

// Field returns the i'th field.
func (p *Packed) Field(i int) *PackedField { return p.fields[i] }

// Bits returns the storage size in bits.
func (p *Packed) Bits() int64 { return p.bits }

func (p *Packed) Underlying() Type { return p }
func (p *Packed) String() string   { return TypeString(p, nil) }

// lookupField returns the index of the named field, or -1.
func (p *Packed) lookupField(name string) int {
	for i, f := range p.fields {
		if f.obj.name == name {
			return i
		}
	}
	return -1
}

// packedType type-checks a packed type literal and computes its layout.
func (check *Checker) packedType(e *ast.PackedType) Type {
	p := &Packed{order: e.Order, align: 1}

	var limit int64 = -1 // bits available, or -1 when sized from the fields
	if e.Backing != nil {
		p.backing = check.varType(e.Backing)
		switch u := p.backing.Underlying().(type) {
		case *Basic:
			if !isInteger(u) {
				check.errorf(e.Backing, InvalidTypeArg, "packed backing type %s must be an integer or byte array", p.backing)
				return Typ[Invalid]
			}
			limit = check.conf.sizeof(p.backing) * 8
			p.align = check.conf.alignof(p.backing)
		case *Array:
			if !isByte(u.elem) {
				check.errorf(e.Backing, InvalidTypeArg, "packed backing array %s must be an array of bytes", p.backing)
				return Typ[Invalid]
			}
			limit = u.len * 8
		case *Slice:
			if !isByte(u.elem) {
				check.errorf(e.Backing, InvalidTypeArg, "packed backing type %s must be []byte", p.backing)
				return Typ[Invalid]
			}
			p.backing = nil // []byte means "size it from the fields"
		default:
			if isValid(p.backing) {
				check.errorf(e.Backing, InvalidTypeArg, "packed backing type %s must be an integer or byte array", p.backing)
			}
			return Typ[Invalid]
		}
	}

	var fset objset
	var offset int64
	add := func(ident *ast.Ident, typ Type, width int64, isBits bool) {
		fld := NewField(ident.Pos(), check.pkg, ident.Name, typ, false)
		if ident.Name == "_" || check.declareInSet(&fset, ident.Pos(), fld) {
			check.recordDef(ident, fld)
			p.fields = append(p.fields, &PackedField{obj: fld, offset: offset, width: width, isBits: isBits})
		}
		offset += width
	}

	for _, f := range e.Fields.List {
		switch ft := f.Type.(type) {
		case *ast.BitsGroup:
			// A flag group is a field whose type is a packed type of one-bit
			// bool flags, placed at the current offset.
			group := &Packed{align: 1}
			var gset objset
			for i, flag := range ft.Names {
				fld := NewField(flag.Pos(), check.pkg, flag.Name, Typ[Bool], false)
				if flag.Name == "_" || check.declareInSet(&gset, flag.Pos(), fld) {
					check.recordDef(flag, fld)
					group.fields = append(group.fields, &PackedField{obj: fld, offset: int64(i), width: 1, isBits: true})
				}
			}
			group.bits = int64(len(ft.Names))
			for _, name := range f.Names {
				add(name, group, group.bits, true)
			}

		default:
			typ, width, isBits := check.packedFieldType(f.Type)
			if width <= 0 {
				continue // error already reported
			}
			if w, ok := typ.(*Wrapper); ok && w.kind == WrapAligned {
				// aligned[T, N] pads to an N-byte boundary and raises the
				// packed type's alignment.
				offset = (offset + w.n*8 - 1) &^ (w.n*8 - 1)
				p.align = max(p.align, w.n)
				typ = w.elem
			}
			for _, name := range f.Names {
				add(name, typ, width, isBits)
			}
		}
	}

	switch {
	case limit >= 0:
		if offset > limit {
			check.errorf(e, InvalidTypeArg, "packed fields need %d bits but the backing type %s holds %d", offset, p.backing, limit)
			return Typ[Invalid]
		}
		p.bits = limit
	default:
		p.bits = (offset + 7) &^ 7
	}
	if p.bits%(p.align*8) != 0 {
		p.bits = (p.bits + p.align*8 - 1) &^ (p.align*8 - 1)
	}
	return p
}

// packedFieldType resolves the type of one packed field and returns the type
// a read produces, the field's width in bits, and whether it is a bit field.
// It returns width 0 after reporting an error.
func (check *Checker) packedFieldType(e ast.Expr) (typ Type, width int64, isBits bool) {
	// bits[T, N] is recognised by name: inside a packed type, bits always
	// means a bit field, whatever an enclosing scope declares.
	if ix, ok := e.(*ast.IndexListExpr); ok {
		if id, ok := ix.X.(*ast.Ident); ok && id.Name == "bits" {
			if len(ix.Indices) != 2 {
				check.error(e, WrongTypeArgCount, "bits takes a type and a width: bits[T, N]")
				return nil, 0, false
			}
			typ = check.varType(ix.Indices[0])
			if !isValid(typ) {
				return nil, 0, false
			}
			n := check.positiveConst(ix.Indices[1], "bits")
			if n <= 0 {
				return nil, 0, false
			}
			switch {
			case isBoolean(typ):
				if n != 1 {
					check.errorf(ix.Indices[1], InvalidTypeArg, "bits[bool, %d]: a bool field is one bit wide", n)
					return nil, 0, false
				}
			case isInteger(typ):
				if max := check.conf.sizeof(typ) * 8; n > max {
					check.errorf(ix.Indices[1], InvalidTypeArg, "bits[%s, %d]: %s holds only %d bits", typ, n, typ, max)
					return nil, 0, false
				}
				if en := EnumOf(typ); en != nil {
					if need := enumBitWidth(en, typ); n < need {
						check.errorf(ix.Indices[1], InvalidTypeArg, "bits[%s, %d]: enum %s needs %d bits", typ, n, typ, need)
						return nil, 0, false
					}
				}
			default:
				check.errorf(ix.Indices[0], InvalidTypeArg, "bit field type %s must be an integer or bool", typ)
				return nil, 0, false
			}
			return typ, n, true
		}
	}

	typ = check.varType(e)
	if !isValid(typ) {
		return nil, 0, false
	}
	if en := EnumOf(typ); en != nil {
		// An enum field takes the bits its values need.
		return typ, enumBitWidth(en, typ), true
	}
	elem := typ
	if w, ok := typ.(*Wrapper); ok && w.kind == WrapAligned {
		elem = w.elem
	}
	if !isPackable(elem) {
		check.errorf(e, InvalidTypeArg, "packed field type %s must be an integer, bool, float, packed type or array of those", elem)
		return nil, 0, false
	}
	if p, ok := elem.Underlying().(*Packed); ok {
		return typ, p.bits, false
	}
	return typ, check.conf.sizeof(elem) * 8, false
}

// packedStorageConvertible reports whether a packed type p converts to or
// from s: an integer of the packed size, or a byte array of its length.
func packedStorageConvertible(check *Checker, p, s Type) bool {
	pt, ok := p.(*Packed)
	if !ok {
		return false
	}
	switch s := s.(type) {
	case *Basic:
		return isInteger(s) && s.info&IsUntyped == 0 && check.conf.sizeof(s)*8 == pt.bits
	case *Array:
		return isByte(s.elem) && s.len*8 == pt.bits
	}
	return false
}

// packedConstConvertible reports whether integer constant x fits the
// storage of packed type T (at most 64 bits).
func packedConstConvertible(x *operand, T Type) bool {
	p, ok := T.Underlying().(*Packed)
	if !ok || p.bits > 64 || !isInteger(x.typ()) {
		return false
	}
	v := constant.ToInt(x.val)
	if v.Kind() != constant.Int || constant.Sign(v) < 0 {
		return false
	}
	u, exact := constant.Uint64Val(v)
	return exact && (p.bits == 64 || u < uint64(1)<<uint(p.bits))
}

// isPackable reports whether a value of T is plain bits that can sit at any
// bit offset inside a packed type.
func isPackable(T Type) bool {
	switch u := T.Underlying().(type) {
	case *Basic:
		return u.info&(IsInteger|IsBoolean|IsFloat) != 0
	case *Packed:
		return true
	case *Array:
		return isPackable(u.elem)
	}
	return false
}

// isByte reports whether T is byte (uint8).
func isByte(T Type) bool {
	b, ok := T.Underlying().(*Basic)
	return ok && b.kind == Uint8
}

// ----------------------------------------------------------------------------
// Predeclared type constructors

// A typeConstructor is the type of the predeclared names volatile, aligned,
// ring, owned, shared and synced. It is not a type a value can have; it only
// marks the TypeName so that an index expression on it builds a Wrapper.
type typeConstructor struct {
	kind WrapperKind
}

func (t *typeConstructor) Underlying() Type { return t }
func (t *typeConstructor) String() string   { return t.kind.String() }

// defPlutoniumTypes declares the Plutonium type constructors in the
// universe. Like Go's predeclared identifiers they may be shadowed.
func defPlutoniumTypes() {
	for k := range wrapperNames {
		kind := WrapperKind(k)
		def(NewTypeName(nopos, nil, kind.String(), &typeConstructor{kind: kind}))
	}
}

// wrapperType builds a Wrapper from an index expression whose operand names
// a type constructor. It returns nil if ix.x does not denote one.
func (check *Checker) wrapperType(ix *indexedExpr) Type {
	id, ok := ix.x.(*ast.Ident)
	if !ok {
		return nil
	}
	_, obj := check.lookupScope(id.Name)
	tn, ok := obj.(*TypeName)
	if !ok {
		return nil
	}
	ctor, ok := tn.typ.(*typeConstructor)
	if !ok {
		return nil
	}
	check.recordUse(id, obj)

	kind := ctor.kind
	want := 1
	if kind.hasCount() {
		want = 2
	}
	if len(ix.indices) != want {
		if want == 1 {
			check.errorf(ix.orig, WrongTypeArgCount, "%s takes one type argument: %s[T]", kind, kind)
		} else {
			check.errorf(ix.orig, WrongTypeArgCount, "%s takes a type and a constant: %s[T, N]", kind, kind)
		}
		return Typ[Invalid]
	}

	elem := check.varType(ix.indices[0])
	if !isValid(elem) {
		return Typ[Invalid]
	}

	var n int64
	if kind.hasCount() {
		n = check.positiveConst(ix.indices[1], kind.String())
		if n <= 0 {
			return Typ[Invalid]
		}
		if n&(n-1) != 0 {
			what := "alignment"
			if kind == WrapRing {
				what = "capacity"
			}
			check.errorf(ix.indices[1], InvalidArrayLen, "%s %s %d is not a power of two", kind, what, n)
			return Typ[Invalid]
		}
	}

	switch kind {
	case WrapVolatile:
		check.later(func() {
			if !isVolatileElem(elem) {
				check.errorf(ix.indices[0], InvalidTypeArg, "volatile element type %s must be an integer, bool or pointer", elem)
			}
		})
	}

	return NewWrapper(kind, elem, n)
}

// isVolatileElem reports whether T can be wrapped in volatile: a scalar
// that one load or store instruction moves.
// A type parameter qualifies if every type in its type set does.
func isVolatileElem(T Type) bool {
	if tp, ok := Unalias(T).(*TypeParam); ok {
		return tp.typeset(func(_, u Type) bool { return u != nil && isVolatileElem(u) })
	}
	switch u := T.Underlying().(type) {
	case *Basic:
		return u.info&(IsInteger|IsBoolean) != 0 || u.kind == UnsafePointer
	case *Pointer:
		return true
	case *Packed:
		return u.backing != nil && isInteger(u.backing)
	}
	return false
}

// positiveConst evaluates e as a constant integer greater than zero and
// returns it, or reports an error and returns 0.
func (check *Checker) positiveConst(e ast.Expr, what string) int64 {
	var x operand
	check.expr(nil, &x, e)
	if x.mode() != constant_ {
		if x.isValid() {
			check.errorf(&x, InvalidArrayLen, "%s argument %s must be a constant", what, &x)
		}
		return 0
	}
	if isUntyped(x.typ()) || isInteger(x.typ()) {
		if val := constant.ToInt(x.val); val.Kind() == constant.Int {
			if v, ok := constant.Int64Val(val); ok && v > 0 {
				return v
			}
		}
	}
	check.errorf(&x, InvalidArrayLen, "%s argument %s must be a positive integer", what, &x)
	return 0
}
