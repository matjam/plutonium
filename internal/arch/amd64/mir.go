package amd64

import (
	"fmt"
	"strings"

	"github.com/matjam/plutonium/internal/ssa"
)

// An MOp is a machine instruction kind. Most map to one x86-64 instruction;
// a few pseudo-ops are expanded by the encoder.
type MOp uint16

const (
	MNone MOp = iota

	// Moves. MOV size is the operand size; MOVZX/MOVSX extend Src (of size
	// SrcSize) into Dst (of size Size).
	MMOV
	MMOVZX
	MMOVSX
	MLEA

	// Two-address integer ALU: Dst = Dst op Src.
	MADD
	MSUB
	MAND
	MOR
	MXOR
	MIMUL // Dst = Dst * Src
	MCMP  // flags = Dst - Src
	MTEST // flags = Dst & Src

	// One-operand.
	MNEG
	MNOT
	MBSWAP

	// Shifts: Dst = Dst op Src, where Src is an immediate or CL.
	MSHL
	MSHR
	MSAR
	MROL
	MROR

	// Division. MDIV/MIDIV divide RDX:RAX by Src. MCQO sign-extends RAX into
	// RDX at the instruction size (CDQ for 4 bytes).
	MDIV
	MIDIV
	MCQO
	MMUL1  // RDX:RAX = RAX * Src (unsigned)
	MIMUL1 // RDX:RAX = RAX * Src (signed)
	MIMUL3 // Dst = Src * Imm

	MSETCC  // Dst8 = cond
	MCMOVCC // Dst = cond ? Src : Dst

	MBSF
	MBSR
	MPOPCNT

	// SSE scalar floating point. Size is 4 (ss) or 8 (sd).
	MMOVSF    // load, store or register move of a scalar float
	MADDSF    // Dst += Src
	MSUBSF    // Dst -= Src
	MMULSF    // Dst *= Src
	MDIVSF    // Dst /= Src
	MSQRTSF   // Dst = sqrt(Src)
	MUCOMI    // compare Dst with Src, setting ZF/PF/CF
	MXORPS    // Dst ^= Src (128-bit)
	MANDPS    // Dst &= Src (128-bit)
	MCVTSI2F  // Dst(float, Size) = Src(int, SrcSize)
	MCVTTF2SI // Dst(int, Size) = trunc(Src(float, SrcSize))
	MCVTF2F   // Dst(float, Size) = Src(float, SrcSize)
	MMOVQ     // move between a general register and an XMM register (Size 4 or 8)

	// Control flow.
	MJMP  // to Target
	MJCC  // to Target if Cond
	MCALL // Sym or register/memory target
	MRET
	MUD2

	// Stack.
	MPUSH
	MPOP

	// Block memory: RDI, RSI, RCX are implicit.
	MREPMOVSB
	MREPSTOSB

	// Pseudo-ops.
	MCOPY       // register copy that the allocator may coalesce away
	MPCOPY      // parallel copy (phi resolution); expanded before allocation
	MDEF        // defines Defs without code (e.g. incoming argument registers)
	MUSE        // uses Uses without code (e.g. result registers at return)
	MLABEL      // not emitted
	MSPILL      // store register to spill slot (inserted by the allocator)
	MRELOAD     // load register from spill slot
	MLOADCONSTF // load a float constant into an XMM register (Imm holds the bits)
	MEPILOGUE   // restore callee-saved registers and the stack pointer, then RET
)

var mopNames = map[MOp]string{
	MMOV: "mov", MMOVZX: "movzx", MMOVSX: "movsx", MLEA: "lea",
	MADD: "add", MSUB: "sub", MAND: "and", MOR: "or", MXOR: "xor", MIMUL: "imul", MCMP: "cmp", MTEST: "test",
	MNEG: "neg", MNOT: "not", MBSWAP: "bswap",
	MSHL: "shl", MSHR: "shr", MSAR: "sar", MROL: "rol", MROR: "ror",
	MDIV: "div", MIDIV: "idiv", MCQO: "cqo", MMUL1: "mul", MIMUL1: "imul1", MIMUL3: "imul3",
	MSETCC: "set", MCMOVCC: "cmov", MBSF: "bsf", MBSR: "bsr", MPOPCNT: "popcnt",
	MMOVSF: "movs", MADDSF: "adds", MSUBSF: "subs", MMULSF: "muls", MDIVSF: "divs", MSQRTSF: "sqrts",
	MUCOMI: "ucomis", MXORPS: "xorps", MANDPS: "andps", MCVTSI2F: "cvtsi2s", MCVTTF2SI: "cvtts2si",
	MCVTF2F: "cvts2s", MMOVQ: "movq",
	MJMP: "jmp", MJCC: "j", MCALL: "call", MRET: "ret", MUD2: "ud2",
	MPUSH: "push", MPOP: "pop", MREPMOVSB: "rep movsb", MREPSTOSB: "rep stosb",
	MCOPY: "copy", MPCOPY: "pcopy", MDEF: "def", MUSE: "use", MLABEL: "label",
	MSPILL: "spill", MRELOAD: "reload", MLOADCONSTF: "loadconstf", MEPILOGUE: "epilogue",
}

func (op MOp) String() string {
	if s, ok := mopNames[op]; ok {
		return s
	}
	return fmt.Sprintf("mop%d", op)
}

// A Cond is an x86 condition code, in hardware encoding order.
type Cond uint8

const (
	CondO  Cond = iota // overflow
	CondNO             // no overflow
	CondB              // below (unsigned <), carry
	CondAE             // above or equal (unsigned >=)
	CondE              // equal
	CondNE             // not equal
	CondBE             // below or equal (unsigned <=)
	CondA              // above (unsigned >)
	CondS              // sign
	CondNS             // no sign
	CondP              // parity (unordered float compare)
	CondNP             // no parity
	CondL              // less (signed <)
	CondGE             // greater or equal (signed >=)
	CondLE             // less or equal (signed <=)
	CondG              // greater (signed >)
)

var condNames = [...]string{"o", "no", "b", "ae", "e", "ne", "be", "a", "s", "ns", "p", "np", "l", "ge", "le", "g"}

func (c Cond) String() string { return condNames[c] }

// Invert returns the opposite condition.
func (c Cond) Invert() Cond { return c ^ 1 }

// An OperandKind says what an operand is.
type OperandKind uint8

const (
	OpdNone OperandKind = iota
	OpdReg
	OpdImm
	OpdMem
)

// A Mem is a memory operand: [Base + Index*Scale + Disp], or a symbol
// address (RIP-relative), or a stack slot resolved during frame layout.
type Mem struct {
	Base  Reg
	Index Reg
	Scale uint8
	Disp  int64
	Sym   *ssa.Sym  // RIP-relative symbol (Base and Index are NoReg)
	Slot  *ssa.Slot // stack slot (Base is NoReg until frame layout)
	// ArgOff, when ArgSlot is set, addresses an incoming stack argument at
	// this offset above the return address.
	ArgSlot bool
	ArgOff  int64
	// OutArg addresses the outgoing argument area at offset Disp from RSP.
	OutArg bool
	// RIP addresses Disp bytes past the end of the instruction, with no
	// symbol (Base and Index are NoReg).
	RIP bool
}

// An Operand is a register, immediate or memory reference.
type Operand struct {
	Kind OperandKind
	Reg  Reg
	Imm  int64
	Mem  Mem
}

func RegOp(r Reg) Operand   { return Operand{Kind: OpdReg, Reg: r} }
func ImmOp(i int64) Operand { return Operand{Kind: OpdImm, Imm: i} }
func MemOp(m Mem) Operand   { return Operand{Kind: OpdMem, Mem: m} }

// BaseMem returns [base + disp].
func BaseMem(base Reg, disp int64) Mem {
	return Mem{Base: base, Index: NoReg, Disp: disp}
}

// An MInst is one machine instruction.
type MInst struct {
	Op      MOp
	Size    uint8 // operand size in bytes
	SrcSize uint8 // source size for extending moves and conversions
	Dst     Operand
	Src     Operand
	Cond    Cond
	Target  *MBlock  // jumps
	Sym     *ssa.Sym // direct calls
	Imm     int64    // MIMUL3 immediate, MLOADCONSTF bits

	// Registers defined and used beyond those in Dst and Src: fixed
	// registers of calls, divisions and the like.
	ImplicitDefs []Reg
	ImplicitUses []Reg

	// For MPCOPY: parallel copies Dsts[i] = Srcs[i].
	Dsts, Srcs []Reg

	Slot *ssa.Slot // spill slot for MSPILL/MRELOAD
}

// An MBlock is a basic block of machine instructions.
type MBlock struct {
	ID     int
	Insts  []*MInst
	Succs  []*MBlock
	Preds  []*MBlock
	Depth  int // loop depth, for spill costs
	Likely int8

	// Filled in by the encoder.
	offset int
}

func (b *MBlock) String() string { return fmt.Sprintf(".L%d", b.ID) }

// An MFunc is a function in machine IR.
type MFunc struct {
	Name   string
	Sym    *ssa.Sym
	Blocks []*MBlock
	Order  []*MBlock   // emission order (see ssa.Layout)
	Slots  []*ssa.Slot // stack slots: locals and spills
	Local  bool

	nextVReg  Reg
	regClass  map[Reg]RegClass
	frameFP   bool  // use RBP as a frame pointer (over-aligned slots)
	maxAlign  int64 // largest slot alignment
	outArgs   int64 // bytes of outgoing stack arguments
	usedSaved []Reg // callee-saved registers the function changes
	frameSize int64 // bytes below the saved registers, excluding them
	interrupt bool
	nextSlot  int
	consts    []Const
	constSyms map[[16]byte]*ssa.Sym
}

func newMFunc(name string) *MFunc {
	return &MFunc{Name: name, nextVReg: FirstVirtual, regClass: map[Reg]RegClass{}}
}

// newVReg allocates a virtual register.
func (f *MFunc) newVReg(c RegClass) Reg {
	r := f.nextVReg
	f.nextVReg++
	f.regClass[r] = c
	return r
}

// classOf returns the register class of any register.
func (f *MFunc) classOf(r Reg) RegClass {
	if r.IsPhys() {
		return r.PhysClass()
	}
	return f.regClass[r]
}

// newSlot adds a stack slot.
func (f *MFunc) newSlot(name string, size, align int64) *ssa.Slot {
	s := &ssa.Slot{ID: 1<<20 + f.nextSlot, Name: name, Size: size, Align: align}
	f.nextSlot++
	f.Slots = append(f.Slots, s)
	return s
}

// String formats the function in an assembly-like syntax for -S output.
func (f *MFunc) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s:\n", f.Name)
	for _, b := range f.Blocks {
		fmt.Fprintf(&sb, "%s:\n", b)
		for _, in := range b.Insts {
			fmt.Fprintf(&sb, "\t%s\n", in)
		}
	}
	return sb.String()
}

func (o Operand) format(size int) string {
	switch o.Kind {
	case OpdReg:
		return "%" + o.Reg.Name(size)
	case OpdImm:
		return fmt.Sprintf("$%d", o.Imm)
	case OpdMem:
		return o.Mem.String()
	}
	return ""
}

func (m Mem) String() string {
	var sb strings.Builder
	switch {
	case m.RIP:
		fmt.Fprintf(&sb, "%d(%%rip)", m.Disp)
		return sb.String()
	case m.Sym != nil:
		fmt.Fprintf(&sb, "%s", m.Sym.Name)
		if m.Disp != 0 {
			fmt.Fprintf(&sb, "%+d", m.Disp)
		}
		if m.Base == NoReg && m.Index == NoReg {
			sb.WriteString("(%rip)")
			return sb.String()
		}
		// Absolute address plus registers.
		sb.WriteString("(")
		if m.Base != NoReg {
			sb.WriteString(m.Base.String())
		}
		if m.Index != NoReg {
			fmt.Fprintf(&sb, ",%s,%d", m.Index, max(m.Scale, 1))
		}
		sb.WriteString(")")
		return sb.String()
	case m.Slot != nil && m.Base == NoReg:
		fmt.Fprintf(&sb, "%s", m.Slot)
		if m.Disp != 0 {
			fmt.Fprintf(&sb, "%+d", m.Disp)
		}
		sb.WriteString("(frame)")
		return sb.String()
	case m.ArgSlot && m.Base == NoReg:
		fmt.Fprintf(&sb, "arg%d(frame)", m.ArgOff)
		return sb.String()
	case m.OutArg && m.Base == NoReg:
		fmt.Fprintf(&sb, "out%d(%%rsp)", m.Disp)
		return sb.String()
	}
	if m.Disp != 0 {
		fmt.Fprintf(&sb, "%d", m.Disp)
	}
	sb.WriteByte('(')
	if m.Base != NoReg {
		sb.WriteString("%" + m.Base.Name(8))
	}
	if m.Index != NoReg {
		fmt.Fprintf(&sb, ",%%%s,%d", m.Index.Name(8), m.Scale)
	}
	sb.WriteByte(')')
	return sb.String()
}

func (in *MInst) String() string {
	size := int(in.Size)
	switch in.Op {
	case MJMP:
		return fmt.Sprintf("jmp %s", in.Target)
	case MJCC:
		return fmt.Sprintf("j%s %s", in.Cond, in.Target)
	case MSETCC:
		return fmt.Sprintf("set%s %s", in.Cond, in.Dst.format(1))
	case MCMOVCC:
		return fmt.Sprintf("cmov%s %s, %s", in.Cond, in.Src.format(size), in.Dst.format(size))
	case MCALL:
		if in.Sym != nil {
			return fmt.Sprintf("call %s", in.Sym.Name)
		}
		return fmt.Sprintf("call *%s", in.Src.format(8))
	case MPCOPY:
		var parts []string
		for i := range in.Dsts {
			parts = append(parts, fmt.Sprintf("%s<-%s", in.Dsts[i], in.Srcs[i]))
		}
		return "pcopy " + strings.Join(parts, " ")
	case MDEF:
		return fmt.Sprintf("def %v", in.ImplicitDefs)
	case MUSE:
		return fmt.Sprintf("use %v", in.ImplicitUses)
	case MSPILL:
		return fmt.Sprintf("spill %s -> %s", in.Src.format(8), in.Slot)
	case MRELOAD:
		return fmt.Sprintf("reload %s -> %s", in.Slot, in.Dst.format(8))
	case MMOVZX, MMOVSX, MCVTSI2F, MCVTTF2SI, MCVTF2F:
		return fmt.Sprintf("%s%d%d %s, %s", in.Op, in.SrcSize, in.Size, in.Src.format(int(in.SrcSize)), in.Dst.format(size))
	case MIMUL3:
		return fmt.Sprintf("imul $%d, %s, %s", in.Imm, in.Src.format(size), in.Dst.format(size))
	case MLOADCONSTF:
		return fmt.Sprintf("loadconstf $%#x, %s", in.Imm, in.Dst.format(size))
	}
	var args []string
	if in.Src.Kind != OpdNone {
		args = append(args, in.Src.format(size))
	}
	if in.Dst.Kind != OpdNone {
		args = append(args, in.Dst.format(size))
	}
	return fmt.Sprintf("%s%d %s", in.Op, in.Size, strings.Join(args, ", "))
}
