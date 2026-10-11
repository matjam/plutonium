package lower

import (
	"github.com/matjam/plutonium/internal/diag"
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
	if _, isLabel := st.(*ast.LabeledStmt); !isLabel {
		// The statement's temporaries, and for a block or a statement
		// with an implicit block its variables, are released at its end
		// (strscope.go).
		e := s.pushCleanup(st, isScopeStmt(st))
		defer s.popCleanup(e)
	}
	switch st := st.(type) {
	case *ast.EmptyStmt:
	case *ast.ExprStmt:
		if call, ok := ast.Unparen(st.X).(*ast.CallExpr); ok {
			// Results that are owners are temporaries.
			for _, r := range s.callResults(call) {
				if s.b != nil && r.T != nil && s.l.needsDrop(r.T) {
					slot := s.temp(r.T, "drop")
					s.storeTo(slot, r, r.T)
					s.registerTemp(slot, r.T)
				}
			}
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
		if lv.elem != nil && lv.elem.mapT != nil {
			lv = s.mapElemPtr(lv) // one lookup (maps.go)
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
	case *ast.GoStmt:
		s.goStmt(st)
	case *ast.SendStmt:
		s.sendStmt(st)
	case *ast.SelectStmt:
		s.selectStmt(st, nil)
	default:
		s.l.unsupportedf(st.Pos(), "unsupported statement %T", st)
	}
}

// isScopeStmt reports whether st is a block or has an implicit block, whose
// variables are released when it ends.
func isScopeStmt(st ast.Stmt) bool {
	switch st.(type) {
	case *ast.BlockStmt, *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		return true
	}
	return false
}

// block lowers a statement list as a block of its own, node, whose
// variables are released when it ends.
func (s *fnState) block(node ast.Node, list []ast.Stmt) {
	e := s.pushCleanup(node, true)
	s.stmtList(list)
	s.popCleanup(e)
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
					s.assign(lvalue{ptr: addr, T: s.subst(v.Type())}, vals[i], true)
				}
			}
		default: // var a, b = f()
			vals := s.multiValue(vs.Values[0], len(vars))
			for i, v := range vars {
				if v != nil {
					addr := s.declareVar(v)
					T := s.subst(v.Type())
					s.assign(lvalue{ptr: addr, T: T}, s.assignConvert(vals[i], T, vs), true)
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
	case *ast.UnaryExpr:
		if x.Op == token.ARROW {
			v, ok := s.recv(x, true)
			return []value{s.materialize(v), scalarValue(ok, types2.Typ[types2.Bool])}
		}
	case *ast.IndexExpr:
		if s.isContainerIndex(x) || s.isMapIndex(x) {
			return s.containerIndexOk(x)
		}
	}
	s.l.internalf(e.Pos(), "expression does not produce %d values", n)
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
		op := compoundOp(st.Tok)
		if lv.elem != nil && lv.elem.mapT != nil {
			// m[k] op= y: y first, then one lookup of the element, which
			// nothing changes before it is stored (maps.go).
			var y value
			if op == token.SHL || op == token.SHR {
				y = s.materialize(s.expr(st.Rhs[0]))
			} else {
				y = s.materialize(s.exprAs(st.Rhs[0], T))
			}
			y = s.holdOwned(y)
			lv = s.mapElemPtr(lv)
			old := s.loadLV(lv)
			if isString(T) {
				s.storeCounted(lv, s.concatValues([]value{old, y}, T), false)
				return
			}
			var res *ssa.Value
			if op == token.SHL || op == token.SHR {
				res = s.shift(op, old.v, T, y.v, y.T, st.Pos())
			} else {
				res = s.arith(op, old.v, y.v, T, st.Pos())
			}
			s.storeLV(lv, scalarValue(res, T))
			return
		}
		if isString(T) && lv.bits == nil {
			s.concatAssign(lv, st.Rhs[0], st.Pos())
			return
		}
		old := s.loadLV(lv)
		var res *ssa.Value
		if op == token.SHL || op == token.SHR {
			y := s.expr(st.Rhs[0])
			res = s.shift(op, old.v, T, y.v, y.T, st.Pos())
		} else {
			y := s.exprAs(st.Rhs[0], T)
			if s.l.scoped.Checked[st] {
				res = s.checkedArith(op, old.v, y.v, T, st.Pos())
			} else {
				res = s.arith(op, old.v, y.v, T, st.Pos())
			}
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
				// Storing one value may release another's source
				// (a, b = b, a), so each holds its own references.
				v = s.holdOwned(s.materialize(v))
			}
			vals[i] = v
		}
	} else {
		vals = s.multiValue(st.Rhs[0], len(st.Lhs))
	}

	// Assigning to a place that holds an owner destroys the old value
	// first, unless the assignment only permutes places (a, b = b, a).
	permutes := s.permutes(st)
	for i, t := range targets {
		switch {
		case t.blank:
			// A discarded owner from a call with several results.
			if len(st.Rhs) != len(st.Lhs) && i < len(vals) && vals[i].T != nil && s.l.needsDrop(vals[i].T) {
				slot := s.temp(vals[i].T, "drop")
				s.storeTo(slot, vals[i], vals[i].T)
				s.registerTemp(slot, vals[i].T)
			}
		case t.def != nil:
			T := s.subst(t.def.Type())
			addr := s.declareVar(t.def)
			s.assign(lvalue{ptr: addr, T: T}, s.assignConvert(vals[i], T, st), true)
		default:
			if T := unwrap(t.lv.T); t.lv.elem == nil && t.lv.bits == nil && !permutes && s.l.needsDrop(T) {
				s.dropAt(t.lv.ptr, T)
			}
			s.assign(*t.lv, s.assignConvert(vals[i], targetType(i), st), false)
		}
	}
}

// dropResult destroys what result i holds before a return replaces it: a
// named result given an owner earlier. Results start clear.
func (s *fnState) dropResult(i int) {
	if T := s.abi.results[i].typ; s.l.needsDrop(T) {
		s.dropAt(s.results[i], T)
	}
}

// permutes reports whether an assignment's right-hand sides are its
// left-hand sides in another order, which moves nothing in or out.
func (s *fnState) permutes(st *ast.AssignStmt) bool {
	if len(st.Lhs) < 2 || len(st.Lhs) != len(st.Rhs) {
		return false
	}
	left := map[string]int{}
	for _, l := range st.Lhs {
		left[types2.ExprString(l)]++
	}
	for _, r := range st.Rhs {
		k := types2.ExprString(r)
		if left[k] == 0 {
			return false
		}
		left[k]--
	}
	return true
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
	s.block(st.Body, st.Body.List)
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
	switch {
	case st.Cond == nil:
		s.endPlain(body)
	case s.makesTemps(st.Cond):
		// The condition's temporaries are released after each
		// evaluation, before the branch.
		e := s.pushCleanup(st.Cond, false)
		c := s.scalar(st.Cond)
		s.popCleanup(e)
		s.endIf(c, body, done)
	default:
		s.cond(st.Cond, body, done)
	}
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
	s.targets = append(s.targets, &target{label: label, breakTo: done, contTo: post, depth: len(s.cleanups)})
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
	if tv := s.info.Types[st.X]; tv.Type != nil && types2.RangesByMethod(tv.Type) {
		s.rangeContainer(st, label)
		return
	}
	xt := unwrap(s.typeOf(st.X))
	assign := func(lv *lvalue, v value) {
		if lv != nil {
			T := unwrap(lv.T)
			s.assign(*lv, s.assignConvert(v, T, st), false)
		}
	}
	intT := types2.Typ[types2.Int]

	switch u := asPointer(xt.Underlying()).(type) {
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
			s.block(st.Body, st.Body.List)
		})
		return

	case *types2.Array, *types2.Pointer, *types2.Slice:
		var base, n *ssa.Value
		var et types2.Type
		// As in Go, a range over an array or a pointer to one with no
		// value variable needs only the constant length and does not
		// evaluate the range expression, unless the expression calls
		// a function (then its length is not a constant).
		hasValue := st.Value != nil
		if id, ok := st.Value.(*ast.Ident); ok && id.Name == "_" {
			hasValue = false
		}
		if _, isSlice := u.(*types2.Slice); !isSlice && !hasValue && s.callsFunction(st.X) {
			s.expr(st.X)
		}
		switch u := u.(type) {
		case *types2.Array:
			// The range expression is evaluated once, before the loop,
			// so the loop sees a copy of the array.
			if hasValue {
				if s.l.counted(xt) {
					// The copy keeps its elements alive whatever the body
					// does to the array.
					base = s.holdOwned(s.expr(st.X)).v
				} else {
					base = s.temp(xt, "range")
					s.storeTo(base, s.expr(st.X), xt)
				}
			}
			n = s.constInt(ssa.TI64, u.Len())
			et = s.subst(u.Elem())
		case *types2.Pointer:
			arr := unwrap(u.Elem()).Underlying().(*types2.Array)
			if hasValue {
				base = s.scalar(st.X)
			}
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
			s.block(st.Body, st.Body.List)
		})
		return

	case *types2.Signature:
		s.rangeFuncStmt(st, label, u)
		return

	case *types2.Chan:
		s.rangeChan(st, label)
		return

	case *types2.Map:
		s.rangeMap(st, label)
		return
	}
	s.l.unsupportedf(st.Pos(), "cannot range over %s", xt)
}

// callsFunction reports whether e contains a function call (a conversion
// or a call of a built-in folded to a constant is not one).
func (s *fnState) callsFunction(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if tv := s.info.Types[call]; tv.Value == nil && !s.info.Types[call.Fun].IsType() {
				found = true
			}
		}
		return !found
	})
	return found
}

// rangeString iterates over the runes of a string.
func (s *fnState) rangeString(st *ast.RangeStmt, label *types2.Label) {
	// The string is evaluated once and must stay alive for the loop. An
	// untyped constant takes the type string.
	str := s.hold(s.materialize(s.exprAs(st.X, types2.Typ[types2.String])))
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
		// The tag is compared with each case in turn; the cases may call
		// functions.
		tag = s.hold(s.materialize(s.exprAs(st.Tag, tagT)))
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

	s.targets = append(s.targets, &target{label: label, breakTo: done, isSwitch: true, depth: len(s.cleanups)})
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
		// The clause is a block; fallthrough leaves it too.
		s.block(cc, body)
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
	if types2.UnionVariants(s.typeOf(x)) != nil {
		s.unionTypeSwitch(st, x, label)
		return
	}
	if s.staticTypeSwitch(st, x, label) {
		return
	}
	I := s.typeOf(x)
	iv := s.materialize(s.expr(x))
	moves := s.l.moves != nil && s.l.moves.SwitchMoves[st]
	if moves {
		// The owner moves out of x into the binding of whichever clause
		// runs (language spec section 7.9): x holds nothing afterwards.
		copied := s.temp(I, "switched")
		s.storeTo(copied, iv, I)
		iv = aggValue(copied, I)
		s.zero(s.lv(x).ptr, s.l.sizeof(I))
	}
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
	if moves && defaultBody == done {
		// No clause took the value: drop it here.
		s.dropAt(iv.v, I)
	}
	s.endPlain(defaultBody)

	s.targets = append(s.targets, &target{label: label, breakTo: done, isSwitch: true, depth: len(s.cleanups)})
	for i, c := range clauses {
		cc := c.(*ast.CaseClause)
		s.seal(bodies[i])
		s.startBlock(bodies[i])
		clause := s.pushCleanup(cc, true)
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
				s.assign(lvalue{ptr: addr, T: T}, s.fromDataWord(data, T), true)
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

// staticTypeSwitch lowers a type switch on any(v), or another conversion
// to an interface, of a value v whose type is known when the function is
// compiled: in generic code, a value of a type parameter's type. The clause
// that matches is known, so only it is lowered (spec section 5.7: such a
// switch folds at compile time), and the conversion is made only if a
// binding needs it. It reports false for any other type switch.
func (s *fnState) staticTypeSwitch(st *ast.TypeSwitchStmt, x ast.Expr, label *types2.Label) bool {
	conv, ok := ast.Unparen(x).(*ast.CallExpr)
	if !ok || len(conv.Args) != 1 || !s.info.Types[conv.Fun].IsType() || s.callsFunction(conv.Args[0]) {
		return false
	}
	I := s.typeOf(x)
	C := s.typeOf(conv.Args[0])
	if !isInterface(I) || isInterface(C) {
		return false
	}
	if b, ok := C.Underlying().(*types2.Basic); ok && b.Info()&types2.IsUntyped != 0 {
		return false
	}
	var chosen *ast.CaseClause
	var only types2.Type // the clause's one type, if it names one
	for _, c := range st.Body.List {
		cc := c.(*ast.CaseClause)
		if cc.List == nil {
			continue
		}
		for _, e := range cc.List {
			tv := s.info.Types[e]
			if tv.IsNil() {
				continue
			}
			T := s.subst(tv.Type)
			var match bool
			if iface, ok := T.Underlying().(*types2.Interface); ok {
				match = types2.Implements(C, iface)
			} else {
				match = types2.Identical(C, T)
			}
			if match {
				chosen = cc
				if len(cc.List) == 1 {
					only = T
				}
				break
			}
		}
		if chosen != nil {
			break
		}
	}
	if chosen == nil {
		for _, c := range st.Body.List {
			if cc := c.(*ast.CaseClause); cc.List == nil {
				chosen = cc
			}
		}
	}
	done := s.newBlock(ssa.BlockInvalid)
	s.targets = append(s.targets, &target{label: label, breakTo: done, isSwitch: true, depth: len(s.cleanups)})
	if chosen != nil {
		clause := s.pushCleanup(chosen, true)
		if v, ok := s.info.Implicits[chosen].(*types2.Var); ok {
			T := s.subst(v.Type())
			addr := s.declareVar(v)
			if only != nil && !isInterface(only) {
				s.assign(lvalue{ptr: addr, T: T}, s.exprAs(conv.Args[0], T), true)
			} else {
				s.assign(lvalue{ptr: addr, T: T}, s.exprAs(x, T), true)
			}
		}
		s.stmtList(chosen.Body)
		s.popCleanup(clause)
	}
	s.endPlain(done)
	s.targets = s.targets[:len(s.targets)-1]
	s.seal(done)
	s.startBlock(done)
	return true
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
	case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		e := s.pushCleanup(inner, true)
		switch inner := inner.(type) {
		case *ast.ForStmt:
			s.forStmt(inner, label)
		case *ast.RangeStmt:
			s.rangeStmt(inner, label)
		case *ast.SwitchStmt:
			s.switchStmt(inner, label)
		case *ast.TypeSwitchStmt:
			s.typeSwitchStmt(inner, label)
		case *ast.SelectStmt:
			s.selectStmt(inner, label)
		}
		s.popCleanup(e)
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
		if s.rf != nil && !s.rf.contains(label.Pos()) {
			s.rangeBranch(st, label)
			return
		}
		s.releaseForGoto(label)
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
		if t.rf != nil {
			// The range-over-func loop whose body this is.
			s.endIter(st.Tok == token.CONTINUE)
			return
		}
		if st.Tok == token.CONTINUE {
			if t.contTo == nil {
				continue // switches are not continue targets
			}
			s.releaseAbove(t.depth)
			s.endPlain(t.contTo)
			return
		}
		s.releaseAbove(t.depth)
		s.endPlain(t.breakTo)
		return
	}
	if s.rf != nil {
		s.rangeBranch(st, label)
		return
	}
	s.l.internalf(st.Pos(), "no target for %s", st.Tok)
}

func (s *fnState) returnStmt(st *ast.ReturnStmt) {
	if s.rf != nil {
		s.rangeReturn(st)
		return
	}
	switch {
	case len(st.Results) == 0:
	case len(st.Results) == len(s.results):
		named := s.namedResults()
		vals := make([]value, len(st.Results))
		for i, e := range st.Results {
			vals[i] = s.materialize(s.exprAs(e, s.abi.results[i].typ))
			if named && len(vals) > 1 {
				// return b, a with named results a and b: storing one
				// releases the other's source.
				vals[i] = s.holdOwned(vals[i])
			}
		}
		moved := map[*types2.Var]bool{}
		for i, v := range vals {
			T := s.abi.results[i].typ
			s.dropResult(i)
			if s.moveOnReturn(st.Results[i], v, T, i, moved) {
				continue
			}
			s.assign(lvalue{ptr: s.results[i], T: T}, v, !named)
		}
	default: // return f() with several results
		named := s.namedResults()
		vals := s.multiValue(st.Results[0], len(s.results))
		for i, v := range vals {
			T := s.abi.results[i].typ
			s.dropResult(i)
			s.assign(lvalue{ptr: s.results[i], T: T}, s.assignConvert(v, T, st), !named)
		}
	}
	s.emitReturn(st.Pos())
}

// namedResults reports whether the function's results are named, and so
// are variables that may hold values before a return statement sets them.
func (s *fnState) namedResults() bool {
	for _, v := range s.signatureVars().results {
		if v != nil && v.Name() != "" && v.Name() != "_" {
			return true
		}
	}
	return false
}

// moveOnReturn hands the references of a local variable returned as
// result i over to the caller instead of retaining them and releasing the
// variable when its scope ends: return x is x's last use when the function
// has no deferred calls that could still read it. A variable is moved once;
// return x, x retains the second copy. It reports whether it stored the
// result.
func (s *fnState) moveOnReturn(e ast.Expr, v value, T types2.Type, i int, moved map[*types2.Var]bool) bool {
	if !s.l.counted(T) || s.hasDefers() || s.namedResults() {
		return false
	}
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	obj, ok := s.info.Uses[id].(*types2.Var)
	if !ok || moved[obj] || !s.ownedVars[obj] || !types2.Identical(s.subst(obj.Type()), T) {
		return false
	}
	moved[obj] = true
	addr := s.vars[obj]
	s.checkScopeMove(addr, T)
	s.move(s.results[i], addr, s.l.sizeof(T))
	s.zero(addr, s.l.sizeof(T))
	return true
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

// inGotoLoop reports whether p is in a loop that a goto makes, by jumping
// back to a label before p from after it (loopSpans).
func (s *fnState) inGotoLoop(p token.Pos) bool {
	root := s.rootFn()
	var body *ast.BlockStmt
	switch {
	case root.job.decl != nil:
		body = root.job.decl.Body
	case root.job.lit != nil:
		body = root.job.lit.Body
	}
	if body == nil {
		return false
	}
	loops, _ := loopSpans(body)
	for _, sp := range loops {
		if sp.label != "" && p >= sp.lo && p < sp.hi {
			return true
		}
	}
	return false
}

// deferStmt evaluates the deferred call's function and arguments now and
// records the call to run at every return.
func (s *fnState) deferStmt(st *ast.DeferStmt) {
	if s.loopDepth > 0 || s.inGotoLoop(st.Pos()) {
		s.l.errorf(st.Pos(), diag.DeferInLoop, "defer inside a loop is not supported in Plutonium (it would need unbounded stack); move the loop body into a function")
		return
	}
	if s.rf != nil && !s.deferLeavesLoop(st) {
		s.l.errorf(st.Pos(), diag.DeferInRangeFunc, "defer in the body of a range-over-func loop must be followed by a return, or a break or goto out of the loop (otherwise it may run once per iteration, which would need unbounded stack)")
		return
	}
	root := s.rootFn()
	if len(root.defers) >= 64 {
		s.l.errorf(st.Pos(), diag.TooManyDefers, "too many defer statements in one function (at most 64)")
		return
	}
	if root != s {
		s.lastingFrame = root
		defer func() { s.lastingFrame = nil }()
	}
	bit := len(root.defers)
	call := st.Call

	var run func(s *fnState, rec *ssa.Value)
	funTV := s.info.Types[call.Fun]
	switch {
	case funTV.IsBuiltin():
		name := s.info.Uses[identOf(ast.Unparen(call.Fun))].Name()
		switch name {
		case "print", "println":
			// Arguments are evaluated at the defer statement.
			args := make([]savedArg, len(call.Args))
			for i, a := range call.Args {
				args[i] = s.saveValue(s.printOperand(a))
			}
			run = func(s *fnState, rec *ssa.Value) {
				s.printStatement(name == "println", len(args), func(i int, buffered bool) {
					s.printValue(s.loadSaved(args[i], rec), args[i].T, call, buffered)
				})
			}
		case "close":
			ch := s.saveValue(s.materialize(scalarValue(s.scalar(call.Args[0]), s.typeOf(call.Args[0]))))
			run = func(s *fnState, rec *ssa.Value) {
				s.callRuntime("closechan", nil, []*ssa.Value{s.loadSaved(ch, rec).v})
			}
		case "panic":
			arg := s.saveValue(s.exprAs(call.Args[0], types2.NewInterfaceType(nil, nil)))
			run = func(s *fnState, rec *ssa.Value) {
				parts := s.leafValues(s.loadSaved(arg, rec), []leaf{{0, ssa.TPtr}, {8, ssa.TPtr}})
				s.callRuntimeNoReturn("panicValue", parts...)
				dead := s.newBlock(ssa.BlockInvalid)
				s.seal(dead)
				s.startBlock(dead)
			}
		default:
			s.l.errorf(st.Pos(), diag.DeferBuiltin, "defer of builtin %s is not supported", name)
			return
		}
	case s.l.moves != nil && s.l.moves.DeferredDrops[call]:
		// defer x.Drop() on an owner variable: when the function returns,
		// x's whole drop glue runs and x is left clear, so that the end of
		// its scope destroys nothing (language spec section 18.6).
		lv := s.lv(ast.Unparen(call.Fun).(*ast.SelectorExpr).X)
		T := unwrap(lv.T)
		addr := s.saveValue(scalarValue(lv.ptr, types2.NewPointer(T)))
		run = func(s *fnState, rec *ssa.Value) {
			p := s.loadSaved(addr, rec).v
			s.dropAt(p, T)
			s.zero(p, s.l.sizeof(T))
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
					sym, full := s.l.methodFunc(fn, targs, w, se.Sel.Pos(), "deferred")
					sig := s.subst(funTV.Type).(*types2.Signature)
					args := s.saveValues(s.callArgs(call, sig, def))
					site := &callSite{desc: types2.ExprString(call.Fun), recv: types2.ExprString(se.X)}
					run = func(s *fnState, rec *ssa.Value) {
						r := s.loadSaved(recv, rec)
						s.emitCall(callee{sym: sym, site: site, deferred: true}, full, &r, s.loadValues(args, rec), call.Pos())
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
				site := &callSite{desc: types2.ExprString(call.Fun)}
				run = func(s *fnState, rec *ssa.Value) {
					s.emitCall(callee{sym: sym, site: site, deferred: true}, sig, nil, s.loadValues(args, rec), call.Pos())
				}
				break
			}
		}
		// Any other callee: evaluate the func value now.
		sig := s.typeOf(call.Fun).Underlying().(*types2.Signature)
		var f *ssa.Value
		if lit, ok := fun.(*ast.FuncLit); ok && s.rf != nil {
			if f = s.deferredLit(lit); f == nil {
				return
			}
		} else {
			f = s.scalar(call.Fun)
		}
		fv := s.saveValue(scalarValue(f, s.typeOf(call.Fun)))
		args := s.saveValues(s.callArgs(call, sig, nil))
		site := &callSite{desc: types2.ExprString(call.Fun), fsig: sig}
		run = func(s *fnState, rec *ssa.Value) {
			f := s.loadSaved(fv, rec).v
			s.emitCall(callee{code: s.load(ssa.TPtr, f), ctx: f, site: site, deferred: true}, sig, nil, s.loadValues(args, rec), call.Pos())
		}
	}

	// Arm the call. In a hosted build the store is volatile: the runtime
	// reads the bits when the function panics (deferrec.go), so the store
	// must be in memory before anything after it can fault.
	bitsp := s.outerPtr(root.deferBitsAddr(), root)
	bits := s.load(ssa.TI64, bitsp)
	armed := s.newValue(ssa.OpOr, ssa.TI64, bits, s.constInt(ssa.TI64, 1<<bit))
	if root.deferRec != nil {
		s.storeVolatile(ssa.TI64, bitsp, armed)
	} else {
		s.store(ssa.TI64, bitsp, armed)
	}
	root.defers = append(root.defers, &deferInfo{run: run, mayGoexit: s.l.deferMayGoexit(s, call)})
}

// A savedArg is a value a defer statement saved for its call: in the
// defer record at off (hosted builds, deferrec.go), or in a slot of its own
// at addr, an address in the frame whose returns run the call.
type savedArg struct {
	off  int64
	addr *ssa.Value
	T    types2.Type
	cnt  countKind
}

// saveValue copies a value into storage that lasts until the deferred call
// has run, in the frame whose returns run deferred calls: in a
// range-over-func loop body, the enclosing function's.
func (s *fnState) saveValue(v value) savedArg {
	root := s.rootFn()
	counted := v.addr && s.l.counted(v.T)
	if root.deferRec != nil {
		off := root.recordField(v.T)
		ptr := s.offPtr(s.outerPtr(root.deferRec, root), off)
		a := savedArg{off: off, T: v.T}
		if counted {
			// The saved argument keeps references until the function
			// returns, after the deferred call has run.
			s.storeCounted(lvalue{ptr: ptr, T: v.T}, v, false)
			root.addFrameSlot(root.deferRec, off, v.T)
			a.cnt = cntStable
			return a
		}
		s.storeTo(ptr, v, v.T)
		return a
	}
	if counted {
		slot := root.frameSlot(v.T, "defer")
		s.storeCounted(lvalue{ptr: s.outerPtr(slot, root), T: v.T}, v, false)
		return savedArg{addr: slot, T: v.T, cnt: cntStable}
	}
	tmp := root.temp(v.T, "defer")
	s.storeTo(s.outerPtr(tmp, root), v, v.T)
	return savedArg{addr: tmp, T: v.T}
}

func (s *fnState) saveValues(vs []value) []savedArg {
	out := make([]savedArg, len(vs))
	for i, v := range vs {
		out[i] = s.saveValue(v)
	}
	return out
}

// loadSaved reads a saved value for the deferred call; rec is the address
// of the defer record in the function emitting the call.
func (s *fnState) loadSaved(a savedArg, rec *ssa.Value) value {
	ptr := a.addr
	if ptr == nil {
		ptr = s.offPtr(rec, a.off)
	}
	v := s.loadFrom(ptr, a.T)
	v.cnt = a.cnt
	return v
}

func (s *fnState) loadValues(saved []savedArg, rec *ssa.Value) []value {
	out := make([]value, len(saved))
	for i, a := range saved {
		out[i] = s.loadSaved(a, rec)
	}
	return out
}
