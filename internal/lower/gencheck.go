package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// Generational references (prototype, Config.GenCheck).
//
// With the gencheck build tag, the slab allocator in pluto/alloc tags every
// small allocation: bits 63..48 of the pointer it returns hold the slot's
// generation. Slots live in 64 KiB-aligned chunks that start with one
// uint16 generation per 16-byte granule, and Free increments the
// generation of every granule of the slot. Pointers to the stack, static
// data, devices and large allocations have a zero tag (lib/pluto/alloc/gen.p).
//
// Lowering marks every function with ssa.PragmaGenCheck, and opt.GenCheck,
// the last pass, checks the tag of each memory access whose address is not
// statically a frame slot or a symbol (opt/gencheck.go). The runtime and the
// allocator get ssa.PragmaGenMaskOnly: they remove tags without checking.
//
// Lowering itself handles what the pass cannot see: converting a pointer
// to uintptr removes the tag, as does passing it to a function implemented
// outside Plutonium, so that the kernel and assembly see plain addresses;
// and the operands of memmove and memclr are checked once at the call,
// since those runtime functions only remove tags.

const genTagShift = 48

// genPragmas marks a new function for the generation checks.
func (l *Lowerer) genPragmas(f *ssa.Func, pkgPath string) {
	if !l.cfg.GenCheck {
		return
	}
	if pkgPath == "pluto/rt" || pkgPath == "pluto/alloc" {
		f.Pragmas |= ssa.PragmaGenMaskOnly
		return
	}
	f.Pragmas |= ssa.PragmaGenCheck
	if l.genPanic == nil {
		l.genPanic = l.funcSym(l.rtFunc("panicStale"), nil)
	}
	f.GenPanic = l.genPanic
}

// checked returns ptr ready for a runtime function that does not check
// tags itself: under GenCheck, its generation is checked and its tag
// removed.
func (s *fnState) checked(ptr *ssa.Value) *ssa.Value { return s.checkedLen(ptr, nil) }

// checkedLen is checked for a runtime function that accesses n elements at
// ptr: nothing is checked when n is 0, since ptr may then point just past
// the end of its allocation (b[len(b):]), into a granule of other memory.
func (s *fnState) checkedLen(ptr, n *ssa.Value) *ssa.Value {
	if !s.l.cfg.GenCheck || s.b == nil || s.f.Pragmas&ssa.PragmaGenCheck == 0 {
		return s.untagged(ptr)
	}
	switch ptr.Op {
	case ssa.OpSlotAddr, ssa.OpSymAddr, ssa.OpConst:
		return ptr
	}
	return s.genCheck(ptr, n, token.NoPos)
}

// genCheck emits the check of p's generation, unless n is 0 (n may be nil),
// and returns p without its tag.
func (s *fnState) genCheck(p, n *ssa.Value, pos token.Pos) *ssa.Value {
	i64 := func(c int64) *ssa.Value { return s.constInt(ssa.TI64, c) }
	ai := s.newValue(ssa.OpBitcast, ssa.TI64, p)
	tag := s.newValue(ssa.OpShrL, ssa.TI64, ai, i64(genTagShift))
	plain := s.newValue(ssa.OpShrL, ssa.TI64, s.newValue(ssa.OpShl, ssa.TI64, ai, i64(64-genTagShift)), i64(64-genTagShift))
	tagged := s.newValue(ssa.OpNe, ssa.TBool, tag, i64(0))
	if n != nil {
		tagged = s.newValue(ssa.OpAnd, ssa.TBool, tagged, s.newValue(ssa.OpNe, ssa.TBool, n, i64(0)))
	}

	check := s.newBlock(ssa.BlockInvalid)
	cont := s.newBlock(ssa.BlockInvalid)
	s.endIf(tagged, check, cont)
	s.seal(check)
	s.startBlock(check)
	chunk := s.newValue(ssa.OpAnd, ssa.TI64, plain, i64(-(1 << 16)))
	off := s.newValue(ssa.OpAnd, ssa.TI64, s.newValue(ssa.OpShrL, ssa.TI64, ai, i64(3)), i64(0x1ffe))
	gaddr := s.newValue(ssa.OpBitcast, ssa.TPtr, s.newValue(ssa.OpAdd, ssa.TI64, chunk, off))
	gen := s.newValue(ssa.OpZeroExt, ssa.TI64, s.newValue(ssa.OpLoad, ssa.TI16, gaddr, s.mem()))
	ok := s.newValue(ssa.OpEq, ssa.TBool, gen, tag)
	s.panicUnless(ok, pos, func() {
		s.callRuntimeNoReturn("panicStale", p)
	})
	s.endPlain(cont)
	s.seal(cont)
	s.startBlock(cont)
	return s.newValue(ssa.OpBitcast, ssa.TPtr, plain)
}

// untagged removes a pointer's generation tag without checking it, for
// conversions to uintptr and calls into code outside Plutonium.
func (s *fnState) untagged(p *ssa.Value) *ssa.Value {
	if !s.l.cfg.GenCheck {
		return p
	}
	i64 := func(c int64) *ssa.Value { return s.constInt(ssa.TI64, c) }
	ai := s.newValue(ssa.OpBitcast, ssa.TI64, p)
	plain := s.newValue(ssa.OpShrL, ssa.TI64, s.newValue(ssa.OpShl, ssa.TI64, ai, i64(64-genTagShift)), i64(64-genTagShift))
	if p.Type.IsPtr() {
		return s.newValue(ssa.OpBitcast, ssa.TPtr, plain)
	}
	return plain
}
