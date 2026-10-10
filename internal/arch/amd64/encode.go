package amd64

import (
	"encoding/binary"
	"fmt"

	"github.com/matjam/plutonium/internal/ssa"
)

// RelocType is an x86-64 ELF relocation type.
type RelocType uint8

const (
	RelPC32  RelocType = iota // R_X86_64_PC32: S + A - P
	RelPLT32                  // R_X86_64_PLT32 (calls)
	Rel64                     // R_X86_64_64: S + A
	RelAbs32S                 // R_X86_64_32S: S + A, sign-extended 32 bits
)

// A Reloc is a relocation in emitted code.
type Reloc struct {
	Off    int
	Sym    *ssa.Sym
	Type   RelocType
	Addend int64
}

// encoder emits machine code for one function.
type encoder struct {
	code   []byte
	relocs []Reloc
	fixups []fixup
	err    error
}

// fixup is a branch whose rel32 must be patched once block offsets are known.
type fixup struct {
	start  int // offset of the instruction
	at     int // offset of the rel32 field
	end    int // offset just after the instruction
	target *MBlock
	jcc    bool
	cond   Cond
}

func (e *encoder) byte(b ...byte) { e.code = append(e.code, b...) }

func (e *encoder) imm(v int64, size int) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(v))
	e.code = append(e.code, buf[:size]...)
}

func (e *encoder) fail(format string, args ...any) {
	if e.err == nil {
		e.err = fmt.Errorf(format, args...)
	}
}

// rmOperand is the r/m half of a ModRM-encoded instruction.
type rmOperand struct {
	isReg bool
	reg   Reg
	mem   Mem
}

func rmReg(r Reg) rmOperand { return rmOperand{isReg: true, reg: r} }
func rmMem(m Mem) rmOperand { return rmOperand{mem: m} }

func rmOf(o Operand) rmOperand {
	if o.Kind == OpdReg {
		return rmReg(o.Reg)
	}
	return rmMem(o.Mem)
}

// needsREX8 reports whether an 8-bit access to r needs a REX prefix (SPL,
// BPL, SIL, DIL rather than AH, CH, DH, BH).
func needsREX8(r Reg) bool { return r >= RSP && r <= RDI }

// encodeModRM emits optional prefixes, REX, opcode bytes and the ModRM/SIB/
// displacement for "op reg, rm". regField is either a register or an opcode
// extension (when isExt). immSize is the size of any immediate that follows,
// which RIP-relative displacements must account for.
type insn struct {
	prefixes []byte // legacy prefixes (66, F2, F3)
	w        bool   // REX.W
	opcode   []byte
	reg      Reg  // ModRM.reg as a register, or NoReg
	ext      byte // ModRM.reg as an opcode extension when reg == NoReg
	rm       rmOperand
	byteRegs bool // 8-bit register operands (REX needed for SPL..DIL)
	immSize  int
}

func (e *encoder) emitInsn(in insn) {
	if !in.rm.isReg && in.rm.mem.Seg == SegGS {
		e.byte(0x65)
	}
	e.byte(in.prefixes...)
	var rex byte
	if in.w {
		rex |= 0x48
	}
	regEnc := in.ext
	if in.reg != NoReg {
		regEnc = in.reg.enc()
		if regEnc >= 8 {
			rex |= 0x44
		}
		if in.byteRegs && needsREX8(in.reg) {
			rex |= 0x40
		}
	}
	if in.rm.isReg {
		if in.rm.reg.enc() >= 8 {
			rex |= 0x41
		}
		if in.byteRegs && needsREX8(in.rm.reg) {
			rex |= 0x40
		}
	} else {
		m := in.rm.mem
		if m.Base != NoReg && m.Base.enc() >= 8 {
			rex |= 0x41
		}
		if m.Index != NoReg && m.Index.enc() >= 8 {
			rex |= 0x42
		}
	}
	if rex != 0 {
		e.byte(rex | 0x40)
	}
	e.byte(in.opcode...)
	regEnc &= 7

	if in.rm.isReg {
		e.byte(0xC0 | regEnc<<3 | in.rm.reg.enc()&7)
		return
	}
	m := in.rm.mem
	if m.Sym != nil && m.Base == NoReg && m.Index == NoReg && m.Seg == SegNone {
		// RIP-relative: disp32 relative to the end of the instruction.
		e.byte(0x05 | regEnc<<3)
		e.relocs = append(e.relocs, Reloc{Off: len(e.code), Sym: m.Sym, Type: RelPC32, Addend: m.Disp - 4 - int64(in.immSize)})
		e.imm(0, 4)
		return
	}
	// disp32 emits a 32-bit displacement: the symbol's absolute address
	// plus Disp (sign-extended, so the symbol must lie in the low or the
	// top 2 GiB), or Disp alone.
	disp32 := func() {
		if m.Sym != nil {
			e.relocs = append(e.relocs, Reloc{Off: len(e.code), Sym: m.Sym, Type: RelAbs32S, Addend: m.Disp})
			e.imm(0, 4)
			return
		}
		e.imm(m.Disp, 4)
	}
	if !fitsImm32(m.Disp) {
		e.fail("amd64: displacement %d does not fit in 32 bits", m.Disp)
	}
	if m.Base == NoReg {
		// SIB with no base: disp32 plus an optional scaled index.
		e.byte(0x04 | regEnc<<3)
		idx := byte(4) // no index
		if m.Index != NoReg {
			idx = m.Index.enc() & 7
		}
		e.byte(e.scaleBits(m)<<6 | idx<<3 | 5)
		disp32()
		return
	}
	var mod byte
	switch {
	case m.Sym != nil:
		mod = 2
	case m.Disp == 0 && m.Base.enc()&7 != 5: // RBP/R13 always need a displacement
		mod = 0
	case m.Disp >= -128 && m.Disp <= 127:
		mod = 1
	default:
		mod = 2
	}
	if m.Index != NoReg || m.Base.enc()&7 == 4 {
		// SIB form (RSP/R12 bases always need it).
		e.byte(mod<<6 | regEnc<<3 | 4)
		idx := byte(4) // no index
		if m.Index != NoReg {
			idx = m.Index.enc() & 7
		}
		e.byte(e.scaleBits(m)<<6 | idx<<3 | m.Base.enc()&7)
	} else {
		e.byte(mod<<6 | regEnc<<3 | m.Base.enc()&7)
	}
	switch mod {
	case 1:
		e.imm(m.Disp, 1)
	case 2:
		disp32()
	}
}

// scaleBits returns the SIB scale field for m's index scale.
func (e *encoder) scaleBits(m Mem) byte {
	switch m.Scale {
	case 0, 1:
		return 0
	case 2:
		return 1
	case 4:
		return 2
	case 8:
		return 3
	}
	e.fail("amd64: bad scale %d", m.Scale)
	return 0
}

func sizePrefix(size uint8) []byte {
	if size == 2 {
		return []byte{0x66}
	}
	return nil
}

// alu encodes the classic two-operand ALU group (ADD, OR, AND, SUB, XOR, CMP).
func (e *encoder) alu(n byte, in *MInst) {
	size := in.Size
	pre := sizePrefix(size)
	w := size == 8
	b8 := size == 1
	switch {
	case in.Src.Kind == OpdImm:
		c := in.Src.Imm
		switch {
		case b8:
			e.emitInsn(insn{prefixes: pre, opcode: []byte{0x80}, reg: NoReg, ext: n, rm: rmOf(in.Dst), byteRegs: true, immSize: 1})
			e.imm(c, 1)
		case c >= -128 && c <= 127:
			e.emitInsn(insn{prefixes: pre, w: w, opcode: []byte{0x83}, reg: NoReg, ext: n, rm: rmOf(in.Dst), immSize: 1})
			e.imm(c, 1)
		default:
			isz := 4
			if size == 2 {
				isz = 2
			}
			e.emitInsn(insn{prefixes: pre, w: w, opcode: []byte{0x81}, reg: NoReg, ext: n, rm: rmOf(in.Dst), immSize: isz})
			e.imm(c, isz)
		}
	case in.Dst.Kind == OpdReg:
		op := n*8 + 3
		if b8 {
			op = n*8 + 2
		}
		e.emitInsn(insn{prefixes: pre, w: w, opcode: []byte{op}, reg: in.Dst.Reg, rm: rmOf(in.Src), byteRegs: b8})
	default:
		op := n*8 + 1
		if b8 {
			op = n * 8
		}
		e.emitInsn(insn{prefixes: pre, w: w, opcode: []byte{op}, reg: in.Src.Reg, rm: rmOf(in.Dst), byteRegs: b8})
	}
}

// unary encodes the F6/F7 group (TEST imm, NOT, NEG, MUL, IMUL, DIV, IDIV).
func (e *encoder) unary(n byte, size uint8, rm rmOperand, immSize int) {
	op := byte(0xF7)
	if size == 1 {
		op = 0xF6
	}
	e.emitInsn(insn{prefixes: sizePrefix(size), w: size == 8, opcode: []byte{op}, reg: NoReg, ext: n, rm: rm, byteRegs: size == 1, immSize: immSize})
}

func (e *encoder) sse(prefix byte, op byte, w bool, reg Reg, rm rmOperand) {
	var pre []byte
	if prefix != 0 {
		pre = []byte{prefix}
	}
	e.emitInsn(insn{prefixes: pre, w: w, opcode: []byte{0x0F, op}, reg: reg, rm: rm})
}

func floatPrefix(size uint8) byte {
	if size == 4 {
		return 0xF3
	}
	return 0xF2
}

// encode emits one instruction.
func (e *encoder) encode(in *MInst) {
	size := in.Size
	switch in.Op {
	case MMOV:
		e.encodeMov(in)
	case MCOPY:
		if in.Dst.Reg.PhysClass() == ClassXMM {
			e.sse(0, 0x28, false, in.Dst.Reg, rmReg(in.Src.Reg)) // movaps
		} else {
			w := in.Size == 8 || in.Size == 0
			if in.Size < 4 {
				w = false
			}
			e.emitInsn(insn{w: w, opcode: []byte{0x8B}, reg: in.Dst.Reg, rm: rmReg(in.Src.Reg)})
		}
	case MMOVZX, MMOVSX:
		e.encodeExtend(in)
	case MLEA:
		// A 32-bit LEA computes the low half of the sum and zero-extends.
		e.emitInsn(insn{w: in.Size != 4, opcode: []byte{0x8D}, reg: in.Dst.Reg, rm: rmMem(in.Src.Mem)})
	case MADD:
		e.alu(0, in)
	case MOR:
		e.alu(1, in)
	case MAND:
		e.alu(4, in)
	case MSUB:
		e.alu(5, in)
	case MXOR:
		e.alu(6, in)
	case MCMP:
		e.alu(7, in)
	case MTEST:
		if in.Src.Kind == OpdImm {
			isz := int(min(size, 4))
			e.unary(0, size, rmOf(in.Dst), isz)
			e.imm(in.Src.Imm, isz)
			break
		}
		op := byte(0x85)
		if size == 1 {
			op = 0x84
		}
		e.emitInsn(insn{prefixes: sizePrefix(size), w: size == 8, opcode: []byte{op}, reg: in.Src.Reg, rm: rmOf(in.Dst), byteRegs: size == 1})
	case MIMUL:
		e.emitInsn(insn{prefixes: sizePrefix(size), w: size == 8, opcode: []byte{0x0F, 0xAF}, reg: in.Dst.Reg, rm: rmOf(in.Src)})
	case MIMUL3:
		if in.Imm >= -128 && in.Imm <= 127 {
			e.emitInsn(insn{prefixes: sizePrefix(size), w: size == 8, opcode: []byte{0x6B}, reg: in.Dst.Reg, rm: rmOf(in.Src), immSize: 1})
			e.imm(in.Imm, 1)
		} else {
			isz := 4
			if size == 2 {
				isz = 2
			}
			e.emitInsn(insn{prefixes: sizePrefix(size), w: size == 8, opcode: []byte{0x69}, reg: in.Dst.Reg, rm: rmOf(in.Src), immSize: isz})
			e.imm(in.Imm, isz)
		}
	case MNOT:
		e.unary(2, size, rmOf(in.Dst), 0)
	case MNEG:
		e.unary(3, size, rmOf(in.Dst), 0)
	case MMUL1:
		e.unary(4, size, rmOf(in.Src), 0)
	case MIMUL1:
		e.unary(5, size, rmOf(in.Src), 0)
	case MDIV:
		e.unary(6, size, rmOf(in.Src), 0)
	case MIDIV:
		e.unary(7, size, rmOf(in.Src), 0)
	case MCQO:
		switch size {
		case 8:
			e.byte(0x48, 0x99)
		case 4:
			e.byte(0x99)
		case 2:
			e.byte(0x66, 0x99)
		}
	case MSHL, MSHR, MSAR, MROL, MROR:
		n := map[MOp]byte{MROL: 0, MROR: 1, MSHL: 4, MSHR: 5, MSAR: 7}[in.Op]
		b8 := size == 1
		if in.Src.Kind == OpdImm {
			op := byte(0xC1)
			if b8 {
				op = 0xC0
			}
			e.emitInsn(insn{prefixes: sizePrefix(size), w: size == 8, opcode: []byte{op}, reg: NoReg, ext: n, rm: rmOf(in.Dst), byteRegs: b8, immSize: 1})
			e.imm(in.Src.Imm, 1)
		} else {
			op := byte(0xD3)
			if b8 {
				op = 0xD2
			}
			e.emitInsn(insn{prefixes: sizePrefix(size), w: size == 8, opcode: []byte{op}, reg: NoReg, ext: n, rm: rmOf(in.Dst), byteRegs: b8})
		}
	case MBSWAP:
		r := in.Dst.Reg
		var rex byte
		if size == 8 {
			rex = 0x48
		}
		if r.enc() >= 8 {
			rex |= 0x41
		}
		if rex != 0 {
			e.byte(rex | 0x40)
		}
		e.byte(0x0F, 0xC8+r.enc()&7)
	case MSETCC:
		e.emitInsn(insn{opcode: []byte{0x0F, 0x90 + byte(in.Cond)}, reg: NoReg, ext: 0, rm: rmOf(in.Dst), byteRegs: true})
	case MCMOVCC:
		sz := max(size, 4)
		e.emitInsn(insn{w: sz == 8, opcode: []byte{0x0F, 0x40 + byte(in.Cond)}, reg: in.Dst.Reg, rm: rmOf(in.Src)})
	case MBSF:
		e.emitInsn(insn{prefixes: sizePrefix(size), w: size == 8, opcode: []byte{0x0F, 0xBC}, reg: in.Dst.Reg, rm: rmOf(in.Src)})
	case MBSR:
		e.emitInsn(insn{prefixes: sizePrefix(size), w: size == 8, opcode: []byte{0x0F, 0xBD}, reg: in.Dst.Reg, rm: rmOf(in.Src)})
	case MPOPCNT:
		e.emitInsn(insn{prefixes: []byte{0xF3}, w: size == 8, opcode: []byte{0x0F, 0xB8}, reg: in.Dst.Reg, rm: rmOf(in.Src)})

	case MMOVSF:
		p := floatPrefix(size)
		if in.Dst.Kind == OpdMem {
			e.sse(p, 0x11, false, in.Src.Reg, rmMem(in.Dst.Mem))
		} else if in.Src.Kind == OpdReg {
			e.sse(0, 0x28, false, in.Dst.Reg, rmReg(in.Src.Reg)) // register copy: movaps
		} else {
			e.sse(p, 0x10, false, in.Dst.Reg, rmMem(in.Src.Mem))
		}
	case MADDSF:
		e.sse(floatPrefix(size), 0x58, false, in.Dst.Reg, rmOf(in.Src))
	case MMULSF:
		e.sse(floatPrefix(size), 0x59, false, in.Dst.Reg, rmOf(in.Src))
	case MSUBSF:
		e.sse(floatPrefix(size), 0x5C, false, in.Dst.Reg, rmOf(in.Src))
	case MDIVSF:
		e.sse(floatPrefix(size), 0x5E, false, in.Dst.Reg, rmOf(in.Src))
	case MSQRTSF:
		e.sse(floatPrefix(size), 0x51, false, in.Dst.Reg, rmOf(in.Src))
	case MUCOMI:
		if size == 8 {
			e.sse(0x66, 0x2E, false, in.Dst.Reg, rmOf(in.Src))
		} else {
			e.sse(0, 0x2E, false, in.Dst.Reg, rmOf(in.Src))
		}
	case MXORPS:
		e.sse(0, 0x57, false, in.Dst.Reg, rmOf(in.Src))
	case MANDPS:
		e.sse(0, 0x54, false, in.Dst.Reg, rmOf(in.Src))
	case MCVTSI2F:
		e.sse(floatPrefix(size), 0x2A, in.SrcSize == 8, in.Dst.Reg, rmOf(in.Src))
	case MCVTTF2SI:
		e.sse(floatPrefix(in.SrcSize), 0x2C, size == 8, in.Dst.Reg, rmOf(in.Src))
	case MCVTF2F:
		e.sse(floatPrefix(in.SrcSize), 0x5A, false, in.Dst.Reg, rmOf(in.Src))
	case MMOVQ:
		if in.Dst.Kind == OpdReg && in.Dst.Reg.PhysClass() == ClassXMM {
			e.sse(0x66, 0x6E, size == 8, in.Dst.Reg, rmOf(in.Src)) // movq xmm, r/m
		} else {
			e.sse(0x66, 0x7E, size == 8, in.Src.Reg, rmOf(in.Dst)) // movq r/m, xmm
		}

	case MCALL:
		if in.Sym != nil {
			e.byte(0xE8)
			e.relocs = append(e.relocs, Reloc{Off: len(e.code), Sym: in.Sym, Type: RelPLT32, Addend: -4})
			e.imm(0, 4)
		} else {
			e.emitInsn(insn{opcode: []byte{0xFF}, reg: NoReg, ext: 2, rm: rmOf(in.Src)})
		}
	case MJMP:
		start := len(e.code)
		e.byte(0xE9)
		e.fixups = append(e.fixups, fixup{start: start, at: len(e.code), end: len(e.code) + 4, target: in.Target})
		e.imm(0, 4)
	case MJCC:
		start := len(e.code)
		e.byte(0x0F, 0x80+byte(in.Cond))
		e.fixups = append(e.fixups, fixup{start: start, at: len(e.code), end: len(e.code) + 4, target: in.Target, jcc: true, cond: in.Cond})
		e.imm(0, 4)
	case MRET:
		e.byte(0xC3)
	case MUD2:
		e.byte(0x0F, 0x0B)
	case MPUSH:
		r := in.Src.Reg
		if r.enc() >= 8 {
			e.byte(0x41)
		}
		e.byte(0x50 + r.enc()&7)
	case MPOP:
		r := in.Dst.Reg
		if r.enc() >= 8 {
			e.byte(0x41)
		}
		e.byte(0x58 + r.enc()&7)
	case MREPMOVSB:
		e.byte(0xF3, 0xA4)
	case MREPSTOSB:
		e.byte(0xF3, 0xAA)

	case MCLI:
		e.byte(0xFA)
	case MSTI:
		e.byte(0xFB)
	case MSTIHLT:
		e.byte(0xFB, 0xF4)
	case MHLT:
		e.byte(0xF4)
	case MPAUSE:
		e.byte(0xF3, 0x90)
	case MSAVEFLAGS:
		e.byte(0x9C) // pushfq
		e.encode(&MInst{Op: MPOP, Dst: in.Dst})
	case MRESTOREFLAGS:
		e.encode(&MInst{Op: MPUSH, Src: in.Src})
		e.byte(0x9D) // popfq
	case MIN, MOUT:
		e.byte(sizePrefix(size)...)
		op := byte(0xEC) // in al, dx
		if in.Op == MOUT {
			op = 0xEE
		}
		if in.Src.Kind == OpdImm {
			op -= 8 // E4 / E6: imm8 port
		}
		if size != 1 {
			op++
		}
		e.byte(op)
		if in.Src.Kind == OpdImm {
			e.imm(in.Src.Imm, 1)
		}
	case MRDMSR:
		e.byte(0x0F, 0x32)
	case MWRMSR:
		e.byte(0x0F, 0x30)
	case MRDCR:
		e.emitInsn(insn{opcode: []byte{0x0F, 0x20}, reg: NoReg, ext: byte(in.Imm), rm: rmReg(in.Dst.Reg)})
	case MWRCR:
		e.emitInsn(insn{opcode: []byte{0x0F, 0x22}, reg: NoReg, ext: byte(in.Imm), rm: rmReg(in.Src.Reg)})
	case MINVLPG:
		e.emitInsn(insn{opcode: []byte{0x0F, 0x01}, reg: NoReg, ext: 7, rm: rmMem(in.Src.Mem)})
	case MLGDT:
		e.emitInsn(insn{opcode: []byte{0x0F, 0x01}, reg: NoReg, ext: 2, rm: rmMem(in.Src.Mem)})
	case MLIDT:
		e.emitInsn(insn{opcode: []byte{0x0F, 0x01}, reg: NoReg, ext: 3, rm: rmMem(in.Src.Mem)})
	case MLTR:
		e.emitInsn(insn{opcode: []byte{0x0F, 0x00}, reg: NoReg, ext: 3, rm: rmOf(in.Src)})
	case MCPUID:
		e.byte(0x0F, 0xA2)
	case MRDTSC:
		e.byte(0x0F, 0x31)
	case MRDTSCP:
		e.byte(0x0F, 0x01, 0xF9)
	case MSWAPGS:
		e.byte(0x0F, 0x01, 0xF8)
	case MCLTS:
		e.byte(0x0F, 0x06)
	case MFXSAVE:
		e.emitInsn(insn{w: true, opcode: []byte{0x0F, 0xAE}, reg: NoReg, ext: 0, rm: rmMem(in.Src.Mem)})
	case MFXRSTOR:
		e.emitInsn(insn{w: true, opcode: []byte{0x0F, 0xAE}, reg: NoReg, ext: 1, rm: rmMem(in.Src.Mem)})
	default:
		e.fail("amd64: cannot encode %s", in)
	}
}

func (e *encoder) encodeMov(in *MInst) {
	size := in.Size
	pre := sizePrefix(size)
	b8 := size == 1
	switch {
	case in.Src.Kind == OpdImm && in.Dst.Kind == OpdReg:
		r := in.Dst.Reg
		c := in.Src.Imm
		switch {
		case b8:
			rex := byte(0)
			if r.enc() >= 8 {
				rex = 0x41
			} else if needsREX8(r) {
				rex = 0x40
			}
			if rex != 0 {
				e.byte(rex)
			}
			e.byte(0xB0 + r.enc()&7)
			e.imm(c, 1)
		case size == 8 && c >= 0 && c <= 0xFFFFFFFF:
			// mov r32, imm32 zero-extends.
			if r.enc() >= 8 {
				e.byte(0x41)
			}
			e.byte(0xB8 + r.enc()&7)
			e.imm(c, 4)
		case size == 8 && fitsImm32(c):
			e.emitInsn(insn{w: true, opcode: []byte{0xC7}, reg: NoReg, ext: 0, rm: rmReg(r), immSize: 4})
			e.imm(c, 4)
		case size == 8:
			rex := byte(0x48)
			if r.enc() >= 8 {
				rex |= 0x01
			}
			e.byte(rex, 0xB8+r.enc()&7)
			e.imm(c, 8)
		default:
			e.byte(pre...)
			if r.enc() >= 8 {
				e.byte(0x41)
			}
			e.byte(0xB8 + r.enc()&7)
			e.imm(c, int(size))
		}
	case in.Src.Kind == OpdImm:
		isz := int(min(size, 4))
		op := byte(0xC7)
		if b8 {
			op = 0xC6
		}
		e.emitInsn(insn{prefixes: pre, w: size == 8, opcode: []byte{op}, reg: NoReg, ext: 0, rm: rmOf(in.Dst), immSize: isz})
		e.imm(in.Src.Imm, isz)
	case in.Dst.Kind == OpdReg:
		op := byte(0x8B)
		if b8 {
			op = 0x8A
		}
		e.emitInsn(insn{prefixes: pre, w: size == 8, opcode: []byte{op}, reg: in.Dst.Reg, rm: rmOf(in.Src), byteRegs: b8})
	default:
		op := byte(0x89)
		if b8 {
			op = 0x88
		}
		e.emitInsn(insn{prefixes: pre, w: size == 8, opcode: []byte{op}, reg: in.Src.Reg, rm: rmOf(in.Dst), byteRegs: b8})
	}
}

func (e *encoder) encodeExtend(in *MInst) {
	w := in.Size == 8
	switch {
	case in.Op == MMOVSX && in.SrcSize == 4:
		e.emitInsn(insn{w: true, opcode: []byte{0x63}, reg: in.Dst.Reg, rm: rmOf(in.Src)})
	case in.SrcSize == 1:
		op := byte(0xB6)
		if in.Op == MMOVSX {
			op = 0xBE
		}
		e.emitInsn(insn{w: w, opcode: []byte{0x0F, op}, reg: in.Dst.Reg, rm: rmOf(in.Src), byteRegs: true})
	case in.SrcSize == 2:
		op := byte(0xB7)
		if in.Op == MMOVSX {
			op = 0xBF
		}
		e.emitInsn(insn{w: w, opcode: []byte{0x0F, op}, reg: in.Dst.Reg, rm: rmOf(in.Src)})
	default:
		e.fail("amd64: bad extension %s", in)
	}
}
