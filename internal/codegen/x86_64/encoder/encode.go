package encoder

import "encoding/binary"

// Operand represents an instruction operand.
type Operand interface {
	operandKind() opKind
}

type opKind int

const (
	opReg opKind = iota // register
	opImm               // immediate
	opMem               // memory [base + index*scale + disp]
	opRel               // RIP-relative (for calls/jumps)
)

// RegOp is a register operand.
type RegOp struct {
	Reg   Reg
	Width Width
}

func (r RegOp) operandKind() opKind { return opReg }

// ImmOp is an immediate operand.
type ImmOp struct {
	Val   int64
	Width Width
}

func (i ImmOp) operandKind() opKind { return opImm }

// MemOp is a memory operand: [Base + Index*Scale + Disp].
type MemOp struct {
	Base  Reg
	Index Reg   // NoReg if no index
	Scale uint8 // 1, 2, 4, or 8
	Disp  int32
	Width Width // access width
}

func (m MemOp) operandKind() opKind { return opMem }

// RelOp is a RIP-relative operand (for Jcc, CALL, JMP near).
type RelOp struct {
	// Offset from end of instruction to target.
	// The encoder fills in a placeholder; caller patches after layout.
	Offset int32
}

func (r RelOp) operandKind() opKind { return opRel }

// --- REX prefix helpers ---

// rex builds a REX prefix byte. Set W for 64-bit, R for ModRM.reg extension,
// X for SIB.index extension, B for ModRM.rm/SIB.base/opcode reg extension.
func rex(W, R, X, B bool) byte {
	b := byte(0x40)
	if W {
		b |= 0x08
	}
	if R {
		b |= 0x04
	}
	if X {
		b |= 0x02
	}
	if B {
		b |= 0x01
	}
	return b
}

// modrm builds the ModRM byte.
// mod: 00=indirect, 01=disp8, 10=disp32, 11=direct
func modrm(mod, reg, rm byte) byte {
	return (mod << 6) | ((reg & 7) << 3) | (rm & 7)
}

// sib builds the SIB byte.
func sib(scale, index, base byte) byte {
	return (scale << 6) | ((index & 7) << 3) | (base & 7)
}

// scaleToSS converts a scale factor to the 2-bit SS field.
func scaleToSS(scale uint8) byte {
	switch scale {
	case 1:
		return 0
	case 2:
		return 1
	case 4:
		return 2
	case 8:
		return 3
	}
	return 0
}

// --- Instruction encoding ---

// EncMOV_RR encodes MOV reg, reg (register to register).
func EncMOV_RR(dst, src Reg, w Width) []byte {
	var out []byte
	switch w {
	case W64:
		out = append(out, rex(true, src.IsExtended(), false, dst.IsExtended()))
		out = append(out, 0x89)
		out = append(out, modrm(3, src.Low3(), dst.Low3()))
	case W32:
		if src.IsExtended() || dst.IsExtended() {
			out = append(out, rex(false, src.IsExtended(), false, dst.IsExtended()))
		}
		out = append(out, 0x89)
		out = append(out, modrm(3, src.Low3(), dst.Low3()))
	case W16:
		out = append(out, 0x66)
		if src.IsExtended() || dst.IsExtended() {
			out = append(out, rex(false, src.IsExtended(), false, dst.IsExtended()))
		}
		out = append(out, 0x89)
		out = append(out, modrm(3, src.Low3(), dst.Low3()))
	case W8:
		if src.IsExtended() || dst.IsExtended() {
			out = append(out, rex(false, src.IsExtended(), false, dst.IsExtended()))
		}
		out = append(out, 0x88)
		out = append(out, modrm(3, src.Low3(), dst.Low3()))
	}
	return out
}

// EncMOV_RI encodes MOV reg, imm (immediate to register).
func EncMOV_RI(dst Reg, imm int64, w Width) []byte {
	var out []byte
	switch w {
	case W64:
		out = append(out, rex(true, false, false, dst.IsExtended()))
		out = append(out, 0xB8+dst.Low3())
		out = appendInt64(out, imm)
	case W32:
		if dst.IsExtended() {
			out = append(out, rex(false, false, false, true))
		}
		out = append(out, 0xB8+dst.Low3())
		out = appendInt32(out, int32(imm))
	case W8:
		if dst.IsExtended() {
			out = append(out, rex(false, false, false, true))
		}
		out = append(out, 0xB0+dst.Low3())
		out = append(out, byte(imm))
	}
	return out
}

// EncMOV_MR encodes MOV [mem], reg (store reg to memory).
func EncMOV_MR(mem MemOp, src Reg) []byte {
	return encMemReg(0x89, 0x88, mem, src)
}

// EncMOV_RM encodes MOV reg, [mem] (load from memory).
func EncMOV_RM(dst Reg, mem MemOp) []byte {
	return encRegMem(0x8B, 0x8A, dst, mem)
}

// EncLEA encodes LEA reg, [mem].
func EncLEA(dst Reg, mem MemOp) []byte {
	return encRegMem(0x8D, 0x8D, dst, mem)
}

// EncADD_RR encodes ADD dst, src.
func EncADD_RR(dst, src Reg, w Width) []byte {
	return encALU_RR(0x01, 0x00, dst, src, w)
}

// EncSUB_RR encodes SUB dst, src.
func EncSUB_RR(dst, src Reg, w Width) []byte {
	return encALU_RR(0x29, 0x28, dst, src, w)
}

// EncADD_RI encodes ADD reg, imm32.
func EncADD_RI(dst Reg, imm int32, w Width) []byte {
	return encALU_RI(0x81, 0, dst, imm, w)
}

// EncSUB_RI encodes SUB reg, imm32.
func EncSUB_RI(dst Reg, imm int32, w Width) []byte {
	return encALU_RI(0x81, 5, dst, imm, w)
}

// EncIMUL_RR encodes IMUL dst, src (dst *= src).
func EncIMUL_RR(dst, src Reg, w Width) []byte {
	var out []byte
	if w == W64 {
		out = append(out, rex(true, dst.IsExtended(), false, src.IsExtended()))
	} else if dst.IsExtended() || src.IsExtended() {
		out = append(out, rex(false, dst.IsExtended(), false, src.IsExtended()))
	}
	out = append(out, 0x0F, 0xAF)
	out = append(out, modrm(3, dst.Low3(), src.Low3()))
	return out
}

// EncCMP_RR encodes CMP a, b.
func EncCMP_RR(a, b Reg, w Width) []byte {
	return encALU_RR(0x39, 0x38, a, b, w)
}

// EncCMP_RI encodes CMP reg, imm.
func EncCMP_RI(reg Reg, imm int32, w Width) []byte {
	return encALU_RI(0x81, 7, reg, imm, w)
}

// EncAND_RR encodes AND dst, src.
func EncAND_RR(dst, src Reg, w Width) []byte {
	return encALU_RR(0x21, 0x20, dst, src, w)
}

// EncOR_RR encodes OR dst, src.
func EncOR_RR(dst, src Reg, w Width) []byte {
	return encALU_RR(0x09, 0x08, dst, src, w)
}

// EncXOR_RR encodes XOR dst, src.
func EncXOR_RR(dst, src Reg, w Width) []byte {
	return encALU_RR(0x31, 0x30, dst, src, w)
}

// EncAND_RI encodes AND reg, imm32.
func EncAND_RI(dst Reg, imm int32, w Width) []byte {
	return encALU_RI(0x81, 4, dst, imm, w)
}

// EncSHL_RCL encodes SHL dst, CL.
func EncSHL_RCL(dst Reg, w Width) []byte {
	return encShift(0xD3, 4, dst, w)
}

// EncSHR_RCL encodes SHR dst, CL (logical shift right).
func EncSHR_RCL(dst Reg, w Width) []byte {
	return encShift(0xD3, 5, dst, w)
}

// EncSAR_RCL encodes SAR dst, CL (arithmetic shift right).
func EncSAR_RCL(dst Reg, w Width) []byte {
	return encShift(0xD3, 7, dst, w)
}

// EncSHL_RI encodes SHL dst, imm8.
func EncSHL_RI(dst Reg, imm uint8, w Width) []byte {
	return encShiftImm(0xC1, 4, dst, imm, w)
}

// EncSHR_RI encodes SHR dst, imm8.
func EncSHR_RI(dst Reg, imm uint8, w Width) []byte {
	return encShiftImm(0xC1, 5, dst, imm, w)
}

// EncNEG encodes NEG reg.
func EncNEG(dst Reg, w Width) []byte {
	return encUnary(0xF7, 3, dst, w)
}

// EncNOT encodes NOT reg.
func EncNOT(dst Reg, w Width) []byte {
	return encUnary(0xF7, 2, dst, w)
}

// EncPUSH encodes PUSH reg.
func EncPUSH(reg Reg) []byte {
	var out []byte
	if reg.IsExtended() {
		out = append(out, rex(false, false, false, true))
	}
	out = append(out, 0x50+reg.Low3())
	return out
}

// EncPOP encodes POP reg.
func EncPOP(reg Reg) []byte {
	var out []byte
	if reg.IsExtended() {
		out = append(out, rex(false, false, false, true))
	}
	out = append(out, 0x58+reg.Low3())
	return out
}

// EncCALL_REL32 encodes CALL rel32 (near call, 32-bit relative offset).
// Offset is from the end of the CALL instruction to the target.
func EncCALL_REL32(offset int32) []byte {
	out := []byte{0xE8}
	return appendInt32(out, offset)
}

// EncRET encodes RET (near return).
func EncRET() []byte { return []byte{0xC3} }

// EncNOP encodes NOP.
func EncNOP() []byte { return []byte{0x90} }

// EncSYSCALL encodes SYSCALL.
func EncSYSCALL() []byte { return []byte{0x0F, 0x05} }

// EncJMP_REL32 encodes JMP rel32.
func EncJMP_REL32(offset int32) []byte {
	out := []byte{0xE9}
	return appendInt32(out, offset)
}

// EncJMP_REL8 encodes JMP rel8 (short jump).
func EncJMP_REL8(offset int8) []byte {
	return []byte{0xEB, byte(offset)}
}

// Jcc condition codes
type Cond byte

const (
	CondO   Cond = 0x0 // overflow
	CondNO  Cond = 0x1
	CondB   Cond = 0x2 // below (unsigned <)
	CondNB  Cond = 0x3
	CondE   Cond = 0x4 // equal
	CondNE  Cond = 0x5
	CondBE  Cond = 0x6 // below or equal
	CondNBE Cond = 0x7
	CondS   Cond = 0x8 // sign
	CondNS  Cond = 0x9
	CondP   Cond = 0xA
	CondNP  Cond = 0xB
	CondL   Cond = 0xC // less (signed <)
	CondNL  Cond = 0xD
	CondLE  Cond = 0xE // less or equal
	CondNLE Cond = 0xF
)

// EncJcc_REL32 encodes a conditional jump with 32-bit offset.
func EncJcc_REL32(cond Cond, offset int32) []byte {
	out := []byte{0x0F, 0x80 + byte(cond)}
	return appendInt32(out, offset)
}

// EncJcc_REL8 encodes a conditional jump with 8-bit offset.
func EncJcc_REL8(cond Cond, offset int8) []byte {
	return []byte{0x70 + byte(cond), byte(offset)}
}

// EncMOVZX encodes MOVZX dst, src (zero-extend).
func EncMOVZX(dst Reg, src Reg, srcWidth, dstWidth Width) []byte {
	var out []byte
	if dstWidth == W64 {
		out = append(out, rex(true, dst.IsExtended(), false, src.IsExtended()))
	} else if dst.IsExtended() || src.IsExtended() {
		out = append(out, rex(false, dst.IsExtended(), false, src.IsExtended()))
	}
	switch srcWidth {
	case W8:
		out = append(out, 0x0F, 0xB6)
	case W16:
		out = append(out, 0x0F, 0xB7)
	default:
		// 32→64: use 32-bit MOV (zero-extends to 64-bit automatically)
		out = append(out, 0x8B)
	}
	out = append(out, modrm(3, dst.Low3(), src.Low3()))
	return out
}

// EncMOVSX encodes MOVSX dst, src (sign-extend).
func EncMOVSX(dst Reg, src Reg, srcWidth, dstWidth Width) []byte {
	var out []byte
	if dstWidth == W64 {
		out = append(out, rex(true, dst.IsExtended(), false, src.IsExtended()))
	} else if dst.IsExtended() || src.IsExtended() {
		out = append(out, rex(false, dst.IsExtended(), false, src.IsExtended()))
	}
	switch srcWidth {
	case W8:
		out = append(out, 0x0F, 0xBE)
	case W16:
		out = append(out, 0x0F, 0xBF)
	case W32:
		out = append(out, 0x63) // MOVSXD
	}
	out = append(out, modrm(3, dst.Low3(), src.Low3()))
	return out
}

// EncXOR_RR_zero encodes XOR reg, reg (efficient zero-register).
func EncXOR_RR_zero(reg Reg) []byte {
	// XOR r32, r32 — zeros the register and zero-extends to 64-bit
	var out []byte
	if reg.IsExtended() {
		out = append(out, rex(false, reg.IsExtended(), false, reg.IsExtended()))
	}
	out = append(out, 0x31)
	out = append(out, modrm(3, reg.Low3(), reg.Low3()))
	return out
}

// --- Internal helpers ---

func encALU_RR(op64, op8 byte, dst, src Reg, w Width) []byte {
	var out []byte
	switch w {
	case W64:
		out = append(out, rex(true, src.IsExtended(), false, dst.IsExtended()))
		out = append(out, op64)
		out = append(out, modrm(3, src.Low3(), dst.Low3()))
	case W32:
		if src.IsExtended() || dst.IsExtended() {
			out = append(out, rex(false, src.IsExtended(), false, dst.IsExtended()))
		}
		out = append(out, op64)
		out = append(out, modrm(3, src.Low3(), dst.Low3()))
	case W8:
		if src.IsExtended() || dst.IsExtended() {
			out = append(out, rex(false, src.IsExtended(), false, dst.IsExtended()))
		}
		out = append(out, op8)
		out = append(out, modrm(3, src.Low3(), dst.Low3()))
	}
	return out
}

func encALU_RI(op byte, regField byte, dst Reg, imm int32, w Width) []byte {
	var out []byte
	if w == W64 {
		out = append(out, rex(true, false, false, dst.IsExtended()))
	} else if dst.IsExtended() {
		out = append(out, rex(false, false, false, true))
	}
	// Use imm8 form if possible
	if imm >= -128 && imm <= 127 {
		out = append(out, 0x83)
		out = append(out, modrm(3, regField, dst.Low3()))
		out = append(out, byte(int8(imm)))
	} else {
		out = append(out, op)
		out = append(out, modrm(3, regField, dst.Low3()))
		out = appendInt32(out, imm)
	}
	return out
}

func encShift(op byte, regField byte, dst Reg, w Width) []byte {
	var out []byte
	if w == W64 {
		out = append(out, rex(true, false, false, dst.IsExtended()))
	} else if dst.IsExtended() {
		out = append(out, rex(false, false, false, true))
	}
	out = append(out, op)
	out = append(out, modrm(3, regField, dst.Low3()))
	return out
}

func encShiftImm(op byte, regField byte, dst Reg, imm uint8, w Width) []byte {
	var out []byte
	if w == W64 {
		out = append(out, rex(true, false, false, dst.IsExtended()))
	} else if dst.IsExtended() {
		out = append(out, rex(false, false, false, true))
	}
	out = append(out, op)
	out = append(out, modrm(3, regField, dst.Low3()))
	out = append(out, imm)
	return out
}

func encUnary(op byte, regField byte, dst Reg, w Width) []byte {
	var out []byte
	if w == W64 {
		out = append(out, rex(true, false, false, dst.IsExtended()))
	} else if dst.IsExtended() {
		out = append(out, rex(false, false, false, true))
	}
	out = append(out, op)
	out = append(out, modrm(3, regField, dst.Low3()))
	return out
}

func encMemReg(op64, op8 byte, mem MemOp, src Reg) []byte {
	return encMem(op64, op8, src, mem)
}

func encRegMem(op64, op8 byte, dst Reg, mem MemOp) []byte {
	return encMem(op64, op8, dst, mem)
}

func encMem(op64, op8 byte, reg Reg, mem MemOp) []byte {
	var out []byte
	w := mem.Width
	if w == 0 {
		w = W64
	}

	// REX prefix
	needREX := w == W64 || reg.IsExtended() || mem.Base.IsExtended() ||
		(mem.Index != NoReg && mem.Index.IsExtended())
	if needREX {
		out = append(out, rex(w == W64, reg.IsExtended(),
			mem.Index != NoReg && mem.Index.IsExtended(),
			mem.Base.IsExtended()))
	}

	if w == W8 {
		out = append(out, op8)
	} else {
		out = append(out, op64)
	}

	// Determine mod and displacement
	var mod byte
	if mem.Disp == 0 && mem.Base != RBP && mem.Base != R13 {
		mod = 0
	} else if mem.Disp >= -128 && mem.Disp <= 127 {
		mod = 1
	} else {
		mod = 2
	}

	hasSIB := mem.Index != NoReg || mem.Base == RSP || mem.Base == R12

	if hasSIB {
		out = append(out, modrm(mod, reg.Low3(), 4)) // SIB follows
		indexReg := mem.Index
		if indexReg == NoReg {
			indexReg = RSP // no-index sentinel
		}
		out = append(out, sib(scaleToSS(mem.Scale), indexReg.Low3(), mem.Base.Low3()))
	} else {
		out = append(out, modrm(mod, reg.Low3(), mem.Base.Low3()))
	}

	if mod == 1 {
		out = append(out, byte(int8(mem.Disp)))
	} else if mod == 2 {
		out = appendInt32(out, mem.Disp)
	}

	return out
}

func appendInt32(out []byte, v int32) []byte {
	b := [4]byte{}
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	return append(out, b[:]...)
}

func appendInt64(out []byte, v int64) []byte {
	b := [8]byte{}
	binary.LittleEndian.PutUint64(b[:], uint64(v))
	return append(out, b[:]...)
}
