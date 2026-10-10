package lower

import (
	"fmt"

	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Range over functions.
//
// A loop
//
//	for k, v := range f { body }
//
// is lowered as Go's compiler rewrites it (cmd/compile/internal/rangefunc):
// the body becomes a yield function, f is called with it, and a state
// variable catches iterators that call yield when they may not.
//
//	state := ready
//	f(func(k K, v V) bool {
//		if state != ready { panicRangeState(state) }
//		state = running
//		body                  // continue: state = ready; return true
//		                      // break:    state = done; return false
//		state = ready
//		return true
//	})
//	state = exhausted
//	dispatch on next
//
// The yield function is a closure lowered at once, while the enclosing
// function is being lowered: it reaches the enclosing function's variables,
// result slots and defer state through addresses in its closure record,
// which lives in the enclosing frame (closures do not outlive their frame,
// and the yield function is only passed down to f). A return, or a break,
// continue or goto to a statement outside the body, stores a code in the
// enclosing function's next variable and returns false; after f returns,
// the code that ran the loop jumps to the target, or, when the target is
// further out still, returns false from its own body in turn.

// Iteration states, as Go's abi.RF_* values.
const (
	rfDone      = 0 // the body returned false: yield may not be called again
	rfReady     = 1 // yield may be called
	rfRunning   = 2 // the body is running (abi.RF_PANIC)
	rfExhausted = 3 // the iterator returned
)

// A rangeFunc is a range-over-func loop whose body is lowered as a yield
// function.
type rangeFunc struct {
	st     *ast.RangeStmt
	label  *types2.Label
	parent *fnState   // the function the loop statement is in
	state  *ssa.Value // the state variable's address, in parent
	caps   []*ssa.Value
	exits  []*rangeExit
}

// A rangeExit is a way out of a loop body to a target outside it.
type rangeExit struct {
	key    any
	code   int64
	level  *fnState // the function containing the target
	action func(p *fnState)
}

// contains reports whether pos lies in the loop's body.
func (rf *rangeFunc) contains(pos token.Pos) bool {
	return pos >= rf.st.Body.Pos() && pos < rf.st.Body.End()
}

// rootFn returns the function whose frame holds this function's state: the
// function itself, or for a loop body the function the outermost loop is in.
func (s *fnState) rootFn() *fnState {
	for s.rf != nil {
		s = s.rf.parent
	}
	return s
}

// outerPtr returns, in s, the address addr has in owner, which is s or a
// function whose loop body s is nested in.
func (s *fnState) outerPtr(addr *ssa.Value, owner *fnState) *ssa.Value {
	if owner == s {
		return addr
	}
	return s.capture(s.rf.parent.outerPtr(addr, owner))
}

// capture returns, in the loop body s, the address pa has in the enclosing
// function, adding it to s's closure record if it is not there yet. The
// pointer is loaded at s's entry: the record never changes.
func (s *fnState) capture(pa *ssa.Value) *ssa.Value {
	if p := s.capPtrs[pa]; p != nil {
		return p
	}
	i := len(s.rf.caps)
	s.rf.caps = append(s.rf.caps, pa)
	entry := s.f.Entry
	off := entry.NewValue(ssa.OpOffPtr, ssa.TPtr, s.ctx)
	off.AuxInt = int64(8 * (i + 1))
	ld := entry.NewValue(ssa.OpLoad, ssa.TPtr, off, s.initMem)
	// Place the load ahead of everything but the record pointer and the
	// entry memory, so that it precedes its uses and every store.
	vs := entry.Values
	at := 0
	for j, v := range vs[:len(vs)-2] {
		if v == s.ctx || v == s.initMem || v.Op == ssa.OpSlotAddr || v.Op == ssa.OpSymAddr || v.Op == ssa.OpFuncAddr {
			at = j + 1
		}
	}
	copy(vs[at+2:], vs[at:len(vs)-2])
	vs[at], vs[at+1] = off, ld
	s.capPtrs[pa] = ld
	return ld
}

// lookupVar returns the address of a local variable in s, reaching through
// the closure record for a variable of the function a loop body is in.
func (s *fnState) lookupVar(v *types2.Var) *ssa.Value {
	if a := s.vars[v]; a != nil {
		return a
	}
	if s.rf == nil {
		return nil
	}
	pa := s.rf.parent.lookupVar(v)
	if pa == nil {
		return nil
	}
	a := s.capture(pa)
	s.vars[v] = a
	return a
}

// nextVar returns the address of the root function's next variable.
func (s *fnState) nextVar() *ssa.Value {
	root := s.rootFn()
	if root.rangeNext == nil {
		root.rangeNext = root.temp(types2.Typ[types2.Int], "rangenext")
	}
	return s.outerPtr(root.rangeNext, root)
}

// rangeFuncStmt lowers a range over a function of signature sig.
func (s *fnState) rangeFuncStmt(st *ast.RangeStmt, label *types2.Label, sig *types2.Signature) {
	// The range expression is evaluated once, before the loop.
	c, ok := s.staticCallee(st.X)
	if !ok {
		fv := s.scalar(st.X)
		c = callee{code: s.load(ssa.TPtr, fv), ctx: fv}
	}
	yieldT := s.subst(sig.Params().At(0).Type())
	ySig := yieldT.Underlying().(*types2.Signature)

	rf := &rangeFunc{st: st, label: label, parent: s}
	rf.state = s.temp(types2.Typ[types2.Int], "rangestate")
	s.rangeLoops++
	sym := s.l.out.Sym(fmt.Sprintf("%s-range%d", s.f.Name, s.rangeLoops), ssa.SymFunc)
	s.l.lowerFunc(&funcJob{
		sym: sym, pkg: s.pkg, sig: ySig, tmap: s.tmap, parent: s, pos: st.Pos(), end: st.End(),
		build: func(bs *fnState) { bs.rangeBody(rf) },
	})

	rec := s.closureRecord(sym, rf.caps)
	s.store(ssa.TI64, rf.state, s.constInt(ssa.TI64, rfReady))
	if s.rf == nil && s.rangeNext != nil {
		s.store(ssa.TI64, s.rangeNext, s.constInt(ssa.TI64, 0))
	}
	s.emitCall(c, sig, nil, []value{scalarValue(rec, yieldT)}, st.Pos())
	s.store(ssa.TI64, rf.state, s.constInt(ssa.TI64, rfExhausted))
	s.rangeDispatch(rf)
}

// staticCallee returns the function x names if it is a declared function
// with a body, so that it can be called directly.
func (s *fnState) staticCallee(x ast.Expr) (callee, bool) {
	id := staticFuncIdent(x, s.info)
	if id == nil {
		return callee{}, false
	}
	fn, ok := s.info.Uses[id].(*types2.Func)
	if !ok {
		return callee{}, false
	}
	d := s.l.decls[fn.Origin()]
	if d == nil || d.decl.Body == nil || types2.IsErrorsNew(fn) {
		return callee{}, false
	}
	sym := s.l.funcSym(fn, s.instanceArgs(id))
	if s.l.isCFunc(sym) || s.l.interruptSyms[sym] {
		return callee{}, false
	}
	return callee{sym: sym}, true
}

// closureRecord builds a closure record in this frame for the function sym
// with the given captured addresses.
func (s *fnState) closureRecord(sym *ssa.Sym, caps []*ssa.Value) *ssa.Value {
	rec := s.tempSize(int64(8*(1+len(caps))), 8, "closure")
	slot := rec.Aux.(*ssa.Slot)
	slot.Closure = true
	for i := 0; i <= len(caps); i++ {
		slot.Leaves = append(slot.Leaves, ssa.Leaf{Off: int64(8 * i), Type: ssa.TPtr})
	}
	s.store(ssa.TPtr, rec, s.funcAddr(sym))
	for i, c := range caps {
		s.store(ssa.TPtr, s.offPtr(rec, int64(8*(i+1))), c)
	}
	return rec
}

// rangeBody lowers the body of rf's loop as the yield function bs.
func (bs *fnState) rangeBody(rf *rangeFunc) {
	st := rf.st
	bs.rf = rf
	bs.capPtrs = map[*ssa.Value]*ssa.Value{}
	bs.info = rf.parent.info
	bs.pkg = rf.parent.pkg
	bs.loopDepth = rf.parent.loopDepth
	bs.ctx = bs.newValue(ssa.OpClosurePtr, ssa.TPtr)
	bs.initMem = bs.mem()
	in := bs.incomingArgs(bs.job.sig)
	res := bs.temp(types2.Typ[types2.Bool], "result")
	bs.results = []*ssa.Value{res}

	// An iterator may not call yield after it returned false, after the
	// loop ended, or while the body runs.
	statep := bs.capture(rf.state)
	cur := bs.load(ssa.TI64, statep)
	ok := bs.newBlock(ssa.BlockInvalid)
	bad := bs.newBlock(ssa.BlockInvalid)
	bs.endIf(bs.newValue(ssa.OpEq, ssa.TBool, cur, bs.constInt(ssa.TI64, rfReady)), ok, bad)
	bs.seal(bad)
	bs.startBlock(bad)
	bs.callRuntimeNoReturn("panicRangeState", cur)
	bs.seal(ok)
	bs.startBlock(ok)
	bs.store(ssa.TI64, statep, bs.constInt(ssa.TI64, rfRunning))

	for i, e := range []ast.Expr{st.Key, st.Value} {
		if i >= len(in) {
			break
		}
		if lv := bs.rangeVar(st, e); lv != nil {
			bs.storeLV(*lv, bs.assignConvert(in[i], unwrap(lv.T), st))
		}
	}
	bs.targets = []*target{{label: rf.label, rf: rf}}
	bs.stmtList(st.Body.List)
	if bs.b != nil {
		bs.endIter(true)
	}
}

// endIter returns from a loop body: true to go on to the next iteration,
// false to stop the loop.
func (s *fnState) endIter(more bool) {
	state := int64(rfDone)
	if more {
		state = rfReady
	}
	s.store(ssa.TI64, s.capture(s.rf.state), s.constInt(ssa.TI64, state))
	s.store(ssa.TBool, s.results[0], s.constBool(more))
	s.emitReturn(token.NoPos)
}

// exitTo leaves the loop body s for a target in level, an enclosing
// function: it records the exit in every loop between, stores its code in
// next, and stops the loop. A return has code -1.
func (s *fnState) exitTo(level *fnState, key any, action func(p *fnState)) {
	root := s.rootFn()
	if root.rangeCodes == nil {
		root.rangeCodes = map[any]int64{}
	}
	code, ok := root.rangeCodes[key]
	if !ok {
		code = int64(len(root.rangeCodes) + 1)
		if key == (returnExit{}) {
			code = -1
		}
		root.rangeCodes[key] = code
	}
	for cur := s; cur != level; cur = cur.rf.parent {
		found := false
		for _, ex := range cur.rf.exits {
			if ex.key == key {
				found = true
				break
			}
		}
		if !found {
			cur.rf.exits = append(cur.rf.exits, &rangeExit{key: key, code: code, level: level, action: action})
		}
	}
	s.store(ssa.TI64, s.nextVar(), s.constInt(ssa.TI64, code))
	s.endIter(false)
}

type returnExit struct{}

type branchExit struct {
	t   *target
	tok token.Token
}

type gotoExit struct{ label *types2.Label }

// rangeDispatch follows the exit a loop body asked for, if any, after the
// iterator returned.
func (s *fnState) rangeDispatch(rf *rangeFunc) {
	if len(rf.exits) == 0 {
		return
	}
	var mine []*rangeExit
	for _, ex := range rf.exits {
		if ex.level == s {
			mine = append(mine, ex)
		}
	}
	next := s.nextVar()
	n := s.load(ssa.TI64, next)
	disp := s.newBlock(ssa.BlockInvalid)
	done := s.newBlock(ssa.BlockInvalid)
	s.endIf(s.newValue(ssa.OpNe, ssa.TBool, n, s.constInt(ssa.TI64, 0)), disp, done)
	s.seal(disp)
	s.startBlock(disp)
	for i, ex := range mine {
		// In the outermost function every code that reaches here is one
		// of its own.
		if i < len(mine)-1 || s.rf != nil {
			yes := s.newBlock(ssa.BlockInvalid)
			no := s.newBlock(ssa.BlockInvalid)
			s.endIf(s.newValue(ssa.OpEq, ssa.TBool, n, s.constInt(ssa.TI64, ex.code)), yes, no)
			s.seal(yes)
			s.startBlock(yes)
			s.takeExit(ex, next)
			s.seal(no)
			s.startBlock(no)
			continue
		}
		s.takeExit(ex, next)
	}
	if s.b != nil {
		// The target is further out: stop this loop too.
		s.endIter(false)
	}
	s.seal(done)
	s.startBlock(done)
}

func (s *fnState) takeExit(ex *rangeExit, next *ssa.Value) {
	if ex.code != -1 {
		s.store(ssa.TI64, next, s.constInt(ssa.TI64, 0))
	}
	ex.action(s)
	s.b = nil
}

// rangeBranch lowers break, continue or goto in a loop body to a target
// outside it.
func (s *fnState) rangeBranch(st *ast.BranchStmt, label *types2.Label) {
	if st.Tok == token.GOTO {
		p := s.rf.parent
		for p.rf != nil && !p.rf.contains(label.Pos()) {
			p = p.rf.parent
		}
		s.exitTo(p, gotoExit{label}, func(p *fnState) { p.endPlain(p.labelBlock(label)) })
		return
	}
	for p := s.rf.parent; p != nil; p = p.rf.parent {
		for i := len(p.targets) - 1; i >= 0; i-- {
			t := p.targets[i]
			if t.label != label {
				continue
			}
			tok := st.Tok
			s.exitTo(p, branchExit{t, tok}, func(p *fnState) {
				switch {
				case t.rf != nil:
					p.endIter(tok == token.CONTINUE)
				case tok == token.CONTINUE:
					p.endPlain(t.contTo)
				default:
					p.endPlain(t.breakTo)
				}
			})
			return
		}
		if p.rf == nil {
			break
		}
	}
	s.l.errorf(st.Pos(), "lower: no target for %s", st.Tok)
}

// rangeReturn lowers a return statement in a loop body: the results go to
// the enclosing function's result variables, and that function returns
// once the iterator has.
func (s *fnState) rangeReturn(st *ast.ReturnStmt) {
	root := s.rootFn()
	ptrs := make([]*ssa.Value, len(root.results))
	for i, r := range root.results {
		ptrs[i] = s.outerPtr(r, root)
	}
	types := root.abi.results
	switch {
	case len(st.Results) == 0:
	case len(st.Results) == len(ptrs):
		vals := make([]value, len(st.Results))
		for i, e := range st.Results {
			vals[i] = s.materialize(s.exprAs(e, types[i].typ))
		}
		for i, v := range vals {
			s.storeTo(ptrs[i], v, types[i].typ)
		}
	default: // return f() with several results
		vals := s.multiValue(st.Results[0], len(ptrs))
		for i, v := range vals {
			T := types[i].typ
			s.storeTo(ptrs[i], s.assignConvert(v, T, st), T)
		}
	}
	pos := st.Pos()
	s.exitTo(root, returnExit{}, func(p *fnState) { p.emitReturn(pos) })
}
