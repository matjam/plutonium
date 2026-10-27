// Translated by go2pu from internal/escape2/eir/rangestmt.go at the go-cutover tag; edited by hand since.

package eir

import (
	"compiler/compat"
	"compiler/internal/moves"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
)

// Range statements. A range over a function, or over a container's All
// method, is a call of the iterator with the loop body as a function of its
// own (section 12.11), as internal/lower (rangefunc.go) builds it: the body
// reaches the enclosing function's variables through its closure record,
// and a return, or a break, continue or goto to a statement outside the
// body, ends the body; after the iterator returns, the enclosing function
// takes the exit.

// rangeCtx is a range-over-func loop whose body is being built.
type rangeCtx struct {
	st     *ast.RangeStmt
	label  *types2.Label
	parent *fnb // the function the loop statement is in
	exits  []*rangeExitInfo
}

// contains reports whether pos lies in the loop's body.
func (rc *rangeCtx) contains(pos token.Pos) bool {
	return pos >= rc.st.Body.Pos() && pos < rc.st.Body.End()
}

// rangeExitInfo is a way out of a loop body to a target outside it.
type rangeExitInfo struct {
	key    any
	level  *fnb // the function containing the target
	action goFn1
	known  moves.Known
}

type returnExitKey struct{}

type branchExitKey struct {
	t   *jumpTarget
	tok token.Token
}

type gotoExitKey struct{ label *types2.Label }

func (fb *fnb) rangeStmt(st *ast.RangeStmt, label *types2.Label) {
	fb.headerMark = len(fb.f.Objects)
	defer func() { fb.headerMark = -1 }()
	tv := fb.b.types[st.X]
	if tv.IsType() {
		// A range over an enum type's members.
		key := fb.rangeLV(st, st.Key)
		fb.loop(label, func(body, done *bblock) { fb.branchBoth(body, done) }, func() {
			if key != nil {
				fb.storeLV(*key, 0)
			}
			fb.block(st.Body, st.Body.List)
		})
		return
	}
	if tv.Type != nil && types2.RangesByMethod(tv.Type) {
		fb.rangeContainer(st, label)
		return
	}
	xt := unwrap(fb.typeOf(st.X))
	core := coreType(xt)
	if p, ok := core.(*types2.Pointer); ok {
		if _, isArr := unwrap(p.Elem()).Underlying().(*types2.Array); isArr {
			core = p
		}
	}
	switch u := core.(type) {
	case *types2.Basic:
		x := fb.expr(st.X)
		_ = x
		key := fb.rangeLV(st, st.Key)
		val := fb.rangeLV(st, st.Value)
		fb.loop(label, func(body, done *bblock) { fb.branchBoth(body, done) }, func() {
			if key != nil {
				fb.storeLV(*key, 0)
			}
			if val != nil {
				fb.storeLV(*val, 0)
			}
			fb.block(st.Body, st.Body.List)
		})
		return
	case *types2.Array, *types2.Pointer, *types2.Slice:
		hasValue := st.Value != nil
		if id, ok := st.Value.(*ast.Ident); ok && id.Name == "_" {
			hasValue = false
		}
		var x Reg
		_, isSlice := u.(*types2.Slice)
		if isSlice || hasValue || callsFunction(fb.b, st.X) {
			x = fb.expr(st.X)
		}
		var et types2.Type
		var elemAddr func() Reg
		switch u := u.(type) {
		case *types2.Array:
			et = u.Elem()
			if hasValue && hasPointers(xt) {
				// The loop ranges over a copy of the array.
				o := fb.newObj(Object{Kind: ObjTemp, Type: xt, Name: "range"})
				fb.store(fb.addr(o), x, xt)
				elemAddr = func() Reg { return fb.addr(o, compat.Clone([]int{ElemStep})...) }
			}
		case *types2.Pointer:
			et = unwrap(u.Elem()).Underlying().(*types2.Array).Elem()
			elemAddr = func() Reg { return fb.elem(x) }
		case *types2.Slice:
			et = u.Elem()
			elemAddr = func() Reg { return fb.elem(x) }
		}
		key := fb.rangeLV(st, st.Key)
		val := fb.rangeLV(st, st.Value)
		fb.loop(label, func(body, done *bblock) { fb.branchBoth(body, done) }, func() {
			if key != nil {
				fb.storeLV(*key, 0)
			}
			if val != nil {
				var v Reg
				if elemAddr != nil {
					v = fb.load(elemAddr(), et)
				}
				fb.storeLV(*val, fb.convertReg(v, et, val.T, st.Pos()))
			}
			fb.block(st.Body, st.Body.List)
		})
		return
	case *types2.Signature:
		fb.rangeFunc(st, label, u, nil)
		return
	case *types2.Chan:
		fb.expr(st.X)
		key := fb.rangeLV(st, st.Key)
		fb.loop(label, func(body, done *bblock) { fb.branchBoth(body, done) }, func() {
			if key != nil {
				var v Reg
				if hasPointers(u.Elem()) {
					v = fb.heap(HeapAlloc)
				}
				fb.storeLV(*key, v)
			}
			fb.block(st.Body, st.Body.List)
		})
		return
	case *types2.Map:
		fb.rangeMap(st, label, xt, u)
		return
	}
	fb.b.errorf(st.Pos(), "cannot range over %s", xt)
}

// rangeLV returns where a range statement stores its key or value, nil for
// none; with :=, the variable is declared in the range statement's scope.
func (fb *fnb) rangeLV(st *ast.RangeStmt, e ast.Expr) *lval {
	if e == nil {
		return nil
	}
	if id, ok := e.(*ast.Ident); ok {
		if id.Name == "_" {
			return nil
		}
		if st.Tok == token.DEFINE {
			if v, ok := fb.b.defs[id].(*types2.Var); ok {
				fb.declareLocal(v)
				return pu.New(lval{kind: lvVar, v: v, T: v.Type()})
			}
		}
	}
	lv := pu.New(fb.lvalue(e))
	return lv
}

// rangeMap builds a range over a map: an rt.MapIter walks it, started
// with Start and advanced with Next or NextKey; the iterator is an owner of
// the loop's scope.
func (fb *fnb) rangeMap(st *ast.RangeStmt, label *types2.Label, xt types2.Type, m *types2.Map) {
	mv := fb.expr(st.X)
	mapObj := fb.newObj(Object{Kind: ObjTemp, Type: xt, Name: "rangemap", Life: LifeBlock})
	fb.store(fb.addr(mapObj), mv, xt)
	itT := fb.b.rtInstance("MapIter", m)
	if itT == nil {
		fb.b.errorf(st.Pos(), "pluto/rt.MapIter is missing")
		return
	}
	it := fb.newObj(Object{Kind: ObjTemp, Type: itT, Name: "mapiter", Life: LifeBlock})
	fb.addVarSlot(it, itT, nil, fb.pos)
	table := fb.load(fb.addr(mapObj), xt)
	fb.methodOn(itT, "Start", fb.addr(it), []Reg{table}, st.Pos(), ImplNone)
	key := fb.rangeLV(st, st.Key)
	val := fb.rangeLV(st, st.Value)
	fb.loop(label, func(body, done *bblock) {
		sc := fb.pushScope(st, false)
		var res []Reg
		if val != nil {
			res = fb.methodOn(itT, "Next", fb.addr(it), nil, st.Pos(), ImplNone)
		} else {
			res = fb.methodOn(itT, "NextKey", fb.addr(it), nil, st.Pos(), ImplNone)
		}
		if key != nil && len(res) > 0 {
			fb.storeLV(*key, fb.convertReg(res[0], m.Key(), key.T, st.Pos()))
		}
		if val != nil && len(res) > 1 {
			fb.storeLV(*val, fb.convertReg(res[1], m.Elem(), val.T, st.Pos()))
		}
		fb.popScope(sc)
		fb.branchBoth(body, done)
	}, func() {
		fb.block(st.Body, st.Body.List)
	})
}

// rangeContainer builds a range over a container: over the iterator its
// All method returns (section 11.11).
func (fb *fnb) rangeContainer(st *ast.RangeStmt, label *types2.Label) {
	res, types := fb.containerCall(st.X, st.X, types2.RangeMethodName, nil, ImplAll)
	if len(res) != 1 {
		fb.b.errorf(st.Pos(), "range over %s", fb.typeOf(st.X))
		return
	}
	sig := signatureOf(types[0])
	if sig == nil {
		fb.b.errorf(st.Pos(), "All returns %s", types[0])
		return
	}
	fb.rangeFunc(st, label, sig, &res[0])
}

// rangeFunc builds a range over a function of signature sig: the value of
// st.X, or iter when it is already evaluated (a container's All).
func (fb *fnb) rangeFunc(st *ast.RangeStmt, label *types2.Label, sig *types2.Signature, iter *Reg) {
	static := NoFunc
	var target Reg
	if iter != nil {
		target = *iter
	} else if fn := fb.staticCallee(st.X); fn != nil {
		static = fb.b.funcOf(fn)
		fb.noteInstance(st.X)
	} else {
		target = fb.expr(st.X)
	}
	yieldT := sig.Params().At(0).Type()
	ySig := signatureOf(yieldT)
	if ySig == nil {
		fb.b.errorf(st.Pos(), "range over %s: its parameter is not a function", fb.typeOf(st.X))
		return
	}

	// The body, a function of its own.
	rc := pu.New(rangeCtx{st: st, label: label, parent: fb})
	bf := fb.b.newFunc(FuncKey{Range: st}, compat.Concat(fb.f.Name, " range body at ", fb.b.fset.Position(st.Pos()).String()), fb.f.Pkg, ySig, st.Pos(), st.End())
	body := fb.b.newFnb(bf, fb.pkg, fb)
	body.root = fb.root
	body.rng = rc
	body.bodyNode = fb.bodyNode
	fb.b.buildRangeBody(body, rc, ySig)

	rec := fb.newObj(Object{Kind: ObjClosure, Type: yieldT, Name: "range body", Pos: st.Pos()})
	clo := fb.emitDst(Instr{Op: OpClosure, Callee: bf.ID, Obj: rec, Args: fb.recordArgs(body), Pos: st.Pos()})
	fb.f.Facts.FuncValues = compat.AppendPlain(fb.f.Facts.FuncValues, bf.ID)
	if ring := fb.ringIterOf(st); ring && target != 0 {
		// The iterator of a ring (its All or Values, or a range over the
		// ring itself) is code the lowerer makes, which calls the body
		// with each element: its value, or for an element type that is
		// an owner (section 7.17) its slot's address, a view into the
		// ring. The iterator value is the ring's address (the RingMethod
		// contract).
		args := compat.Make[Reg](ySig.Params().Len())
		for i := range args {
			pt := ySig.Params().At(i).Type()
			if !hasPointers(pt) {
				continue
			}
			args[i] = fb.either(target, fb.load(target, pt))
		}
		fb.emitCall(pu.New(Call{Kind: CallFunc, Target: target, Sig: sig, Args: compat.Clone([]Reg{clo}), Implicit: ImplRangeIter}), st.Pos())
		fb.emitCall(pu.New(Call{Kind: CallStatic, Callee: bf.ID, Sig: ySig, Args: compat.AppendPlain(args, clo), Implicit: ImplRangeIter}), st.Pos())
		fb.rangeDispatch(rc)
		return
	}
	c := pu.New(Call{Sig: sig, Args: compat.Clone([]Reg{clo}), Implicit: ImplRangeIter})
	if static != NoFunc {
		c.Kind, c.Callee = CallStatic, static
	} else {
		c.Kind, c.Target = CallFunc, target
	}
	fb.emitCall(c, st.Pos())
	fb.rangeDispatch(rc)
}

// ringIterOf reports whether the range statement st ranges over a ring's
// iterator: a ring itself, or its All or Values method's result.
func (fb *fnb) ringIterOf(st *ast.RangeStmt) bool {
	if fb.isRingType(fb.typeOf(st.X)) {
		return true
	}
	call, ok := ast.Unparen(st.X).(*ast.CallExpr)
	if !ok {
		return false
	}
	se, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	sel := fb.b.selections[se]
	if sel == nil {
		return false
	}
	fn, ok := sel.Obj().(*types2.Func)
	return ok && fb.isRingMethod(fn) && types2.IsRingIterator(fn.Name())
}

// buildRangeBody builds the body function of a range-over-func loop.
func (b *builder) buildRangeBody(body *fnb, rc *rangeCtx, ySig *types2.Signature) {
	st := rc.st
	body.pos = st.Pos()
	// Inputs: the yield function's parameters, then the closure record.
	for i := 0; i < ySig.Params().Len(); i++ {
		p := ySig.Params().At(i)
		body.f.Inputs = compat.Append(body.f.Inputs, Input{Var: p, Type: p.Type()})
	}
	body.f.Inputs = compat.Append(body.f.Inputs, Input{Type: types2.Typ[types2.UnsafePointer]})
	in := compat.Make[Reg](ySig.Params().Len())
	for i := range in {
		in[i] = body.input(i)
	}
	body.record = body.input(len(in))
	res := types2.NewVar(st.Pos(), nil, "", types2.Typ[types2.Bool])
	body.results = compat.Clone([]*types2.Var{res})
	body.declareVar(res, ObjVar, st.Pos())

	sc := body.pushScope(st, true)
	for i, e := range []ast.Expr{st.Key, st.Value} {
		if i >= len(in) || e == nil {
			continue
		}
		if lv := body.rangeLV(st, e); lv != nil {
			body.storeLV(*lv, body.convertReg(in[i], ySig.Params().At(i).Type(), lv.T, st.Pos()))
		}
	}
	body.targets = compat.Clone([]*jumpTarget{pu.New(jumpTarget{label: rc.label, rangeFor: rc})})
	body.block(st.Body, st.Body.List)
	body.popScope(sc)
	if body.cur != nil {
		body.emitReturn(token.NoPos, nil)
	}
	body.finish()
}

// bodyReturn returns from a loop body (continue, break, or an exit to an
// outer target), dropping its open scopes.
func (fb *fnb) bodyReturn(known moves.Known) {
	fb.emitReturn(token.NoPos, known)
}

// rangeExit leaves a loop body for a break, continue or goto whose target
// is outside it.
func (fb *fnb) rangeExit(st *ast.BranchStmt, known moves.Known) {
	label := pu.Alloc[*types2.Label]()
	if st.Label != nil {
		(*label), _ = fb.b.uses[st.Label].(*types2.Label)
	}
	if st.Tok == token.GOTO {
		p := fb.rng.parent
		for p.rng != nil && !p.rng.contains((*label).Pos()) {
			p = p.rng.parent
		}
		fb.exitTo(p, gotoExitKey{(*label)}, known, pu.New(goClosure_rangeExit_1{label: label}))
		return
	}
	for p := fb.rng.parent; p != nil; {
		for i := len(p.targets) - 1; i >= 0; i-- {
			t := p.targets[i]
			if t.label != (*label) {
				continue
			}
			tok := st.Tok
			fb.exitTo(p, pu.Box(branchExitKey{t, tok}), known, pu.New(goClosure_rangeExit_2{t: t, tok: tok}))
			return
		}
		if p.rng == nil {
			break
		}
		p = p.rng.parent
	}
	fb.b.errorf(st.Pos(), "no target for %s", st.Tok)
}

// rangeReturn builds a return statement in a loop body: the results go to
// the enclosing function's result variables, and that function returns once
// the iterator has.
func (fb *fnb) rangeReturn(st *ast.ReturnStmt) {
	root := fb.root
	fb.assignResults(root, st)
	pos := st.Pos()
	fb.exitTo(root, pu.Box(returnExitKey{}), fb.b.moves.Empty[st], pu.New(goClosure_rangeReturn_1{pos: pos}))
}

// exitTo records an exit from the loop body fb to a target in level, in
// every loop between, and ends the body.
func (fb *fnb) exitTo(level *fnb, key any, known moves.Known, action goFn1) {
	for cur := fb; cur != level && cur.rng != nil; cur = cur.rng.parent {
		found := false
		for _, ex := range cur.rng.exits {
			if ex.key == key {
				found = true
				break
			}
		}
		if !found {
			cur.rng.exits = compat.Append(cur.rng.exits, pu.New(rangeExitInfo{key: key, level: level, action: action, known: known}))
		}
	}
	fb.bodyReturn(known)
}

// rangeDispatch follows the exits a loop body may have taken, after the
// iterator returned.
func (fb *fnb) rangeDispatch(rc *rangeCtx) {
	if len(rc.exits) == 0 {
		return
	}
	done := fb.newBlock()
	from := fb.ensureBlock()
	fb.edge(from, done)
	further := false
	for _, ex := range rc.exits {
		if ex.level != fb {
			further = true
			continue
		}
		b := fb.newBlock()
		fb.edge(from, b)
		fb.seal(b)
		fb.startBlock(b)
		ex.action.Call(fb)
		fb.cur = nil
	}
	if further {
		b := fb.newBlock()
		fb.edge(from, b)
		fb.seal(b)
		fb.startBlock(b)
		fb.bodyReturn(nil)
	}
	fb.cur = nil
	fb.seal(done)
	fb.startBlock(done)
}

// goClosure_rangeExit_1 is the function literal at rangestmt.go:373, converted to a struct
// that holds the variables it captures (go2pu closure conversion).
type goClosure_rangeExit_1 struct {
	label **types2.Label
}

func (goSelf *goClosure_rangeExit_1) Call(p *fnb) {
	p.releaseForGoto((*goSelf.label), nil)
	p.jump(p.labelBlock((*goSelf.label)))
}

// goClosure_rangeExit_2 is the function literal at rangestmt.go:386, converted to a struct
// that holds the variables it captures (go2pu closure conversion).
type goClosure_rangeExit_2 struct {
	t   *jumpTarget
	tok token.Token
}

func (goSelf *goClosure_rangeExit_2) Call(p *fnb) {
	switch {
	case goSelf.t.rangeFor != nil:
		p.bodyReturn(nil)
	case goSelf.tok == token.CONTINUE:
		p.releaseAbove(goSelf.t.depth, nil)
		p.jump(goSelf.t.contTo)
	default:
		p.releaseAbove(goSelf.t.depth, nil)
		p.jump(goSelf.t.breakTo)
	}
}

// goClosure_rangeReturn_1 is the function literal at rangestmt.go:415, converted to a struct
// that holds the variables it captures (go2pu closure conversion).
type goClosure_rangeReturn_1 struct {
	pos token.Pos
}

func (goSelf *goClosure_rangeReturn_1) Call(p *fnb) { p.emitReturn(goSelf.pos, nil) }
