package eir

import (
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/scoped"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Statements, scopes and the drops at their ends (section 18.6), as
// internal/lower makes them (stmt.go, scope.go, strscope.go): every
// statement opens an entry for its temporaries; a block, and a statement
// with an implicit block, one that also holds its variables. Leaving an
// entry, by falling off its end or by return, break, continue or goto,
// drops what it holds, the last first, except the variables the move
// checker knows hold nothing there.

// A scope is one entry of the cleanup stack.
type scope struct {
	node  ast.Node
	vars  bool // variables declared in it are dropped with it
	slots []dropSlot
}

// A dropSlot is an owner in a frame object, dropped when its entry ends.
type dropSlot struct {
	obj   ObjID
	T     types2.Type
	v     *types2.Var // the variable, or nil for a temporary
	pos   token.Pos
	first bool // a sync.Scope variable: dropped before the others
}

// A jumpTarget is an enclosing loop, switch or select.
type jumpTarget struct {
	label    *types2.Label
	breakTo  *bblock
	contTo   *bblock // nil for a switch or select
	depth    int     // the scope depth at the statement
	rangeFor *rangeCtx
}

// A labelInfo is a label's block.
type labelInfo struct {
	block *bblock
}

func (fb *fnb) pushScope(node ast.Node, vars bool) *scope {
	sc := &scope{node: node, vars: vars}
	fb.scopes = append(fb.scopes, sc)
	return sc
}

// popScope closes the innermost scope, dropping what it holds if the code
// falling out of it is reachable.
func (fb *fnb) popScope(sc *scope) {
	n := len(fb.scopes)
	if n == 0 || fb.scopes[n-1] != sc {
		fb.b.errorf(fb.pos, "unbalanced scopes")
		return
	}
	if fb.cur != nil {
		fb.releaseSlots(sc.slots, fb.b.moves.Empty[sc.node])
	}
	fb.scopes = fb.scopes[:n-1]
}

// releaseSlots drops slots, sync.Scope variables first, the last first,
// except the variables known says hold nothing.
func (fb *fnb) releaseSlots(slots []dropSlot, known moves.Known) {
	for _, first := range [2]bool{true, false} {
		for i := len(slots) - 1; i >= 0; i-- {
			c := slots[i]
			if c.first != first {
				continue
			}
			if c.v != nil {
				if held, ok := known[c.v]; ok && !held.Box && len(held.Parts) == 0 {
					continue
				}
			}
			fb.dropAt(fb.addr(c.obj), c.T, c.pos)
		}
	}
}

// releaseAbove drops the scopes above depth, for a jump out of them.
func (fb *fnb) releaseAbove(depth int, known moves.Known) {
	if fb.cur == nil {
		return
	}
	for i := len(fb.scopes) - 1; i >= depth; i-- {
		fb.releaseSlots(fb.scopes[i].slots, known)
	}
}

// addVarSlot registers owner variable v, in object obj, with the innermost
// scope that holds variables.
func (fb *fnb) addVarSlot(obj ObjID, T types2.Type, v *types2.Var, pos token.Pos) {
	if !fb.b.needsDrop(T) {
		return
	}
	c := dropSlot{obj: obj, T: T, v: v, pos: pos, first: scoped.IsScopeType(T)}
	for i := len(fb.scopes) - 1; i >= 0; i-- {
		if fb.scopes[i].vars {
			fb.scopes[i].slots = append(fb.scopes[i].slots, c)
			return
		}
	}
	fb.b.errorf(pos, "owner variable %s outside any scope", v.Name())
}

// addTemp registers an owner temporary with the innermost scope; outside
// any (a package variable's initializer) it is never dropped.
func (fb *fnb) addTemp(obj ObjID, T types2.Type) {
	if !fb.b.needsDrop(T) || len(fb.scopes) == 0 {
		return
	}
	sc := fb.scopes[len(fb.scopes)-1]
	sc.slots = append(sc.slots, dropSlot{obj: obj, T: T, pos: fb.pos})
}

// endStmtTemps ends the lifetime of the statement temporaries (LifeStmt
// objects) made since object objStart, by the statement st just built and
// not by a statement within it: an OpDrop of each at its end.
func (fb *fnb) endStmtTemps(objStart int, st ast.Stmt) {
	for i := objStart; i < len(fb.f.Objects); i++ {
		if fb.f.Objects[i].Life != LifeStmt || fb.stmtEnded[ObjID(i)] {
			continue
		}
		if fb.stmtEnded == nil {
			fb.stmtEnded = map[ObjID]bool{}
		}
		fb.stmtEnded[ObjID(i)] = true
		if fb.cur != nil {
			fb.emit(Instr{Op: OpDrop, Obj: ObjID(i), Pos: st.End()})
		}
	}
}

// ownerTemp keeps the owner value r, of type T, in a statement temporary
// that is dropped when the statement ends, and returns the value.
func (fb *fnb) ownerTemp(r Reg, T types2.Type) Reg {
	if !fb.b.needsDrop(T) {
		return r
	}
	o := fb.newObj(Object{Kind: ObjStmtTemp, Type: T, Life: LifeStmt, Name: "temporary"})
	fb.store(fb.addr(o), r, T)
	fb.addTemp(o, T)
	return r
}

// declareLocal declares a local variable of this function and registers
// its drop.
func (fb *fnb) declareLocal(v *types2.Var) {
	if fb.b.needsDrop(v.Type()) || scoped.IsScopeType(v.Type()) {
		fb.addressed[v] = true
	}
	fb.declareVar(v, ObjVar, v.Pos())
	if o, ok := fb.objs[v]; ok {
		fb.addVarSlot(o, v.Type(), v, v.Pos())
	}
}

// ----------------------------------------------------------------------------
// Statements

func (fb *fnb) stmtList(list []ast.Stmt) {
	for _, st := range list {
		fb.stmt(st)
	}
}

// block builds a statement list as a block of its own.
func (fb *fnb) block(node ast.Node, list []ast.Stmt) {
	sc := fb.pushScope(node, true)
	fb.stmtList(list)
	fb.popScope(sc)
}

func isScopeStmt(st ast.Stmt) bool {
	switch st.(type) {
	case *ast.BlockStmt, *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		return true
	}
	return false
}

func (fb *fnb) stmt(st ast.Stmt) {
	saved := fb.pos
	if p := st.Pos(); p.IsValid() {
		fb.pos = p
	}
	defer func() { fb.pos = saved }()
	if _, isLabel := st.(*ast.LabeledStmt); !isLabel {
		fb.ensureBlock()
		// A drop may end with rt.ExitTask: once the statement's drops are
		// done, the function leaves if one did (lower/stmt.go stmt).
		// The lowerer checks after any statement that released a counted
		// reference or dropped an owner, which may have run code that
		// ended with rt.ExitTask. EIR, which does not follow releases,
		// checks after every statement that dropped an owner or has a
		// value that holds counted references or owners (a way out more is
		// only less precise; one less would be a hole).
		dropsBefore := fb.drops
		defer func() {
			if fb.b.gx.active && !fb.gxCatch() && (fb.drops != dropsBefore || fb.b.mayRelease(st)) {
				fb.leaveForExit(st.Pos())
			}
		}()
		// The statement's temporaries end with it, after its drops: a
		// register still holding a view of one is E3009 (section 18.3).
		objStart := len(fb.f.Objects)
		defer fb.endStmtTemps(objStart, st)
		sc := fb.pushScope(st, isScopeStmt(st))
		defer fb.popScope(sc)
		savedStmt := fb.curStmt
		fb.curStmt = st
		defer func() { fb.curStmt = savedStmt }()
	}
	switch st := st.(type) {
	case *ast.EmptyStmt:
	case *ast.ExprStmt:
		if call, ok := ast.Unparen(st.X).(*ast.CallExpr); ok {
			res, types := fb.callResults(call)
			for i, r := range res {
				// Results that are owners are temporaries.
				if i < len(types) && types[i] != nil {
					fb.ownerTemp(r, types[i])
				}
			}
			return
		}
		fb.expr(st.X)
	case *ast.DeclStmt:
		fb.declStmt(st.Decl.(*ast.GenDecl))
	case *ast.AssignStmt:
		fb.assignStmt(st)
	case *ast.OrStmt:
		fb.orStmt(st)
	case *ast.IncDecStmt:
		lv := fb.lvalue(st.X)
		fb.opAssign(lv, 0, st.Pos())
	case *ast.BlockStmt:
		fb.stmtList(st.List)
	case *ast.IfStmt:
		fb.ifStmt(st)
	case *ast.ForStmt:
		fb.forStmt(st, nil)
	case *ast.RangeStmt:
		fb.rangeStmt(st, nil)
	case *ast.SwitchStmt:
		fb.switchStmt(st, nil)
	case *ast.TypeSwitchStmt:
		fb.typeSwitchStmt(st, nil)
	case *ast.LabeledStmt:
		fb.labeledStmt(st)
	case *ast.BranchStmt:
		fb.branchStmt(st)
	case *ast.ReturnStmt:
		fb.returnStmt(st)
	case *ast.DeferStmt:
		fb.deferStmt(st)
	case *ast.TaskStmt:
		fb.taskStmt(st)
	case *ast.SendStmt:
		fb.sendStmt(st)
	case *ast.SelectStmt:
		fb.selectStmt(st, nil)
	default:
		fb.b.errorf(st.Pos(), "unknown statement %T", st)
	}
}

// headerStmt builds the init statement of an if or switch statement, whose
// temporaries live until the whole statement ends.
func (fb *fnb) headerStmt(st ast.Stmt) {
	if st == nil {
		return
	}
	// The header's temporaries are the enclosing statement's: build it
	// without an entry of its own.
	saved := fb.curStmt
	fb.curStmt = st
	switch st := st.(type) {
	case *ast.AssignStmt:
		fb.assignStmt(st)
	case *ast.ExprStmt:
		if call, ok := ast.Unparen(st.X).(*ast.CallExpr); ok {
			res, types := fb.callResults(call)
			for i, r := range res {
				if i < len(types) && types[i] != nil {
					fb.ownerTemp(r, types[i])
				}
			}
		} else {
			fb.expr(st.X)
		}
	default:
		fb.curStmt = saved
		fb.stmt(st)
		return
	}
	fb.curStmt = saved
}

func (fb *fnb) declStmt(d *ast.GenDecl) {
	if d.Tok != token.VAR {
		return
	}
	for _, spec := range d.Specs {
		vs := spec.(*ast.ValueSpec)
		vars := make([]*types2.Var, len(vs.Names))
		for i, n := range vs.Names {
			vars[i], _ = fb.b.defs[n].(*types2.Var)
		}
		switch {
		case len(vs.Values) == 0:
			for _, v := range vars {
				if v != nil {
					fb.declareLocal(v)
				}
			}
		case len(vs.Values) == len(vs.Names):
			vals := make([]Reg, len(vars))
			for i, e := range vs.Values {
				var T types2.Type
				if vars[i] != nil {
					T = vars[i].Type()
				}
				vals[i] = fb.exprAs(e, T)
			}
			for i, v := range vars {
				if v != nil {
					fb.declareLocal(v)
					fb.assignVar(v, vals[i])
				} else {
					fb.ownerTemp(vals[i], fb.typeOf(vs.Values[i]))
				}
			}
		default:
			vals, types := fb.multiValue(vs.Values[0], len(vars))
			for i, v := range vars {
				if v != nil {
					fb.declareLocal(v)
					fb.assignVar(v, fb.convertReg(vals[i], types[i], v.Type(), vs.Pos()))
				}
			}
		}
	}
}

// multiValue builds an expression with n values: a call, or a comma-ok
// form. It returns the values and their types.
func (fb *fnb) multiValue(e ast.Expr, n int) ([]Reg, []types2.Type) {
	switch x := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		if tv := fb.b.types[x.Fun]; tv.IsType() {
			// An enum conversion with ok.
			r := fb.expr(x.Args[0])
			_ = r
			return make([]Reg, n), []types2.Type{fb.firstType(x), types2.Typ[types2.Bool]}
		}
		return fb.callResults(x)
	case *ast.TypeAssertExpr:
		v := fb.typeAssert(x, true)
		return []Reg{v, 0}, []types2.Type{fb.firstType(x), types2.Typ[types2.Bool]}
	case *ast.UnaryExpr:
		if x.Op == token.ARROW {
			v := fb.recv(x)
			return []Reg{v, 0}, []types2.Type{fb.firstType(x), types2.Typ[types2.Bool]}
		}
	case *ast.IndexExpr:
		if fb.isContainerIndex(x) || fb.isMapIndex(x) {
			return fb.indexOk(x)
		}
	}
	fb.b.errorf(e.Pos(), "expression does not produce %d values", n)
	return make([]Reg, n), make([]types2.Type, n)
}

func (fb *fnb) assignStmt(st *ast.AssignStmt) {
	switch st.Tok {
	case token.ASSIGN, token.DEFINE:
	default:
		lv := fb.lvalue(st.Lhs[0])
		y := fb.expr(st.Rhs[0])
		fb.opAssign(lv, y, st.Pos())
		return
	}
	type target struct {
		lv    *lval
		def   *types2.Var
		blank bool
	}
	targets := make([]target, len(st.Lhs))
	for i, lhs := range st.Lhs {
		if id, ok := lhs.(*ast.Ident); ok && id.Name == "_" {
			targets[i].blank = true
			continue
		}
		if st.Tok == token.DEFINE {
			if v, ok := fb.b.defs[lhs.(*ast.Ident)].(*types2.Var); ok {
				targets[i].def = v
				continue
			}
		}
		lv := fb.lvalue(lhs)
		targets[i].lv = &lv
	}
	var vals []Reg
	var types []types2.Type
	if len(st.Rhs) == len(st.Lhs) {
		vals = make([]Reg, len(st.Rhs))
		types = make([]types2.Type, len(st.Rhs))
		for i, e := range st.Rhs {
			var T types2.Type
			switch t := targets[i]; {
			case t.def != nil:
				T = t.def.Type()
			case t.lv != nil:
				T = t.lv.T
			}
			vals[i] = fb.exprAs(e, T)
			types[i] = T
			if T == nil {
				types[i] = fb.typeOf(e)
			}
		}
	} else {
		vals, types = fb.multiValue(st.Rhs[0], len(st.Lhs))
	}
	permutes := fb.permutes(st)
	for i, t := range targets {
		switch {
		case t.blank:
			// A discarded owner is a temporary of the statement.
			if i < len(vals) {
				fb.ownerTemp(vals[i], types[i])
			}
		case t.def != nil:
			fb.declareLocal(t.def)
			fb.assignVar(t.def, fb.convertReg(vals[i], types[i], t.def.Type(), st.Pos()))
		default:
			T := t.lv.T
			if !permutes && !fb.b.moves.Fresh[st.Lhs[i]] && t.lv.dropsOld() && fb.b.needsDrop(unwrap(T)) {
				// Assigning to a place that holds an owner drops the old
				// value first.
				fb.dropAt(fb.lvAddr(*t.lv), unwrap(T), st.Pos())
			}
			fb.storeLV(*t.lv, fb.convertReg(vals[i], types[i], T, st.Pos()))
		}
	}
}

// permutes reports whether an assignment's right-hand sides are its
// left-hand sides in another order, which moves nothing in or out.
func (fb *fnb) permutes(st *ast.AssignStmt) bool {
	if len(st.Lhs) < 2 || len(st.Lhs) != len(st.Rhs) {
		return false
	}
	text := func(e ast.Expr) string { return types2.ExprString(e) }
	used := make([]bool, len(st.Lhs))
	for _, r := range st.Rhs {
		found := false
		for j, l := range st.Lhs {
			if !used[j] && text(l) == text(r) {
				used[j] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// orStmt builds an or statement (section 11.27): the call, the
// assignment of its results but the error, and the clause, which runs when
// the error is not nil.
func (fb *fnb) orStmt(st *ast.OrStmt) {
	stmtScope := fb.scopes[len(fb.scopes)-1] // pushed by stmt for st
	res, types := fb.callResults(st.Call)
	if fb.cur == nil || len(res) == 0 {
		return
	}
	n := len(res) - 1
	errR, errT := res[n], types[n]

	// The clause, where the call failed: in its own block, which declares
	// err; the other results and the statement's temporaries are dropped
	// first.
	fail := fb.newBlock()
	ok := fb.newBlock()
	fb.branchBoth(fail, ok)
	fb.seal(fail)
	fb.startBlock(fail)
	sc := fb.pushScope(st.Clause, true)
	errVar, _ := fb.b.implicits[st].(*types2.Var)
	if errVar != nil {
		fb.declareLocal(errVar)
		fb.assignVar(errVar, fb.convertReg(errR, errT, errVar.Type(), st.Pos()))
	}
	for i := 0; i < n; i++ {
		if fb.b.needsDrop(types[i]) {
			o := fb.newObj(Object{Kind: ObjTemp, Type: types[i], Name: "drop"})
			fb.store(fb.addr(o), res[i], types[i])
			fb.dropAt(fb.addr(o), types[i], st.Pos())
		}
	}
	fb.releaseSlots(stmtScope.slots, nil)
	if r, isRet := st.Clause.(*ast.ReturnStmt); isRet && fb.orReturn(st, r, errVar) {
		fb.popScope(sc)
	} else {
		fb.stmt(st.Clause)
		fb.popScope(sc)
	}
	fb.jump(ok)

	fb.seal(ok)
	fb.startBlock(ok)
	if len(st.Lhs) > 0 {
		for i, lhs := range st.Lhs {
			if i >= n {
				break
			}
			if id, ok := lhs.(*ast.Ident); ok && id.Name == "_" {
				fb.ownerTemp(res[i], types[i])
				continue
			}
			if st.Tok == token.DEFINE {
				if v, ok := fb.b.defs[lhs.(*ast.Ident)].(*types2.Var); ok {
					fb.declareLocal(v)
					fb.assignVar(v, fb.convertReg(res[i], types[i], v.Type(), st.Pos()))
					continue
				}
			}
			lv := fb.lvalue(lhs)
			if lv.dropsOld() && !fb.b.moves.Fresh[lhs] && fb.b.needsDrop(unwrap(lv.T)) {
				fb.dropAt(fb.lvAddr(lv), unwrap(lv.T), st.Pos())
			}
			fb.storeLV(lv, fb.convertReg(res[i], types[i], lv.T, st.Pos()))
		}
	} else {
		for i := 0; i < n; i++ {
			fb.ownerTemp(res[i], types[i])
		}
	}
}

// orReturn builds a short form of return in the clause of an or statement
// (section 11.27): the last result is err, or E, and the others are zero
// if unnamed and keep their values if named. It reports false for a plain
// return.
func (fb *fnb) orReturn(st *ast.OrStmt, r *ast.ReturnStmt, errVar *types2.Var) bool {
	root := fb.root
	sig := root.f.Sig
	if root.f.Key.Range != nil || sig == nil {
		return false
	}
	form := types2.OrReturnForm(sig.Results(), r, fb.b.types)
	if form == types2.OrReturnPlain {
		return false
	}
	n := len(root.results)
	if n == 0 {
		return false
	}
	T := root.results[n-1].Type()
	var v Reg
	if form == types2.OrReturnExpr {
		v = fb.exprAs(r.Results[0], T)
	} else if errVar != nil {
		v = fb.readLocal(errVar)
	}
	fb.setResult(root, n-1, v)
	if fb.rng != nil {
		pos := r.Pos()
		fb.exitTo(root, returnExitKey{}, fb.b.moves.Empty[r], func(p *fnb) { p.emitReturn(pos, nil) })
		return true
	}
	fb.emitReturn(r.Pos(), fb.b.moves.Empty[r])
	return true
}

// cond builds a condition with its two exits.
func (fb *fnb) cond(e ast.Expr, yes, no *bblock) {
	switch x := ast.Unparen(e).(type) {
	case *ast.BinaryExpr:
		switch x.Op {
		case token.LAND:
			mid := fb.newBlock()
			fb.cond(x.X, mid, no)
			fb.seal(mid)
			fb.startBlock(mid)
			fb.cond(x.Y, yes, no)
			return
		case token.LOR:
			mid := fb.newBlock()
			fb.cond(x.X, yes, mid)
			fb.seal(mid)
			fb.startBlock(mid)
			fb.cond(x.Y, yes, no)
			return
		}
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			fb.cond(x.X, no, yes)
			return
		}
	}
	fb.expr(e)
	b := fb.ensureBlock()
	fb.edge(b, yes)
	fb.edge(b, no)
	fb.cur = nil
}

func (fb *fnb) ifStmt(st *ast.IfStmt) {
	fb.headerStmt(st.Init)
	then := fb.newBlock()
	done := fb.newBlock()
	els := done
	if st.Else != nil {
		els = fb.newBlock()
	}
	fb.cond(st.Cond, then, els)
	fb.seal(then)
	fb.startBlock(then)
	fb.block(st.Body, st.Body.List)
	fb.jump(done)
	if st.Else != nil {
		fb.seal(els)
		fb.startBlock(els)
		fb.stmt(st.Else)
		fb.jump(done)
	}
	fb.seal(done)
	fb.startBlock(done)
}

func (fb *fnb) forStmt(st *ast.ForStmt, label *types2.Label) {
	fb.headerMark = len(fb.f.Objects)
	if st.Init != nil {
		fb.stmt(st.Init)
	}
	head := fb.newBlock()
	fb.headerVars(head)
	body := fb.newBlock()
	post := fb.newBlock()
	done := fb.newBlock()
	fb.jump(head)
	fb.startBlock(head)
	if st.Cond == nil {
		fb.jump(body)
	} else {
		sc := fb.pushScope(st.Cond, false)
		fb.cond(st.Cond, body, done)
		// The condition's temporaries are dropped after each evaluation;
		// the condition's blocks have ended, so drop them on both ways.
		fb.scopes = fb.scopes[:len(fb.scopes)-1]
		if len(sc.slots) > 0 {
			fb.dropOnEdges(sc, body, done)
		}
	}
	fb.seal(body)
	fb.startBlock(body)
	fb.targets = append(fb.targets, &jumpTarget{label: label, breakTo: done, contTo: post, depth: len(fb.scopes)})
	fb.block(st.Body, st.Body.List)
	fb.targets = fb.targets[:len(fb.targets)-1]
	fb.jump(post)
	fb.seal(post)
	fb.startBlock(post)
	if st.Post != nil {
		fb.stmt(st.Post)
	}
	fb.jump(head)
	fb.seal(head)
	fb.seal(done)
	fb.startBlock(done)
}

// headerVars makes the variables declared since headerMark variables of
// the loop whose header is head.
func (fb *fnb) headerVars(head *bblock) {
	if fb.headerMark < 0 {
		return
	}
	for o := fb.headerMark; o < len(fb.f.Objects); o++ {
		if fb.f.Objects[o].Kind == ObjVar {
			fb.iterHead[ObjID(o)] = head.b
		}
	}
	fb.headerMark = -1
}

// dropOnEdges drops a condition's temporaries at the start of both blocks
// it branches to.
func (fb *fnb) dropOnEdges(sc *scope, blocks ...*bblock) {
	for _, b := range blocks {
		saved := fb.cur
		fb.cur = b
		fb.releaseSlots(sc.slots, nil)
		fb.cur = saved
	}
}

// loopBody builds a loop whose body is built by body, with targets for
// break and continue.
func (fb *fnb) loop(label *types2.Label, header func(body, done *bblock), body func()) {
	head := fb.newBlock()
	fb.headerVars(head)
	bodyB := fb.newBlock()
	post := fb.newBlock()
	done := fb.newBlock()
	fb.jump(head)
	fb.startBlock(head)
	header(bodyB, done)
	fb.seal(bodyB)
	fb.startBlock(bodyB)
	fb.targets = append(fb.targets, &jumpTarget{label: label, breakTo: done, contTo: post, depth: len(fb.scopes)})
	body()
	fb.targets = fb.targets[:len(fb.targets)-1]
	fb.jump(post)
	fb.seal(post)
	fb.startBlock(post)
	fb.jump(head)
	fb.seal(head)
	fb.seal(done)
	fb.startBlock(done)
}

// branchBoth ends the current block with edges to both blocks.
func (fb *fnb) branchBoth(a, b *bblock) {
	cur := fb.ensureBlock()
	fb.edge(cur, a)
	fb.edge(cur, b)
	fb.cur = nil
}

func (fb *fnb) switchStmt(st *ast.SwitchStmt, label *types2.Label) {
	fb.headerStmt(st.Init)
	var tag Reg
	var tagT types2.Type
	if st.Tag != nil {
		tagT = fb.typeOf(st.Tag)
		tag = fb.expr(st.Tag)
	}
	done := fb.newBlock()
	clauses := st.Body.List
	bodies := make([]*bblock, len(clauses))
	for i := range clauses {
		bodies[i] = fb.newBlock()
	}
	defaultBody := done
	for i, c := range clauses {
		cc := c.(*ast.CaseClause)
		if cc.List == nil {
			defaultBody = bodies[i]
			continue
		}
		for _, e := range cc.List {
			next := fb.newBlock()
			if st.Tag == nil {
				fb.cond(e, bodies[i], next)
			} else {
				eT := fb.typeOf(e)
				v := fb.expr(e)
				if isInterface(tagT) && !isInterface(eT) {
					fb.conv(v, eT, tagT, e.Pos())
				} else if !isInterface(tagT) && isInterface(eT) {
					fb.conv(tag, tagT, eT, e.Pos())
				}
				fb.branchBoth(bodies[i], next)
			}
			fb.seal(next)
			fb.startBlock(next)
		}
	}
	fb.jump(defaultBody)
	fb.targets = append(fb.targets, &jumpTarget{label: label, breakTo: done, depth: len(fb.scopes)})
	for i, c := range clauses {
		cc := c.(*ast.CaseClause)
		fb.seal(bodies[i])
		fb.startBlock(bodies[i])
		body := cc.Body
		fall := false
		if n := len(body); n > 0 {
			if br, ok := body[n-1].(*ast.BranchStmt); ok && br.Tok == token.FALLTHROUGH {
				fall = true
				body = body[:n-1]
			}
		}
		fb.block(cc, body)
		if fall && i+1 < len(clauses) {
			fb.jump(bodies[i+1])
		} else {
			fb.jump(done)
		}
	}
	fb.targets = fb.targets[:len(fb.targets)-1]
	fb.seal(done)
	fb.startBlock(done)
}

func (fb *fnb) typeSwitchStmt(st *ast.TypeSwitchStmt, label *types2.Label) {
	fb.headerStmt(st.Init)
	var x ast.Expr
	switch a := st.Assign.(type) {
	case *ast.AssignStmt:
		x = a.Rhs[0].(*ast.TypeAssertExpr).X
	case *ast.ExprStmt:
		x = a.X.(*ast.TypeAssertExpr).X
	}
	I := fb.typeOf(x)
	union := types2.UnionVariants(I) != nil
	chosen := fb.staticTypeSwitch(st, x)
	if chosen != nil {
		// A switch on a conversion to an interface of a value whose type is
		// known: only the clause that matches is built, as internal/lower
		// builds only it.
		done := fb.newBlock()
		fb.targets = append(fb.targets, &jumpTarget{label: label, breakTo: done, depth: len(fb.scopes)})
		if chosen.cc != nil {
			sc := fb.pushScope(chosen.cc, true)
			if v, ok := fb.b.implicits[chosen.cc].(*types2.Var); ok {
				fb.declareLocal(v)
				conv := ast.Unparen(x).(*ast.CallExpr)
				if chosen.only != nil && !isInterface(chosen.only) {
					fb.assignVar(v, fb.exprAs(conv.Args[0], v.Type()))
				} else {
					fb.assignVar(v, fb.exprAs(x, v.Type()))
				}
			}
			fb.stmtList(chosen.cc.Body)
			fb.popScope(sc)
		}
		fb.jump(done)
		fb.targets = fb.targets[:len(fb.targets)-1]
		fb.seal(done)
		fb.startBlock(done)
		return
	}
	iv := fb.expr(x)
	if !union && fb.b.moves.SwitchMoves[st] {
		// The owner moves out of x into the binding of the clause that
		// runs; with no default clause, an unmatched value is dropped.
		copied := fb.newObj(Object{Kind: ObjTemp, Type: I, Name: "switched"})
		fb.store(fb.addr(copied), iv, I)
		hasDefault := false
		for _, c := range st.Body.List {
			if c.(*ast.CaseClause).List == nil {
				hasDefault = true
			}
		}
		if !hasDefault {
			fb.switchDrop = &switchDrop{obj: copied, T: I}
		}
	}
	done := fb.newBlock()
	clauses := st.Body.List
	bodies := make([]*bblock, len(clauses))
	for i := range clauses {
		bodies[i] = fb.newBlock()
	}
	defaultBody := done
	for i, c := range clauses {
		cc := c.(*ast.CaseClause)
		if cc.List == nil {
			defaultBody = bodies[i]
			continue
		}
		for _, e := range cc.List {
			if tv := fb.b.types[e]; !tv.IsNil() && tv.Type != nil {
				fb.b.noteAsserted(tv.Type)
			}
			next := fb.newBlock()
			fb.branchBoth(bodies[i], next)
			fb.seal(next)
			fb.startBlock(next)
		}
	}
	if d := fb.switchDrop; d != nil {
		fb.switchDrop = nil
		fb.dropAt(fb.addr(d.obj), d.T, st.Pos())
	}
	fb.jump(defaultBody)
	fb.targets = append(fb.targets, &jumpTarget{label: label, breakTo: done, depth: len(fb.scopes)})
	for i, c := range clauses {
		cc := c.(*ast.CaseClause)
		fb.seal(bodies[i])
		fb.startBlock(bodies[i])
		sc := fb.pushScope(cc, true)
		if v, ok := fb.b.implicits[cc].(*types2.Var); ok {
			fb.declareLocal(v)
			T := v.Type()
			switch {
			case union:
				// The payload of the variant, or the union itself.
				fb.assignVar(v, iv)
			case isInterface(T) && types2.Identical(T, I):
				fb.assignVar(v, iv)
			case lendsError(I, T):
				fb.assignVar(v, fb.borrowError(x, fb.assertTo(iv, T, cc.Pos()))) // borrow.go
			case countsTo(I, T) == lendCopy:
				fb.assignVar(v, fb.recount(fb.assertTo(iv, T, cc.Pos()), T))
			default:
				fb.assignVar(v, fb.assertTo(iv, T, cc.Pos()))
			}
		}
		fb.stmtList(cc.Body)
		fb.popScope(sc)
		fb.jump(done)
	}
	fb.targets = fb.targets[:len(fb.targets)-1]
	fb.seal(done)
	fb.startBlock(done)
}

type switchDrop struct {
	obj ObjID
	T   types2.Type
}

// A staticClause is the one clause internal/lower builds of a type switch
// on a conversion to an interface of a value of known type (cc nil: none).
type staticClause struct {
	cc   *ast.CaseClause
	only types2.Type
}

// staticTypeSwitch returns the clause internal/lower's staticTypeSwitch
// chooses, or nil if the switch is not one it folds. A type parameter's
// value folds per instance, so the generic body builds every clause.
func (fb *fnb) staticTypeSwitch(st *ast.TypeSwitchStmt, x ast.Expr) *staticClause {
	conv, ok := ast.Unparen(x).(*ast.CallExpr)
	if !ok || len(conv.Args) != 1 || !fb.b.types[conv.Fun].IsType() || callsFunction(fb.b, conv.Args[0]) {
		return nil
	}
	I := fb.typeOf(x)
	C := fb.typeOf(conv.Args[0])
	if !isInterface(I) || isInterface(C) || isTypeParam(C) || containsTypeParam(C) {
		return nil
	}
	if b, ok := C.Underlying().(*types2.Basic); ok && b.Info()&types2.IsUntyped != 0 {
		return nil
	}
	out := &staticClause{}
	for _, c := range st.Body.List {
		cc := c.(*ast.CaseClause)
		for _, e := range cc.List {
			tv := fb.b.types[e]
			if tv.IsNil() {
				continue
			}
			T := tv.Type
			var match bool
			if iface, ok := T.Underlying().(*types2.Interface); ok {
				match = types2.Implements(C, iface)
			} else {
				match = types2.Identical(C, T)
			}
			if match {
				out.cc = cc
				if len(cc.List) == 1 {
					out.only = T
				}
				return out
			}
		}
	}
	for _, c := range st.Body.List {
		if cc := c.(*ast.CaseClause); cc.List == nil {
			out.cc = cc
		}
	}
	return out
}

// callsFunction reports whether e contains a function call (a conversion
// or a built-in folded to a constant is not one).
func callsFunction(b *builder, e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		if c, ok := n.(*ast.CallExpr); ok {
			if tv := b.types[c.Fun]; tv.IsType() {
				return true
			}
			if tv := b.types[c]; !tv.Value.IsNil() {
				return true
			}
			found = true
		}
		return true
	})
	return found
}

func (fb *fnb) labeledStmt(st *ast.LabeledStmt) {
	label, _ := fb.b.defs[st.Label].(*types2.Label)
	b := fb.labelBlock(label)
	fb.jump(b)
	fb.startBlock(b)
	switch inner := st.Stmt.(type) {
	case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		saved := fb.pos
		fb.pos = inner.Pos()
		sc := fb.pushScope(inner, true)
		switch inner := inner.(type) {
		case *ast.ForStmt:
			fb.forStmt(inner, label)
		case *ast.RangeStmt:
			fb.rangeStmt(inner, label)
		case *ast.SwitchStmt:
			fb.switchStmt(inner, label)
		case *ast.TypeSwitchStmt:
			fb.typeSwitchStmt(inner, label)
		case *ast.SelectStmt:
			fb.selectStmt(inner, label)
		}
		fb.popScope(sc)
		fb.pos = saved
	default:
		fb.stmt(inner)
	}
}

// labelBlock returns a label's block. Labels are sealed when the function
// ends, since a goto may reach them from anywhere.
func (fb *fnb) labelBlock(l *types2.Label) *bblock {
	li := fb.labels[l]
	if li == nil {
		li = &labelInfo{block: fb.newBlock()}
		fb.labels[l] = li
	}
	return li.block
}

func (fb *fnb) branchStmt(st *ast.BranchStmt) {
	var label *types2.Label
	if st.Label != nil {
		label, _ = fb.b.uses[st.Label].(*types2.Label)
	}
	known := fb.b.moves.Empty[st]
	switch st.Tok {
	case token.GOTO:
		if fb.rng != nil && !fb.rng.contains(label.Pos()) {
			fb.rangeExit(st, known)
			return
		}
		fb.releaseForGoto(label, known)
		fb.jump(fb.labelBlock(label))
		return
	case token.FALLTHROUGH:
		return
	}
	for i := len(fb.targets) - 1; i >= 0; i-- {
		t := fb.targets[i]
		if label != nil && t.label != label {
			continue
		}
		if t.rangeFor != nil {
			// The range-over-func loop whose body this is: return from
			// the body.
			fb.bodyReturn(known)
			return
		}
		if st.Tok == token.CONTINUE {
			if t.contTo == nil {
				continue
			}
			fb.releaseAbove(t.depth, known)
			fb.jump(t.contTo)
			return
		}
		fb.releaseAbove(t.depth, known)
		fb.jump(t.breakTo)
		return
	}
	if fb.rng != nil {
		fb.rangeExit(st, known)
		return
	}
	fb.b.errorf(st.Pos(), "no target for %s", st.Tok)
}

// releaseForGoto drops, for a goto, the scopes the jump leaves: those that
// do not contain the label, and the variables of the label's own block
// declared after it.
func (fb *fnb) releaseForGoto(label *types2.Label, known moves.Known) {
	if fb.cur == nil {
		return
	}
	enclosing := fb.labelEnclosing(label)
	for i := len(fb.scopes) - 1; i >= 0; i-- {
		sc := fb.scopes[i]
		if sc.vars && enclosing[sc.node] {
			var after []dropSlot
			for _, c := range sc.slots {
				if c.v != nil && c.pos >= label.Pos() {
					after = append(after, c)
				}
			}
			fb.releaseSlots(after, known)
			return
		}
		fb.releaseSlots(sc.slots, known)
	}
}

// labelEnclosing returns the nodes that enclose a label's statement, in the
// declaration being built.
func (fb *fnb) labelEnclosing(label *types2.Label) map[ast.Node]bool {
	out := map[ast.Node]bool{}
	var body ast.Node
	for f := fb; f != nil; f = f.parent {
		body = f.bodyNode
	}
	if body == nil {
		return out
	}
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if ls, ok := n.(*ast.LabeledStmt); ok && fb.b.defs[ls.Label] == label {
			for _, s := range stack {
				out[s] = true
			}
		}
		stack = append(stack, n)
		return true
	})
	return out
}

// returnStmt builds a return: the results are assigned to the result
// variables, the deferred calls run, the scopes and the frame are
// released, and the results go to the caller.
func (fb *fnb) returnStmt(st *ast.ReturnStmt) {
	if fb.rng != nil {
		fb.rangeReturn(st)
		return
	}
	fb.assignResults(fb, st)
	fb.emitReturn(st.Pos(), fb.b.moves.Empty[st])
}

// assignResults assigns the results of st to the result variables of root.
func (fb *fnb) assignResults(root *fnb, st *ast.ReturnStmt) {
	n := len(root.results)
	switch {
	case len(st.Results) == 0:
	case len(st.Results) == n:
		vals := make([]Reg, n)
		for i, e := range st.Results {
			vals[i] = fb.exprAs(e, root.results[i].Type())
		}
		for i, v := range vals {
			fb.dropResult(root, st, i)
			fb.setResult(root, i, v)
		}
	default:
		vals, types := fb.multiValue(st.Results[0], n)
		for i, v := range vals {
			fb.dropResult(root, st, i)
			fb.setResult(root, i, fb.convertReg(v, types[i], root.results[i].Type(), st.Pos()))
		}
	}
}

// setResult gives result i of root the value r.
func (fb *fnb) setResult(root *fnb, i int, r Reg) {
	v := root.results[i]
	if fb == root {
		fb.assignVar(v, r)
		return
	}
	fb.store(fb.outerAddr(v), r, v.Type())
}

// dropResult drops what named result i holds before a return replaces it.
func (fb *fnb) dropResult(root *fnb, st *ast.ReturnStmt, i int) {
	v := root.results[i]
	if v.Name() == "" || !fb.b.needsDrop(v.Type()) || fb.b.moves.EmptyResults[st][v] {
		return
	}
	var a Reg
	if fb == root {
		a = fb.varAddr(v)
	} else {
		a = fb.outerAddr(v)
	}
	fb.dropAt(a, v.Type(), st.Pos())
}

// emitReturn ends the function: deferred calls, then the drops of every
// open scope and of the frame, then the results.
func (fb *fnb) emitReturn(pos token.Pos, known moves.Known) {
	fb.emitReturnFor(pos, known, false)
}

// emitReturnFor is emitReturn; exiting says the function leaves because a
// call ended with rt.ExitTask: its caller never receives the results, so
// the results that are owners are dropped (lower/func.go finishReturn).
func (fb *fnb) emitReturnFor(pos token.Pos, known moves.Known, exiting bool) {
	if fb.cur == nil {
		fb.ensureBlock()
	}
	savedPos := fb.pos
	if pos.IsValid() {
		fb.pos = pos
	}
	fb.runDefers()
	if fb.rng == nil {
		for i := len(fb.scopes) - 1; i >= 0; i-- {
			fb.releaseSlots(fb.scopes[i].slots, known)
		}
		fb.releaseSlots(fb.frameSlots, known)
	} else {
		for i := len(fb.scopes) - 1; i >= 0; i-- {
			fb.releaseSlots(fb.scopes[i].slots, known)
		}
	}
	for i, v := range fb.results {
		if exiting && fb.rng == nil && fb.b.needsDrop(v.Type()) {
			fb.dropAt(fb.varAddr(v), v.Type(), pos)
			continue
		}
		fb.sink(SinkReturn, fb.recount(fb.readLocal(v), v.Type()), i, pos) // borrow.go
	}
	fb.cur = nil
	fb.pos = savedPos
}

// ----------------------------------------------------------------------------
// Sends, receives and select

func (fb *fnb) sendStmt(st *ast.SendStmt) {
	fb.expr(st.Chan)
	elem := chanElem(fb.typeOf(st.Chan))
	v := fb.exprAs(st.Value, elem)
	fb.sink(SinkSend, v, 0, st.Arrow)
}

// chanElem returns the element type of a channel type.
func chanElem(T types2.Type) types2.Type {
	if c, ok := coreType(T).(*types2.Chan); ok {
		return c.Elem()
	}
	return nil
}

// recv builds a receive: what it yields was sent, so it is what a send
// lets out of a frame, heap memory.
func (fb *fnb) recv(e *ast.UnaryExpr) Reg {
	fb.expr(e.X)
	if !hasPointers(fb.firstType(e)) {
		return 0
	}
	return fb.heap(HeapAlloc)
}

func (fb *fnb) selectStmt(st *ast.SelectStmt, label *types2.Label) {
	type selCase struct {
		cc   *ast.CommClause
		recv *ast.UnaryExpr
	}
	var cases []selCase
	// The channels and the values to send are evaluated first, in order.
	for _, c := range st.Body.List {
		cc := c.(*ast.CommClause)
		sc := selCase{cc: cc}
		switch s := cc.Comm.(type) {
		case *ast.SendStmt:
			fb.expr(s.Chan)
			v := fb.exprAs(s.Value, chanElem(fb.typeOf(s.Chan)))
			fb.sink(SinkSend, v, 0, s.Arrow)
		case *ast.ExprStmt:
			sc.recv, _ = ast.Unparen(s.X).(*ast.UnaryExpr)
		case *ast.AssignStmt:
			sc.recv, _ = ast.Unparen(s.Rhs[0]).(*ast.UnaryExpr)
		}
		if sc.recv != nil {
			fb.expr(sc.recv.X)
		}
		cases = append(cases, sc)
	}
	done := fb.newBlock()
	bodies := make([]*bblock, len(cases))
	start := fb.ensureBlock()
	for i := range cases {
		bodies[i] = fb.newBlock()
		fb.edge(start, bodies[i])
	}
	if len(cases) == 0 {
		fb.edge(start, done)
	}
	fb.cur = nil
	fb.targets = append(fb.targets, &jumpTarget{label: label, breakTo: done, depth: len(fb.scopes)})
	for i, c := range cases {
		fb.seal(bodies[i])
		fb.startBlock(bodies[i])
		sc := fb.pushScope(c.cc, true)
		if as, ok := c.cc.Comm.(*ast.AssignStmt); ok && c.recv != nil {
			var v Reg
			if hasPointers(fb.firstType(c.recv)) {
				v = fb.heap(HeapAlloc)
			}
			vals := []Reg{v, 0}
			types := []types2.Type{fb.firstType(c.recv), types2.Typ[types2.Bool]}
			for j, lhs := range as.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == "_" {
					fb.ownerTemp(vals[j], types[j])
					continue
				}
				if as.Tok == token.DEFINE {
					if v, ok := fb.b.defs[lhs.(*ast.Ident)].(*types2.Var); ok {
						fb.declareLocal(v)
						fb.assignVar(v, vals[j])
						continue
					}
				}
				lv := fb.lvalue(lhs)
				fb.storeLV(lv, vals[j])
			}
		} else if es, ok := c.cc.Comm.(*ast.ExprStmt); ok && c.recv != nil {
			_ = es
			if hasPointers(fb.firstType(c.recv)) {
				fb.ownerTemp(fb.heap(HeapAlloc), fb.firstType(c.recv))
			}
		}
		fb.stmtList(c.cc.Body)
		fb.popScope(sc)
		fb.jump(done)
	}
	fb.targets = fb.targets[:len(fb.targets)-1]
	fb.seal(done)
	fb.startBlock(done)
}
