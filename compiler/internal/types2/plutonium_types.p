// Translated by go2pu from internal/types2/plutonium_types.go at the go-cutover tag; edited by hand since.

package types2

import (
	"compiler/compat"
	"compiler/internal/constant"
	"compiler/internal/syntax/ast"
	. "compiler/internal/types2/errors"
	"compiler/pu"
	"pluto/strings"
)

// ----------------------------------------------------------------------------
// Wrapper types: volatile[T], aligned[T, N], ring[T, N], owned[T],
// shared[T], synced[T], weak[P].
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
	WrapWeak                        // weak[P]: non-owning reference to a shared or synced object
)

var wrapperNames = [...]string{
	WrapVolatile: "volatile",
	WrapAligned:  "aligned",
	WrapRing:     "ring",
	WrapOwned:    "owned",
	WrapShared:   "shared",
	WrapSynced:   "synced",
	WrapWeak:     "weak",
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
	builtin []*Func // owned's Drop, weak's Upgrade, built on first lookup
}

// BoxMethods lists the built-in methods of owned, shared, synced and weak,
// which act on the reference rather than on the object it points to. Their
// receiver is the reference, by value. The compiler expands calls to them.
//
//   - owned[T].Drop(): drop the object, then free its memory (section 6.4);
//   - owned[T].Clone() owned[T]: a new box holding a copy of the object, from
//     the current allocator, when T is not an owner (section 5.6);
//   - weak[P].Upgrade() (P, bool): a new strong reference while the object
//     is alive (section 6.5).
var BoxMethods = [...]string{"Drop", "Clone", "Upgrade"}

// builtinMethod returns the index (into BoxMethods) and object of a
// built-in method of a box or weak reference, or nil.
func (w *Wrapper) builtinMethod(name string, foldCase bool) (int, *Func) {
	for i, m := range BoxMethods {
		if name != m && (!foldCase || !strings.EqualFold(name, m)) {
			continue
		}
		switch {
		case w.kind == WrapOwned && m == "Drop", w.kind == WrapWeak && m == "Upgrade":
		case w.kind == WrapOwned && m == "Clone" && !IsOwner(w.elem):
		default:
			return -1, nil
		}
		if w.builtin == nil {
			w.builtin = compat.Make[*Func](len(BoxMethods))
		}
		if w.builtin[i] == nil {
			recv := NewVar(nopos, nil, "r", w)
			var results *Tuple
			switch m {
			case "Upgrade":
				results = NewTuple(compat.Clone([]*Var{NewParam(nopos, nil, "", w.elem), NewParam(nopos, nil, "ok", Typ[Bool])})...)
			case "Clone":
				results = NewTuple(compat.Clone([]*Var{NewParam(nopos, nil, "", w)})...)
			}
			w.builtin[i] = NewFunc(nopos, nil, m, NewSignatureType(recv, nil, nil, nil, results, false))
		}
		return i, w.builtin[i]
	}
	return -1, nil
}

// BoxMethodOf returns the box or weak type a built-in method belongs to, or
// nil if fn is not one.
func BoxMethodOf(fn *Func) *Wrapper {
	sig, _ := fn.typ.(*Signature)
	if sig == nil || sig.recv == nil {
		return nil
	}
	w, ok := sig.recv.typ.(*Wrapper)
	if !ok || (!w.IsPointer() && w.kind != WrapWeak) {
		return nil
	}
	return w
}

// RingMethods lists the built-in methods of ring[T, N] in the order
// ringMethod indexes them. The compiler expands calls to them inline.
//
// All and Values return iterators, closures of the caller's frame built where
// they are called (language spec section 7.17), so they can only be called.
var RingMethods = [...]string{"Push", "Pop", "Peek", "Len", "Cap", "Full", "Empty", "Reset", "All", "Values"}

// IsRingIterator reports whether the ring method name returns an iterator.
func IsRingIterator(name string) bool { return name == "All" || name == "Values" }

// ringMethod returns the index and object of a ring method, or nil.
// The methods have pointer receivers, so calling one needs an addressable
// ring or a pointer to one.
func (w *Wrapper) ringMethod(name string, foldCase bool) (int, *Func) {
	if w.methods == nil {
		recv := NewVar(nopos, nil, "r", NewPointer(w))
		elem := func(name string) *Var { return NewParam(nopos, nil, name, w.elem) }
		// Peek, All and Values yield views (*T) of an owner element
		// (language spec sections 7.17 and 12.10): a copy would be a
		// second holder of what the ring owns.
		viewType := w.elem
		if IsOwner(w.elem) {
			viewType = NewPointer(w.elem)
		}
		view := func(name string) *Var { return NewParam(nopos, nil, name, viewType) }
		boolean := func(name string) *Var { return NewParam(nopos, nil, name, Typ[Bool]) }
		integer := NewParam(nopos, nil, "", Typ[Int])
		sig := func(params, results []*Var) *Signature {
			return NewSignatureType(recv, nil, nil, NewTuple(params...), NewTuple(results...), false)
		}
		// seq is func(yield func(params...) bool), the type of an iter.Seq
		// or iter.Seq2 (which it is assignable to).
		seq := func(yieldParams ...*Var) *Var {
			yield := NewSignatureType(nil, nil, nil, NewTuple(yieldParams...), NewTuple(compat.Clone([]*Var{boolean("")})...), false)
			f := NewSignatureType(nil, nil, nil, NewTuple(compat.Clone([]*Var{NewParam(nopos, nil, "yield", yield)})...), nil, false)
			return NewParam(nopos, nil, "", f)
		}
		sigs := map[string]*Signature{
			"Push":   sig(compat.Clone([]*Var{elem("v")}), compat.Clone([]*Var{boolean("")})),
			"Pop":    sig(nil, compat.Clone([]*Var{elem("v"), boolean("ok")})),
			"Peek":   sig(nil, compat.Clone([]*Var{view("v"), boolean("ok")})),
			"Len":    sig(nil, compat.Clone([]*Var{integer})),
			"Cap":    sig(nil, compat.Clone([]*Var{integer})),
			"Full":   sig(nil, compat.Clone([]*Var{boolean("")})),
			"Empty":  sig(nil, compat.Clone([]*Var{boolean("")})),
			"Reset":  sig(nil, nil),
			"All":    sig(nil, compat.Clone([]*Var{seq(compat.Clone([]*Var{integer, view("")})...)})),
			"Values": sig(nil, compat.Clone([]*Var{seq(compat.Clone([]*Var{view("")})...)})),
		}
		for _, m := range RingMethods {
			f := NewFunc(nopos, nil, m, sigs[m])
			f.hasPtrRecv_ = true
			w.methods = compat.Append(w.methods, f)
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
	return pu.New(Wrapper{kind: kind, elem: elem, n: n})
}

// Kind returns which constructor built w.
func (w *Wrapper) Kind() WrapperKind { return w.kind }

// Elem returns the wrapped element type.
func (w *Wrapper) Elem() Type { return w.elem }

// N returns the alignment (aligned) or capacity (ring), or 0.
func (w *Wrapper) N() int64 { return w.n }

// IsPointer reports whether w is one of the owning or counted pointer kinds:
// owned, shared or synced, which dereference like *T. weak is a reference
// too, but does not dereference.
func (w *Wrapper) IsPointer() bool {
	return w.kind == WrapOwned || w.kind == WrapShared || w.kind == WrapSynced
}

// IsCounted reports whether w is a counted reference: shared, synced or
// weak. Copying one updates a count, and dropping one releases it.
func (w *Wrapper) IsCounted() bool {
	return w.kind == WrapShared || w.kind == WrapSynced || w.kind == WrapWeak
}

// IsBox reports whether w refers to a heap object with a header: owned,
// shared, synced or weak (abi.md section 1).
func (w *Wrapper) IsBox() bool { return w.IsPointer() || w.kind == WrapWeak }

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

	// Plutonium packed unions: the variants, selected by the value of the
	// one field in fields (the tag). Each variant's payload is a packed
	// type with the same storage holding the tag and the variant's fields.
	variants []*Variant
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
	p, limit, ok := check.packedBacking(e.Order, e.Backing)
	if !ok {
		return Typ[Invalid]
	}
	var fset objset
	offset := check.packedFields(p, &fset, e.Fields.List, 0)
	if !check.packedSize(p, e, offset, limit) {
		return Typ[Invalid]
	}
	return p
}

// packedBacking starts a packed type with the given byte order and backing
// type (or none), and returns it with the number of bits the backing holds
// (-1 when the type is sized from its fields). It reports false after an
// error.
func (check *Checker) packedBacking(order ast.Endianness, backing ast.Expr) (p *Packed, limit int64, ok bool) {
	p = pu.New(Packed{order: order, align: 1})
	limit = -1
	if backing == nil {
		return p, limit, true
	}
	p.backing = check.varType(backing)
	switch u := p.backing.Underlying().(type) {
	case *Basic:
		if !isInteger(u) {
			check.errorf(backing, BadPackedBacking, "packed backing type %s must be an integer or byte array", p.backing)
			return nil, 0, false
		}
		limit = check.conf.sizeof(p.backing) * 8
		p.align = check.conf.alignof(p.backing)
	case *Array:
		if !isByte(u.elem) {
			check.errorf(backing, BadPackedBacking, "packed backing array %s must be an array of bytes", p.backing)
			return nil, 0, false
		}
		limit = u.len * 8
	case *Slice:
		if !isByte(u.elem) {
			check.errorf(backing, BadPackedBacking, "packed backing type %s must be []byte", p.backing)
			return nil, 0, false
		}
		p.backing = nil // []byte means "size it from the fields"
	default:
		if isValid(p.backing) {
			check.errorf(backing, BadPackedBacking, "packed backing type %s must be an integer or byte array", p.backing)
		}
		return nil, 0, false
	}
	return p, limit, true
}

// packedSize checks that the fields of p, which end at bit offset, fit its
// backing (limit bits, or -1 to size p from its fields), and sets p's size.
func (check *Checker) packedSize(p *Packed, at positioner, offset, limit int64) bool {
	switch {
	case limit >= 0:
		if offset > limit {
			check.errorf(at, PackedTooLarge, "packed fields need %d bits but the backing type %s holds %d", offset, p.backing, limit)
			return false
		}
		p.bits = limit
	default:
		p.bits = (offset + 7) &^ 7
	}
	if p.bits%(p.align*8) != 0 {
		p.bits = (p.bits + p.align*8 - 1) &^ (p.align*8 - 1)
	}
	return true
}

// packedFields lays out the fields of list in p, starting at bit offset, and
// returns the offset after them. Field names are declared in fset.
func (check *Checker) packedFields(p *Packed, fset *objset, list []*ast.Field, offset int64) int64 {
	add := func(ident *ast.Ident, typ Type, width int64, isBits bool) {
		check.packedBytePlacement(p, ident, offset, width, isBits)
		fld := NewField(ident.Pos(), check.pkg, ident.Name, typ, false)
		if ident.Name == "_" || check.declareInSet(fset, ident.Pos(), fld) {
			check.recordDef(ident, fld)
			p.fields = compat.Append(p.fields, pu.New(PackedField{obj: fld, offset: offset, width: width, isBits: isBits}))
		}
		offset += width
	}

	for _, f := range list {
		switch ft := f.Type.(type) {
		case *ast.BitsGroup:
			// A flag group is a field whose type is a packed type of one-bit
			// bool flags, placed at the current offset.
			group := pu.New(Packed{align: 1})
			var gset objset
			for i, flag := range ft.Names {
				fld := NewField(flag.Pos(), check.pkg, flag.Name, Typ[Bool], false)
				if flag.Name == "_" || check.declareInSet(&gset, flag.Pos(), fld) {
					check.recordDef(flag, fld)
					group.fields = compat.Append(group.fields, pu.New(PackedField{obj: fld, offset: int64(i), width: 1, isBits: true}))
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
	return offset
}

// packedBytePlacement reports E2021 for a field of p at bit offset that a
// big-endian packed type with a byte-array, slice or no backing cannot lay
// out by byte (spec section 7.14, Byte order): a bit field that crosses a
// byte boundary, or a byte-level field that does not start on one. Reserved
// fields (named _) hold no value, so they may be placed anywhere. The flags
// of a bits group are single bits and never cross.
func (check *Checker) packedBytePlacement(p *Packed, ident *ast.Ident, offset, width int64, isBits bool) {
	if p.order != ast.BigEndian || ident.Name == "_" {
		return
	}
	if p.backing != nil {
		if _, ok := p.backing.Underlying().(*Array); !ok {
			return // an integer backing keeps the integer rule
		}
	}
	switch {
	case isBits && width > 1 && offset/8 != (offset+width-1)/8:
		check.errorf(ident, BadPackedBytePlacement, "bit field %s (bits %d to %d) crosses a byte boundary in a big-endian packed type", pu.Box(ident.Name), offset, offset+width-1)
	case !isBits && offset%8 != 0:
		check.errorf(ident, BadPackedBytePlacement, "field %s starts at bit %d, which is not a byte boundary, in a big-endian packed type", pu.Box(ident.Name), offset)
	}
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
				check.error(e, BadBitField, "bits takes a type and a width: bits[T, N]")
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
					check.errorf(ix.Indices[1], BadBitField, "bits[bool, %d]: a bool field is one bit wide", n)
					return nil, 0, false
				}
			case isInteger(typ):
				if max := check.conf.sizeof(typ) * 8; n > max {
					check.errorf(ix.Indices[1], BadBitField, "bits[%s, %d]: %s holds only %d bits", typ, n, typ, max)
					return nil, 0, false
				}
				if en := EnumOf(typ); en != nil {
					if need := enumBitWidth(en, typ); n < need {
						check.errorf(ix.Indices[1], BadBitField, "bits[%s, %d]: enum %s needs %d bits", typ, n, typ, need)
						return nil, 0, false
					}
				}
			default:
				check.errorf(ix.Indices[0], BadBitField, "bit field type %s must be an integer or bool", typ)
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
		check.errorf(e, BadPackedField, "packed field type %s must be an integer, bool, float, packed type or array of those", elem)
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
		def(NewTypeName(nopos, nil, kind.String(), pu.New(typeConstructor{kind: kind})))
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
			check.errorf(ix.orig, WrongWrapperArgs, "%s takes one type argument: %s[T]", kind, kind)
		} else {
			check.errorf(ix.orig, WrongWrapperArgs, "%s takes a type and a constant: %s[T, N]", kind, kind)
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
			check.errorf(ix.indices[1], NotPowerOfTwo, "%s %s %d is not a power of two", kind, pu.Box(what), n)
			return Typ[Invalid]
		}
	}

	switch kind {
	case WrapVolatile:
		check.later(pu.New(goClosure_wrapperType_1{elem: elem, check: check, ix: ix}))
	case WrapOwned, WrapShared, WrapSynced:
		check.later(pu.New(goClosure_wrapperType_2{check: check, ix: ix, kind: kind, elem: elem}))
	case WrapWeak:
		w, ok := Unalias(elem).(*Wrapper)
		if !ok || (w.kind != WrapShared && w.kind != WrapSynced) {
			e := check.sprintf("%s", elem)
			check.explainf(ix.indices[0], BadWeakElem, pu.New(Explanation{
				Text: "A weak reference refers to an object that counted references keep alive, without keeping it alive itself. Only shared[T] and synced[T] count their references.",
				Fixes: compat.Clone([]ExplainFix{{
					Intro: "Name the counted reference type the weak reference comes from",
					Code:  compat.Concat("weak[shared[", e, "]]"),
				}}),
			}), "weak takes shared[T] or synced[T], not %s", pu.Box(e))
			return Typ[Invalid]
		}
	}

	return NewWrapper(kind, elem, n)
}

// boxElem reports an interface type as the element of owned, shared or
// synced (language spec section 7.19): an owning interface owns a value
// behind an interface.
func (check *Checker) boxElem(at positioner, kind WrapperKind, elem Type) {
	if _, isTP := Unalias(elem).(*TypeParam); isTP || !IsInterface(elem) {
		return
	}
	e := check.sprintf("%s", elem)
	check.explainf(at, BoxOfInterface, pu.New(Explanation{
		Text: compat.Concat(kind.String(), "[T] holds one object of type T on the heap. An interface already refers to the value it holds, so a box around one would be a second, slower way to own a value behind an interface."),
		Fixes: compat.Clone([]ExplainFix{{
			Intro: "To own whatever is converted into an interface, declare an owning interface",
			Code:  "type Resource owner interface {\n\t...\n}",
		}, {
			Intro: compat.Concat("To put the value itself in a box, use its own type: ", kind.String(), "[File] rather than ", kind.String(), "[", e, "]."),
		}}),
	}), "%s cannot hold an interface type (%s): use an owning interface", kind, pu.Box(e))
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
	x := pu.Alloc[operand]()
	check.expr(nil, x, e)
	if x.mode() != constant_ {
		if x.isValid() {
			check.errorf(x, BadWrapperSize, "%s argument %s must be a constant", pu.Box(what), x)
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
	check.errorf(x, BadWrapperSize, "%s argument %s must be a positive integer", pu.Box(what), x)
	return 0
}

// goClosure_wrapperType_1 is the function literal at plutonium_types.go:636, converted to a struct
// that holds the variables it captures (go2pu closure conversion).
type goClosure_wrapperType_1 struct {
	elem  Type
	check *Checker
	ix    *indexedExpr
}

func (goSelf *goClosure_wrapperType_1) Call() {
	if !isVolatileElem(goSelf.elem) {
		goSelf.check.errorf(goSelf.ix.indices[0], BadVolatileElem, "volatile element type %s must be an integer, bool or pointer", goSelf.elem)
	}
}

// goClosure_wrapperType_2 is the function literal at plutonium_types.go:642, converted to a struct
// that holds the variables it captures (go2pu closure conversion).
type goClosure_wrapperType_2 struct {
	check *Checker
	ix    *indexedExpr
	kind  WrapperKind
	elem  Type
}

func (goSelf *goClosure_wrapperType_2) Call() {
	goSelf.check.boxElem(goSelf.ix.indices[0], goSelf.kind, goSelf.elem)
}
