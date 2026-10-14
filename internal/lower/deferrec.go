package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Deferred calls when a hosted program panics (language spec section 17.2).
//
// A panic does not unwind, but in a hosted build it runs the panicking
// task's pending deferred calls, innermost frame first, before the panic
// handler. The runtime finds them from the frames alone, so a function
// with defer statements keeps everything its deferred calls need in one
// stack slot, the defer record:
//
//	word 0     the armed bits, one per defer statement (bit i: statement i)
//	then       the function value, receiver and arguments each statement
//	           saved, at offsets chosen here
//
// The stores that arm a call, and the store that clears the bits on entry,
// are volatile, which pins the record in memory: the optimiser neither
// promotes it to registers nor drops or delays a store to it, so whatever
// instruction faults after a defer statement finds the call armed.
//
// For each such function lowering also generates a runner, F·defers, a
// func(rec unsafe.Pointer) that runs the armed calls of the record at rec,
// last first, clearing each bit before its call. The function's own returns
// clear the bits the same way, so a deferred call that panics on the way
// out of a normal return is not run again by the panic. The backend reports
// where the record lies in the frame (ssa.Func.DeferRecord), and the trace
// table lists the record's offset and the runner for each function with
// one (internal/debuginfo); the runtime walks the panicking task's frames
// with the trace table and calls the runner of each frame whose record has
// bits set (pluto/rt/panic.p).
//
// Functions with a record are not inlined, so that each record belongs to
// one machine frame. Freestanding and kernel builds do not run deferred
// calls on a panic and keep the bits and saved values in ordinary slots,
// which the optimiser may promote.

// deferBitsAddr returns the address of the armed bits: word 0 of the
// defer record, or the plain slot of a freestanding build.
func (s *fnState) deferBitsAddr() *ssa.Value {
	if s.deferRec != nil {
		return s.deferRec
	}
	return s.deferBits
}

// hasDefers reports whether the function has defer statements.
func (s *fnState) hasDefers() bool { return s.deferRec != nil || s.deferBits != nil }

// startDefers allocates the function's defer state, before its body is
// lowered.
func (s *fnState) startDefers() {
	if s.l.cfg.Freestanding {
		s.deferBits = s.temp(types2.Typ[types2.Uint64], "deferbits")
		s.store(ssa.TI64, s.deferBits, s.constInt(ssa.TI64, 0))
		return
	}
	s.deferRec = s.tempSize(8, 8, "defers")
	s.deferRecSize = 8
	s.storeVolatile(ssa.TI64, s.deferRec, s.constInt(ssa.TI64, 0))
}

// recordField reserves room for a T in the defer record and returns its
// offset.
func (s *fnState) recordField(T types2.Type) int64 {
	al := max(s.l.alignof(T), 1)
	off := (s.deferRecSize + al - 1) &^ (al - 1)
	s.deferRecSize = off + s.l.sizeof(T)
	slot := s.deferRec.Aux.(*ssa.Slot)
	slot.Align = max(slot.Align, al)
	slot.Size = (s.deferRecSize + 7) &^ 7
	return off
}

// storeVolatile stores val at ptr with a volatile store, which the
// optimiser never removes, merges or reorders.
func (s *fnState) storeVolatile(t *ssa.Type, ptr, val *ssa.Value) {
	st := s.newValue(ssa.OpStore, ssa.TMem, ptr, val, s.mem())
	st.Aux = t
	st.AuxInt |= ssa.MemVolatile
	s.setMem(st)
}

// runDeferList emits the armed calls of defers, last first. bitsp is the
// address of the armed bits and rec that of the defer record (nil in a
// freestanding build). With a record, each bit is cleared before its call.
// gx, if set, is a bool slot that records whether a deferred call ended
// with rt.Goexit (goexit.go): the remaining calls run all the same, as in
// Go, and the function leaves for Goexit once they have.
func (s *fnState) runDeferList(defers []*deferInfo, bitsp, rec, gx *ssa.Value) {
	defer s.collectCleanupGoexit()()
	for i := len(defers) - 1; i >= 0; i-- {
		d := defers[i]
		bits := s.load(ssa.TI64, bitsp)
		bit := s.newValue(ssa.OpAnd, ssa.TI64, bits, s.constInt(ssa.TI64, 1<<i))
		cond := s.newValue(ssa.OpNe, ssa.TBool, bit, s.constInt(ssa.TI64, 0))
		call := s.newBlock(ssa.BlockInvalid)
		next := s.newBlock(ssa.BlockInvalid)
		s.endIf(cond, call, next)
		s.seal(call)
		s.startBlock(call)
		if rec != nil {
			cleared := s.newValue(ssa.OpAnd, ssa.TI64, bits, s.constInt(ssa.TI64, ^(int64(1)<<i)))
			s.storeVolatile(ssa.TI64, bitsp, cleared)
		}
		d.run(s, rec)
		if s.b != nil && d.mayGoexit && gx != nil {
			s.noteGoexit(gx)
		}
		if s.b != nil {
			s.endPlain(next)
		}
		s.seal(next)
		s.startBlock(next)
	}
}

// deferGoexits reports whether a deferred call of the function may end
// with rt.Goexit.
func (s *fnState) deferGoexits() bool {
	for _, d := range s.defers {
		if d.mayGoexit {
			return true
		}
	}
	return false
}

// finishDefers gives a function with a defer record its runner, and tells
// the backend where the record is.
func (s *fnState) finishDefers() {
	if s.deferRec == nil || len(s.defers) == 0 {
		return
	}
	defers := s.defers
	sym := s.l.out.Sym(s.f.Name+"·defers", ssa.SymFunc)
	sig := types2.NewSignatureType(nil, nil, nil,
		types2.NewTuple(types2.NewParam(token.NoPos, nil, "rec", types2.Typ[types2.UnsafePointer])), nil, false)
	goexits := s.deferGoexits()
	s.l.queue = append(s.l.queue, &funcJob{sym: sym, pkg: s.pkg, sig: sig, tmap: s.tmap, build: func(rs *fnState) {
		rec := rs.incomingArgs(sig)[0].v
		var gx *ssa.Value
		if goexits {
			gx = rs.temp(types2.Typ[types2.Bool], "goexit")
			rs.store(ssa.TBool, gx, rs.constBool(false))
		}
		rs.runDeferList(defers, rec, rec, gx)
		if gx != nil {
			rs.passGoexit(gx)
		}
		rs.emitReturn(token.NoPos)
	}})
	s.f.DeferRecord = s.deferRec.Aux.(*ssa.Slot)
	s.f.DeferRunner = sym
	s.f.Pragmas |= ssa.PragmaNoInline
}
