// Translated by go2pu from internal/types2/enum.go at the go-cutover tag; edited by hand since.

package types2

// Plutonium enum types:
//
//	type Color enum { Red; Green; Blue }
//	type Errno enum uint16 { OK = 0; EPERM; EIO = 5 }
//
// An enum is a defined type whose underlying type is an integer type. Its
// members are typed constants reached through the type's name (Color.Red);
// they do not enter any scope. The Named type carries the member list, so
// everything that only needs the integer (layout, constants, comparison,
// interfaces, code generation) works unchanged, and the checker adds the
// enum rules on top: member selection, bare member names in switch cases,
// exhaustive switches, checked conversions, no arithmetic, len(E),
// range E, and a generated String method.

import (
	"pluto/slices"
	"compiler/compat"
	"compiler/internal/constant"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	. "compiler/internal/types2/errors"
	"compiler/pu"
)

// An Enum lists the members of an enum type.
type Enum struct {
	obj      *TypeName
	members  []*Const // in declaration order
	str      *Func    // generated String method; nil if the type declares its own
	building bool     // the declaration is being checked
}

// Obj returns the enum type's name.
func (e *Enum) Obj() *TypeName { return e.obj }

// Len returns the number of members.
func (e *Enum) Len() int { return len(e.members) }

// Member returns the i'th member in declaration order.
func (e *Enum) Member(i int) *Const { return e.members[i] }

// GeneratedString returns the String method the compiler generates for the
// enum, or nil if the type declares its own.
func (e *Enum) GeneratedString() *Func { return e.str }

// lookup returns the member called name, or nil.
func (e *Enum) lookup(name string) *Const {
	for _, m := range e.members {
		if m.name == name {
			return m
		}
	}
	return nil
}

// EnumOf returns the members of T if T is an enum type, or nil.
func EnumOf(T Type) *Enum {
	if n, ok := Unalias(T).(*Named); ok && n.inst == nil {
		return n.enum_
	}
	return nil
}

// enumType checks an enum type literal, which must be the right-hand side
// of the type definition of def, and returns the enum's underlying integer
// type. The members are recorded on def's Named type.
func (check *Checker) enumType(e *ast.EnumType, def *TypeName) Type {
	var named *Named
	if def != nil {
		named, _ = def.typ.(*Named) // an alias's RHS is not a definition
	}
	if named == nil || named.obj != def {
		check.error(e, MisplacedEnum, "enum type must be the type of a type definition: type E enum { ... }")
		return Typ[Invalid]
	}
	if named.tparams != nil {
		check.errorf(e, GenericEnum, "enum type %s cannot have type parameters", pu.Box(def.name))
		return Typ[Invalid]
	}

	var base *Basic
	if e.Base != nil {
		bt := check.varType(e.Base)
		if !isValid(bt) {
			return Typ[Invalid]
		}
		b, _ := bt.Underlying().(*Basic)
		if b == nil || !isInteger(b) || isTypeParam(bt) {
			check.errorf(e.Base, BadEnumBase, "enum %s: underlying type %s must be an integer type", pu.Box(def.name), bt)
			return Typ[Invalid]
		}
		base = b
	}

	en := pu.New(Enum{obj: def, building: true})
	named.enum_ = en
	ok := true
	defer func() {
		en.building = false
		if !ok {
			named.enum_ = nil
		}
	}()

	if len(e.Members) == 0 {
		check.errorf(e, EmptyEnum, "enum %s has no members", pu.Box(def.name))
		ok = false
		return Typ[Invalid]
	}

	// Compute every member's value.
	vals := compat.Make[constant.Value](len(e.Members))
	var names objset
	seen := map[string]*ast.EnumMember{} // exact value -> first member with it
	var prev constant.Value
	for i, m := range e.Members {
		var val constant.Value
		switch {
		case m.Value != nil:
			val = check.enumValue(m)
		case i == 0:
			val = constant.MakeInt64(0)
		case !prev.IsNil():
			val = constBinaryOp(prev, token.ADD, constant.MakeInt64(1))
		}
		prev = val
		vals[i] = val

		name := m.Name.Name
		if name == "_" {
			check.error(m.Name, BlankEnumMember, "enum member cannot be _: every member needs a name")
			ok = false
		} else if !check.declareInSet(&names, m.Name.Pos(), NewConst(m.Name.Pos(), check.pkg, name, nil, constant.Value{})) {
			ok = false
		}
		if val.IsNil() {
			ok = false
			continue
		}
		key := val.ExactString()
		if first := seen[key]; first != nil {
			check.errorf(m.Name, DuplicateEnumValue, "enum member %s has the same value %s as %s", pu.Box(name), pu.Box(key), pu.Box(first.Name.Name))
			ok = false
		} else {
			seen[key] = m
		}
	}
	if !ok {
		return Typ[Invalid]
	}

	if _, hasZero := seen["0"]; !hasZero {
		check.errorf(e, EnumWithoutZero, "enum %s has no member with value 0 (its zero value must be a member)", pu.Box(def.name))
		ok = false
		return Typ[Invalid]
	}

	// Choose or check the underlying type.
	if base == nil {
		base = smallestIntType(vals)
		if base == nil {
			check.errorf(e, EnumOverflow, "enum %s: values do not fit in a 64-bit integer", pu.Box(def.name))
			ok = false
			return Typ[Invalid]
		}
	} else {
		for i, val := range vals {
			if !representableConst(val, check, base, nil) {
				m := e.Members[i]
				var at positioner = m.Name
				if m.Value != nil {
					at = m.Value
				}
				check.errorf(at, EnumOverflow, "enum member %s.%s value %s overflows %s", pu.Box(def.name), pu.Box(m.Name.Name), pu.Box(val), base)
				ok = false
			}
		}
		if !ok {
			return Typ[Invalid]
		}
	}

	for i, m := range e.Members {
		c := NewConst(m.Name.Pos(), check.pkg, m.Name.Name, named, vals[i])
		check.recordDef(m.Name, c)
		en.members = compat.Append(en.members, c)
	}
	return base
}

// enumValue evaluates an explicit member value, which must be an integer
// constant. It returns nil after reporting an error.
func (check *Checker) enumValue(m *ast.EnumMember) constant.Value {
	x := pu.Alloc[operand]()
	check.expr(nil, x, m.Value)
	if !x.isValid() {
		return constant.Value{}
	}
	if x.mode() != constant_ {
		check.errorf(x, BadEnumValue, "enum member %s: value %s is not constant", pu.Box(m.Name.Name), x)
		return constant.Value{}
	}
	if !isInteger(x.typ()) && !isUntyped(x.typ()) {
		check.errorf(x, BadEnumValue, "enum member %s: value %s is not an integer", pu.Box(m.Name.Name), x)
		return constant.Value{}
	}
	val := constant.ToInt(x.val)
	if val.Kind() != constant.Int {
		check.errorf(x, BadEnumValue, "enum member %s: value %s is not an integer", pu.Box(m.Name.Name), x)
		return constant.Value{}
	}
	if isUntyped(x.typ()) {
		check.updateExprType(m.Value, Typ[UntypedInt], true)
	}
	return val
}

// smallestIntType returns the smallest unsigned integer type that holds
// every value, or the smallest signed one if any value is negative, or nil
// if none does.
func smallestIntType(vals []constant.Value) *Basic {
	kinds := []BasicKind{Uint8, Uint16, Uint32, Uint64}
	for _, v := range vals {
		if constant.Sign(v) < 0 {
			kinds = compat.Clone([]BasicKind{Int8, Int16, Int32, Int64})
			break
		}
	}
	for _, k := range kinds {
		if !slices.ContainsFunc(vals, func(v constant.Value) bool { return !representableConst(v, nil, Typ[k], nil) }) {
			return Typ[k]
		}
	}
	return nil
}

// addEnumString gives an enum type the generated String method, unless the
// type declares one of its own. It runs after the type's methods are
// collected.
func (check *Checker) addEnumString(obj *TypeName) {
	named := asNamed(obj.typ)
	if named == nil || named.enum_ == nil || named.methodIndex("String", false) >= 0 {
		return
	}
	recv := NewParam(obj.pos, obj.pkg, "e", named)
	result := NewParam(obj.pos, obj.pkg, "", Typ[String])
	sig := NewSignatureType(recv, nil, nil, nil, NewTuple(compat.Clone([]*Var{result})...), false)
	fn := NewFunc(obj.pos, obj.pkg, "String", sig)
	named.enum_.str = fn
	named.AddMethod(fn)
}

// enumSelector handles E.Member, where x is the type expression E. It
// reports whether e selects a member, with *x holding the member constant
// (or invalid after an error).
func (check *Checker) enumSelector(x *operand, e *ast.SelectorExpr) bool {
	en := EnumOf(x.typ())
	if en == nil {
		return false
	}
	if en.building {
		check.errorf(e, EnumSelfReference, "invalid use of %s in the declaration of enum %s", ast.Expr(e), pu.Box(en.obj.name))
		x.invalidate()
		return true
	}
	c := en.lookup(e.Sel.Name)
	if c == nil {
		return false // a method expression, or an error reported by the caller
	}
	if !c.Exported() && c.pkg != check.pkg {
		check.errorf(e.Sel, UnexportedEnumMember, "%s undefined (cannot refer to unexported member %s of enum %s)", ast.Expr(e), pu.Box(c.name), pu.Box(en.obj.name))
		x.invalidate()
		return true
	}
	check.recordUse(e.Sel, c)
	x.mode_ = constant_
	x.typ_ = c.typ
	x.val = c.val
	x.expr = e
	return true
}

// enumCaseName handles a bare member name in a case of a switch whose tag
// x has enum type: a case expression that is an identifier naming a member
// denotes that member, whatever else the name means in scope. It reports
// whether e is such a name, with *v holding the member constant.
func (check *Checker) enumCaseName(v *operand, x *operand, e ast.Expr) bool {
	id, _ := e.(*ast.Ident)
	if id == nil || !x.isValid() {
		return false
	}
	en := EnumOf(x.typ())
	if en == nil {
		return false
	}
	c := en.lookup(id.Name)
	if c == nil || !c.Exported() && c.pkg != check.pkg {
		return false
	}
	check.recordUse(id, c)
	*v = operand{mode_: constant_, expr: id, typ_: c.typ, val: c.val}
	check.record(v)
	return true
}

// enumExhaustive reports the members of the enum type of the switch tag x
// that a switch without a default case does not list among its constant
// case values (seen).
func (check *Checker) enumExhaustive(s *ast.SwitchStmt, x *operand, seen valueMap) {
	en := EnumOf(x.typ())
	if en == nil || !x.isValid() {
		return
	}
	var missing []string
	for _, m := range en.members {
		covered := slices.ContainsFunc(seen[goVal(m.val)], func(vt valueType) bool { return Identical(vt.typ, x.typ()) })
		if !covered {
			missing = compat.Append(missing, m.name)
		}
	}
	if len(missing) == 0 {
		// A value that is not a member panics (spec section 12.9), so
		// the switch is a terminating statement when its clauses are.
		if check.exhaustive == nil {
			check.exhaustive = map[ast.Stmt]bool{}
		}
		check.exhaustive[s] = true
	}
	check.missingCases(atPos(s.Switch), x, missing)
}

// missingCases reports the members of a closed set (an enum's members) that
// a switch on x without a default case leaves out.
func (check *Checker) missingCases(at positioner, x *operand, missing []string) {
	if len(missing) == 0 {
		return
	}
	what, them := "case", "it"
	if len(missing) > 1 {
		what, them = "cases", "them"
	}
	check.errorf(at, MissingEnumCase, "missing %s in switch on %s (%s): %s (add %s or a default case)",
		pu.Box(what), x.expr, x.typ(), pu.Box(compat.Join(missing, ", ")), pu.Box(them))
}

// enumConversion checks the conversion E(x) to enum type T, where x is not
// already of type T. A constant must be a member's value. A non-constant
// integer converts only in the comma-ok form c, ok := E(x), which call must
// be the operand of.
func (check *Checker) enumConversion(x *operand, T Type, en *Enum, call *ast.CallExpr) {
	if en.building {
		check.errorf(x, EnumSelfReference, "invalid conversion to %s in its own declaration", T)
		x.invalidate()
		return
	}
	if !allInteger(x.typ()) && (x.mode() != constant_ || !isUntyped(x.typ())) {
		check.errorf(x, BadEnumConversion, "cannot convert %s to enum type %s (only integers convert to an enum)", x, T)
		x.invalidate()
		return
	}

	if x.mode() == constant_ {
		val := constant.ToInt(x.val)
		var member *Const
		if val.Kind() == constant.Int {
			for _, m := range en.members {
				if constCompare(m.val, token.EQL, val) {
					member = m
					break
				}
			}
		}
		if member == nil {
			check.errorf(x, BadEnumConversion, "cannot convert %s to enum type %s: no member has that value", x, T)
			x.invalidate()
			return
		}
		if isUntyped(x.typ()) {
			// The argument becomes the enum's integer; it was never an
			// enum value itself.
			check.updateExprType(x.expr, T.Underlying(), true)
		}
		x.val = member.val
		x.typ_ = T
		return
	}

	if isUntyped(x.typ()) {
		check.convertUntyped(x, Default(x.typ())) // e.g. E(1 << n)
		if !x.isValid() {
			return
		}
	}
	if check.commaOkExpr != ast.Expr(call) {
		check.errorf(call, UncheckedEnumConversion, "cannot convert non-constant %s to enum type %s without a check: use the comma-ok form (v, ok := %s(...))", x, T, T)
		x.invalidate()
		return
	}
	x.mode_ = commaok
	x.typ_ = T
}

// denotesType reports whether e, an identifier or qualified identifier,
// names a type. It reports no errors and records nothing; the caller then
// checks e as a type.
func (check *Checker) denotesType(e ast.Expr) bool {
	var obj Object
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		obj = check.lookup(e.Name)
	case *ast.SelectorExpr:
		id, _ := e.X.(*ast.Ident)
		if id == nil {
			return false
		}
		pn, _ := check.lookup(id.Name).(*PkgName)
		if pn == nil {
			return false
		}
		obj = pn.imported.scope.Lookup(e.Sel.Name)
	}
	_, ok := obj.(*TypeName)
	return ok
}

// enumTypeOperand checks e, which denotes a type, as the operand of
// len(E) or range E, and returns its members, or nil after an error.
func (check *Checker) enumTypeOperand(e ast.Expr, context string) (Type, *Enum) {
	T := check.typ(e)
	if !isValid(T) {
		return nil, nil
	}
	en := EnumOf(T)
	if en == nil {
		check.errorf(e, NotEnumType, "%s: %s is not an enum type", pu.Box(context), T)
		return nil, nil
	}
	if en.building {
		check.errorf(e, EnumSelfReference, "invalid use of %s in the declaration of enum %s", e, pu.Box(en.obj.name))
		return nil, nil
	}
	return T, en
}

// enumMemberHint returns " (did you mean E.name?)" if name, which is
// undefined, is the name of a member of an enum type in scope.
func (check *Checker) enumMemberHint(name string) string {
	for s := check.scope; s != nil && s != Universe; s = s.parent {
		for _, n := range s.Names() {
			tn, _ := s.Lookup(n).(*TypeName)
			if tn == nil || tn.typ == nil {
				continue
			}
			if en := EnumOf(tn.typ); en != nil && !en.building && en.lookup(name) != nil {
				return compat.Concat(" (did you mean ", tn.name, ".", name, "?)")
			}
		}
	}
	return ""
}

// enumBitWidth returns the number of bits a packed field of enum type T
// needs to hold every member: the bit length of the largest member for an
// unsigned enum, or the two's complement width of the members' range for a
// signed one.
func enumBitWidth(en *Enum, T Type) int64 {
	signed := !isUnsigned(T)
	var w int64 = 1
	for _, m := range en.members {
		v := m.val
		if signed && constant.Sign(v) < 0 {
			// -2^(w-1) <= v  <=>  bitlen(-v-1) < w
			v = constBinaryOp(constUnaryOp(token.SUB, v, 0), token.SUB, constant.MakeInt64(1))
		}
		n := int64(constant.BitLen(v))
		if signed {
			n++ // room for the sign bit
		}
		w = max(w, n)
	}
	return w
}

// isEnum reports whether T is an enum type.
func isEnum(T Type) bool { return EnumOf(T) != nil }
