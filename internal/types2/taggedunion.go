package types2

// Plutonium union types (tagged unions):
//
//	type DeviceState union {
//		Off
//		Probing struct{ attempt int }
//		Failed  syscall.Errno
//	}
//
// A union value holds exactly one of its variants and that variant's
// payload, stored inline after a tag. Variants are named once, in the
// declaration, and reached through the union's type: DeviceState.Off is a
// value of type DeviceState, and DeviceState.Failed(e) and
// DeviceState.Probing{attempt: 1} construct a variant from its payload. A
// type switch on a union value selects the variant and binds its payload;
// a switch without a default case must name every variant. Bare variant
// names are accepted in the cases of such a switch, where the variant wins
// over any other declaration of the name, as for enum members.
//
// A packed union, packed union uint64 { tag; Name case v { fields } }, is a
// packed type whose single tag field selects the variant. Its underlying
// type is a *Packed holding the tag field and recording the variants; each
// variant's payload is a packed type with the same storage that holds the
// tag and the variant's own fields.
//
// The (*TaggedUnion) type is named apart from (*Union), which is Go's
// union of terms in a type constraint.

import (
	"go/constant"
	"slices"
	"strconv"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	. "github.com/matjam/plutonium/internal/types2/errors"
)

// A TaggedUnion is the underlying type of a union type declaration.
type TaggedUnion struct {
	variants []*Variant // in declaration order; Variant(i).Index() == i
}

// NumVariants returns the number of variants.
func (u *TaggedUnion) NumVariants() int { return len(u.variants) }

// Variant returns the i'th variant in declaration order.
func (u *TaggedUnion) Variant(i int) *Variant { return u.variants[i] }

// TagSize returns the size in bytes of the tag: 1 for up to 256 variants,
// otherwise 2.
func (u *TaggedUnion) TagSize() int64 {
	if len(u.variants) <= 256 {
		return 1
	}
	return 2
}

func (u *TaggedUnion) Underlying() Type { return u }
func (u *TaggedUnion) String() string   { return TypeString(u, nil) }

// A Variant is one variant of a union or packed union.
type Variant struct {
	object         // typ is the payload type: struct{} for a variant without one
	index  int     // position in the declaration; the tag of an ordinary union
	bare   bool    // declared without a payload
	cases  []caseValue
}

// A caseValue is a tag value that selects a variant of a packed union.
type caseValue struct {
	val  constant.Value // as written, converted to the tag field's type
	bits uint64         // the tag field's bits
}

// Index returns the variant's position in its union's declaration, which is
// the tag value of an ordinary union.
func (v *Variant) Index() int { return v.index }

// HasPayload reports whether the variant was declared with a payload. A
// variant without one has the payload type struct{}.
func (v *Variant) HasPayload() bool { return !v.bare }

// Payload returns the variant's payload type.
func (v *Variant) Payload() Type { return v.typ }

// TagBits returns the tag field bits that select a variant of a packed
// union, in the order the case clause lists them.
func (v *Variant) TagBits() []uint64 {
	out := make([]uint64, len(v.cases))
	for i, c := range v.cases {
		out[i] = c.bits
	}
	return out
}

// TagValues returns the tag field values that select a variant of a packed
// union, as constants of the tag field's type.
func (v *Variant) TagValues() []constant.Value {
	out := make([]constant.Value, len(v.cases))
	for i, c := range v.cases {
		out[i] = c.val
	}
	return out
}

func (v *Variant) String() string { return ObjectString(v, nil) }

// UnionVariants returns the variants of T if its underlying type is a union
// or a packed union, and nil otherwise.
func UnionVariants(T Type) []*Variant {
	switch u := T.Underlying().(type) {
	case *TaggedUnion:
		return u.variants
	case *Packed:
		return u.variants
	}
	return nil
}

// IsUnion reports whether T's underlying type is a packed union.
func (p *Packed) IsUnion() bool { return p.variants != nil }

// Variants returns the variants of a packed union, or nil.
func (p *Packed) Variants() []*Variant { return p.variants }

func unionVariants(T Type) []*Variant { return UnionVariants(T) }

// isUnion reports whether T is a union or a packed union.
func isUnion(T Type) bool { return unionVariants(T) != nil }

func lookupVariant(vs []*Variant, name string) *Variant {
	for _, v := range vs {
		if v.name == name {
			return v
		}
	}
	return nil
}

// variantNames lists the names of vs for messages: "Off, Probing and Ready".
func variantNames(vs []*Variant) string {
	names := make([]string, len(vs))
	for i, v := range vs {
		names[i] = v.name
	}
	return joinAnd(names)
}

// joinAnd joins names as "a", "a and b" or "a, b and c".
func joinAnd(names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// UnionLayout returns the size of a union's tag and the offset of its
// payload, as laid out by s: the tag first, then the payload at the first
// offset its alignment allows.
func UnionLayout(s Sizes, t *TaggedUnion) (tagSize, payloadOffset int64) {
	tag, off, _, _ := unionLayout(s, t)
	return tag, off
}

// unionLayout returns the tag size, payload offset, size and alignment of a
// union laid out by s. The size is negative if a payload is too large.
func unionLayout(s Sizes, t *TaggedUnion) (tag, off, size, al int64) {
	tag = t.TagSize()
	var psize, palign int64 = 0, 1
	for _, v := range t.variants {
		sz := s.Sizeof(v.typ)
		if sz < 0 {
			return tag, tag, -1, tag
		}
		psize = max(psize, sz)
		palign = max(palign, s.Alignof(v.typ))
	}
	off = align(tag, palign)
	al = max(tag, palign)
	return tag, off, align(off+psize, al), al
}

// definedNamed returns the Named type that def defines, or nil if def is
// not the name of a type definition (an alias, or no declaration at all).
func definedNamed(def *TypeName) *Named {
	if def == nil {
		return nil
	}
	named, _ := def.typ.(*Named)
	if named == nil || named.obj != def {
		return nil
	}
	return named
}

// unionType checks a union type literal, which must be the type of the
// type definition of def.
func (check *Checker) unionType(e *ast.UnionType, def *TypeName) Type {
	named := definedNamed(def)
	if named == nil {
		what := "union"
		if e.Packed.IsValid() {
			what = "packed union"
		}
		check.errorf(e, BadDecl, "%s type must be the type of a type definition: type U %s { ... }"+
			"\n    A union's variants are reached through the union's name (U.V), so a union"+
			"\n    without a name of its own would have no way to name them.", what, what)
		return Typ[Invalid]
	}
	if e.Packed.IsValid() {
		if named.tparams != nil {
			check.errorf(e, MisplacedTypeParam, "packed union %s cannot have type parameters: its layout must be fixed bit by bit", def.name)
			return Typ[Invalid]
		}
		return check.packedUnionType(e, def)
	}

	u := &TaggedUnion{}
	var names objset
	add := func(id *ast.Ident, payload Type, bare bool) {
		if id.Name == "_" {
			check.error(id, InvalidBlank, "union variant cannot be _: every variant needs a name")
			return
		}
		v := &Variant{object: object{pos: id.Pos(), pkg: check.pkg, name: id.Name, typ: payload}, index: len(u.variants), bare: bare}
		if check.declareInSet(&names, id.Pos(), v) {
			check.recordDef(id, v)
			u.variants = append(u.variants, v)
		}
	}
	for _, f := range e.Fields.List {
		if f.Tag != nil {
			check.errorf(f.Tag, BadDecl, "union variant cannot have a tag: tags belong to struct fields")
		}
		if len(f.Names) == 0 {
			id, _ := f.Type.(*ast.Ident)
			if id == nil {
				check.errorf(f.Type, BadDecl, "union variant %s needs a name"+
					"\n    Each variant of a union has a name, followed by its payload type if it"+
					"\n    has one:"+
					"\n        Name %s", f.Type, f.Type)
				continue
			}
			add(id, NewStruct(nil, nil), true)
			continue
		}
		payload := check.varType(f.Type)
		for _, id := range f.Names {
			add(id, payload, false)
		}
	}

	switch n := len(u.variants); {
	case n == 0 && len(e.Fields.List) == 0:
		check.errorf(e, BadDecl, "union %s has no variants"+
			"\n    A union value holds one of its variants, so a union needs at least one."+
			"\n    Variants are written like struct fields: a name, and a payload type if"+
			"\n    the variant carries data:"+
			"\n        type %s union {"+
			"\n            None"+
			"\n            Some int"+
			"\n        }", def.name, def.name)
		return Typ[Invalid]
	case n == 0:
		return Typ[Invalid] // errors reported above
	case n > 1<<16:
		check.errorf(e, BadDecl, "union %s has %d variants; its 16-bit tag numbers at most 65536", def.name, n)
		return Typ[Invalid]
	}

	check.later(func() {
		for _, v := range u.variants {
			check.checkOwnerPayload(atPos(v.pos), def.name, v)
		}
	}).describef(e, "union payloads of %s", def.name)
	return u
}

// ownerIn returns the owned, shared or synced pointer type that a value of
// T holds directly (in T itself, a field, an array element or a variant
// payload, but not behind a pointer), or nil.
func ownerIn(T Type, seen map[Type]bool) Type {
	if seen[T] {
		return nil
	}
	seen[T] = true
	switch u := T.Underlying().(type) {
	case *Wrapper:
		if u.IsPointer() {
			return T
		}
		return ownerIn(u.elem, seen)
	case *Struct:
		for _, f := range u.fields {
			if t := ownerIn(f.typ, seen); t != nil {
				return t
			}
		}
	case *Array:
		return ownerIn(u.elem, seen)
	case *TaggedUnion:
		for _, v := range u.variants {
			if t := ownerIn(v.typ, seen); t != nil {
				return t
			}
		}
	}
	return nil
}

// checkOwnerPayload reports a variant whose payload holds an owned, shared
// or synced pointer. Such a union must drop the payload of the variant it
// holds, which needs the ownership checker (language spec section 6.4).
func (check *Checker) checkOwnerPayload(at positioner, union string, v *Variant) bool {
	t := ownerIn(v.typ, map[Type]bool{})
	if t == nil {
		return true
	}
	what := "the pointer"
	if !Identical(t, v.typ) {
		what = check.sprintf("its %s", t)
	}
	check.errorf(at, UnsupportedFeature, "union %s cannot have variant %s with payload %s yet: it holds %s"+
		"\n    %s owns (or counts references to) what it points to. A union holding one"+
		"\n    must drop the payload of the variant it holds when the union is dropped or"+
		"\n    given a new variant, so it must be declared owner (language spec section"+
		"\n    6.4). Ownership is not implemented yet, and until it is a union payload may"+
		"\n    not hold owned, shared or synced pointers."+
		"\n    Hold a plain pointer and free it explicitly instead:"+
		"\n        %s *T",
		union, v.name, v.typ, t, what, v.name)
	return false
}

// checkUnionInstance reports a union instance whose type arguments put an
// owned, shared or synced pointer in a payload (see checkOwnerPayload).
func (check *Checker) checkUnionInstance(at positioner, inst Type) {
	u, _ := inst.Underlying().(*TaggedUnion)
	if u == nil {
		return
	}
	for _, v := range u.variants {
		if !check.checkOwnerPayload(at, check.sprintf("%s", inst), v) {
			return
		}
	}
}

// ----------------------------------------------------------------------------
// Packed unions

// packedUnionType checks "packed union [le|be] Backing { tag; variants }".
// The result is the union's tag as a packed type, with the variants
// recorded on it.
func (check *Checker) packedUnionType(e *ast.UnionType, def *TypeName) Type {
	if e.Backing == nil {
		check.errorf(e, InvalidTypeArg, "packed union %s needs a backing type: packed union uint64 { ... }"+
			"\n    A packed union is a bit-exact view of a hardware word or record, so its"+
			"\n    size is that of its backing integer or byte array.", def.name)
		return Typ[Invalid]
	}
	var tagFields, variantFields []*ast.Field
	for _, f := range e.Fields.List {
		if _, ok := f.Type.(*ast.VariantCase); ok {
			variantFields = append(variantFields, f)
			continue
		}
		if len(variantFields) > 0 {
			check.errorf(f, BadDecl, "tag field %s of packed union %s must come before the first variant", f.Names[0].Name, def.name)
			continue
		}
		tagFields = append(tagFields, f)
	}

	p, limit, ok := check.packedBacking(e.Order, e.Backing)
	if !ok {
		return Typ[Invalid]
	}
	var tagNames objset
	tagEnd := check.packedFields(p, &tagNames, tagFields, 0)
	if !check.packedSize(p, e, tagEnd, limit) {
		return Typ[Invalid]
	}
	if len(p.fields) != 1 || p.fields[0].obj.name == "_" {
		check.errorf(e, BadDecl, "packed union %s needs exactly one named tag field before its variants (it has %d)"+
			"\n    The field before the first variant is the tag: it selects the variant and"+
			"\n    occupies the same bits in every variant. Each variant lists the tag values"+
			"\n    that select it:"+
			"\n        type PTE packed union uint64 {"+
			"\n            present bits[bool, 1]"+
			"\n            Swapped case false { slot bits[uint64, 63] }"+
			"\n            Mapped  case true { frame bits[uint64, 40] }"+
			"\n        }", def.name, len(p.fields))
		return Typ[Invalid]
	}
	tag := p.fields[0]
	if tt := tag.obj.typ; !isBoolean(tt) && !isInteger(tt) {
		check.errorf(atPos(tag.obj.pos), InvalidTypeArg, "the tag %s of packed union %s must be a bool, integer or enum field, not %s", tag.obj.name, def.name, tt)
		return Typ[Invalid]
	}
	if len(variantFields) == 0 {
		check.errorf(e, BadDecl, "packed union %s has no variants: list them after the tag, as Name case value { fields }", def.name)
		return Typ[Invalid]
	}

	var names objset
	seen := map[uint64]*Variant{}
	for _, f := range variantFields {
		vc := f.Type.(*ast.VariantCase)
		if len(f.Names) != 1 {
			check.errorf(f, BadDecl, "each variant of packed union %s needs its own case clause", def.name)
			continue
		}
		id := f.Names[0]
		if id.Name == "_" {
			check.error(id, InvalidBlank, "union variant cannot be _: every variant needs a name")
			continue
		}

		// The payload: the tag, then the variant's fields.
		payload := &Packed{order: p.order, backing: p.backing, align: p.align, fields: []*PackedField{tag}}
		fset := objset{}
		fset.insert(tag.obj)
		end := check.packedFields(payload, &fset, vc.Fields.List, tagEnd)
		if !check.packedSize(payload, vc, end, limit) {
			continue
		}

		v := &Variant{object: object{pos: id.Pos(), pkg: check.pkg, name: id.Name, typ: payload}, index: len(p.variants)}
		if !check.declareInSet(&names, id.Pos(), v) {
			continue
		}
		check.recordDef(id, v)
		for _, ve := range vc.Values {
			c, ok := check.tagValue(ve, tag, v.name)
			if !ok {
				continue
			}
			if other := seen[c.bits]; other != nil {
				check.errorf(ve, DuplicateCase, "variants %s and %s of packed union %s both have tag value %s", other.name, v.name, def.name, c.val)
				continue
			}
			seen[c.bits] = v
			v.cases = append(v.cases, c)
		}
		if len(v.cases) == 0 {
			continue // errors reported above
		}
		p.variants = append(p.variants, v)
	}
	if len(p.variants) != len(variantFields) {
		return Typ[Invalid]
	}
	return p
}

// tagValue evaluates a case value of a packed union variant, which must be a
// constant of the tag field's type that fits its width.
func (check *Checker) tagValue(e ast.Expr, tag *PackedField, variant string) (caseValue, bool) {
	var x operand
	check.expr(nil, &x, e)
	if !x.isValid() {
		return caseValue{}, false
	}
	check.assignment(&x, tag.obj.typ, "packed union case")
	if !x.isValid() {
		return caseValue{}, false
	}
	if x.mode() != constant_ {
		check.errorf(&x, InvalidConstVal, "case value %s of variant %s is not constant: the variants of a packed union are selected by constant tag values", &x, variant)
		return caseValue{}, false
	}
	if x.val.Kind() == constant.Bool {
		var bits uint64
		if constant.BoolVal(x.val) {
			bits = 1
		}
		return caseValue{x.val, bits}, true
	}
	v := constant.ToInt(x.val)
	w := uint(tag.width)
	var lo, hi constant.Value // inclusive range of the field
	one := constant.MakeInt64(1)
	if isUnsigned(tag.obj.typ) {
		lo = constant.MakeInt64(0)
		hi = constBinaryOp(constShift(one, token.SHL, w), token.SUB, one)
	} else {
		hi = constShift(one, token.SHL, w-1)
		lo = constUnaryOp(token.SUB, hi, 0)
		hi = constBinaryOp(hi, token.SUB, one)
	}
	if constCompare(v, token.LSS, lo) || constCompare(v, token.GTR, hi) {
		check.errorf(&x, NumericOverflow, "case value %s of variant %s does not fit the %d-bit tag %s", x.val, variant, w, tag.obj.name)
		return caseValue{}, false
	}
	i, _ := constant.Int64Val(v)
	if u, ok := constant.Uint64Val(v); ok {
		i = int64(u)
	}
	mask := uint64(1)<<w - 1
	if w == 64 {
		mask = ^uint64(0)
	}
	return caseValue{x.val, uint64(i) & mask}, true
}

// ----------------------------------------------------------------------------
// Selecting variants

// denotesTypeExpr reports whether e is the syntax of a named type: an
// identifier or qualified identifier naming a type, possibly instantiated
// (U[int]). It reports no errors and records nothing.
func (check *Checker) denotesTypeExpr(e ast.Expr) bool {
	switch e := ast.Unparen(e).(type) {
	case *ast.IndexExpr:
		return check.denotesTypeExpr(e.X)
	case *ast.IndexListExpr:
		return check.denotesTypeExpr(e.X)
	}
	return check.denotesType(e)
}

// unionSelector handles U.V, where x is the type expression U. It reports
// whether e selects a variant, with *x holding the result: a value of type
// U for a variant without a payload, and otherwise a function from the
// payload to U, which constructs the variant when called.
func (check *Checker) unionSelector(x *operand, e *ast.SelectorExpr) bool {
	v := lookupVariant(unionVariants(x.typ()), e.Sel.Name)
	if v == nil {
		return false
	}
	if !check.variantVisible(e, v, x.typ()) {
		x.invalidate()
		return true
	}
	check.recordUse(e.Sel, v)
	U := x.typ()
	x.mode_ = value
	x.expr = e
	if v.bare {
		x.typ_ = U
		return true
	}
	param := NewParam(v.pos, v.pkg, "", v.typ)
	result := NewParam(v.pos, v.pkg, "", U)
	x.typ_ = NewSignatureType(nil, nil, nil, NewTuple(param), NewTuple(result), false)
	return true
}

// variantVisible reports whether v, selected by e, may be used here: an
// unexported variant is visible only in its own package.
func (check *Checker) variantVisible(e ast.Expr, v *Variant, U Type) bool {
	if v.Exported() || v.pkg == check.pkg {
		return true
	}
	check.errorf(e, UnexportedName, "%s undefined (cannot refer to unexported variant %s of union %s)", e, v.name, U)
	return false
}

// variantHint explains a selector x.sel on a union value that names a
// variant rather than a field or method, or returns "". (On the union's
// type, unionSelector handles x.sel.)
func (check *Checker) variantHint(x *operand, sel string) string {
	if lookupVariant(unionVariants(x.typ()), sel) == nil {
		return ""
	}
	return check.sprintf("%s is a variant of union %s, not a field: read it in a type switch on %s (case %s:), or construct one with %s.%s", sel, x.typ(), x.expr, sel, x.typ(), sel)
}

// variantNotAType reports U.V used as a type.
func (check *Checker) variantNotAType(x *operand, e *ast.SelectorExpr) bool {
	v := lookupVariant(unionVariants(x.typ()), e.Sel.Name)
	if v == nil {
		return false
	}
	check.errorf(e.Sel, NotAType, "%s is a variant of union %s, not a type"+
		"\n    A variant is constructed with %s(payload) or %s{...} and read in a"+
		"\n    type switch, whose case %s binds its payload, of type %s.",
		ast.Expr(e), x.typ(), ast.Expr(e), ast.Expr(e), e.Sel.Name, v.typ)
	return true
}

// variantLiteralType checks the type of a composite literal that names a
// variant, U.V{...}. It reports whether e names one; if so it returns the
// union type and the variant (or nil after an error).
func (check *Checker) variantLiteralType(e ast.Expr) (U Type, v *Variant, ok bool) {
	sel, _ := e.(*ast.SelectorExpr)
	if sel == nil || !check.denotesTypeExpr(sel.X) {
		return nil, nil, false
	}
	U = check.typ(sel.X)
	if !isValid(U) {
		return U, nil, true
	}
	vs := unionVariants(U)
	if vs == nil {
		check.errorf(sel.Sel, NotAType, "%s is not a type (%s is not a union, so it has no variants)", ast.Expr(sel), U)
		return U, nil, true
	}
	v = lookupVariant(vs, sel.Sel.Name)
	if v == nil {
		check.errorf(sel.Sel, MissingFieldOrMethod, "union %s has no variant %s (its variants are %s)", U, sel.Sel.Name, variantNames(vs))
		return U, nil, true
	}
	if !check.variantVisible(sel, v, U) {
		return U, nil, true
	}
	check.recordUse(sel.Sel, v)
	if v.bare {
		check.errorf(e, InvalidLit, "variant %s of union %s has no payload: write %s, not %s{}", v.name, U, ast.Expr(sel), ast.Expr(sel))
		return U, nil, true
	}
	return U, v, true
}

// variantRef resolves e, which must name a variant of the union type U: a
// case of a type switch on a U value (bare names allowed), or the type of a
// type assertion. It returns nil after reporting an error.
func (check *Checker) variantRef(e ast.Expr, U Type, bare bool) *Variant {
	vs := unionVariants(U)
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		if v := lookupVariant(vs, x.Name); v != nil && (v.Exported() || v.pkg == check.pkg) {
			if bare {
				check.recordUse(x, v)
				return v
			}
			check.errorf(e, InvalidAssert, "write %s.%s: a variant's bare name is accepted only in the cases of a type switch", U, x.Name)
			return nil
		}
	case *ast.SelectorExpr:
		if check.denotesTypeExpr(x.X) {
			T := check.typ(x.X)
			if !isValid(T) {
				return nil
			}
			tvs := unionVariants(T)
			v := lookupVariant(tvs, x.Sel.Name)
			switch {
			case tvs == nil:
				check.errorf(x.X, InvalidTypeSwitch, "%s is not a union, so %s is not a variant of %s", T, e, U)
				return nil
			case v == nil:
				check.errorf(x.Sel, MissingFieldOrMethod, "union %s has no variant %s (its variants are %s)", T, x.Sel.Name, variantNames(tvs))
				return nil
			case !check.variantVisible(x, v, T):
				return nil
			}
			check.recordUse(x.Sel, v)
			if !Identical(T, U) {
				check.errorf(e, InvalidTypeSwitch, "%s is a variant of %s, not of %s", e, T, U)
				return nil
			}
			return v
		}
	}
	check.errorf(e, InvalidTypeSwitch, "%s is not a variant of union %s"+
		"\n    A type switch on a union value, and a type assertion on one, name the"+
		"\n    union's variants: %s.", e, U, variantNames(vs))
	return nil
}

// ----------------------------------------------------------------------------
// Type switches and assertions

// unionTypeSwitch checks a type switch whose guard x has a union type. Each
// case names variants; with one variant the clause's variable has the
// variant's payload type, otherwise the union's type. Without a default
// case, the cases must name every variant of an ordinary union and cover
// every tag value of a packed union.
func (check *Checker) unionTypeSwitch(inner stmtContext, s *ast.TypeSwitchStmt, lhs *ast.Ident, x *operand) {
	check.multipleDefaults(s.Body.List)
	vs := unionVariants(x.typ())
	U := x.typ()

	var lhsVars []*Var
	seen := map[int]ast.Expr{} // variant index -> case naming it
	hasDefault := false
	for _, c := range s.Body.List {
		clause, _ := c.(*ast.CaseClause)
		if clause == nil {
			check.error(c, InvalidSyntaxTree, "incorrect type switch case")
			continue
		}
		if clause.List == nil {
			hasDefault = true
		}
		T := U
		for _, e := range clause.List {
			if check.isNil(e) {
				check.errorf(e, InvalidTypeSwitch, "a union value is never nil: the zero value of %s is its first variant, %s", U, vs[0].name)
				continue
			}
			v := check.variantRef(e, U, true)
			if v == nil {
				continue
			}
			if prev := seen[v.index]; prev != nil {
				err := check.newError(DuplicateCase)
				err.addf(e, "duplicate case %s in type switch", e)
				err.addf(prev, "previous case")
				err.report()
				continue
			}
			seen[v.index] = e
			if len(clause.List) == 1 {
				T = v.typ
			}
		}
		check.openScope(clause, "case")
		if lhs != nil {
			obj := newVar(LocalVar, lhs.Pos(), check.pkg, lhs.Name, T)
			check.declare(check.scope, nil, obj, clause.Colon)
			check.recordImplicit(clause, obj)
			lhsVars = append(lhsVars, obj)
		}
		check.stmtList(inner, clause.Body)
		check.closeScope()
	}

	if lhs != nil {
		used := false
		for _, v := range lhsVars {
			if check.usedVars[v] {
				used = true
			}
			check.usedVars[v] = true
		}
		if !used {
			check.softErrorf(lhs, UnusedVar, "%s declared and not used", lhs.Name)
		}
	}

	if hasDefault {
		return
	}
	if p, ok := U.Underlying().(*Packed); ok {
		check.packedSwitchCoverage(s, x, p, seen)
		return
	}
	var missing []string
	for _, v := range vs {
		if seen[v.index] == nil {
			missing = append(missing, v.name)
		}
	}
	if len(missing) == 0 {
		check.markExhaustive(s)
		return
	}
	what := "case"
	if len(missing) > 1 {
		what = "cases"
	}
	check.errorf(atPos(s.Switch), InvalidTypeSwitch, "missing %s in type switch on %s (%s): %s"+
		"\n    A type switch on a union must handle every variant, so that a variant"+
		"\n    added later cannot be missed silently. Add a case for %s, or a default"+
		"\n    case for the variants this switch does not need to tell apart.",
		what, x.expr, U, joinAnd(missing), joinAnd(missing))
}

// markExhaustive records a type switch that handles every variant of its
// union without a default case.
func (check *Checker) markExhaustive(s *ast.TypeSwitchStmt) {
	if check.exhaustive == nil {
		check.exhaustive = map[*ast.TypeSwitchStmt]bool{}
	}
	check.exhaustive[s] = true
}

// packedSwitchCoverage checks that a type switch without a default case on
// a packed union covers every value its tag field can hold.
func (check *Checker) packedSwitchCoverage(s *ast.TypeSwitchStmt, x *operand, p *Packed, seen map[int]ast.Expr) {
	tag := p.fields[0]
	covered := map[uint64]bool{}
	for _, v := range p.variants {
		if seen[v.index] != nil {
			for _, c := range v.cases {
				covered[c.bits] = true
			}
		}
	}
	if tag.width < 64 && uint64(len(covered)) == uint64(1)<<uint(tag.width) {
		check.markExhaustive(s)
		return
	}
	values := "2^" + strconv.FormatInt(tag.width, 10)
	if tag.width <= 16 {
		values = strconv.FormatInt(int64(1)<<uint(tag.width), 10)
	}
	check.errorf(atPos(s.Switch), InvalidTypeSwitch, "type switch on packed union %s (%s) needs a default case"+
		"\n    Its tag %s is %d bits wide and can hold %s values, and the variants named"+
		"\n    here are selected by %d of them. The bits come from hardware or from a"+
		"\n    raw conversion, so they may hold any value: say what happens for the others.",
		x.expr, x.typ(), tag.obj.name, tag.width, values, len(covered))
}

// unionAssertion checks u.(U.V) on a union value x and sets x to the
// variant's payload, in comma-ok mode.
func (check *Checker) unionAssertion(x *operand, e *ast.TypeAssertExpr) bool {
	v := check.variantRef(e.Type, x.typ(), false)
	if v == nil {
		return false
	}
	x.mode_ = commaok
	x.typ_ = v.typ
	return true
}

// unionLiteralError reports a composite literal of a union type itself,
// which must name a variant instead.
func (check *Checker) unionLiteralError(e *ast.CompositeLit, typ Type) {
	vs := unionVariants(typ)
	check.errorf(e, InvalidLit, "cannot write a composite literal of union type %s: name the variant it holds"+
		"\n    A union value holds one of its variants (%s). Construct one by name:"+
		"\n        %s.%s{...} or %s.%s(payload)", typ, variantNames(vs), typ, vs[len(vs)-1].name, typ, vs[len(vs)-1].name)
}

// variantFieldsOnly reports a key of a packed union variant literal that
// names the tag field, which the variant sets itself.
func (check *Checker) packedVariantKey(key *ast.Ident, v *Variant) bool {
	p := v.typ.(*Packed)
	if key.Name != p.fields[0].obj.name {
		return true
	}
	check.errorf(key, InvalidLitField, "cannot set the tag %s in a literal of variant %s: the variant sets it to %s", key.Name, v.name, v.cases[0].val)
	return false
}

// unionComparable explains why a union type is not comparable, or returns
// nil if it is.
func unionComparable(t *TaggedUnion, dynamic bool, seen map[Type]bool) *typeError {
	for _, v := range t.variants {
		if comparableType(v.typ, dynamic, seen) != nil {
			return typeErrorf("union with variant %s of type %s cannot be compared", v.name, v.typ)
		}
	}
	return nil
}

// identicalVariants reports whether two variant lists are identical: the
// same names in the same order, with identical payloads and tag values.
func identicalVariants(x, y []*Variant, identical func(x, y Type) bool) bool {
	return slices.EqualFunc(x, y, func(a, b *Variant) bool {
		return a.bare == b.bare && a.sameId(b.pkg, b.name, false) && identical(a.typ, b.typ) &&
			slices.EqualFunc(a.cases, b.cases, func(c, d caseValue) bool { return c.bits == d.bits })
	})
}
