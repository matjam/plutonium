// Translated by go2pu from internal/lower/expr.go at the go-cutover tag; edited by hand since.

package lower

import (
	"compiler/compat"
	"compiler/internal/constant"
	"compiler/internal/ssa"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
)

// expr lowers an expression to a value.
func (s *fnState) expr(e ast.Expr) value {
	if res, ok := s.takeEvaluated(e); ok {
		// Evaluated ahead of its expression (order.go).
		if len(res) == 0 {
			return value{}
		}
		return res[0]
	}
	if p := e.Pos(); p.IsValid() {
		defer s.f.SetPos(s.f.SetPos(p))
	}
	defer s.at(e.Pos())()
	tv := s.info.Types[e]
	if !tv.Value.IsNil() {
		return s.constValue(tv.Value, s.typeOf(e), e)
	}
	T := s.typeOf(e)
	if s.isTemp(e) {
		// An owner nothing takes: destroyed when the statement ends.
		return s.loadFrom(s.tempSlot(e, T), T)
	}
	switch e.(type) {
	case *ast.CompositeLit, *ast.BinaryExpr, *ast.IndexExpr, *ast.SliceExpr:
		// Calls in the operands come first (order.go).
		s.callsFirst(e)
	}

	switch e := e.(type) {
	case *ast.ParenExpr:
		return s.expr(e.X)

	case *ast.Ident:
		switch obj := s.info.Uses[e].(type) {
		case *types2.Var:
			if s.isMove(e) {
				return s.movedOut(s.varLV(obj, e), obj)
			}
			return s.loadLV(s.varLV(obj, e))
		case *types2.Func:
			return scalarValue(s.funcValue(obj, s.instanceArgs(e), e.Pos()), T)
		case *types2.Nil:
			return s.zeroValue(T)
		}
		s.l.internalf(e.Pos(), "unexpected identifier %s", pu.Box(e.Name))
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
	// Unreachable: a basic literal is a constant, handled above; an
	// ellipsis or a type is not a value; a bad expression stops the build
	// in the checker; and the cases above cover the other expressions.
	s.l.internalf(e.Pos(), "unexpected expression %T", e)
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
	if !tv.Value.IsNil() {
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
func (s *fnState) lv(e ast.Expr) lvalue { return s.nilGuard(s.lvRaw(e)) }

// lvRaw is lv for a caller that selects a part of the storage: storage
// reached through a pointer is not yet checked for nil (nilcheck.go).
func (s *fnState) lvRaw(e ast.Expr) lvalue {
	switch e.(type) {
	case *ast.IndexExpr, *ast.CompositeLit:
		s.callsFirst(e) // order.go
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return s.lvRaw(e.X)
	case *ast.Ident:
		obj, _ := s.info.Uses[e].(*types2.Var)
		if obj == nil {
			obj, _ = s.info.Defs[e].(*types2.Var)
		}
		if obj == nil {
			s.l.internalf(e.Pos(), "%s is not a variable", pu.Box(e.Name))
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
		// *(*T)(unsafe.Pointer(&x)) reads a stable variable x as a T: it
		// is as stable as x.
		stable := false
		if amp := reinterpretedAddr(e, s.info); amp != nil {
			if v := rootVar(amp.X, s.info); v != nil && s.vars[v] != nil {
				stable = s.stableVar(v)
			}
		}
		lv := s.deref(p.v, base, e.Pos())
		lv.stable = stable
		return lv
	case *ast.SelectorExpr:
		return s.selectorRaw(e)
	case *ast.IndexExpr:
		return s.indexRaw(e)
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
		if !v.addr || s.lastingFrame != nil {
			tmp := s.lastingTemp(T, "lit")
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
	if addr := s.lookupVar(obj); addr != nil {
		return lvalue{ptr: addr, T: T, stable: s.stableVar(obj)}
	}
	if obj.Pkg() != nil && obj.Pkg().Scope().Lookup(obj.Name()) == obj {
		return lvalue{ptr: s.symAddr(s.l.globalSym(obj)), T: T}
	}
	// A local seen before its declaration was lowered cannot happen in
	// checked code, but give it storage rather than crash.
	s.l.internalf(at.Pos(), "variable %s has no storage", pu.Box(obj.Name()))
	addr := s.temp(T, obj.Name())
	s.vars[obj] = addr
	return lvalue{ptr: addr, T: T}
}

// declareVar gives a local variable its storage. An owner is registered in
// its scope, to be destroyed when the scope ends, and cleared here, so that
// the scope never destroys a value it was not given (scope.go).
func (s *fnState) declareVar(obj *types2.Var) *ssa.Value {
	if T := s.subst(obj.Type()); s.regionVars[obj] && s.l.needsDrop(T) {
		// Captured by a closure this function returns: the region owns
		// the variable and drops it, when the call runs again and when
		// the caller's frame returns, not the scope (spec section 11.5).
		// A declaration that runs again (in a loop) drops what the last
		// run left.
		addr, flag := s.regionVar(T)
		s.vars[obj] = addr
		s.ifThen(s.newValue(ssa.OpNe, ssa.TBool, s.load(ssa.TI64, flag), s.constInt(ssa.TI64, 0)), func() {
			s.dropAt(addr, T)
		})
		s.zero(addr, s.l.sizeof(T))
		s.holdInRegion(obj, flag)
		return addr
	}
	addr := s.declareView(obj)
	if T := s.subst(obj.Type()); s.l.needsDrop(T) {
		s.zero(addr, s.l.sizeof(T))
		s.registerVar(addr, T, obj)
	}
	return addr
}

// declareView gives a local variable storage that is never destroyed: a
// copy of something another value still holds.
func (s *fnState) declareView(obj *types2.Var) *ssa.Value {
	T := s.subst(obj.Type())
	if s.regionVars[obj] {
		// Captured by a closure this function returns (regions.go). The
		// region holds its strings; a declaration that runs again (in a
		// loop) releases what the last run left.
		addr, _ := s.regionVar(T)
		s.vars[obj] = addr
		if s.l.counted(T) {
			s.releaseAt(addr, T)
			s.zero(addr, s.l.sizeof(T))
		}
		return addr
	}
	addr := s.temp(T, obj.Name())
	s.vars[obj] = addr
	if s.l.counted(T) {
		// Released when its scope ends (strscope.go). A goto back over
		// the declaration runs it again without leaving the scope: what
		// the variable held is released first.
		s.addVar(addr, T, nil)
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
func (s *fnState) indexLV(e *ast.IndexExpr) lvalue { return s.nilGuard(s.indexRaw(e)) }

// indexRaw is indexLV for lvRaw.
func (s *fnState) indexRaw(e *ast.IndexExpr) lvalue {
	if s.isContainerIndex(e) {
		return s.containerLV(e)
	}
	if s.isMapIndex(e) {
		return s.mapLV(e)
	}
	xt := s.typeOf(e.X)
	u := asPointer(unwrap(xt).Underlying())
	if p, ok := u.(*types2.Pointer); ok {
		u = unwrap(p.Elem()).Underlying()
	}
	switch u := u.(type) {
	case *types2.Array:
		var arr lvalue
		if _, isPtr := asPointer(unwrap(xt).Underlying()).(*types2.Pointer); isPtr {
			arr = s.deref(s.scalar(e.X), u, e.Pos())
		} else if ix, ok := ast.Unparen(e.X).(*ast.IndexExpr); ok && (s.isMapIndex(ix) || s.isContainerIndex(ix)) {
			// An element of a map or container (m[k][i]) has no address
			// of its own (its lvalue is the lookup): index a copy, as a
			// field of one is read (selector).
			x := s.expr(e.X)
			if !x.addr {
				tmp := s.temp(xt, "tmp")
				s.storeTo(tmp, x, xt)
				arr = lvalue{ptr: tmp, T: xt}
			} else {
				arr = lvalue{ptr: x.v, T: xt, stable: stableFrom(x) != cntUnstable}
			}
		} else {
			arr = s.lvRaw(e.X)
		}
		if arr.bits != nil {
			// An array field of a packed type that is not plain memory.
			return s.packedIndex(arr, u, e)
		}
		i := s.indexValue(e.Index)
		c, isConst := constIndex(s.info, e.Index)
		if !isConst || c < 0 || c >= u.Len() {
			// Any element may be the one indexed: the whole array must
			// be within reach of a nil pointer's fault, or checked.
			arr = s.nilGuard(arr)
			s.boundsCheck(i, s.constInt(ssa.TI64, u.Len()), e.Index.Pos())
		}
		elem := s.subst(u.Elem())
		lv := lvalue{ptr: s.elemAddr(arr.ptr, i, u.Elem()), T: elem, stable: arr.stable, win: arr.win}
		if s.staticLocalAddress(arr.ptr) && (isConst && c >= 0 && c < u.Len() || s.f.Pragmas&ssa.PragmaNoBounds == 0) {
			if s.shutdownLocal == nil {
				s.shutdownLocal = map[*ssa.Value]bool{}
			}
			// This array index is constant in range or was checked above.
			// Unlike arbitrary pointer arithmetic, it stays in this slot.
			s.shutdownLocal[lv.ptr] = true
		}
		if arr.nilBase != nil {
			lv.nilBase, lv.nilOff, lv.nilPos = arr.nilBase, arr.nilOff+c*s.l.sizeof(elem), arr.nilPos
		}
		return lv
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
	if tv := info.Types[e]; !tv.Value.IsNil() {
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
func (s *fnState) selectorLV(e *ast.SelectorExpr) lvalue { return s.nilGuard(s.selectorRaw(e)) }

// selectorRaw is selectorLV for lvRaw.
func (s *fnState) selectorRaw(e *ast.SelectorExpr) lvalue {
	if id, ok := e.X.(*ast.Ident); ok {
		if _, isPkg := s.info.Uses[id].(*types2.PkgName); isPkg {
			obj := s.info.Uses[e.Sel].(*types2.Var)
			return s.varLV(obj, e)
		}
	}
	sel := s.info.Selections[e]
	if sel == nil || sel.Kind() != types2.FieldVal {
		s.l.internalf(e.Pos(), "selector %s is not a field", pu.Box(e.Sel.Name))
		return lvalue{ptr: s.temp(types2.Typ[types2.Int], "bad"), T: types2.Typ[types2.Int]}
	}
	// Start from the addressable base, or a pointer value.
	var cur lvalue
	xt := s.typeOf(e.X)
	if isPointerLike(xt) {
		cur = s.deref(s.scalar(e.X), pointerElem(xt), e.Pos())
	} else {
		cur = s.lvRaw(e.X)
	}
	return s.fieldPath(cur, sel.Index())
}

// fieldPath follows a field index path, dereferencing embedded pointers.
// The storage it returns may need nilGuard before it is used.
func (s *fnState) fieldPath(cur lvalue, path []int) lvalue {
	for n, idx := range path {
		if _, packed_ := unwrap(cur.T).Underlying().(*types2.Packed); packed_ || cur.bits != nil {
			cur = s.nilGuard(cur)
		}
		if cur.bits != nil {
			// A field of a packed field (a flag group or nested packed type).
			cur = s.packedField(cur, cur.bits.T.Underlying().(*types2.Packed), idx)
			continue
		}
		if n > 0 && isPointerLike(unwrap(cur.T)) {
			pos := cur.nilPos
			cur = s.deref(s.loadLV(cur).v, pointerElem(unwrap(cur.T)), pos)
		}
		switch u := unwrap(cur.T).Underlying().(type) {
		case *types2.Struct:
			fields := compat.Make[*types2.Var](u.NumFields())
			for i := range fields {
				fields[i] = u.Field(i)
			}
			off := s.l.sizes.Offsetsof(fields)[idx]
			next := lvalue{ptr: s.offPtr(cur.ptr, off), T: s.subst(u.Field(idx).Type()), stable: cur.stable}
			if cur.nilBase != nil {
				next.nilBase, next.nilOff, next.nilPos = cur.nilBase, cur.nilOff+off, cur.nilPos
			}
			cur = next
		case *types2.Packed:
			cur = s.packedField(cur, u, idx)
		default:
			if cur.bits != nil {
				if p, ok := cur.bits.T.Underlying().(*types2.Packed); ok {
					cur = s.packedField(cur, p, idx)
					continue
				}
			}
			panic(pu.Box(compat.Concat("lower: field of non-struct ", cur.T.String())))
		}
	}
	return cur
}

// asPointer returns *T for owned[T], shared[T] and synced[T], which index,
// slice and range like the pointer they are; otherwise it returns u.
func asPointer(u types2.Type) types2.Type {
	if w, ok := u.(*types2.Wrapper); ok && w.IsPointer() {
		return types2.NewPointer(w.Elem())
	}
	return u
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
	if v, ok := s.info.Uses[e.Sel].(*types2.Variant); ok {
		return s.variantValue(e, v, T)
	}
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
		s.l.internalf(e.Pos(), "unresolved selector %s", pu.Box(e.Sel.Name))
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
					return s.fieldOfValue(x.v, p, sel.Index()[0])
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
		s.l.funcValues[sym] = T.Underlying().(*types2.Signature)
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
			// A field reached through a pointer or a box is part of the
			// object the pointer designates, whatever the pointer's own
			// expression is (language spec sections 11.14 and 7.10).
			if isPointerLike(info.Types[e.X].Type) {
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
		if t == nil || types2.IndexesByMethod(t) {
			return false // an element of a container is a copy (indexing.go)
		}
		switch u := t.Underlying().(type) {
		case *types2.Slice, *types2.Pointer:
			return true
		case *types2.Array:
			return isAddressable(e.X, info)
		case *types2.Wrapper:
			if u.IsPointer() {
				return true // owned, shared and synced index like a pointer
			}
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
		return scalarValue(s.placeAddr(s.lv(e.X)), T)
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
	case token.ARROW:
		v, _ := s.recv(e, false)
		return v
	}
	// Unreachable: ~ appears only in constraints, which the checker
	// keeps out of expressions.
	s.l.internalf(e.Pos(), "unary operator %s", e.Op)
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
	if s.l.scoped.Checked[e] {
		return scalarValue(s.checkedArith(e.Op, x, y, opT, e.Pos()), T)
	}
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
	} else if isInterface(xT) && !types2.Identical(xT, yT) && types2.AssignableTo(xT, yT) {
		// Comparing interfaces of two types: compare in the one the other
		// converts to. An Expr compared with a Node is compared as a Node;
		// converting the Node to an Expr would assert that it is one.
		T = yT
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
		c := s.callRuntime("strcmp", compat.Clone([]*ssa.Type{ssa.TI64}), s.stringLeaves(value{v: x, addr: true, T: T}), s.stringLeaves(value{v: y, addr: true, T: T}))[0]
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
	if mask := comparedBits(T); mask != nil {
		return s.packedEqual(x, y, T, mask)
	}
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
		return s.callRuntime("strequal", compat.Clone([]*ssa.Type{ssa.TBool}), s.stringLeaves(x), s.stringLeaves(y))[0]
	case *types2.Interface:
		return s.ifaceEqual(x, y, T)
	case *types2.Struct:
		fields := compat.Make[*types2.Var](u.NumFields())
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
			return s.callRuntime("memequal", compat.Clone([]*ssa.Type{ssa.TBool}), []*ssa.Value{x.v, y.v, s.constInt(ssa.TI64, u.Len()*esize)})[0]
		}
		return s.equalArrayLoop(x.v, y.v, u.Len(), et, pos)
	case *types2.Packed, *types2.Wrapper:
		return s.callRuntime("memequal", compat.Clone([]*ssa.Type{ssa.TBool}), []*ssa.Value{x.v, y.v, s.constInt(ssa.TI64, s.l.sizeof(T))})[0]
	case *types2.TaggedUnion:
		return s.unionEqual(x, y, unwrap(T), pos)
	}
	// Unreachable: values in registers are compared above, and the cases
	// above cover every comparable type held in memory. Slices, maps and
	// functions compare only with nil, a scalar compare, and Plutonium has
	// no complex numbers.
	s.l.internalf(pos, "comparison of values of type %s", T)
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
	var nilBase *ssa.Value // the pointer an array is reached through, if it may be nil
	var nilPos token.Pos
	var elem types2.Type
	var str value
	isStr := false
	switch u := asPointer(xt.Underlying()).(type) {
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
		// Slicing an array reached through a nil pointer panics however
		// small the array (language spec section 17.5): the slice would
		// have its length and no memory.
		lv := s.lvRaw(e.X)
		nilBase, nilPos = lv.nilBase, lv.nilPos
		base = s.placeAddr(lv)
		length = s.constInt(ssa.TI64, u.Len())
		capacity = length
		elem = u.Elem()
	case *types2.Pointer:
		arr := unwrap(u.Elem()).Underlying().(*types2.Array)
		base = s.scalar(e.X)
		if mayBeNil(base) {
			nilBase, nilPos = base, e.Pos()
		}
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
	if nilBase != nil {
		// Checked after the indexes, so that an index read through the
		// same pointer (a.d[:a.n]) shows it is not nil (opt.Prove).
		s.nilCheck(nilBase, nilPos)
	}
	// 0 <= lo <= hi <= max <= cap
	if s.f.Pragmas&ssa.PragmaNoBounds == 0 {
		ok := s.newValue(ssa.OpLequ, ssa.TBool, mx, capacity)
		ok = s.newValue(ssa.OpAnd, ssa.TBool, ok, s.newValue(ssa.OpLequ, ssa.TBool, hi, mx))
		ok = s.newValue(ssa.OpAnd, ssa.TBool, ok, s.newValue(ssa.OpLequ, ssa.TBool, lo, hi))
		s.panicUnless(ok, e.Pos(), func() { s.callRuntimeNoReturn("panicSlice", lo, hi, mx) })
	}
	if isStr && s.f.Pragmas&ssa.PragmaNoBounds == 0 && !s.bytewise && (e.Low != nil || e.High != nil) {
		s.runeBoundaryCheck(e, base, length, lo, hi, ctl)
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
	if e.Type != nil {
		if v := s.variantOf(e.Type); v != nil {
			return s.variantLit(e, v, unwrap(T))
		}
	}
	return s.compositeLitOf(e, T)
}

// compositeLitOf lowers the elements of a composite literal as a literal of
// type T.
func (s *fnState) compositeLitOf(e *ast.CompositeLit, T types2.Type) value {
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
		if !v.addr || s.lastingFrame != nil {
			tmp := s.lastingTemp(elem, "lit")
			s.storeTo(tmp, v, elem)
			return scalarValue(tmp, T)
		}
		return scalarValue(v.v, T)
	}
	switch u := u.(type) {
	case *types2.Struct:
		tmp := s.temp(T, "lit")
		s.zero(tmp, s.l.sizeof(T))
		fields := compat.Make[*types2.Var](u.NumFields())
		for i := range fields {
			fields[i] = u.Field(i)
		}
		offs := s.l.sizes.Offsetsof(fields)
		for i, elt := range e.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				ft := s.subst(fields[i].Type())
				s.assign(lvalue{ptr: s.offPtr(tmp, offs[i]), T: ft}, s.compositeElem(elt, ft), true)
				continue
			}
			off, ft := s.literalField(T, kv.Key.(*ast.Ident))
			s.assign(lvalue{ptr: s.offPtr(tmp, off), T: ft}, s.compositeElem(kv.Value, ft), true)
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
			arr = s.lastingTemp(arrT, "lit")
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

	case *types2.Map:
		return s.mapLit(e, T)
	}
	// Unreachable: the checker accepts composite literals only of the
	// types above (literals.go), and of a ring only the empty one.
	s.l.internalf(e.Pos(), "composite literal of type %s", T)
	return s.zeroValue(T)
}

// literalField returns the offset in a struct of type T, and the type, of
// the field that the key of a struct literal names: a field of T, or a
// field promoted through embedded structs, which are not pointers
// (language spec section 11.3).
func (s *fnState) literalField(T types2.Type, key *ast.Ident) (int64, types2.Type) {
	off, ft, ok := s.l.literalField(s.info, T, key)
	if !ok {
		s.l.internalf(key.Pos(), "no field %s in struct literal of type %s", pu.Box(key.Name), T)
		return 0, types2.Typ[types2.Int]
	}
	return off, ft
}

// literalField is fnState.literalField for package initialization, with
// the package's type information: ok is false if key names no field.
func (l *Lowerer) literalField(info *types2.Info, T types2.Type, key *ast.Ident) (off int64, ft types2.Type, ok bool) {
	var pkg *types2.Package
	if f, ok := info.Uses[key].(*types2.Var); ok {
		pkg = f.Pkg()
	}
	path := types2.LiteralFields(T, pkg, key.Name)
	if len(path) == 0 {
		return 0, nil, false
	}
	cur := T
	for _, f := range path {
		st, ok := unwrap(cur).Underlying().(*types2.Struct)
		if !ok {
			return 0, nil, false
		}
		fields := compat.Make[*types2.Var](st.NumFields())
		for j := range fields {
			fields[j] = st.Field(j)
			if fields[j] == f {
				off += l.sizes.Offsetsof(fields[:j+1])[j]
			}
		}
		cur = f.Type()
	}
	return off, cur, true
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
		out = compat.Append(out, s.subst(inst.TypeArgs.At(i)))
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
		out = compat.Append(out, n.TypeArgs().At(i))
	}
	return out
}
