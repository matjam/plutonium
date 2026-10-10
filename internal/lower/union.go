package lower

import (
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
	typerr "github.com/matjam/plutonium/internal/types2/errors"
)

// Unions. An ordinary union is laid out as its tag (one byte, or two for
// more than 256 variants) followed by the payload of the variant it holds,
// at the first offset the largest payload alignment allows (abi.md section
// 1). The tag numbers the variants in declaration order, so the zero value
// is the first variant with a zero payload. A union value is an aggregate:
// it lives in memory and passes as its words (layout.go).
//
// A packed union is a packed type: its tag is a packed field, and each
// variant's payload is a packed type with the same storage, so a payload
// and the union holding it are the same bits.

// A unionLayout describes where an ordinary union keeps its tag and payload.
type unionLayout struct {
	T       types2.Type // the union type
	u       *types2.TaggedUnion
	tag     *ssa.Type // TI8 or TI16
	word    *ssa.Type // the union's first leaf, which holds the tag (layout.go)
	payload int64     // offset of the payload
}

// unionLayout returns the layout of union type T, reporting at pos a payload
// that holds an owned, shared or synced pointer once type arguments are
// substituted (the checker reports those it can see).
func (l *Lowerer) unionLayoutOf(T types2.Type, pos token.Pos) *unionLayout {
	u := T.Underlying().(*types2.TaggedUnion)
	tagSize, off := types2.UnionLayout(l.sizes, u)
	for i := 0; i < u.NumVariants(); i++ {
		if p := u.Variant(i).Payload(); ownerPointerIn(p) {
			l.errorf(pos, diag.CheckerCode(typerr.UnionOwnerPayload), "union %s cannot have variant %s with payload %s yet: unions with owned, shared or synced payloads are not supported until ownership lands (language spec section 6.4)",
				T, u.Variant(i).Name(), p)
			break
		}
	}
	word := int64(8)
	for word > off {
		word /= 2
	}
	return &unionLayout{T: T, u: u, tag: ssa.IntType(tagSize), word: ssa.IntType(word), payload: off}
}

// ownerPointerIn reports whether a value of T holds an owned, shared or
// synced pointer directly (not behind a pointer).
func ownerPointerIn(T types2.Type) bool {
	switch u := T.Underlying().(type) {
	case *types2.Wrapper:
		return u.IsPointer() || ownerPointerIn(u.Elem())
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if ownerPointerIn(u.Field(i).Type()) {
				return true
			}
		}
	case *types2.Array:
		return ownerPointerIn(u.Elem())
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants(); i++ {
			if ownerPointerIn(u.Variant(i).Payload()) {
				return true
			}
		}
	}
	return false
}

// variantOf returns the variant an expression names (U.V, or a bare V in a
// case), or nil.
func (s *fnState) variantOf(e ast.Expr) *types2.Variant {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		v, _ := s.info.Uses[e].(*types2.Variant)
		return v
	case *ast.SelectorExpr:
		v, _ := s.info.Uses[e.Sel].(*types2.Variant)
		return v
	}
	return nil
}

// payloadType returns the payload type of variant v of union type T, with
// T's type arguments substituted.
func payloadType(T types2.Type, v *types2.Variant) types2.Type {
	return types2.UnionVariants(T)[v.Index()].Payload()
}

// ----------------------------------------------------------------------------
// Construction

// makeVariant builds a value of union type T holding variant v with the
// given payload (ignored for a variant without one).
func (s *fnState) makeVariant(T types2.Type, v *types2.Variant, payload value, pos token.Pos) value {
	if p, ok := T.Underlying().(*types2.Packed); ok {
		return s.makePackedVariant(T, p, v, payload)
	}
	ul := s.l.unionLayoutOf(T, pos)
	tmp := s.temp(T, "union")
	s.zero(tmp, s.l.sizeof(T))
	// The tag is written as the whole first leaf, zero-extended over the
	// padding before the payload, so that the union can live in registers.
	s.store(ul.word, tmp, s.constInt(ul.word, int64(v.Index())))
	if P := payloadType(T, v); v.HasPayload() && s.l.sizeof(P) > 0 {
		s.assign(lvalue{ptr: s.offPtr(tmp, ul.payload), T: P}, payload, true)
	}
	return s.litValue(tmp, T)
}

// makePackedVariant makes a packed union value from a payload of variant v,
// whose bits it shares. The tag is set to the variant's first tag value
// unless it already holds one of the variant's values.
func (s *fnState) makePackedVariant(T types2.Type, p *types2.Packed, v *types2.Variant, payload value) value {
	tmp := s.temp(T, "union")
	s.storeTo(tmp, payload, T)
	lv := lvalue{ptr: tmp, T: T}
	tag := s.packedField(lv, p, 0)
	cur := s.loadLV(tag)
	fieldT := p.Field(0).Var().Type()
	match := s.constBool(false)
	for _, c := range v.TagValues() {
		eq := s.equal(cur, s.constValue(c, fieldT, nil), fieldT, token.NoPos)
		match = s.newValue(ssa.OpOr, ssa.TBool, match, eq)
	}
	s.ifThen(s.newValue(ssa.OpNot, ssa.TBool, match), func() {
		s.storeLV(tag, s.constValue(v.TagValues()[0], fieldT, nil))
	})
	return s.loadLV(lv)
}

// variantValue lowers U.V used as a value: a variant without a payload, or
// the function that constructs one with a payload.
func (s *fnState) variantValue(e *ast.SelectorExpr, v *types2.Variant, T types2.Type) value {
	if sig, ok := T.Underlying().(*types2.Signature); ok {
		U := sig.Results().At(0).Type()
		return scalarValue(s.symAddr(s.l.closureRecord(s.l.variantFunc(U, v))), T)
	}
	return s.makeVariant(T, v, value{}, e.Pos())
}

// variantCall lowers U.V(x), which constructs variant V from payload x.
func (s *fnState) variantCall(e *ast.CallExpr, v *types2.Variant) value {
	T := s.typeOf(e)
	P := payloadType(T, v)
	return s.makeVariant(T, v, s.exprAs(e.Args[0], P), e.Pos())
}

// variantLit lowers U.V{...}: a literal of V's payload type wrapped in a
// value of union type T.
func (s *fnState) variantLit(e *ast.CompositeLit, v *types2.Variant, T types2.Type) value {
	P := payloadType(T, v)
	payload := s.compositeLitOf(e, P)
	if p, ok := T.Underlying().(*types2.Packed); ok {
		// The literal cannot set the tag: the variant does.
		tmp := s.temp(T, "union")
		s.storeTo(tmp, payload, T)
		s.storeLV(s.packedField(lvalue{ptr: tmp, T: T}, p, 0), s.constValue(v.TagValues()[0], p.Field(0).Var().Type(), e))
		return s.loadLV(lvalue{ptr: tmp, T: T})
	}
	return s.makeVariant(T, v, payload, e.Pos())
}

// variantFunc returns a function from V's payload to a union of type U
// holding V, for U.V used as a func value.
func (l *Lowerer) variantFunc(U types2.Type, v *types2.Variant) *ssa.Sym {
	name := typeString(U) + "." + v.Name() + "·variant"
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	P := payloadType(U, v)
	params := types2.NewTuple(types2.NewParam(token.NoPos, nil, "p", P))
	results := types2.NewTuple(types2.NewParam(token.NoPos, nil, "", U))
	sig := types2.NewSignatureType(nil, nil, nil, params, results, false)
	l.queue = append(l.queue, &funcJob{sym: sym, sig: sig, build: func(s *fnState) {
		in := s.incomingArgs(sig)
		s.returnValues([]value{s.makeVariant(U, v, in[0], token.NoPos)})
	}})
	return sym
}

// ----------------------------------------------------------------------------
// Selection

// unionTag returns the tag of an ordinary union value, read as its first
// leaf so that a union in registers needs no memory.
func (s *fnState) unionTag(ul *unionLayout, u value) *ssa.Value {
	w := s.load(ul.word, u.v)
	if ul.word.Size > ul.tag.Size {
		w = s.newValue(ssa.OpTrunc, ul.tag, w)
	}
	return w
}

// unionPayload reads the payload of variant v from an ordinary union value.
func (s *fnState) unionPayload(ul *unionLayout, u value, v *types2.Variant) value {
	return s.loadFrom(s.offPtr(u.v, ul.payload), payloadType(ul.T, v))
}

// holds returns a condition that is true when union value u (of type T)
// holds variant v.
func (s *fnState) holds(T types2.Type, u value, v *types2.Variant, ul *unionLayout) *ssa.Value {
	if p, ok := T.Underlying().(*types2.Packed); ok {
		fieldT := p.Field(0).Var().Type()
		var tag value
		if u.addr {
			tag = s.loadLV(s.packedField(lvalue{ptr: u.v, T: T}, p, 0))
		} else {
			tag = scalarValue(s.bitsOfValue(u.v, p, 0), fieldT)
		}
		match := s.constBool(false)
		for _, c := range v.TagValues() {
			eq := s.equal(tag, s.constValue(c, fieldT, nil), fieldT, token.NoPos)
			match = s.newValue(ssa.OpOr, ssa.TBool, match, eq)
		}
		return match
	}
	return s.newValue(ssa.OpEq, ssa.TBool, s.unionTag(ul, u), s.constInt(ul.tag, int64(v.Index())))
}

// payloadOf returns the payload of variant v held by union value u, which
// is in memory.
func (s *fnState) payloadOf(T types2.Type, u value, v *types2.Variant, ul *unionLayout) value {
	if _, ok := T.Underlying().(*types2.Packed); ok {
		// The payload is the same bits as the union.
		return s.loadFrom(u.v, payloadType(T, v))
	}
	return s.unionPayload(ul, u, v)
}

// layoutFor returns the layout of an ordinary union type, or nil for a
// packed union.
func (s *fnState) layoutFor(T types2.Type, pos token.Pos) *unionLayout {
	if _, ok := T.Underlying().(*types2.TaggedUnion); ok {
		return s.l.unionLayoutOf(T, pos)
	}
	return nil
}

// unionTypeSwitch lowers a type switch on a union value: a compare of the
// tag for each case, and the clause's variable bound to the payload of a
// lone variant or to the union itself.
func (s *fnState) unionTypeSwitch(st *ast.TypeSwitchStmt, x ast.Expr, label *types2.Label) {
	T := s.typeOf(x)
	ul := s.layoutFor(T, x.Pos())
	// Held: the clauses may change the union the payloads are read from.
	u := s.hold(s.materialize(s.expr(x)))
	if !u.addr {
		// A packed union held in a register: give it a home, so that cases
		// and bindings read the same bits.
		tmp := s.temp(T, "union")
		s.storeTo(tmp, u, T)
		u = aggValue(tmp, T)
	}

	done := s.newBlock(ssa.BlockInvalid)
	clauses := st.Body.List
	bodies := make([]*ssa.Block, len(clauses))
	for i := range clauses {
		bodies[i] = s.newBlock(ssa.BlockInvalid)
	}
	var defaultBody *ssa.Block
	for i, c := range clauses {
		cc := c.(*ast.CaseClause)
		if cc.List == nil {
			defaultBody = bodies[i]
			continue
		}
		for _, e := range cc.List {
			next := s.newBlock(ssa.BlockInvalid)
			s.endIf(s.holds(T, u, s.variantOf(e), ul), bodies[i], next)
			s.seal(next)
			s.startBlock(next)
		}
	}
	if defaultBody != nil {
		s.endPlain(defaultBody)
	} else {
		// The cases name every variant (the checker makes sure), so only a
		// corrupted tag gets here.
		s.invalidTag(T, u, ul, st.Pos())
	}

	s.targets = append(s.targets, &target{label: label, breakTo: done, isSwitch: true, depth: len(s.cleanups)})
	for i, c := range clauses {
		cc := c.(*ast.CaseClause)
		s.seal(bodies[i])
		s.startBlock(bodies[i])
		clause := s.pushCleanup(cc, true)
		if obj, ok := s.info.Implicits[cc].(*types2.Var); ok {
			// The variable is a copy of the payload the union still holds,
			// so it is not destroyed on its own.
			addr := s.declareView(obj)
			VT := s.subst(obj.Type())
			if len(cc.List) == 1 {
				s.assign(lvalue{ptr: addr, T: VT}, s.payloadOf(T, u, s.variantOf(cc.List[0]), ul), true)
			} else {
				s.assign(lvalue{ptr: addr, T: VT}, s.loadFrom(u.v, T), true)
			}
		}
		s.stmtList(cc.Body)
		s.popCleanup(clause)
		s.endPlain(done)
	}
	s.targets = s.targets[:len(s.targets)-1]
	s.seal(done)
	s.startBlock(done)
}

// invalidTag ends the current block with a panic for a union value whose tag
// names no variant, which only memory corruption or unsafe code can cause.
func (s *fnState) invalidTag(T types2.Type, u value, ul *unionLayout, pos token.Pos) {
	var tag *ssa.Value
	if ul != nil {
		tag = s.newValue(ssa.OpZeroExt, ssa.TI64, s.unionTag(ul, u))
	} else {
		p := T.Underlying().(*types2.Packed)
		t := s.loadLV(s.packedField(lvalue{ptr: u.v, T: T}, p, 0))
		tag = s.convertScalar(t.v, t.T, types2.Typ[types2.Int64])
	}
	if pos.IsValid() {
		defer s.f.SetPos(s.f.SetPos(pos))
	}
	name := s.stringLeavesOf(s.typeName(T))
	s.callRuntimeNoReturn("panicUnionTag", append(name, tag)...)
}

// typeName returns the name of T as messages print it.
func (s *fnState) typeName(T types2.Type) string {
	return types2.TypeString(T, func(p *types2.Package) string { return p.Name() })
}

// stringLeavesOf returns the three words of a constant string.
func (s *fnState) stringLeavesOf(str string) []*ssa.Value {
	return s.stringLeaves(s.stringConst(str, types2.Typ[types2.String]))
}

// unionAssert lowers u.(U.V) on a union value: the payload of V, and with
// commaOk whether u holds V (the zero payload if not). The single-value form
// panics if u holds another variant.
func (s *fnState) unionAssert(e *ast.TypeAssertExpr, commaOk bool) (value, *ssa.Value) {
	T := s.typeOf(e.X)
	ul := s.layoutFor(T, e.Pos())
	u := s.materialize(s.expr(e.X))
	if !u.addr {
		tmp := s.temp(T, "union")
		s.storeTo(tmp, u, T)
		u = aggValue(tmp, T)
	}
	v := s.variantOf(e.Type)
	P := payloadType(T, v)
	ok := s.holds(T, u, v, ul)
	if !commaOk {
		s.panicUnless(ok, e.Pos(), func() {
			have := s.stringLeavesOf("")
			if ul != nil {
				have = s.variantName(T, ul, u)
			}
			args := append(s.stringLeavesOf(s.typeName(T)), have...)
			args = append(args, s.stringLeavesOf(v.Name())...)
			s.callRuntimeNoReturn("panicUnion", args...)
		})
		return s.payloadOf(T, u, v, ul), nil
	}
	out := s.temp(P, "assert")
	s.storeTo(out, s.zeroValue(P), P)
	s.ifThen(ok, func() { s.storeTo(out, s.payloadOf(T, u, v, ul), P) })
	return s.loadFrom(out, P), ok
}

// variantName returns the three words of the name of the variant an ordinary
// union value holds, read from a table of the names, or "an invalid tag".
func (s *fnState) variantName(T types2.Type, ul *unionLayout, u value) []*ssa.Value {
	n := ul.u.NumVariants()
	names := make([]string, n+1)
	for i := range n {
		names[i] = ul.u.Variant(i).Name()
	}
	names[n] = "an invalid tag"
	sym := s.l.variantNames(T, names)
	tag := s.newValue(ssa.OpZeroExt, ssa.TI64, s.unionTag(ul, u))
	limit := s.constInt(ssa.TI64, int64(n))
	idx := s.newValue(ssa.OpSelect, ssa.TI64, s.newValue(ssa.OpLessu, ssa.TBool, tag, limit), tag, limit)
	entry := s.newValue(ssa.OpAddPtr, ssa.TPtr, s.symAddr(sym), s.newValue(ssa.OpMul, ssa.TI64, idx, s.constInt(ssa.TI64, strSize)))
	return s.stringLeaves(aggValue(entry, types2.Typ[types2.String]))
}

// variantNames returns the table of a union type's variant names.
func (l *Lowerer) variantNames(T types2.Type, names []string) *ssa.Sym {
	key := "variantnames:" + typeString(T)
	if sym := l.funcs[key]; sym != nil {
		return sym
	}
	sym := l.stringTable(key, names)
	l.funcs[key] = sym
	return sym
}

// unionEqual compares two values of ordinary union type T: equal if they
// hold the same variant with equal payloads.
func (s *fnState) unionEqual(x, y value, T types2.Type, pos token.Pos) *ssa.Value {
	ul := s.l.unionLayoutOf(T, pos)
	res := s.temp(types2.Typ[types2.Bool], "eq")
	s.store(ssa.TBool, res, s.constBool(false))
	tx := s.unionTag(ul, x)
	same := s.newValue(ssa.OpEq, ssa.TBool, tx, s.unionTag(ul, y))
	s.ifThen(same, func() {
		s.store(ssa.TBool, res, s.constBool(true))
		for i := 0; i < ul.u.NumVariants(); i++ {
			v := ul.u.Variant(i)
			P := v.Payload()
			if s.l.sizeof(P) == 0 {
				continue
			}
			s.ifThen(s.newValue(ssa.OpEq, ssa.TBool, tx, s.constInt(ul.tag, int64(i))), func() {
				eq := s.equal(s.unionPayload(ul, x, v), s.unionPayload(ul, y, v), P, pos)
				s.store(ssa.TBool, res, eq)
			})
		}
	})
	return s.load(ssa.TBool, res)
}
