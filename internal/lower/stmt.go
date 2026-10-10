package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

func (s *fnState) stmtList(list []ast.Stmt) {
	for _, st := range list {
		s.stmt(st)
	}
}

// ensureBlock gives unreachable code a block of its own, so that lowering can
// continue; the block has no predecessors and is removed later.
func (s *fnState) ensureBlock() {
	if s.b == nil {
		b := s.newBlock(ssa.BlockInvalid)
		s.seal(b)
		s.startBlock(b)
	}
}

func (s *fnState) stmt(st ast.Stmt) {
	defer s.at(st.Pos())()
	if _, isLabel := st.(*ast.LabeledStmt); !isLabel {
		s.ensureBlock()
	}
	if p := st.Pos(); p.IsValid() {
		s.f.SetPos(p)
	}
	switch st := st.(type) {
	case *ast.EmptyStmt:
	case *ast.ExprStmt:
		if call, ok := ast.Unparen(st.X).(*ast.CallExpr); ok {
			s.callResults(call)
		} else {
			s.expr(st.X)
		}
	case *ast.DeclStmt:
		s.declStmt(st.Decl.(*ast.GenDecl))
	case *ast.AssignStmt:
		s.assignStmt(st)
	case *ast.IncDecStmt:
		lv := s.lv(st.X)
		T := unwrap(lv.T)
		if lv.bits != nil {
			T = lv.bits.T
		}
		old := s.loadLV(lv)
		one := s.constValue(constOne, T, st)
		op := token.ADD
		if st.Tok == token.DEC {
			op = token.SUB
		}
		s.storeLV(lv, scalarValue(s.arith(op, old.v, one.v, T, st.Pos()), T))
	case *ast.BlockStmt:
		s.stmtList(st.List)
	case *ast.IfStmt:
		s.ifStmt(st)
	case *ast.ForStmt:
		s.forStmt(st, nil)
	case *ast.RangeStmt:
		s.rangeStmt(st, nil)
	case *ast.SwitchStmt:
		s.switchStmt(st, nil)
	case *ast.TypeSwitchStmt:
		s.typeSwitchStmt(st, nil)
	case *ast.LabeledStmt:
		s.labeledStmt(st)
	case *ast.BranchStmt:
		s.branchStmt(st)
	case *ast.ReturnStmt:
		s.returnStmt(st)
	case *ast.DeferStmt:
		s.deferStmt(st)
	default:
		s.l.errorf(st.Pos(), "lower: unsupported statement %T", st)
	}
}

// ----------------------------------------------------------------------------
// Declarations and assignment

func (s *fnState) declStmt(d *ast.GenDecl) {
	if d.Tok != token.VAR {
		return // constants and types need no code
	}
	for _, spec := range d.Specs {
		vs := spec.(*ast.ValueSpec)
		vars := make([]*types2.Var, len(vs.Names))
		for i, n := range vs.Names {
			vars[i], _ = s.info.Defs[n].(*types2.Var)
		}
		switch {
		case len(vs.Values) == 0:
			for _, v := range vars {
				if v != nil {
					addr := s.declareVar(v)
					s.zero(addr, s.l.sizeof(s.subst(v.Type())))
				}
			}
		case len(vs.Values) == len(vs.Names):
			vals := make([]value, len(vars))
			for i, e := range vs.Values {
				T := s.typeOf(e)
				if vars[i] != nil {
					T = s.subst(vars[i].Type())
				}
				vals[i] = s.materialize(s.exprAs(e, T))
			}
			for i, v := range vars {
				if v != nil {
					addr := s.declareVar(v)
					s.storeTo(addr, vals[i], s.subst(v.Type()))
				}
			}
		default: // var a, b = f()
			vals := s.multiValue(vs.Values[0], len(vars))
			for i, v := range vars {
				if v != nil {
					addr := s.declareVar(v)
					T := s.subst(v.Type())
					s.storeTo(addr, s.assignConvert(vals[i], T, vs), T)
				}
			}
		}
	}
}

// multiValue lowers an expression producing n values: a call, or a comma-ok
// form (type assertion, conversion to an enum type).
func (s *fnState) multiValue(e ast.Expr, n int) []value {
	switch x := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		if s.isEnumConversion(x) {
			v, ok := s.enumConversion(x)
			return []value{v, scalarValue(ok, types2.Typ[types2.Bool])}
		}
		res := s.callResults(x)
		for i := range res {
			res[i] = s.materialize(res[i])
		}
		return res
	case *ast.TypeAssertExpr:
		v, ok := s.typeAssert(x, true)
		return []value{s.materialize(v), scalarValue(ok, types2.Typ[types2.Bool])}
	}
	s.l.errorf(e.Pos(), "lower: expression does not produce %d values", n)
	out := make([]value, n)
	for i := range out {
		out[i] = s.zeroValue(types2.Typ[types2.Int])
	}
	return out
}

func (s *fnState) assignStmt(st *ast.AssignStmt) {
	switch st.Tok {
	case token.ASSIGN, token.DEFINE:
	default:
		// x op= y
		lv := s.lv(st.Lhs[0])
		T := unwrap(lv.T)
		if lv.bits != nil {
			T = lv.bits.T
		}
		old := s.loadLV(lv)
		op := compoundOp(st.Tok)
		var res *ssa.Value
		if op == token.SHL || op == token.SHR {
			y := s.expr(st.Rhs[0])
			res = s.shift(op, old.v, T, y.v, y.T, st.Pos())
		} else {
			y := s.exprAs(st.Rhs[0], T)
			res = s.arith(op, old.v, y.v, T, st.Pos())
		}
		s.storeLV(lv, scalarValue(res, T))
		return
	}

	// Left-hand side targets. For :=, new variables get storage after the
	// right-hand side is evaluated (it may mention a shadowed variable).
	type target struct {
		lv    *lvalue
		def   *types2.Var // new variable
		blank bool
	}
	targets := make([]target, len(st.Lhs))
	for i, lhs := range st.Lhs {
		if id, ok := lhs.(*ast.Ident); ok && id.Name == "_" {
			targets[i].blank = true
			continue
		}
		if st.Tok == token.DEFINE {
			if v, ok := s.info.Defs[lhs.(*ast.Ident)].(*types2.Var); ok {
				targets[i].def = v
				continue
			}
		}
		lv := s.lv(lhs)
		targets[i].lv = &lv
	}
	targetType := func(i int) types2.Type {
		switch t := targets[i]; {
		case t.def != nil:
			return s.subst(t.def.Type())
		case t.lv != nil && t.lv.bits != nil:
			return t.lv.bits.T
		case t.lv != nil:
			return unwrap(t.lv.T)
		}
		return nil
	}

	var vals []value
	if len(st.Rhs) == len(st.Lhs) {
		vals = make([]value, len(st.Rhs))
		for i, e := range st.Rhs {
			T := targetType(i)
			if T == nil {
				T = s.typeOf(e)
			}
			v := s.exprAs(e, T)
			if len(st.Rhs) > 1 {
				v = s.materialize(v)
			}
			vals[i] = v
		}
	} else {
		vals = s.multiValue(st.Rhs[0], len(st.Lhs))
	}

	for i, t := range targets {
		switch {
		case t.blank:
		case t.def != nil:
			T := s.subst(t.def.Type())
			addr := s.declareVar(t.def)
			s.storeTo(addr, s.assignConvert(vals[i], T, st), T)
		default:
			s.storeLV(*t.lv, s.assignConvert(vals[i], targetType(i), st))
		}
	}
}

func compoundOp(tok token.Token) token.Token {
	switch tok {
	case token.ADD_ASSIGN:
		return token.ADD
	case token.SUB_ASSIGN:
		return token.SUB
	case token.MUL_ASSIGN:
		return token.MUL
	case token.QUO_ASSIGN:
		return token.QUO
	case token.REM_ASSIGN:
		return token.REM
	case token.AND_ASSIGN:
		return token.AND
	case token.OR_ASSIGN:
		return token.OR
	case token.XOR_ASSIGN:
		return token.XOR
	case token.SHL_ASSIGN:
		return token.SHL
	case token.SHR_ASSIGN:
		return token.SHR
	case token.AND_NOT_ASSIGN:
		return token.AND_NOT
	}
	return token.ILLEGAL
}

// ----------------------------------------------------------------------------
// Control flow

// cond lowers a boolean condition directly into a branch.
func (s *fnState) cond(e ast.Expr, yes, no *ssa.Block) {
	switch x := ast.Unparen(e).(type) {
	case *ast.BinaryExpr:
		switch x.Op {
		case token.LAND:
			mid := s.newBlock(ssa.BlockInvalid)
			s.cond(x.X, mid, no)
			s.seal(mid)
			s.startBlock(mid)
			s.cond(x.Y, yes, no)
			return
		case token.LOR:
			mid := s.newBlock(ssa.BlockInvalid)
			s.cond(x.X, yes, mid)
			s.seal(mid)
			s.startBlock(mid)
			s.cond(x.Y, yes, no)
			return
		}
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			s.cond(x.X, no, yes)
			return
		}
	}
	s.endIf(s.scalar(e), yes, no)
}

func (s *fnState) ifStmt(st *ast.IfStmt) {
	if st.Init != nil {
		s.stmt(st.Init)
	}
	then := s.newBlock(ssa.BlockInvalid)
	done := s.newBlock(ssa.BlockInvalid)
	els := done
	if st.Else != nil {
		els = s.newBlock(ssa.BlockInvalid)
	}
	s.cond(st.Cond, then, els)
	s.seal(then)
	s.startBlock(then)
	s.stmtList(st.Body.List)
	s.endPlain(done)
	if st.Else != nil {
		s.seal(els)
		s.startBlock(els)
		s.stmt(st.Else)
		s.endPlain(done)
	}
	s.seal(done)
	s.startBlock(done)
}

func (s *fnState) forStmt(st *ast.ForStmt, label *types2.Label) {
	if st.Init != nil {
		s.stmt(st.Init)
	}
	head := s.newBlock(ssa.BlockInvalid)
	body := s.newBlock(ssa.BlockInvalid)
	post := s.newBlock(ssa.BlockInvalid)
	done := s.newBlock(ssa.BlockInvalid)
	s.endPlain(head)
	s.startBlock(head)
	if st.Cond != nil {
		s.cond(st.Cond, body, done)
	} else {
		s.endPlain(body)
	}
	s.seal(body)
	s.startBlock(body)
	s.targets = append(s.targets, &target{label: label, breakTo: done, contTo: post})
	s.loopDepth++
	s.stmtList(st.Body.List)
	s.loopDepth--
	s.targets = s.targets[:len(s.targets)-1]
	s.endPlain(post)
	s.seal(post)
	s.startBlock(post)
	if st.Post != nil {
		s.stmt(st.Post)
	}
	s.endPlain(head)
	s.seal(head)
	s.seal(done)
	s.startBlock(done)
}

// loop emits a counted loop over [0, n) and calls body with the index. The
// body may use the target for break and continue.
func (s *fnState) countedLoop(n *ssa.Value, label *types2.Label, body func(i *ssa.Value)) {
	idx := s.temp(types2.Typ[types2.Int], "i")
	s.store(ssa.TI64, idx, s.constInt(ssa.TI64, 0))
	head := s.newBlock(ssa.BlockInvalid)
	bodyB := s.newBlock(ssa.BlockInvalid)
	post := s.newBlock(ssa.BlockInvalid)
	done := s.newBlock(ssa.BlockInvalid)
	s.endPlain(head)
	s.startBlock(head)
	i := s.load(ssa.TI64, idx)
	s.endIf(s.newValue(ssa.OpLess, ssa.TBool, i, n), bodyB, done)
	s.seal(bodyB)
	s.startBlock(bodyB)
	s.targets = append(s.targets, &target{label: label, breakTo: done, contTo: post})
	s.loopDepth++
	body(s.load(ssa.TI64, idx))
	s.loopDepth--
	s.targets = s.targets[:len(s.targets)-1]
	s.endPlain(post)
	s.seal(post)
	s.startBlock(post)
	s.store(ssa.TI64, idx, s.newValue(ssa.OpAdd, ssa.TI64, s.load(ssa.TI64, idx), s.constInt(ssa.TI64, 1)))
	s.endPlain(head)
	s.seal(head)
	s.seal(done)
	s.startBlock(done)
}

// rangeVar returns where a range statement stores its key or value.
func (s *fnState) rangeVar(st *ast.RangeStmt, e ast.Expr) *lvalue {
	if e == nil {
		return nil
	}
	if id, ok := e.(*ast.Ident); ok {
		if id.Name == "_" {
			return nil
		}
		if st.Tok == token.DEFINE {
			if v, ok := s.info.Defs[id].(*types2.Var); ok {
				return &lvalue{ptr: s.declareVar(v), T: s.subst(v.Type())}
			}
		}
	}
	lv := s.lv(e)
	return &lv
}

func (s *fnState) rangeStmt(st *ast.RangeStmt, label *types2.Label) {
	if s.info.Types[st.X].IsType() {
		s.rangeEnum(st, label) // range E over an enum type's members
		return
	}
	xt := unwrap(s.typeOf(st.X))
	assign := func(lv *lvalue, v value) {
		if lv != nil {
			T := unwrap(lv.T)
			s.storeLV(*lv, s.assignConvert(v, T, st))
		}
	}
	intT := types2.Typ[types2.Int]

	switch u := xt.Underlying().(type) {
	case *types2.Basic:
		if u.Info()&types2.IsString != 0 {
			s.rangeString(st, label)
			return
		}
		// range over an integer
		n := s.exprAs(st.X, xt).v
		keyLV := s.rangeVar(st, st.Key)
		keyT := xt
		n64 := s.convertScalar(n, xt, intT)
		s.countedLoop(n64, label, func(i *ssa.Value) {
			assign(keyLV, scalarValue(s.convertScalar(i, intT, keyT), keyT))
			s.stmtList(st.Body.List)
		})
		return

	case *types2.Array, *types2.Pointer, *types2.Slice:
		var base, n *ssa.Value
		var et types2.Type
		switch u := u.(type) {
		case *types2.Array:
			// Arrays are ranged over in place (see the spec).
			base = s.lv(st.X).ptr
			n = s.constInt(ssa.TI64, u.Len())
			et = s.subst(u.Elem())
		case *types2.Pointer:
			arr := unwrap(u.Elem()).Underlying().(*types2.Array)
			base = s.scalar(st.X)
			n = s.constInt(ssa.TI64, arr.Len())
			et = s.subst(arr.Elem())
		case *types2.Slice:
			sl := s.expr(st.X)
			base, n, _ = s.sliceParts(sl)
			et = s.subst(u.Elem())
		}
		keyLV := s.rangeVar(st, st.Key)
		valLV := s.rangeVar(st, st.Value)
		s.countedLoop(n, label, func(i *ssa.Value) {
			assign(keyLV, scalarValue(i, intT))
			if valLV != nil {
				assign(valLV, s.materialize(s.loadFrom(s.elemAddr(base, i, et), et)))
			}
			s.stmtList(st.Body.List)
		})
		return

	case *types2.Signature:
		s.l.errorf(st.Pos(), "range over functions is not supported yet")
		return
	}
	s.l.errorf(st.Pos(), "lower: cannot range over %s", xt)
}

// rangeString iterates over the runes of a string.
func (s *fnState) rangeString(st *ast.RangeStmt, label *types2.Label) {
	str := s.materialize(s.expr(st.X))
	ptr, n := s.stringParts(str)
	keyLV := s.rangeVar(st, st.Key)
	valLV := s.rangeVar(st, st.Value)
	intT := types2.Typ[types2.Int]
	runeT := types2.Typ[types2.Rune]

	idx := s.temp(intT, "i")
	next := s.temp(intT, "next")
	s.store(ssa.TI64, next, s.constInt(ssa.TI64, 0))
	head := s.newBlock(ssa.BlockInvalid)
	body := s.newBlock(ssa.BlockInvalid)
	post := s.newBlock(ssa.BlockInvalid)
	done := s.newBlock(ssa.BlockInvalid)
	s.endPlain(head)
	s.startBlock(head)
	i := s.load(ssa.TI64, next)
	s.endIf(s.newValue(ssa.OpLess, ssa.TBool, i, n), body, done)
	s.seal(body)
	s.startBlock(body)
	i = s.load(ssa.TI64, next)
	s.store(ssa.TI64, idx, i)
	res := s.callRuntime("decodeRune", []*ssa.Type{ssa.TI32, ssa.TI64}, []*ssa.Value{ptr, n, i})
	s.store(ssa.TI64, next, s.newValue(ssa.OpAdd, ssa.TI64, i, res[1]))
	if keyLV != nil {
		s.storeLV(*keyLV, scalarValue(i, intT))
	}
	if valLV != nil {
		s.storeLV(*valLV, scalarValue(res[0], runeT))
	}
	s.targets = append(s.targets, &target{label: label, breakTo: done, contTo: post})
	s.loopDepth++
	s.stmtList(st.Body.List)
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

func (s *fnState) switchStmt(st *ast.SwitchStmt, label *types2.Label) {
	if st.Init != nil {
		s.stmt(st.Init)
	}
	var tag value
	var tagT types2.Type
	if st.Tag != nil {
		tagT = unwrap(s.typeOf(st.Tag))
		if b, ok := tagT.Underlying().(*types2.Basic); ok && b.Info()&types2.IsUntyped != 0 {
			tagT = defaultType(s.info.Types[st.Tag].Value)
		}
		tag = s.materialize(s.exprAs(st.Tag, tagT))
	}
	done := s.newBlock(ssa.BlockInvalid)
	clauses := st.Body.List
	bodies := make([]*ssa.Block, len(clauses))
	for i := range clauses {
		bodies[i] = s.newBlock(ssa.BlockInvalid)
	}
	defaultBody := done
	for i, c := range clauses {
		cc := c.(*ast.CaseClause)
		if cc.List == nil {
			defaultBody = bodies[i]
			continue
		}
		for _, e := range cc.List {
			next := s.newBlock(ssa.BlockInvalid)
			if st.Tag == nil {
				s.cond(e, bodies[i], next)
			} else {
				// An interface compares with a concrete value as an
				// interface, whichever side it is on.
				eT := s.typeOf(e)
				var eq *ssa.Value
				switch {
				case isInterface(tagT) && !isInterface(eT):
					eq = s.equal(tag, s.toInterface(s.expr(e), eT, tagT), tagT, e.Pos())
				case !isInterface(tagT) && isInterface(eT):
					eq = s.equal(s.toInterface(tag, tagT, eT), s.expr(e), eT, e.Pos())
				default:
					eq = s.equal(tag, s.exprAs(e, tagT), tagT, e.Pos())
				}
				s.endIf(eq, bodies[i], next)
			}
			s.seal(next)
			s.startBlock(next)
		}
	}
	s.endPlain(defaultBody)

	s.targets = append(s.targets, &target{label: label, breakTo: done, isSwitch: true})
	for i, c := range clauses {
		cc := c.(*ast.CaseClause)
		s.seal(bodies[i])
		s.startBlock(bodies[i])
		body := cc.Body
		fallthrough_ := false
		if n := len(body); n > 0 {
			if br, ok := body[n-1].(*ast.BranchStmt); ok && br.Tok == token.FALLTHROUGH {
				fallthrough_ = true
				body = body[:n-1]
			}
		}
		s.stmtList(body)
		if fallthrough_ && i+1 < len(clauses) {
			s.endPlain(bodies[i+1])
		} else {
			s.endPlain(done)
		}
	}
	s.targets = s.targets[:len(s.targets)-1]
	s.seal(done)
	s.startBlock(done)
}

func (s *fnState) typeSwitchStmt(st *ast.TypeSwitchStmt, label *types2.Label) {
	if st.Init != nil {
		s.stmt(st.Init)
	}
	var x ast.Expr
	switch a := st.Assign.(type) {
	case *ast.AssignStmt:
		x = a.Rhs[0].(*ast.TypeAssertExpr).X
	case *ast.ExprStmt:
		x = a.X.(*ast.TypeAssertExpr).X
	}
	I := s.typeOf(x)
	iv := s.materialize(s.expr(x))
	tab, data := s.ifaceParts(iv)

	done := s.newBlock(ssa.BlockInvalid)
	clauses := st.Body.List
	bodies := make([]*ssa.Block, len(clauses))
	for i := range clauses {
		bodies[i] = s.newBlock(ssa.BlockInvalid)
	}
	defaultBody := done
	for i, c := range clauses {
		cc := c.(*ast.CaseClause)
		if cc.List == nil {
			defaultBody = bodies[i]
			continue
		}
		for _, e := range cc.List {
			next := s.newBlock(ssa.BlockInvalid)
			tv := s.info.Types[e]
			T := s.subst(tv.Type)
			var match *ssa.Value
			switch {
			case tv.IsNil():
				match = s.newValue(ssa.OpEq, ssa.TBool, tab, s.constInt(ssa.TPtr, 0))
			case isInterface(T):
				if T.Underlying().(*types2.Interface).Empty() {
					match = s.newValue(ssa.OpNe, ssa.TBool, tab, s.constInt(ssa.TPtr, 0))
				} else {
					s.l.types.asserted[typeString(T)] = T
					itab := s.callRuntime("assertItab", []*ssa.Type{ssa.TPtr}, []*ssa.Value{s.typeDescOf(tab, I), s.symAddr(s.l.types.desc(T))})[0]
					match = s.newValue(ssa.OpNe, ssa.TBool, itab, s.constInt(ssa.TPtr, 0))
				}
			default:
				match = s.typeMatches(tab, I, T)
			}
			s.endIf(match, bodies[i], next)
			s.seal(next)
			s.startBlock(next)
		}
	}
	s.endPlain(defaultBody)

	s.targets = append(s.targets, &target{label: label, breakTo: done, isSwitch: true})
	for i, c := range clauses {
		cc := c.(*ast.CaseClause)
		s.seal(bodies[i])
		s.startBlock(bodies[i])
		if v, ok := s.info.Implicits[cc].(*types2.Var); ok {
			T := s.subst(v.Type())
			addr := s.declareVar(v)
			switch {
			case isInterface(T) && types2.Identical(T, I):
				s.storeTo(addr, iv, T)
			case isInterface(T):
				vv, _ := s.assertTo(iv, I, T, true, cc.Pos())
				s.storeTo(addr, vv, T)
			default:
				s.storeTo(addr, s.fromDataWord(data, T), T)
			}
		}
		s.stmtList(cc.Body)
		s.endPlain(done)
	}
	s.targets = s.targets[:len(s.targets)-1]
	s.seal(done)
	s.startBlock(done)
}

func (s *fnState) labelBlock(l *types2.Label) *ssa.Block {
	li := s.labels[l]
	if li == nil {
		li = &labelInfo{block: s.newBlock(ssa.BlockInvalid)}
		s.labels[l] = li
	}
	return li.block
}

func (s *fnState) labeledStmt(st *ast.LabeledStmt) {
	label, _ := s.info.Defs[st.Label].(*types2.Label)
	b := s.labelBlock(label)
	s.endPlain(b)
	s.startBlock(b)
	switch inner := st.Stmt.(type) {
	case *ast.ForStmt:
		s.forStmt(inner, label)
	case *ast.RangeStmt:
		s.rangeStmt(inner, label)
	case *ast.SwitchStmt:
		s.switchStmt(inner, label)
	case *ast.TypeSwitchStmt:
		s.typeSwitchStmt(inner, label)
	default:
		s.stmt(inner)
	}
}

func (s *fnState) branchStmt(st *ast.BranchStmt) {
	var label *types2.Label
	if st.Label != nil {
		label, _ = s.info.Uses[st.Label].(*types2.Label)
	}
	switch st.Tok {
	case token.GOTO:
		s.endPlain(s.labelBlock(label))
		return
	case token.FALLTHROUGH:
		return // handled by switchStmt
	}
	for i := len(s.targets) - 1; i >= 0; i-- {
		t := s.targets[i]
		if label != nil && t.label != label {
			continue
		}
		if st.Tok == token.CONTINUE {
			if t.contTo == nil {
				continue // switches are not continue targets
			}
			s.endPlain(t.contTo)
			return
		}
		s.endPlain(t.breakTo)
		return
	}
	s.l.errorf(st.Pos(), "lower: no target for %s", st.Tok)
}

func (s *fnState) returnStmt(st *ast.ReturnStmt) {
	switch {
	case len(st.Results) == 0:
	case len(st.Results) == len(s.results):
		vals := make([]value, len(st.Results))
		for i, e := range st.Results {
			vals[i] = s.materialize(s.exprAs(e, s.abi.results[i].typ))
		}
		for i, v := range vals {
			s.storeTo(s.results[i], v, s.abi.results[i].typ)
		}
	default: // return f() with several results
		vals := s.multiValue(st.Results[0], len(s.results))
		for i, v := range vals {
			T := s.abi.results[i].typ
			s.storeTo(s.results[i], s.assignConvert(v, T, st), T)
		}
	}
	s.emitReturn(st.Pos())
}

// ----------------------------------------------------------------------------
// Defer

// hasDefer reports whether a function body contains a defer statement
// outside nested function literals.
func hasDefer(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.DeferStmt:
			found = true
		case *ast.FuncLit:
			return false
		}
		return !found
	})
	return found
}

// deferStmt evaluates the deferred call's function and arguments now and
// records the call to run at every return.
func (s *fnState) deferStmt(st *ast.DeferStmt) {
	if s.loopDepth > 0 {
		s.l.errorf(st.Pos(), "defer inside a loop is not supported in Plutonium (it would need unbounded stack); move the loop body into a function")
		return
	}
	if len(s.defers) >= 64 {
		s.l.errorf(st.Pos(), "too many defer statements in one function (at most 64)")
		return
	}
	bit := len(s.defers)
	call := st.Call

	var run func(s *fnState)
	funTV := s.info.Types[call.Fun]
	switch {
	case funTV.IsBuiltin():
		name := s.info.Uses[identOf(ast.Unparen(call.Fun))].Name()
		switch name {
		case "print", "println":
			// Arguments are evaluated at the defer statement.
			args := make([]value, len(call.Args))
			for i, a := range call.Args {
				args[i] = s.saveValue(s.printOperand(a))
			}
			run = func(s *fnState) {
				for i, a := range args {
					if i > 0 && name == "println" {
						s.callRuntime("printSpace", nil)
					}
					v := s.loadFrom(a.v, a.T)
					s.printValue(v, a.T, call)
				}
				if name == "println" {
					s.callRuntime("printNewline", nil)
				}
			}
		case "panic":
			arg := s.saveValue(s.exprAs(call.Args[0], types2.NewInterfaceType(nil, nil)))
			run = func(s *fnState) {
				parts := s.leafValues(s.loadFrom(arg.v, arg.T), []leaf{{0, ssa.TPtr}, {8, ssa.TPtr}})
				s.callRuntimeNoReturn("panicValue", parts...)
				dead := s.newBlock(ssa.BlockInvalid)
				s.seal(dead)
				s.startBlock(dead)
			}
		default:
			s.l.errorf(st.Pos(), "defer of builtin %s is not supported", name)
			return
		}
	default:
		fun := ast.Unparen(call.Fun)
		if se, ok := fun.(*ast.SelectorExpr); ok {
			if sel := s.info.Selections[se]; sel != nil && sel.Kind() == types2.MethodVal && !isInterface(s.typeOf(se.X)) {
				fn, path, ok := s.selectedMethod(se, sel)
				if !ok {
					return
				}
				// A method promoted from an embedded interface is deferred
				// as a method value, below.
				if !isIfaceMethod(fn) {
					recv := s.saveValue(s.methodReceiver(se.X, fn, path))
					var targs []types2.Type
					var def *defaultsOf
					w := s.ringOf(fn)
					if w == nil {
						targs = s.methodTypeArgs(fn, se, s.typeOf(se.X))
						def = &defaultsOf{fn, targs}
					}
					sym, full := s.l.methodFunc(fn, targs, w)
					sig := s.subst(funTV.Type).(*types2.Signature)
					args := s.saveValues(s.callArgs(call, sig, def))
					run = func(s *fnState) {
						r := s.loadFrom(recv.v, recv.T)
						s.emitCall(callee{sym: sym}, full, &r, s.loadValues(args), call.Pos())
					}
				}
			}
		}
		if run != nil {
			break
		}
		if id := staticFuncIdent(fun, s.info); id != nil {
			if fn, ok := s.info.Uses[id].(*types2.Func); ok {
				if s.l.errorsNewValue(fn, call.Pos(), "deferred") {
					return
				}
				targs := s.instanceArgs(id)
				sym := s.l.funcSym(fn, targs)
				sig := s.subst(funTV.Type).(*types2.Signature)
				args := s.saveValues(s.callArgs(call, sig, &defaultsOf{fn, targs}))
				run = func(s *fnState) { s.emitCall(callee{sym: sym}, sig, nil, s.loadValues(args), call.Pos()) }
				break
			}
		}
		// Any other callee: evaluate the func value now.
		sig := s.typeOf(call.Fun).Underlying().(*types2.Signature)
		fv := s.saveValue(scalarValue(s.scalar(call.Fun), s.typeOf(call.Fun)))
		args := s.saveValues(s.callArgs(call, sig, nil))
		run = func(s *fnState) {
			f := s.loadFrom(fv.v, fv.T).v
			s.emitCall(callee{code: s.load(ssa.TPtr, f), ctx: f}, sig, nil, s.loadValues(args), call.Pos())
		}
	}

	bits := s.load(ssa.TI64, s.deferBits)
	s.store(ssa.TI64, s.deferBits, s.newValue(ssa.OpOr, ssa.TI64, bits, s.constInt(ssa.TI64, 1<<bit)))
	s.defers = append(s.defers, &deferInfo{run: run})
}

// saveValue copies a value into a fresh slot and returns its address as an
// aggregate-style value (v.v is the slot address).
func (s *fnState) saveValue(v value) value {
	tmp := s.temp(v.T, "defer")
	s.storeTo(tmp, v, v.T)
	return aggValue(tmp, v.T)
}

func (s *fnState) saveValues(vs []value) []value {
	out := make([]value, len(vs))
	for i, v := range vs {
		out[i] = s.saveValue(v)
	}
	return out
}

func (s *fnState) loadValues(saved []value) []value {
	out := make([]value, len(saved))
	for i, v := range saved {
		out[i] = s.loadFrom(v.v, v.T)
	}
	return out
}
