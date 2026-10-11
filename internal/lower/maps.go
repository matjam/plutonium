package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Built-in maps (language spec section 7.10, abi.md section 1.4).
//
// A map[K]V value is a counted reference to a pluto/rt.MapTable[K, V] in a
// box with the header of a synced[T] (box.go), or nil. It is counted by the
// machinery of counted strings and boxes (strcount.go), as if it were a
// synced[rt.MapTable[K, V]]: copies retain, drops release, and the last
// release drops the table (MapTable.Drop) and frees the box. The operations
// call generic functions of pluto/rt (map.p) instantiated with K and V,
// passing the table borrowed for the call.

// mapOf returns T's map type, or nil.
func mapOf(T types2.Type) *types2.Map {
	m, _ := unwrap(T).Underlying().(*types2.Map)
	return m
}

// mapTableType returns pluto/rt.MapTable[K, V] for map type m.
func (l *Lowerer) mapTableType(m *types2.Map) types2.Type {
	return l.rtInstance("MapTable", m)
}

// mapIterType returns pluto/rt.MapIter[K, V] for map type m.
func (l *Lowerer) mapIterType(m *types2.Map) types2.Type {
	return l.rtInstance("MapIter", m)
}

// rtInstance instantiates the generic type name of pluto/rt with m's key
// and element types.
func (l *Lowerer) rtInstance(name string, m *types2.Map) types2.Type {
	key := name + "[" + typeString(m.Key()) + "," + typeString(m.Elem()) + "]"
	if T := l.mapTypes[key]; T != nil {
		return T
	}
	T, err := types2.Instantiate(l.ctxt, l.rtType(name), []types2.Type{m.Key(), m.Elem()}, false)
	if err != nil {
		panic("lower: instantiating pluto/rt." + name + ": " + err.Error())
	}
	l.mapTypes[key] = T
	return T
}

// mapBox returns the counted reference type a map value is counted as:
// synced[rt.MapTable[K, V]].
func (l *Lowerer) mapBox(m *types2.Map) *types2.Wrapper {
	return types2.NewWrapper(types2.WrapSynced, l.mapTableType(m), 0)
}

// mapCall calls the generic function name of pluto/rt (map.p),
// instantiated with m's key and element types, with args.
func (s *fnState) mapCall(name string, m *types2.Map, args []value, pos token.Pos) []value {
	fn := s.l.rtFunc(name)
	targs := []types2.Type{m.Key(), m.Elem()}
	sym := s.l.funcSym(fn, targs)
	sig := newTypeMap(s.l, fn, targs).subst(fn.Type()).(*types2.Signature)
	for i := range args {
		args[i] = s.assignConvert(args[i], sig.Params().At(i).Type(), nil)
	}
	return s.emitCall(callee{sym: sym, noReturn: s.l.isNoReturn(fn)}, sig, nil, args, pos)
}

// mapTable returns the table pointer of the map value v, borrowed, as the
// *rt.MapTable[K, V] the runtime's functions take.
func (s *fnState) mapTable(v value, m *types2.Map) value {
	return scalarValue(s.wordOf(v), types2.NewPointer(s.l.mapTableType(m)))
}

// makeMap lowers make(map[K]V), make(map[K]V, n), make(map[K]V, n, a) and
// make(map[K]V, a): an argument is a size hint if it is an integer, and an
// allocator otherwise (spec section 13.7).
func (s *fnState) makeMap(e *ast.CallExpr, T types2.Type) value {
	m := mapOf(T)
	hint := scalarValue(s.constInt(ssa.TI64, 0), types2.Typ[types2.Int])
	rest := e.Args[1:]
	if len(rest) > 0 && (isIntegerType(unwrap(s.typeOf(rest[0]))) || s.info.Types[rest[0]].Value != nil) {
		hint = scalarValue(s.indexValue(rest[0]), types2.Typ[types2.Int])
		rest = rest[1:]
	}
	var res []value
	if len(rest) > 0 {
		allocT := s.l.rtFunc("mapMakeWith").Type().(*types2.Signature).Params().At(0).Type()
		a := s.exprAs(rest[0], allocT)
		res = s.mapCall("mapMakeWith", m, []value{a, hint}, e.Pos())
	} else {
		res = s.mapCall("mapMake", m, []value{hint}, e.Pos())
	}
	return s.newMap(res, T)
}

// newMap makes the table pointer a runtime constructor returned into a map
// value of type T, which the statement holds.
func (s *fnState) newMap(res []value, T types2.Type) value {
	if len(res) != 1 {
		return s.zeroValue(T)
	}
	return s.wordTemp(res[0].v, T, "map")
}

// mapLit lowers a map composite literal: a map from the current allocator
// with room for its elements, which are then stored in order.
func (s *fnState) mapLit(e *ast.CompositeLit, T types2.Type) value {
	m := mapOf(T)
	hint := scalarValue(s.constInt(ssa.TI64, int64(len(e.Elts))), types2.Typ[types2.Int])
	mv := s.newMap(s.mapCall("mapMake", m, []value{hint}, e.Pos()), T)
	K, V := s.subst(m.Key()), s.subst(m.Elem())
	for _, el := range e.Elts {
		kv := el.(*ast.KeyValueExpr)
		k := s.materialize(s.compositeElem(kv.Key, K))
		v := s.materialize(s.compositeElem(kv.Value, V))
		s.mapCall("mapAssign", m, []value{s.mapTable(mv, m), s.hold(k), s.hold(v)}, kv.Pos())
	}
	return mv
}

// mapRef evaluates the map operand x of an index expression once, for
// m[k] as an operand or as the target of an assignment.
func (s *fnState) mapRef(x ast.Expr, at ast.Expr) *containerRef {
	T := unwrap(s.typeOf(x))
	m := mapOf(T)
	mv := s.hold(s.materialize(s.expr(x)))
	return &containerRef{T: T, at: at, x: x, mapT: m, mapV: &mv}
}

// mapLV evaluates the map and key of e, an index expression on a map, and
// returns the element as an lvalue: loading it calls mapAccess, storing it
// mapAssign.
func (s *fnState) mapLV(e *ast.IndexExpr) lvalue {
	r := s.mapRef(e.X, e)
	r.key = s.materialize(s.exprAs(e.Index, s.subst(r.mapT.Key())))
	return lvalue{T: s.subst(r.mapT.Elem()), elem: r}
}

// mapCallRef calls the runtime's function name on the map r refers to,
// with its key and the extra arguments.
func (s *fnState) mapCallRef(r *containerRef, name string, extra ...value) []value {
	args := []value{s.mapTable(*r.mapV, r.mapT), s.hold(r.key)}
	for _, v := range extra {
		args = append(args, s.hold(s.materialize(v)))
	}
	return s.mapCall(name, r.mapT, args, r.at.Pos())
}

// mapElemPtr returns the place of m[k] in the map's table, adding k if it
// is not there, for m[k] op= v and m[k]++ once v has been evaluated: as Go
// does, the operands of the right-hand side are evaluated before the
// element is read, and nothing then changes the map until it is stored.
func (s *fnState) mapElemPtr(lv lvalue) lvalue {
	res := s.mapCallRef(lv.elem, "mapAssignPtr")
	if len(res) != 1 {
		return lv
	}
	return lvalue{ptr: res[0].v, T: lv.T}
}

// isMapIndex reports whether e indexes a map.
func (s *fnState) isMapIndex(e *ast.IndexExpr) bool {
	tv, ok := s.info.Types[e.X]
	return ok && tv.Type != nil && mapOf(s.subst(tv.Type)) != nil
}

// mapLen lowers len(m).
func (s *fnState) mapLen(x ast.Expr, T types2.Type) value {
	m := mapOf(s.typeOf(x))
	v := s.materialize(s.expr(x))
	res := s.mapCall("mapLen", m, []value{s.mapTable(v, m)}, x.Pos())
	if len(res) != 1 {
		return s.zeroValue(T)
	}
	return scalarValue(res[0].v, T)
}

// mapDelete lowers delete(m, k).
func (s *fnState) mapDelete(e *ast.CallExpr) {
	m := mapOf(s.typeOf(e.Args[0]))
	mv := s.hold(s.materialize(s.expr(e.Args[0])))
	k := s.hold(s.materialize(s.exprAs(e.Args[1], s.subst(m.Key()))))
	s.mapCall("mapDelete", m, []value{s.mapTable(mv, m), k}, e.Pos())
}

// mapClear lowers clear(m): every entry is deleted, releasing what the
// keys and values hold.
func (s *fnState) mapClear(e *ast.CallExpr) {
	m := mapOf(s.typeOf(e.Args[0]))
	mv := s.materialize(s.expr(e.Args[0]))
	s.mapCall("mapClear", m, []value{s.mapTable(mv, m)}, e.Pos())
}

// rangeMap lowers a range over a map. The loop holds a reference to the
// map and walks it with an rt.MapIter, an owner declared in the loop's
// scope: dropping it on every way out of the loop ends the walk, which
// lets the table free the tables it kept for it (map.p). Each iteration
// gets copies of the next entry's key and value, with Go's guarantees for
// entries added or deleted during the loop.
func (s *fnState) rangeMap(st *ast.RangeStmt, label *types2.Label) {
	xt := unwrap(s.typeOf(st.X))
	m := mapOf(xt)
	// The map: a variable of the loop's scope holding a reference, so
	// that the loop walks the map it started with whatever the body
	// assigns.
	mapSlot := s.temp(xt, "rangemap")
	s.addVar(mapSlot, xt)
	s.assign(lvalue{ptr: mapSlot, T: xt}, s.materialize(s.expr(st.X)), true)
	mv := aggValue(mapSlot, xt)
	if t := s.l.scalarType(xt); t != nil {
		mv = scalarValue(s.load(t, mapSlot), xt)
	}

	// The walk, registered after the map so that it ends first.
	itT := s.l.mapIterType(m)
	it := s.temp(itT, "mapiter")
	s.zero(it, s.l.sizeof(itT))
	s.addVar(it, itT)
	itPtr := scalarValue(it, types2.NewPointer(itT))
	s.mapIterCall("Start", m, itT, itPtr, []value{s.mapTable(mv, m)}, st.Pos())

	keyLV := s.rangeVar(st, st.Key)
	valLV := s.rangeVar(st, st.Value)
	head := s.newBlock(ssa.BlockInvalid)
	body := s.newBlock(ssa.BlockInvalid)
	post := s.newBlock(ssa.BlockInvalid)
	done := s.newBlock(ssa.BlockInvalid)
	s.endPlain(head)
	s.startBlock(head)
	// The next entry's copies are temporaries of their own statement,
	// released (or handed to the variables) before the body runs.
	e := s.pushCleanup(st, false)
	var k, v value
	var ok *ssa.Value
	if valLV != nil {
		res := s.mapIterCall("Next", m, itT, itPtr, nil, st.Pos())
		k, v, ok = res[0], res[1], res[2].v
	} else {
		res := s.mapIterCall("NextKey", m, itT, itPtr, nil, st.Pos())
		k, ok = res[0], res[1].v
	}
	more := s.newBlock(ssa.BlockInvalid)
	exit := s.newBlock(ssa.BlockInvalid)
	s.endIf(ok, more, exit)
	s.seal(more)
	s.seal(exit)
	s.startBlock(more)
	if keyLV != nil {
		s.assign(*keyLV, s.assignConvert(k, unwrap(keyLV.T), st), false)
	}
	if valLV != nil {
		s.assign(*valLV, s.assignConvert(v, unwrap(valLV.T), st), false)
	}
	s.popCleanup(e)
	s.endPlain(body)
	// On the way out the temporaries are zero (NextKey and Next return
	// zero values at the end), so they need no release.
	s.startBlock(exit)
	s.endPlain(done)
	s.seal(body)
	s.startBlock(body)
	s.targets = append(s.targets, &target{label: label, breakTo: done, contTo: post, depth: len(s.cleanups)})
	s.loopDepth++
	s.block(st.Body, st.Body.List)
	s.loopDepth--
	s.targets = s.targets[:len(s.targets)-1]
	s.endPlain(post)
	s.seal(post)
	s.startBlock(post)
	s.endPlain(head)
	s.seal(head)
	s.seal(done)
	s.startBlock(done)
}

// mapIterCall calls method name of rt.MapIter[K, V] on the iterator at it.
func (s *fnState) mapIterCall(name string, m *types2.Map, itT types2.Type, it value, args []value, pos token.Pos) []value {
	obj, _, _ := types2.LookupFieldOrMethod(itT, true, nil, name)
	fn := obj.(*types2.Func)
	targs := []types2.Type{m.Key(), m.Elem()}
	sym := s.l.funcSym(fn, targs)
	full := s.l.methodSig(fn, targs)
	for i := range args {
		args[i] = s.assignConvert(args[i], full.Params().At(i).Type(), nil)
	}
	return s.emitCall(callee{sym: sym}, full, &it, args, pos)
}
