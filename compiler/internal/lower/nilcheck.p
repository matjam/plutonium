// Translated by go2pu from internal/lower/nilcheck.go at the go-cutover tag; edited by hand since.

package lower

import (
	"compiler/internal/ssa"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
)

// Nil dereferences (language spec section 17.5). Address zero and the
// nilGuardSize bytes above it are unmapped in every build (hosted programs
// by the operating system, kernels by arrangement), so an access at a small
// offset from a nil pointer faults, and the runtime reports the fault as a
// nil dereference. Only an access whose offset from the pointer can reach
// past that region needs an explicit check: a field at a large offset, or
// an element of a large array reached through a pointer to it.
//
// A dereference does not check at once. The lvalue it makes remembers the
// pointer (nilBase) and the static offset of its storage from it (nilOff);
// field selection and constant indexes add to the offset, and the lvalue is
// guarded when it is used (nilGuard): if the storage it denotes can reach
// past the region, the pointer is compared with nil there, and the program
// panics on the path where it is. Repeated checks of one pointer fold away
// (opt.Prove).
//
// A freestanding build outside kernel mode has no such region: its target
// may map address zero, and it may have no handler to turn a fault into a
// panic. There every dereference not proved non-nil is checked, and so is
// the pointer of a call through a func value or an interface
// (explicitNil).

// nilGuardSize is the size of the unmapped region at address zero that
// the runtime reports a fault in as a nil dereference (rt.describeFault).
const nilGuardSize = 4096

// deref returns the storage of type T that pointer p points to, to be
// guarded by nilGuard before it is used.
func (s *fnState) deref(p *ssa.Value, T types2.Type, pos token.Pos) lvalue {
	lv := lvalue{ptr: p, T: T}
	if mayBeNil(p) {
		lv.nilBase, lv.nilPos = p, pos
	}
	return lv
}

// mayBeNil reports whether pointer p may be nil: anything but the address
// of a frame slot or a symbol, or an offset from one.
func mayBeNil(p *ssa.Value) bool {
	for p.Op == ssa.OpOffPtr {
		p = p.Args[0]
	}
	return p.Op != ssa.OpSlotAddr && p.Op != ssa.OpSymAddr
}

// nilGuard checks the pointer lv was reached through, if the storage lv
// denotes extends past the unmapped region at address zero when the
// pointer is nil, and returns lv as storage that needs no further check.
func (s *fnState) nilGuard(lv lvalue) lvalue {
	if lv.nilBase == nil {
		return lv
	}
	if s.explicitNil() || lv.nilOff+s.l.sizeof(s.subst(unwrap(lv.T))) > nilGuardSize {
		s.nilCheck(lv.nilBase, lv.nilPos)
	}
	lv.nilBase = nil
	return lv
}

// nilCheck panics if p is nil. The comparison is of p's bits as an
// integer, so that opt.Prove folds a check that an earlier one implies.
func (s *fnState) nilCheck(p *ssa.Value, pos token.Pos) {
	if s.b == nil {
		return
	}
	ok := s.newValue(ssa.OpNe, ssa.TBool, s.newValue(ssa.OpBitcast, ssa.TI64, p), s.constInt(ssa.TI64, 0))
	s.panicUnless(ok, pos, func() {
		s.callRuntimeNoReturn("panicNil")
	})
}

// explicitNil reports whether every dereference is checked for nil
// explicitly: in a freestanding build outside kernel mode, where no fault
// reports one (language spec section 17.5).
func (s *fnState) explicitNil() bool {
	return s.l.cfg.Freestanding && !s.l.cfg.Kernel
}

// callNilCheck checks p, the closure of a func value or the method table of
// an interface value that is about to be called through, where no fault
// reports a nil one (explicitNil).
func (s *fnState) callNilCheck(p *ssa.Value, pos token.Pos) {
	if s.explicitNil() && mayBeNil(p) {
		s.nilCheck(p, pos)
	}
}
