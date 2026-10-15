package lower

import (
	"fmt"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/scoped"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
	typerr "github.com/matjam/plutonium/internal/types2/errors"
)

// Scoped tasks (language spec section 19.4).
//
// A call s.Task(func() {...}) on a sync.Scope declared in this function
// (internal/scoped) starts the literal on a new task that s waits for
// before its block ends, so the literal may use this frame. It is lowered
// in place, as a task statement is (conc.go), without calling Scope.Task:
//
//   - The argument block holds, for each variable the literal captures,
//     its address, or, for a variable the task takes (its block may end
//     before s waits), its value: copied, with a reference of its own for
//     strings, or moved out of the variable for an owner.
//   - The thunk, which runs first on the new task, moves the taken values
//     into slots of its own frame, builds the literal's closure record
//     there (the code pointer, then the address of each captured
//     variable: the shared ones in this frame, the taken ones in the
//     thunk's), and calls the literal. When the literal has returned, or
//     ended with rt.ExitTask, the thunk returns, which releases its slots and
//     drops the taken owners.
//   - rt.spawnScoped counts the task in s's rt.Join before it starts;
//     rt.taskMain uncounts it once the thunk has returned.
//
// Each task has its own record, so a literal started in a loop sees each
// iteration's values of the variables it takes; the record of a literal
// built in the frame would be one slot, reused by every iteration.

// scopedTask lowers the scoped Task call e.
func (s *fnState) scopedTask(e *ast.CallExpr, t *scoped.Task) {
	lit := t.Lit
	caps := s.capturedVars(lit)
	for _, v := range caps {
		T := s.subst(v.Type())
		if bad := types2.Unsendable(T); bad != nil {
			what := "uses"
			if t.Taken[v] {
				what = "takes"
			}
			s.l.errorf(lit.Pos(), diag.CheckerCode(typerr.NotSendable), "the task started by %s %s %s, of type %s, which is not sendable: it contains %s, whose count is not atomic, so it cannot be used by two tasks; use synced[T] (spec section 19.2)", shortExpr(e.Fun), what, v.Name(), T, bad)
			return
		}
	}
	join := s.varLV(t.Scope.Var, e).ptr // the Join is the Scope's first field

	// A loop that steps by a variable gives each task a different loop
	// variable only if the step is positive (internal/scoped.Loop).
	for _, L := range t.Loops {
		if !L.StepCheck {
			continue
		}
		step := s.indexValue(L.Step)
		ok := s.newValue(ssa.OpLess, ssa.TBool, s.constInt(ssa.TI64, 0), step)
		s.panicUnless(ok, L.Step.Pos(), func() { s.callRuntimeNoReturn("panicTaskStep", step) })
	}

	// Lay out the argument block: an address per shared variable, a value
	// per taken one.
	type slot struct {
		v     *types2.Var
		T     types2.Type
		taken bool
		off   int64
	}
	var slots []slot
	var off, align int64 = 0, 8
	for _, v := range caps {
		T := s.subst(v.Type())
		sz, al := int64(8), int64(8)
		if t.Taken[v] {
			sz, al = s.l.sizeof(T), max(s.l.alignof(T), 1)
		}
		align = max(align, al)
		off = (off + al - 1) &^ (al - 1)
		slots = append(slots, slot{v: v, T: T, taken: t.Taken[v], off: off})
		off += sz
	}
	size := max(off, 1)
	block := s.tempSize((size+7)&^7, align, "taskargs")
	for _, sl := range slots {
		lv := s.varLV(sl.v, e)
		dst := s.offPtr(block, sl.off)
		if !sl.taken {
			s.store(ssa.TPtr, dst, lv.ptr)
			continue
		}
		n := s.l.sizeof(sl.T)
		s.move(dst, lv.ptr, n)
		switch {
		case s.l.needsDrop(sl.T):
			// The owner moves to the task: the variable holds nothing.
			s.zero(lv.ptr, n)
		case s.l.counted(sl.T):
			s.retainAt(dst, sl.T)
		}
	}

	litSym := s.closureFunc(lit, caps)
	litSig := s.subst(s.info.Types[lit].Type).(*types2.Signature)

	s.taskStmts++
	name := fmt.Sprintf("%s.task%d", s.f.Name, s.taskStmts)
	thunk := s.l.out.Sym(name, ssa.SymFunc)
	unsafePtr := types2.Typ[types2.UnsafePointer]
	tsig := types2.NewSignatureType(nil, nil, nil, types2.NewTuple(types2.NewParam(token.NoPos, nil, "p", unsafePtr)), nil, false)
	pos := e.Pos()
	s.l.queue = append(s.l.queue, &funcJob{sym: thunk, pkg: s.pkg, sig: tsig, parent: s, build: func(ts *fnState) {
		in := ts.incomingArgs(tsig)
		p := in[0].v
		addrs := make([]*ssa.Value, len(slots))
		for i, sl := range slots {
			src := ts.offPtr(p, sl.off)
			if !sl.taken {
				addrs[i] = ts.load(ssa.TPtr, src)
				continue
			}
			var dst *ssa.Value
			if ts.l.needsDrop(sl.T) || ts.l.counted(sl.T) {
				dst = ts.frameSlot(sl.T, sl.v.Name())
			} else {
				dst = ts.temp(sl.T, sl.v.Name())
			}
			ts.move(dst, src, ts.l.sizeof(sl.T))
			addrs[i] = dst
		}
		rec := ts.closureRecord(litSym, addrs)
		// When the literal ends with rt.ExitTask the thunk leaves through its
		// return path too, which drops the taken owners.
		ts.emitCall(callee{code: ts.funcAddr(litSym), ctx: rec}, litSig, nil, nil, pos)
		ts.returnValues(nil)
	}})
	rec := s.symAddr(s.l.closureRecord(thunk))
	s.callRuntime("spawnScoped", nil, []*ssa.Value{join, rec, block, s.constInt(ssa.TI64, off), s.constInt(ssa.TI64, align), s.constInt(ssa.TI64, 0)})
}

// checkedArith is arith for an addition, subtraction or multiplication of
// int64 values whose overflow panics (internal/scoped.Checked): the
// arithmetic that keeps the parts of a slice that scoped tasks write apart.
// Other types and operators are arith's.
func (s *fnState) checkedArith(op token.Token, x, y *ssa.Value, T types2.Type, pos token.Pos) *ssa.Value {
	r := s.arith(op, x, y, T, pos)
	if x.Type != ssa.TI64 || isUnsigned(T) {
		return r
	}
	zero := s.constInt(ssa.TI64, 0)
	var ok *ssa.Value
	switch op {
	case token.ADD: // overflow when x and y have one sign and r the other
		t := s.newValue(ssa.OpAnd, ssa.TI64, s.newValue(ssa.OpXor, ssa.TI64, x, r), s.newValue(ssa.OpXor, ssa.TI64, y, r))
		ok = s.newValue(ssa.OpLeq, ssa.TBool, zero, t)
	case token.SUB: // overflow when x and y differ in sign and r differs from x
		t := s.newValue(ssa.OpAnd, ssa.TI64, s.newValue(ssa.OpXor, ssa.TI64, x, y), s.newValue(ssa.OpXor, ssa.TI64, x, r))
		ok = s.newValue(ssa.OpLeq, ssa.TBool, zero, t)
	case token.MUL: // the high half must be the sign of the low half
		hi := s.newValue(ssa.OpMulHi, ssa.TI64, x, y)
		sign := s.newValue(ssa.OpShrA, ssa.TI64, r, s.constInt(ssa.TI64, 63))
		ok = s.newValue(ssa.OpEq, ssa.TBool, hi, sign)
	default:
		return r
	}
	s.panicUnless(ok, pos, func() { s.callRuntimeNoReturn("panicTaskOverflow") })
	return r
}
