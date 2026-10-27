// Translated by go2pu from internal/arch/amd64/regs.go at the go-cutover tag; edited by hand since.

// Package amd64 is Plutonium's x86-64 backend.
//
// It selects instructions from SSA into a machine IR over virtual registers,
// leaves SSA by inserting copies for phis, allocates registers by iterated
// register coalescing (George and Appel), lays out the stack frame and encodes
// machine code. The target is baseline x86-64 (SSE2, no later extensions).
package amd64

import (
	"compiler/compat"
)

// A Reg is a register. Physical registers are 0-31: the sixteen general
// purpose registers in encoding order, then XMM0-XMM15. Virtual registers
// start at FirstVirtual.
type Reg int32

const (
	RAX Reg = iota
	RCX
	RDX
	RBX
	RSP
	RBP
	RSI
	RDI
	R8
	R9
	R10
	R11
	R12
	R13
	R14
	R15
	X0
	X1
	X2
	X3
	X4
	X5
	X6
	X7
	X8
	X9
	X10
	X11
	X12
	X13
	X14
	X15

	NumPhys          = 32
	FirstVirtual     = 64
	NoReg        Reg = -1
)

// A RegClass is a register file.
type RegClass uint8

const (
	ClassGP  RegClass = iota // general purpose
	ClassXMM                 // SSE
)

// IsPhys reports whether r is a physical register.
func (r Reg) IsPhys() bool { return r >= 0 && r < NumPhys }

// IsVirtual reports whether r is a virtual register.
func (r Reg) IsVirtual() bool { return r >= FirstVirtual }

// PhysClass returns the class of a physical register.
func (r Reg) PhysClass() RegClass {
	if r >= X0 && r <= X15 {
		return ClassXMM
	}
	return ClassGP
}

// enc returns the 4-bit hardware encoding of a physical register.
func (r Reg) enc() byte {
	if r >= X0 {
		return byte(r - X0)
	}
	return byte(r)
}

var gpNames64 = [...]string{"rax", "rcx", "rdx", "rbx", "rsp", "rbp", "rsi", "rdi", "r8", "r9", "r10", "r11", "r12", "r13", "r14", "r15"}
var gpNames32 = [...]string{"eax", "ecx", "edx", "ebx", "esp", "ebp", "esi", "edi", "r8d", "r9d", "r10d", "r11d", "r12d", "r13d", "r14d", "r15d"}
var gpNames16 = [...]string{"ax", "cx", "dx", "bx", "sp", "bp", "si", "di", "r8w", "r9w", "r10w", "r11w", "r12w", "r13w", "r14w", "r15w"}
var gpNames8 = [...]string{"al", "cl", "dl", "bl", "spl", "bpl", "sil", "dil", "r8b", "r9b", "r10b", "r11b", "r12b", "r13b", "r14b", "r15b"}

// Name returns the register's assembler name at the given operand size.
func (r Reg) Name(size int) string {
	switch {
	case r == NoReg:
		return "<none>"
	case r.IsVirtual():
		return compat.Sprintfb("v%d", compat.ArgI(int64(r-FirstVirtual)))
	case r >= X0 && r <= X15:
		return compat.Sprintfb("xmm%d", compat.ArgI(int64(r-X0)))
	case r >= 0 && r < 16:
		switch size {
		case 1:
			return gpNames8[r]
		case 2:
			return gpNames16[r]
		case 4:
			return gpNames32[r]
		}
		return gpNames64[r]
	}
	return compat.Sprintfb("r?%d", compat.ArgI(int64(int32(r))))
}

func (r Reg) String() string { return r.Name(8) }

// Calling convention.
var (
	// Integer argument registers (System V and Plutonium's internal ABI).
	intArgRegs = []Reg{RDI, RSI, RDX, RCX, R8, R9}
	// Float argument registers.
	floatArgRegs = []Reg{X0, X1, X2, X3, X4, X5, X6, X7}
	// Result registers of the internal ABI. The C ABI uses only the first
	// two of each.
	intResultRegs   = []Reg{RAX, RDX, RCX, RSI}
	floatResultRegs = []Reg{X0, X1, X2, X3}

	// closureReg carries the closure record to a function called through a
	// func value.
	closureReg = RAX

	// Registers a call may change.
	callerSaved = []Reg{RAX, RCX, RDX, RSI, RDI, R8, R9, R10, R11,
		X0, X1, X2, X3, X4, X5, X6, X7, X8, X9, X10, X11, X12, X13, X14, X15}
	// Registers a function must preserve.
	calleeSaved = []Reg{RBX, RBP, R12, R13, R14, R15}
)

// allocatable lists, per class, the registers the allocator may assign, in
// preference order: caller-saved first so short-lived values do not force
// saves in the prologue.
var allocatable = [2][]Reg{
	ClassGP:  {RAX, RCX, RDX, RSI, RDI, R8, R9, R10, R11, RBX, R12, R13, R14, R15, RBP},
	ClassXMM: {X0, X1, X2, X3, X4, X5, X6, X7, X8, X9, X10, X11, X12, X13, X14, X15},
}

func isCalleeSaved(r Reg) bool {
	for _, c := range calleeSaved {
		if c == r {
			return true
		}
	}
	return false
}
