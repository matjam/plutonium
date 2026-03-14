package encoder

// Reg identifies an x86_64 register.
type Reg uint8

const (
	// 64-bit general purpose registers
	RAX Reg = 0
	RCX Reg = 1
	RDX Reg = 2
	RBX Reg = 3
	RSP Reg = 4
	RBP Reg = 5
	RSI Reg = 6
	RDI Reg = 7
	R8  Reg = 8
	R9  Reg = 9
	R10 Reg = 10
	R11 Reg = 11
	R12 Reg = 12
	R13 Reg = 13
	R14 Reg = 14
	R15 Reg = 15

	NoReg Reg = 255
)

func (r Reg) IsExtended() bool { return r >= 8 && r != NoReg }
func (r Reg) Low3() byte       { return byte(r & 7) }

// Width is the operand size in bits.
type Width uint8

const (
	W8  Width = 8
	W16 Width = 16
	W32 Width = 32
	W64 Width = 64
)
