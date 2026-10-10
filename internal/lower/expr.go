package lower

import (
	"go/constant"

	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// expr lowers an expression to a value.
func (s *fnState) expr(e ast.Expr) value {
	if p := e.Pos(); p.IsValid() {
		defer s.f.SetPos(s.f.SetPos(p))
	}
	defer s.at(e.Pos())()
	tv := s.info.Types[e]
	if tv.Value != nil {
		return s.constValue(tv.Value, s.typeOf(e), e)
	}
	T := s.typeOf(e)

	switch e := e.(type) {
	case *ast.ParenExpr:
		return s.expr(e.X)

	case *ast.Ident:
		switch obj := s.info.Uses[e].(type) {
		case *types2.Var:
			return s.loadLV(s.varLV(obj, e))
		case *types2.Func:
			return scalarValue(s.funcValue(obj, s.instanceArgs(e), e.Pos()), T)
		case *types2.Nil:
			return s.zeroValue(T)
		}
		s.l.internalf(e.Pos(), "unexpected identifier %s", e.Name)
		return s.zeroValue(T)

	case *ast.FuncLit:
		return scalarValue(s.closure(e), T)

	case *ast.CompositeLit:
		return s.compositeLit(e, T)

	case *ast.SelectorExpr:
		return s.selector(e, T)

	case *ast.IndexExpr, *ast.IndexListExpr:
		ix := unpackIndex(e)
		xt := s.typeOf(ix.x)
		if _, ok := xt.Underlying().(*types2.Signature); ok {
			// Instantiation of a generic function used as a value, or of
			// a method with type parameters (x.M[int], T.M[int]).
			if se, ok := ast.Unparen(ix.x).(*ast.SelectorExpr); ok && s.info.Selections[se] != nil {
				return s.selector(se, T)
			}
			if id := identOf(ix.x); id != nil {
				if fn, ok := s.info.Uses[id].(*types2.Func); ok {
					return scalarValue(s.funcValue(fn, s.instanceArgs(id), id.Pos()), T)
				}
			}
		}
		if isString(xt) {
			str := s.holdAcross(s.expr(ix.x), ix.index)
			i := s.indexValue(ix.index)
			ptr, n := s.stringParts(str)
			s.boundsCheck(i, n, ix.index.Pos())
			return scalarValue(s.load(ssa.TI8, s.newValue(ssa.OpAddPtr, ssa.TPtr, ptr, i)), T)
		}
		return s.loadLV(s.indexLV(e.(*ast.IndexExpr)))

	case *ast.SliceExpr:
		return s.sliceExpr(e, T)

	case *ast.StarExpr:
		return s.loadLV(s.lv(e))

	case *ast.UnaryExpr:
		return s.unary(e, T)

	case *ast.BinaryExpr:
		return s.binary(e, T)

	case *ast.CallExpr:
		return s.callExpr(e)

	case *ast.TypeAssertExpr:
		v, _ := s.typeAssert(e, false)
		return v

	case *ast.KeyValueExpr:
		s.l.internalf(e.Pos(), "key-value outside composite literal")
	}
	s.l.unsupportedf(e.Pos(), "unsupported expression %T", e)
	return s.zeroValue(T)
}

// scalar lowers an expression that has a scalar type.
func (s *fnState) scalar(e ast.Expr) *ssa.Value {
	v := s.expr(e)
	if v.addr {
		s.l.internalf(e.Pos(), "expected a scalar, have %s", v.T)
		return s.constInt(ssa.TI64, 0)
	}
	return v.v
}

// exprAs lowers e and converts it to T as an assignment would: untyped
// constants take T, concrete values convert to interfaces.
func (s *fnState) exprAs(e ast.Expr, T types2.Type) value {
	tv := s.info.Types[e]
	if tv.Value != nil {
		if isInterface(T) {
			// A constant becomes an interface value of its own type, or of
			// its default type if untyped.
			from := s.typeOf(e)
			if b, ok := from.Underlying().(*types2.Basic); ok && b.Info()&types2.IsUntyped != 0 {
				from = defaultType(tv.Value)
			}
			return s.toInterface(s.constValue(tv.Value, from, e), from, T)
		}
		return s.constValue(tv.Value, T, e)
	}
	v := s.expr(e)
	return s.assignConvert(v, T, e)
}

// assignConvert converts a value for assignment to a variable of type T.
func (s *fnState) assignConvert(v value, T types2.Type, pos ast.Node) value {
	T = unwrap(T)
	if isInterface(T) && !isInterface(v.T) {
		return s.toInterface(v, v.T, T)
	}
	if isInterface(T) && isInterface(v.T) && !types2.Identical(v.T, T) {
		return s.ifaceToIface(v, T, pos)
	}
	if b, ok := v.T.(*types2.Basic); ok && b.Kind() == types2.UntypedNil && s.l.scalarType(T) == nil {
		// nil for a slice is a whole aggregate, not a one-word zero.
		return s.zeroValue(T)
	}
	if v.T != nil && !v.addr {
		if vt, tt := s.l.scalarType(v.T), s.l.scalarType(T); vt != nil && tt != nil && (vt.Kind != tt.Kind || vt.Size != tt.Size) {
			// Untyped values reaching a typed slot (nil to a pointer, an
			// untyped bool result to a named bool) keep their bits.
			v.v = s.convertScalar(v.v, v.T, T)
		}
	}
	v.T = T
	return v
}

// indexValue lowers an index expression and widens it to int.
func (s *fnState) indexValue(e ast.Expr) *ssa.Value {
	v := s.expr(e)
	if v.v.Type.Size < 8 || isUnsigned(v.T) && v.v.Type.Size == 8 {
		return s.convertScalar(v.v, v.T, types2.Typ[types2.Int])
	}
	return v.v
}

// ----------------------------------------------------------------------------
// Addressable expressions

// lv returns the storage location an addressable expression denotes.
func (s *fnState) lv(e ast.Expr) lvalue {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return s.lv(e.X)
	case *ast.Ident:
		obj, _ := s.info.Uses[e].(*types2.Var)
		if obj == nil {
			obj, _ = s.info.Defs[e].(*types2.Var)
		}
		if obj == nil {
			s.l.internalf(e.Pos(), "%s is not a variable", e.Name)
			return lvalue{ptr: s.temp(types2.Typ[types2.Int], "bad"), T: types2.Typ[types2.Int]}
		}
		return s.varLV(obj, e)
	case *ast.StarExpr:
		p := s.expr(e.X)
		pt := s.typeOf(e.X).Underlying()
		var base types2.Type
		switch pt := pt.(type) {
		case *types2.Pointer:
			base = pt.Elem()
		case *types2.Wrapper:
			base = pt.Elem()
		}
		return lvalue{ptr: p.v, T: base}
	case *ast.SelectorExpr:
		return s.selectorLV(e)
	case *ast.IndexExpr:
		return s.indexLV(e)
	case *ast.CompositeLit:
		T := s.typeOf(e)
		if s.staticLits {
			// &T{...} during package initialisation: the object outlives
			// the init function, so it gets static storage.
			sym := s.l.staticObject(T)
			s.assign(lvalue{ptr: s.symAddr(sym), T: T}, s.compositeLit(e, T), true)
			return lvalue{ptr: s.symAddr(sym), T: T}
		}
		if s.l.counted(T) {
			// &T{...} may be used until the function returns, so the
			// literal lives in a frame slot that holds its references.
			slot := s.frameSlot(T, "lit")
			s.storeCounted(lvalue{ptr: slot, T: T}, s.compositeLit(e, T), false)
			return lvalue{ptr: slot, T: T, stable: true}
		}
		v := s.materialize(s.compositeLit(e, T))
		if !v.addr {
			tmp := s.temp(T, "lit")
			s.storeTo(tmp, v, T)
			return lvalue{ptr: tmp, T: T}
		}
		return lvalue{ptr: v.v, T: T}
	}
	// Not addressable: evaluate into a temporary.
	T := s.typeOf(e)
	v := s.expr(e)
	if s.l.counted(T) {
		// Code may change the temporary through its address (a method
		// with a pointer receiver), so it must hold references of its
		// own.
		v = s.holdOwned(v)
		return lvalue{ptr: v.v, T: T, stable: true}
	}
	if v.addr {
		return lvalue{ptr: v.v, T: T}
	}
	tmp := s.temp(T, "tmp")
	s.storeTo(tmp, v, T)
	return lvalue{ptr: tmp, T: T}
}

// varLV returns the storage of a variable: a local, a captured variable or
// a package-level variable.
func (s *fnState) varLV(obj *types2.Var, at ast.Node) lvalue {
	T := s.subst(obj.Type())
	if addr := s.vars[obj]; addr != nil {
		return lvalue{ptr: addr, T: T, stable: s.stableVar(obj)}
	}
	if obj.Pkg() != nil && obj.Pkg().Scope().Lookup(obj.Name()) == obj {
		return lvalue{ptr: s.symAddr(s.l.globalSym(obj)), T: T}
	}
	// A local seen before its declaration was lowered cannot happen in
	// checked code, but give it storage rather than crash.
	s.l.internalf(at.Pos(), "variable %s has no storage", obj.Name())
	addr := s.temp(T, obj.Name())
	s.vars[obj] = addr
	return lvalue{ptr: addr, T: T}
}

// declareVar gives a local variable fresh, zeroed storage.
func (s *fnState) declareVar(obj *types2.Var) *ssa.Value {
	T := s.subst(obj.Type())
	addr := s.temp(T, obj.Name())
	s.vars[obj] = addr
	if s.l.counted(T) {
		// Released when its scope ends (strscope.go). A goto back over
		// the declaration runs it again without leaving the scope: what
		// the variable held is released first.
		s.addVar(addr, T)
		s.ownedVars[obj] = true
		if len(s.labelScopes) > 0 {
			s.releaseAt(addr, T)
			s.zero(addr, s.l.sizeof(T))
		}
	}
	return addr
}

// indexLV returns the element an index expression on an array, pointer to
// array or slice denotes, after a bounds check.
func (s *fnState) indexLV(e *ast.IndexExpr) lvalue {
	xt := s.typeOf(e.X)
	u := unwrap(xt).Underlying()
	if p, ok := u.(*types2.Pointer); ok {
		u = unwrap(p.Elem()).Underlying()
	}
	switch u := u.(type) {
	case *types2.Array:
		var base *ssa.Value
		stable := false
		if _, isPtr := unwrap(xt).Underlying().(*types2.Pointer); isPtr {
			base = s.scalar(e.X)
		} else {
			lv := s.lv(e.X)
			base, stable = lv.ptr, lv.stable
		}
		i := s.indexValue(e.Index)
		if c, ok := constIndex(s.info, e.Index); !ok || c >= u.Len() {
			s.boundsCheck(i, s.constInt(ssa.TI64, u.Len()), e.Index.Pos())
		}
		return lvalue{ptr: s.elemAddr(base, i, u.Elem()), T: s.subst(u.Elem()), stable: stable}
	case *types2.Slice:
		sl := s.expr(e.X)
		ptr := s.load(ssa.TPtr, sl.v)
		n := s.load(ssa.TI64, s.offPtr(sl.v, 8))
		i := s.indexValue(e.Index)
		s.boundsCheck(i, n, e.Index.Pos())
		return lvalue{ptr: s.elemAddr(ptr, i, u.Elem()), T: s.subst(u.Elem())}
	}
	s.l.internalf(e.Pos(), "cannot index %s", xt)
	return lvalue{ptr: s.temp(types2.Typ[types2.Int], "bad"), T: types2.Typ[types2.Int]}
}

func constIndex(info *types2.Info, e ast.Expr) (int64, bool) {
	if tv := info.Types[e]; tv.Value != nil {
		if c, ok := constant.Int64Val(constant.ToInt(tv.Value)); ok {
			return c, true
		}
	}
	return 0, false
}

// elemAddr computes base + i*sizeof(elem).
func (s *fnState) elemAddr(base, i *ssa.Value, elem types2.Type) *ssa.Value {
	size := s.l.sizeof(s.subst(elem))
	if size == 0 {
		return base
	}
	off := i
	if size != 1 {
		off = s.newValue(ssa.OpMul, ssa.TI64, i, s.constInt(ssa.TI64, size))
	}
	return s.newValue(ssa.OpAddPtr, ssa.TPtr, base, off)
}

// boundsCheck panics unless 0 <= i < n (as one unsigned comparison).
func (s *fnState) boundsCheck(i, n *ssa.Value, pos token.Pos) {
	if s.f.Pragmas&ssa.PragmaNoBounds != 0 {
		return
	}
	ok := s.newValue(ssa.OpLessu, ssa.TBool, i, n)
	s.panicUnless(ok, pos, func() {
		s.callRuntimeNoReturn("panicIndex", i, n)
	})
}

// panicUnless branches to a panic path when cond is false.
func (s *fnState) panicUnless(cond *ssa.Value, pos token.Pos, raise func()) {
	cont := s.newBlock(ssa.BlockInvalid)
	fail := s.newBlock(ssa.BlockInvalid)
	b := s.b
	s.endIf(cond, cont, fail)
	b.Likely = ssa.Likely
	s.seal(fail)
	s.startBlock(fail)
	fail.Pos = pos
	if pos.IsValid() {
		defer s.f.SetPos(s.f.SetPos(pos))
	}
	raise()
	s.seal(cont)
	s.startBlock(cont)
}

// ----------------------------------------------------------------------------
// Selectors

// selectorLV returns the storage a field selector denotes.
func (s *fnState) selectorLV(e *ast.SelectorExpr) lvalue {
	if id, ok := e.X.(*ast.Ident); ok {
		if _, isPkg := s.info.Uses[id].(*types2.PkgName); isPkg {
			obj := s.info.Uses[e.Sel].(*types2.Var)
			return s.varLV(obj, e)
		}
	}
	sel := s.info.Selections[e]
	if sel == nil || sel.Kind() != types2.FieldVal {
		s.l.internalf(e.Pos(), "selector %s is not a field", e.Sel.Name)
		return lvalue{ptr: s.temp(types2.Typ[types2.Int], "bad"), T: types2.Typ[types2.Int]}
	}
	// Start from the addressable base, or a pointer value.
	var cur lvalue
	xt := s.typeOf(e.X)
	if isPointerLike(xt) {
		cur = lvalue{ptr: s.scalar(e.X), T: pointerElem(xt)}
	} else {
		cur = s.lv(e.X)
	}
	return s.fieldPath(cur, sel.Index())
}

// fieldPath follows a field index path, dereferencing embedded pointers.
func (s *fnState) fieldPath(cur lvalue, path []int) lvalue {
	for n, idx := range path {
		if cur.bits != nil {
			// A field of a packed field (a flag group or nested packed type).
			cur = s.packedField(cur, cur.bits.T.Underlying().(*types2.Packed), idx)
			continue
		}
		if n > 0 && isPointerLike(unwrap(cur.T)) {
			cur = lvalue{ptr: s.loadLV(cur).v, T: pointerElem(unwrap(cur.T))}
		}
		switch u := unwrap(cur.T).Underlying().(type) {
		case *types2.Struct:
			fields := make([]*types2.Var, u.NumFields())
			for i := range fields {
				fields[i] = u.Field(i)
			}
			off := s.l.sizes.Offsetsof(fields)[idx]
			cur = lvalue{ptr: s.offPtr(cur.ptr, off), T: s.subst(u.Field(idx).Type()), stable: cur.stable}
		case *types2.Packed:
			cur = s.packedField(cur, u, idx)
		default:
			if cur.bits != nil {
				if p, ok := cur.bits.T.Underlying().(*types2.Packed); ok {
					cur = s.packedField(cur, p, idx)
					continue
				}
			}
			panic("lower: field of non-struct " + cur.T.String())
		}
	}
	return cur
}

func isPointerLike(T types2.Type) bool {
	switch u := T.Underlying().(type) {
	case *types2.Pointer:
		return true
	case *types2.Wrapper:
		return u.IsPointer()
	}
	return false
}

func pointerElem(T types2.Type) types2.Type {
	switch u := T.Underlying().(type) {
	case *types2.Pointer:
		return u.Elem()
	case *types2.Wrapper:
		return u.Elem()
	}
	return nil
}

// selector lowers a selector used as a value.
func (s *fnState) selector(e *ast.SelectorExpr, T types2.Type) value {
	if id, ok := e.X.(*ast.Ident); ok {
		if _, isPkg := s.info.Uses[id].(*types2.PkgName); isPkg {
			switch obj := s.info.Uses[e.Sel].(type) {
			case *types2.Var:
				return s.loadLV(s.varLV(obj, e))
			case *types2.Func:
				return scalarValue(s.funcValue(obj, s.instanceArgs(e.Sel), e.Pos()), T)
			}
		}
	}
	sel := s.info.Selections[e]
	if sel == nil {
		s.l.internalf(e.Pos(), "unresolved selector %s", e.Sel.Name)
		return s.zeroValue(T)
	}
	switch sel.Kind() {
	case types2.FieldVal:
		xt := s.typeOf(e.X)
		if !isPointerLike(xt) && !isAddressable(e.X, s.info) {
			// A field of a value: if the value is held in a register (a
			// small packed value), extract from it directly.
			x := s.expr(e.X)
			if !x.addr {
				if p, ok := unwrap(xt).Underlying().(*types2.Packed); ok && len(sel.Index()) == 1 {
					return scalarValue(s.bitsOfValue(x.v, p, sel.Index()[0]), T)
				}
				tmp := s.temp(xt, "tmp")
				s.storeTo(tmp, x, xt)
				return s.loadLV(s.fieldPath(lvalue{ptr: tmp, T: xt}, sel.Index()))
			}
			// A field of a temporary or of a stable value is borrowed from
			// it, and stays valid as long as it does.
			return s.loadLV(s.fieldPath(lvalue{ptr: x.v, T: xt, stable: stableFrom(x) != cntUnstable}, sel.Index()))
		}
		return s.loadLV(s.selectorLV(e))
	case types2.MethodVal:
		return scalarValue(s.methodValue(e, sel), T)
	case types2.MethodExpr:
		sym := s.methodExpr(e, sel, T.Underlying().(*types2.Signature))
		if sym == nil {
			return s.zeroValue(T)
		}
		return scalarValue(s.symAddr(s.l.closureRecord(sym)), T)
	}
	return s.zeroValue(T)
}

// isAddressable reports whether e denotes a variable.
func isAddressable(e ast.Expr, info *types2.Info) bool {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return isAddressable(e.X, info)
	case *ast.Ident:
		_, ok := info.Uses[e].(*types2.Var)
		return ok
	case *ast.StarExpr:
		return true
	case *ast.SelectorExpr:
		if sel := info.Selections[e]; sel != nil && sel.Kind() == types2.FieldVal {
			if _, isPtr := info.Types[e.X].Type.Underlying().(*types2.Pointer); isPtr {
				return true
			}
			return isAddressable(e.X, info)
		}
		if id, ok := e.X.(*ast.Ident); ok {
			if _, isPkg := info.Uses[id].(*types2.PkgName); isPkg {
				_, ok := info.Uses[e.Sel].(*types2.Var)
				return ok
			}
		}
	case *ast.IndexExpr:
		t := info.Types[e.X].Type
		if t == nil {
			return false
		}
		switch u := t.Underlying().(type) {
		case *types2.Slice, *types2.Pointer:
			return true
		case *types2.Array:
			return isAddressable(e.X, info)
		case *types2.Wrapper:
			_ = u
			return isAddressable(e.X, info)
		}
	}
	return false
}

// ----------------------------------------------------------------------------
// Unary and binary operators

func (s *fnState) unary(e *ast.UnaryExpr, T types2.Type) value {
	switch e.Op {
	case token.AND:
		return scalarValue(s.absAddr(s.lv(e.X).ptr), T)
	case token.ADD:
		return s.expr(e.X)
	case token.SUB:
		x := s.scalar(e.X)
		if x.Type.IsFloat() {
			return scalarValue(s.newValue(ssa.OpFNeg, x.Type, x), T)
		}
		return scalarValue(s.newValue(ssa.OpNeg, x.Type, x), T)
	case token.XOR:
		x := s.scalar(e.X)
		return scalarValue(s.newValue(ssa.OpCom, x.Type, x), T)
	case token.NOT:
		return scalarValue(s.newValue(ssa.OpNot, ssa.TBool, s.scalar(e.X)), T)
	}
	s.l.unsupportedf(e.Pos(), "unsupported unary operator %s", e.Op)
	return s.zeroValue(T)
}

func (s *fnState) binary(e *ast.BinaryExpr, T types2.Type) value {
	switch e.Op {
	case token.LAND, token.LOR:
		return scalarValue(s.shortCircuit(e), T)
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return scalarValue(s.compare(e.Op, e.X, e.Y, e.Pos()), T)
	case token.SHL, token.SHR:
		x := s.scalar(e.X)
		y := s.expr(e.Y)
		return scalarValue(s.shift(e.Op, x, unwrap(s.typeOf(e.X)), y.v, y.T, e.Pos()), T)
	}
	opT := unwrap(T)
	if isString(opT) && e.Op == token.ADD {
		return s.concat(e, T)
	}
	x := s.exprAs(e.X, opT).v
	y := s.exprAs(e.Y, opT).v
	return scalarValue(s.arith(e.Op, x, y, opT, e.Pos()), T)
}

// arith emits a binary arithmetic operation on scalars of type T.
func (s *fnState) arith(op token.Token, x, y *ssa.Value, T types2.Type, pos token.Pos) *ssa.Value {
	t := x.Type
	if t.IsFloat() {
		var fop ssa.Op
		switch op {
		case token.ADD:
			fop = ssa.OpFAdd
		case token.SUB:
			fop = ssa.OpFSub
		case token.MUL:
			fop = ssa.OpFMul
		case token.QUO:
			fop = ssa.OpFDiv
		default:
			s.l.internalf(pos, "bad float operator %s", op)
			return x
		}
		return s.newValue(fop, t, x, y)
	}
	unsigned := isUnsigned(T)
	var iop ssa.Op
	switch op {
	case token.ADD:
		iop = ssa.OpAdd
	case token.SUB:
		iop = ssa.OpSub
	case token.MUL:
		iop = ssa.OpMul
	case token.AND:
		iop = ssa.OpAnd
	case token.OR:
		iop = ssa.OpOr
	case token.XOR:
		iop = ssa.OpXor
	case token.AND_NOT:
		iop = ssa.OpAndNot
	case token.QUO, token.REM:
		s.divCheck(y, pos)
		switch {
		case op == token.QUO && unsigned:
			iop = ssa.OpDivu
		case op == token.QUO:
			iop = ssa.OpDiv
		case unsigned:
			iop = ssa.OpModu
		default:
			iop = ssa.OpMod
		}
		if !unsigned && (y.Op != ssa.OpConst || y.AuxInt == -1) {
			return s.signedDiv(iop, x, y)
		}
	default:
		s.l.internalf(pos, "bad integer operator %s", op)
		return x
	}
	if t.IsPtr() {
		// uintptr arithmetic on values typed as pointers does not occur;
		// unsafe.Pointer has no arithmetic.
		t = ssa.TI64
	}
	return s.newValue(iop, t, x, y)
}

// signedDiv emits a signed division or remainder with Go's semantics for
// a divisor of -1: x / -1 is -x (wrapping) and x % -1 is 0. The hardware
// faults on MinInt / -1, so -1 takes a separate path.
func (s *fnState) signedDiv(op ssa.Op, x, y *ssa.Value) *ssa.Value {
	t := x.Type
	res := s.tempSize(t.Size, t.Size, "div")
	isNegOne := s.newValue(ssa.OpEq, ssa.TBool, y, s.constInt(t, -1))
	neg := s.newBlock(ssa.BlockInvalid)
	normal := s.newBlock(ssa.BlockInvalid)
	done := s.newBlock(ssa.BlockInvalid)
	b := s.b
	s.endIf(isNegOne, neg, normal)
	b.Likely = ssa.Unlikely
	s.seal(neg)
	s.startBlock(neg)
	if op == ssa.OpDiv {
		s.store(t, res, s.newValue(ssa.OpNeg, t, x))
	} else {
		s.store(t, res, s.constInt(t, 0))
	}
	s.endPlain(done)
	s.seal(normal)
	s.startBlock(normal)
	s.store(t, res, s.newValue(op, t, x, y))
	s.endPlain(done)
	s.seal(done)
	s.startBlock(done)
	return s.load(t, res)
}

// divCheck panics on integer division by zero.
func (s *fnState) divCheck(y *ssa.Value, pos token.Pos) {
	if y.Op == ssa.OpConst && y.AuxInt != 0 {
		return
	}
	ok := s.newValue(ssa.OpNe, ssa.TBool, y, s.constInt(y.Type, 0))
	s.panicUnless(ok, pos, func() { s.callRuntimeNoReturn("panicDivide") })
}

// shift implements Go's shift semantics: shifting by at least the operand
// width gives 0 (or the sign for signed right shifts). A negative count
// panics.
func (s *fnState) shift(op token.Token, x *ssa.Value, xT types2.Type, count *ssa.Value, countT types2.Type, pos token.Pos) *ssa.Value {
	if isSigned(countT) && (count.Op != ssa.OpConst || count.AuxInt < 0) {
		ok := s.newValue(ssa.OpLeq, ssa.TBool, s.constInt(count.Type, 0), count)
		s.panicUnless(ok, pos, func() { s.callRuntimeNoReturn("panicShift") })
	}
	if count.Type.Size < 8 {
		count = s.newValue(ssa.OpZeroExt, ssa.TI64, count)
	}
	var sop ssa.Op
	switch {
	case op == token.SHL:
		sop = ssa.OpShl
	case isSigned(xT):
		sop = ssa.OpShrA
	default:
		sop = ssa.OpShrL
	}
	return s.newValue(sop, x.Type, x, count)
}

func (s *fnState) shortCircuit(e *ast.BinaryExpr) *ssa.Value {
	res := s.temp(types2.Typ[types2.Bool], "cond")
	x := s.scalar(e.X)
	s.store(ssa.TBool, res, x)
	rhs := s.newBlock(ssa.BlockInvalid)
	done := s.newBlock(ssa.BlockInvalid)
	if e.Op == token.LAND {
		s.endIf(x, rhs, done)
	} else {
		s.endIf(x, done, rhs)
	}
	s.seal(rhs)
	s.startBlock(rhs)
	s.store(ssa.TBool, res, s.scalar(e.Y))
	s.endPlain(done)
	s.seal(done)
	s.startBlock(done)
	return s.load(ssa.TBool, res)
}

// compare lowers a comparison.
func (s *fnState) compare(op token.Token, xe, ye ast.Expr, pos token.Pos) *ssa.Value {
	// Comparison with nil tests the value's first word: the data pointer
	// of a slice, the table of an interface, the pointer itself otherwise.
	if s.info.Types[ye].IsNil() || s.info.Types[xe].IsNil() {
		e := xe
		if s.info.Types[xe].IsNil() {
			e = ye
		}
		v := s.expr(e)
		w := v.v
		if v.addr {
			w = s.load(ssa.TPtr, v.v)
		}
		eq := s.newValue(ssa.OpEq, ssa.TBool, w, s.constInt(ssa.TPtr, 0))
		if op == token.NEQ {
			return s.newValue(ssa.OpNot, ssa.TBool, eq)
		}
		return eq
	}
	xT := unwrap(s.typeOf(xe))
	yT := unwrap(s.typeOf(ye))
	// Compare in the type of the typed operand.
	T := xT
	if b, ok := xT.(*types2.Basic); ok && b.Info()&types2.IsUntyped != 0 {
		T = yT
	}
	if isInterface(xT) != isInterface(yT) {
		// Comparing an interface with a concrete value: convert the
		// concrete side.
		if isInterface(xT) {
			T = xT
		} else {
			T = yT
		}
	}
	x := s.holdAcross(s.exprAs(xe, T), ye)
	y := s.exprAs(ye, T)
	eq := s.equal(x, y, T, pos)
	switch op {
	case token.EQL:
		return eq
	case token.NEQ:
		return s.newValue(ssa.OpNot, ssa.TBool, eq)
	}
	return s.order(op, x.v, y.v, T, pos)
}

// order lowers <, <=, > and >= on scalars and strings.
func (s *fnState) order(op token.Token, x, y *ssa.Value, T types2.Type, pos token.Pos) *ssa.Value {
	if op == token.GTR || op == token.GEQ {
		x, y = y, x
		if op == token.GTR {
			op = token.LSS
		} else {
			op = token.LEQ
		}
	}
	if isString(T) {
		c := s.callRuntime("strcmp", []*ssa.Type{ssa.TI64}, s.stringLeaves(value{v: x, addr: true, T: T}), s.stringLeaves(value{v: y, addr: true, T: T}))[0]
		zero := s.constInt(ssa.TI64, 0)
		if op == token.LSS {
			return s.newValue(ssa.OpLess, ssa.TBool, c, zero)
		}
		return s.newValue(ssa.OpLeq, ssa.TBool, c, zero)
	}
	t := x.Type
	var o ssa.Op
	switch {
	case t.IsFloat() && op == token.LSS:
		o = ssa.OpFLess
	case t.IsFloat():
		o = ssa.OpFLeq
	case isUnsigned(T) && op == token.LSS:
		o = ssa.OpLessu
	case isUnsigned(T):
		o = ssa.OpLequ
	case op == token.LSS:
		o = ssa.OpLess
	default:
		o = ssa.OpLeq
	}
	return s.newValue(o, ssa.TBool, x, y)
}

// stringLeaves returns the three words of a string value, as a string
// argument of a runtime function takes them.
func (s *fnState) stringLeaves(v value) []*ssa.Value {
	return s.leafValues(v, []leaf{{0, ssa.TPtr}, {8, ssa.TI64}, {strCtlOff, ssa.TI64}})
}

// equal compares two values of type T for equality.
func (s *fnState) equal(x, y value, T types2.Type, pos token.Pos) *ssa.Value {
	if !x.addr && !y.addr {
		if x.v.Type.IsFloat() {
			return s.newValue(ssa.OpFEq, ssa.TBool, x.v, y.v)
		}
		yv := y.v
		if !yv.Type.Same(x.v.Type) {
			yv = s.convertScalar(yv, y.T, T)
		}
		return s.newValue(ssa.OpEq, ssa.TBool, x.v, yv)
	}
	switch u := unwrap(T).Underlying().(type) {
	case *types2.Basic: // string
		return s.callRuntime("strequal", []*ssa.Type{ssa.TBool}, s.stringLeaves(x), s.stringLeaves(y))[0]
	case *types2.Interface:
		return s.ifaceEqual(x, y, T)
	case *types2.Struct:
		fields := make([]*types2.Var, u.NumFields())
		for i := range fields {
			fields[i] = u.Field(i)
		}
		offs := s.l.sizes.Offsetsof(fields)
		result := s.constBool(true)
		for i, f := range fields {
			if f.Name() == "_" {
				continue
			}
			ft := s.subst(f.Type())
			fx := s.loadFrom(s.offPtr(x.v, offs[i]), ft)
			fy := s.loadFrom(s.offPtr(y.v, offs[i]), ft)
			result = s.newValue(ssa.OpAnd, ssa.TBool, result, s.equal(fx, fy, ft, pos))
		}
		return result
	case *types2.Array:
		et := s.subst(u.Elem())
		esize := s.l.sizeof(et)
		if u.Len() <= 8 {
			result := s.constBool(true)
			for i := int64(0); i < u.Len(); i++ {
				fx := s.loadFrom(s.offPtr(x.v, i*esize), et)
				fy := s.loadFrom(s.offPtr(y.v, i*esize), et)
				result = s.newValue(ssa.OpAnd, ssa.TBool, result, s.equal(fx, fy, et, pos))
			}
			return result
		}
		if isPlainBytes(et) {
			return s.callRuntime("memequal", []*ssa.Type{ssa.TBool}, []*ssa.Value{x.v, y.v, s.constInt(ssa.TI64, u.Len()*esize)})[0]
		}
		return s.equalArrayLoop(x.v, y.v, u.Len(), et, pos)
	case *types2.Packed, *types2.Wrapper:
		return s.callRuntime("memequal", []*ssa.Type{ssa.TBool}, []*ssa.Value{x.v, y.v, s.constInt(ssa.TI64, s.l.sizeof(T))})[0]
	}
	s.l.unsupportedf(pos, "cannot compare values of type %s", T)
	return s.constBool(false)
}

// equalArrayLoop compares the arrays of n elements of type et at x and y
// in a loop that stops at the first unequal element.
func (s *fnState) equalArrayLoop(x, y *ssa.Value, n int64, et types2.Type, pos token.Pos) *ssa.Value {
	res := s.temp(types2.Typ[types2.Bool], "eq")
	s.store(ssa.TBool, res, s.constBool(true))
	idx := s.temp(types2.Typ[types2.Int], "i")
	s.store(ssa.TI64, idx, s.constInt(ssa.TI64, 0))
	head := s.newBlock(ssa.BlockInvalid)
	body := s.newBlock(ssa.BlockInvalid)
	next := s.newBlock(ssa.BlockInvalid)
	differ := s.newBlock(ssa.BlockInvalid)
	done := s.newBlock(ssa.BlockInvalid)
	s.endPlain(head)

	s.startBlock(head)
	i := s.load(ssa.TI64, idx)
	s.endIf(s.newValue(ssa.OpLess, ssa.TBool, i, s.constInt(ssa.TI64, n)), body, done)

	s.seal(body)
	s.startBlock(body)
	i = s.load(ssa.TI64, idx)
	ex := s.loadFrom(s.elemAddr(x, i, et), et)
	ey := s.loadFrom(s.elemAddr(y, i, et), et)
	s.endIf(s.equal(ex, ey, et, pos), next, differ)

	s.seal(next)
	s.startBlock(next)
	s.store(ssa.TI64, idx, s.newValue(ssa.OpAdd, ssa.TI64, i, s.constInt(ssa.TI64, 1)))
	s.endPlain(head)
	s.seal(head)

	s.seal(differ)
	s.startBlock(differ)
	s.store(ssa.TBool, res, s.constBool(false))
	s.endPlain(done)

	s.seal(done)
	s.startBlock(done)
	return s.load(ssa.TBool, res)
}

// isPlainBytes reports whether equality of T values is equality of their bytes.
func isPlainBytes(T types2.Type) bool {
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		return u.Info()&(types2.IsInteger|types2.IsBoolean) != 0
	case *types2.Pointer:
		return true
	case *types2.Array:
		return isPlainBytes(u.Elem())
	}
	return false
}

// ----------------------------------------------------------------------------
// Strings and slices

// stringParts returns a string value's data pointer and length.
func (s *fnState) stringParts(v value) (*ssa.Value, *ssa.Value) {
	return s.load(ssa.TPtr, v.v), s.load(ssa.TI64, s.offPtr(v.v, 8))
}

// stringCtl returns a string value's control word (strrep.go).
func (s *fnState) stringCtl(v value) *ssa.Value {
	return s.load(ssa.TI64, s.offPtr(v.v, strCtlOff))
}

// makeString builds a string value from a pointer, a length and a control
// word.
func (s *fnState) makeString(ptr, n, ctl *ssa.Value, T types2.Type) value {
	tmp := s.temp(types2.Typ[types2.String], "str")
	s.store(ssa.TPtr, tmp, ptr)
	s.store(ssa.TI64, s.offPtr(tmp, 8), n)
	s.store(ssa.TI64, s.offPtr(tmp, strCtlOff), ctl)
	return aggValue(tmp, T)
}

// makeSlice builds a slice value.
func (s *fnState) makeSlice(ptr, n, c *ssa.Value, T types2.Type) value {
	tmp := s.temp(T, "slice")
	s.store(ssa.TPtr, tmp, ptr)
	s.store(ssa.TI64, s.offPtr(tmp, 8), n)
	s.store(ssa.TI64, s.offPtr(tmp, 16), c)
	return aggValue(tmp, T)
}

// sliceParts returns a slice value's pointer, length and capacity.
func (s *fnState) sliceParts(v value) (ptr, n, c *ssa.Value) {
	return s.load(ssa.TPtr, v.v), s.load(ssa.TI64, s.offPtr(v.v, 8)), s.load(ssa.TI64, s.offPtr(v.v, 16))
}

func (s *fnState) sliceExpr(e *ast.SliceExpr, T types2.Type) value {
	xt := unwrap(s.typeOf(e.X))
	var base, length, capacity, ctl *ssa.Value
	var elem types2.Type
	var str value
	isStr := false
	switch u := xt.Underlying().(type) {
	case *types2.Basic:
		str = s.holdAcross(s.expr(e.X), e.Low, e.High, e.Max)
		base, length = s.stringParts(str)
		ctl = s.stringCtl(str)
		capacity = length
		elem = types2.Typ[types2.Byte]
		isStr = true
	case *types2.Slice:
		sl := s.expr(e.X)
		base, length, capacity = s.sliceParts(sl)
		elem = u.Elem()
	case *types2.Array:
		base = s.absAddr(s.lv(e.X).ptr)
		length = s.constInt(ssa.TI64, u.Len())
		capacity = length
		elem = u.Elem()
	case *types2.Pointer:
		arr := unwrap(u.Elem()).Underlying().(*types2.Array)
		base = s.scalar(e.X)
		length = s.constInt(ssa.TI64, arr.Len())
		capacity = length
		elem = arr.Elem()
	default:
		s.l.internalf(e.Pos(), "cannot slice %s", xt)
		return s.zeroValue(T)
	}
	lo := s.constInt(ssa.TI64, 0)
	if e.Low != nil {
		lo = s.indexValue(e.Low)
	}
	hi := length
	if e.High != nil {
		hi = s.indexValue(e.High)
	}
	mx := capacity
	if e.Max != nil {
		mx = s.indexValue(e.Max)
	}
	// 0 <= lo <= hi <= max <= cap
	if s.f.Pragmas&ssa.PragmaNoBounds == 0 {
		ok := s.newValue(ssa.OpLequ, ssa.TBool, mx, capacity)
		ok = s.newValue(ssa.OpAnd, ssa.TBool, ok, s.newValue(ssa.OpLequ, ssa.TBool, hi, mx))
		ok = s.newValue(ssa.OpAnd, ssa.TBool, ok, s.newValue(ssa.OpLequ, ssa.TBool, lo, hi))
		s.panicUnless(ok, e.Pos(), func() { s.callRuntimeNoReturn("panicSlice", lo, hi, mx) })
	}
	ptr := s.elemAddr(base, lo, elem)
	n := s.newValue(ssa.OpSub, ssa.TI64, hi, lo)
	if isStr {
		// A substring shares the buffer and its count: it is a view
		// derived from str, kept alive by whatever keeps str alive.
		sub := s.makeString(ptr, n, ctl, T)
		sub.cnt, sub.tmp = str.cnt, str.tmp
		return sub
	}
	c := s.newValue(ssa.OpSub, ssa.TI64, mx, lo)
	return s.makeSlice(ptr, n, c, T)
}

// ----------------------------------------------------------------------------
// Composite literals

func (s *fnState) compositeLit(e *ast.CompositeLit, T types2.Type) value {
	T = unwrap(T)
	u := T.Underlying()
	if p, ok := u.(*types2.Pointer); ok {
		// &T{...} elided in a nested literal: the element is a pointer.
		elem := unwrap(s.subst(p.Elem()))
		v := s.compositeLit(e, elem)
		if s.l.counted(elem) {
			// As &T{...}: storage that lives as long as the pointer may.
			var slot *ssa.Value
			if s.staticLits {
				slot = s.symAddr(s.l.staticObject(elem))
			} else {
				slot = s.frameSlot(elem, "lit")
			}
			s.storeCounted(lvalue{ptr: slot, T: elem}, v, s.staticLits)
			return scalarValue(slot, T)
		}
		v = s.materialize(v)
		if !v.addr {
			tmp := s.temp(elem, "lit")
			s.storeTo(tmp, v, elem)
			return scalarValue(tmp, T)
		}
		return scalarValue(v.v, T)
	}
	switch u := u.(type) {
	case *types2.Struct:
		tmp := s.temp(T, "lit")
		s.zero(tmp, s.l.sizeof(T))
		fields := make([]*types2.Var, u.NumFields())
		for i := range fields {
			fields[i] = u.Field(i)
		}
		offs := s.l.sizes.Offsetsof(fields)
		for i, elt := range e.Elts {
			idx := i
			val := elt
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				key := kv.Key.(*ast.Ident)
				for j, f := range fields {
					if f.Name() == key.Name {
						idx = j
						break
					}
				}
				val = kv.Value
			}
			ft := s.subst(fields[idx].Type())
			s.assign(lvalue{ptr: s.offPtr(tmp, offs[idx]), T: ft}, s.compositeElem(val, ft), true)
		}
		return s.litValue(tmp, T)

	case *types2.Array:
		tmp := s.temp(T, "lit")
		s.zero(tmp, s.l.sizeof(T))
		s.fillElems(tmp, e.Elts, s.subst(u.Elem()))
		return s.litValue(tmp, T)

	case *types2.Slice:
		// The backing array lives in this function's frame, like any
		// other temporary: Plutonium never allocates implicitly.
		et := s.subst(u.Elem())
		n := s.literalLen(e.Elts)
		arrT := types2.NewArray(et, n)
		var arr *ssa.Value
		switch {
		case s.staticLits:
			// During package initialisation the slice outlives the init
			// function, so its backing array is static.
			arr = s.symAddr(s.l.staticObject(arrT))
		case s.l.counted(et):
			// The slice may be used until the function returns: the
			// array is a frame slot that holds its elements' references,
			// dropped when the literal is evaluated again or the
			// function returns.
			arr = s.frameSlot(arrT, "lit")
			s.releaseAt(arr, arrT)
		default:
			arr = s.temp(arrT, "lit")
		}
		s.zero(arr, s.l.sizeof(arrT))
		s.fillElems(arr, e.Elts, et)
		nv := s.constInt(ssa.TI64, n)
		return s.makeSlice(arr, nv, nv, T)

	case *types2.Packed:
		tmp := s.temp(T, "lit")
		s.zero(tmp, s.l.sizeof(T))
		lv := lvalue{ptr: tmp, T: T}
		for _, elt := range e.Elts {
			kv := elt.(*ast.KeyValueExpr)
			key := kv.Key.(*ast.Ident)
			for j := 0; j < u.NumFields(); j++ {
				if u.Field(j).Var().Name() == key.Name {
					fv := s.exprAs(kv.Value, u.Field(j).Var().Type())
					s.storeLV(s.packedField(lv, u, j), fv)
					break
				}
			}
		}
		return s.loadLV(lv)

	case *types2.Wrapper:
		if u.Kind() == types2.WrapRing && len(e.Elts) == 0 {
			return s.zeroValue(T) // ring[T, N]{}: the empty ring
		}
	}
	s.l.unsupportedf(e.Pos(), "unsupported composite literal of type %s", T)
	return s.zeroValue(T)
}

// compositeElem lowers one element of a composite literal; nested literals
// may omit their type.
func (s *fnState) compositeElem(e ast.Expr, T types2.Type) value {
	if cl, ok := e.(*ast.CompositeLit); ok && cl.Type == nil {
		return s.compositeLit(cl, T)
	}
	return s.exprAs(e, T)
}

// literalLen returns the length of an array or slice literal.
func (s *fnState) literalLen(elts []ast.Expr) int64 {
	var n, max int64
	for _, elt := range elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			c, _ := constIndex(s.info, kv.Key)
			n = c
		}
		n++
		if n > max {
			max = n
		}
	}
	return max
}

func (s *fnState) fillElems(base *ssa.Value, elts []ast.Expr, et types2.Type) {
	esize := s.l.sizeof(et)
	var i int64
	for _, elt := range elts {
		val := elt
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			i, _ = constIndex(s.info, kv.Key)
			val = kv.Value
		}
		s.assign(lvalue{ptr: s.offPtr(base, i*esize), T: et}, s.compositeElem(val, et), true)
		i++
	}
}

// litValue returns a composite literal built at tmp: a statement temporary
// if it holds references.
func (s *fnState) litValue(tmp *ssa.Value, T types2.Type) value {
	if s.l.counted(T) {
		return s.ownTemp(tmp, T)
	}
	return aggValue(tmp, T)
}

// ----------------------------------------------------------------------------
// Helpers for index expressions

type indexed struct {
	x     ast.Expr
	index ast.Expr
}

func unpackIndex(e ast.Expr) indexed {
	switch e := e.(type) {
	case *ast.IndexExpr:
		return indexed{e.X, e.Index}
	case *ast.IndexListExpr:
		return indexed{e.X, nil}
	}
	return indexed{}
}

// staticFuncIdent is identOf for a callee, except that it returns nil for
// a method selection (x.m or T.m), which names a method rather than a
// function that can be called directly.
func staticFuncIdent(e ast.Expr, info *types2.Info) *ast.Ident {
	if se, ok := ast.Unparen(e).(*ast.SelectorExpr); ok && info.Selections[se] != nil {
		return nil
	}
	return identOf(e)
}

func identOf(e ast.Expr) *ast.Ident {
	switch e := e.(type) {
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	case *ast.ParenExpr:
		return identOf(e.X)
	}
	return nil
}

// instanceArgs returns the concrete type arguments of a generic function
// instance named by id, or nil.
func (s *fnState) instanceArgs(id *ast.Ident) []types2.Type {
	inst, ok := s.info.Instances[id]
	if !ok {
		return nil
	}
	var out []types2.Type
	for i := 0; i < inst.TypeArgs.Len(); i++ {
		out = append(out, s.subst(inst.TypeArgs.At(i)))
	}
	return out
}

// recvTypeArgs returns the type arguments of a generic receiver type.
func (s *fnState) recvTypeArgs(recv types2.Type) []types2.Type {
	if p, ok := recv.(*types2.Pointer); ok {
		recv = p.Elem()
	}
	n, ok := types2.Unalias(s.subst(recv)).(*types2.Named)
	if !ok || n.TypeArgs() == nil {
		return nil
	}
	var out []types2.Type
	for i := 0; i < n.TypeArgs().Len(); i++ {
		out = append(out, n.TypeArgs().At(i))
	}
	return out
}
