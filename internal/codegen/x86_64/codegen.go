package x86_64

import (
	"encoding/binary"
	"fmt"

	"github.com/matjam/plutonium/internal/codegen"
	"github.com/matjam/plutonium/internal/codegen/x86_64/encoder"
	"github.com/matjam/plutonium/internal/codegen/x86_64/regalloc"
	"github.com/matjam/plutonium/internal/ir"
)

// Backend is the x86_64 code generation backend.
type Backend struct{}

func New() *Backend { return &Backend{} }

func (b *Backend) Name() string                 { return "x86_64" }
func (b *Backend) Triple() string               { return "x86_64-linux" }
func (b *Backend) WordSize() int                { return 8 }
func (b *Backend) Endianness() binary.ByteOrder { return binary.LittleEndian }

func (b *Backend) CompileFunction(fn *ir.Function) (*codegen.MachineFunc, error) {
	cg := &codeGen{
		fn:    fn,
		alloc: regalloc.Allocate(fn),
	}
	code, relocs, err := cg.compile()
	if err != nil {
		return nil, fmt.Errorf("codegen %s: %w", fn.Name, err)
	}
	return &codegen.MachineFunc{
		Name:   fn.Name,
		Code:   code,
		Relocs: relocs,
		Size:   len(code),
	}, nil
}

type codeGen struct {
	fn     *ir.Function
	alloc  *regalloc.Allocation
	code   []byte
	relocs []codegen.Reloc
	// Map basic block label -> code offset (for branch patching)
	blockOffsets map[string]int
	// Pending forward references: [(code offset of imm32, target block label)]
	fwdRefs []fwdRef
	// Stack frame layout for alloca slots
	stackSlots map[int]int32 // IR Value ID -> RBP offset (negative)
	frameSize  int32         // total frame size for locals (alloca slots)
}

type fwdRef struct {
	codeOffset int    // byte offset in code of the 4-byte imm32 to patch
	target     string // block label
	instrEnd   int    // byte offset of end of the branch instruction
}

func (cg *codeGen) emit(b []byte) {
	cg.code = append(cg.code, b...)
}

func (cg *codeGen) offset() int { return len(cg.code) }

// layoutStackFrame scans all instructions for OpAlloca and assigns each
// a stack slot as a negative offset from RBP.
// When an alloca has AllocAlign > 8, the stack offset is rounded to meet
// the alignment requirement (e.g., aligned[T, 4096] gets a 4096-aligned slot).
func (cg *codeGen) layoutStackFrame() {
	cg.stackSlots = make(map[int]int32)
	offset := int32(0)
	for _, bb := range cg.fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpAlloca && instr.Result != nil {
				// Determine size from AllocType
				slotSize := int32(8)
				if instr.AllocType != nil {
					sz := ir.SizeOf(instr.AllocType)
					if sz > 0 {
						// Round up to 8-byte alignment
						slotSize = int32((sz + 7) &^ 7)
					}
				}
				// If the alloca has explicit alignment > 8, round offset
				// to meet the alignment requirement.
				align := int32(8)
				if instr.AllocAlign > 8 {
					align = int32(instr.AllocAlign)
				}
				offset -= slotSize
				if align > 8 {
					// Round down (more negative) to the next alignment boundary
					offset = -((-offset + align - 1) &^ (align - 1))
				}
				cg.stackSlots[instr.Result.ID] = offset
			}
		}
	}
	cg.frameSize = -offset // total positive size
	// Align to 16 bytes
	cg.frameSize = (cg.frameSize + 15) &^ 15
}

// System V AMD64 ABI parameter registers.
var abiParamRegs = []encoder.Reg{
	encoder.RDI, encoder.RSI, encoder.RDX, encoder.RCX, encoder.R8, encoder.R9,
}

// emitParamSetup moves function parameters from their ABI registers to
// their alloca stack slots. The IR pattern for each param is:
//
//	param = fn.NewParam(...)
//	slot  = alloca type
//	store param -> slot
//
// We find the store instruction that stores each param into an alloca
// slot and emit MOV [RBP+off], abi_reg.
func (cg *codeGen) emitParamSetup() {
	for i, param := range cg.fn.Params {
		if i >= len(abiParamRegs) {
			break // TODO: stack-passed params
		}
		// Find the store instruction that stores this param to an alloca slot
		for _, bb := range cg.fn.Blocks {
			for _, instr := range bb.Instrs {
				if instr.Op == ir.OpStore && len(instr.Args) >= 2 {
					if instr.Args[0].ID == param.ID && instr.Args[0].Kind == ir.ValParam {
						// The store target (Args[1]) should be an alloca slot
						if off, ok := cg.stackSlots[instr.Args[1].ID]; ok {
							mem := encoder.MemOp{Base: encoder.RBP, Index: encoder.NoReg, Disp: off, Width: encoder.W64}
							cg.emit(encoder.EncMOV_MR(mem, abiParamRegs[i]))
						}
					}
				}
			}
		}
	}
}

func (cg *codeGen) compile() ([]byte, []codegen.Reloc, error) {
	cg.blockOffsets = make(map[string]int)
	cg.fwdRefs = nil

	// Layout stack frame FIRST — scan for alloca instructions
	cg.layoutStackFrame()

	// Function prologue
	cg.emit(encoder.EncPUSH(encoder.RBP))
	cg.emit(encoder.EncMOV_RR(encoder.RBP, encoder.RSP, encoder.W64))

	// Allocate stack space for locals (alloca slots) AND register spills
	totalStack := cg.frameSize
	if cg.alloc.StackSize > 0 {
		totalStack += int32(cg.alloc.StackSize)
	}
	if totalStack > 0 {
		totalStack = (totalStack + 15) &^ 15 // 16-byte align
		cg.emit(encoder.EncSUB_RI(encoder.RSP, totalStack, encoder.W64))
	}

	// Move ABI params to their stack slots
	cg.emitParamSetup()

	// Compile each basic block
	for _, bb := range cg.fn.Blocks {
		cg.blockOffsets[bb.Label] = cg.offset()
		cg.compileBlock(bb)
	}

	// Patch forward references
	for _, ref := range cg.fwdRefs {
		targetOff, ok := cg.blockOffsets[ref.target]
		if !ok {
			return nil, nil, fmt.Errorf("undefined block %q", ref.target)
		}
		rel := int32(targetOff - ref.instrEnd)
		cg.code[ref.codeOffset] = byte(rel)
		cg.code[ref.codeOffset+1] = byte(rel >> 8)
		cg.code[ref.codeOffset+2] = byte(rel >> 16)
		cg.code[ref.codeOffset+3] = byte(rel >> 24)
	}

	return cg.code, cg.relocs, nil
}

func (cg *codeGen) compileBlock(bb *ir.BasicBlock) {
	for _, instr := range bb.Instrs {
		cg.compileInstr(instr)
	}
	if bb.Term != nil {
		cg.compileTerm(bb.Term)
	}
}

func (cg *codeGen) regFor(v *ir.Value) encoder.Reg {
	if v == nil {
		return encoder.RAX
	}

	// Constants: load immediate into RAX
	if v.Kind == ir.ValConst {
		cg.emit(encoder.EncMOV_RI(encoder.RAX, v.IntVal, widthFor(v.Type)))
		return encoder.RAX
	}

	// Global symbol references (e.g., string literals in .rodata).
	// Load the address via RIP-relative LEA with a relocation.
	if v.Kind == ir.ValGlobal {
		// LEA RAX, [RIP + disp32]  →  48 8D 05 xx xx xx xx
		relocOff := cg.offset() + 3 // offset of the disp32
		cg.emit([]byte{0x48, 0x8D, 0x05, 0x00, 0x00, 0x00, 0x00})
		cg.relocs = append(cg.relocs, codegen.Reloc{
			Offset: relocOff,
			Symbol: v.Name,
			Addend: -4, // standard PC32
			Type:   codegen.RelocPC32,
		})
		return encoder.RAX
	}

	// Stack slots (alloca results) — return address via LEA
	if off, ok := cg.stackSlots[v.ID]; ok {
		mem := encoder.MemOp{Base: encoder.RBP, Index: encoder.NoReg, Disp: off, Width: encoder.W64}
		cg.emit(encoder.EncLEA(encoder.RAX, mem))
		return encoder.RAX
	}

	// Register-allocated values
	if reg, ok := cg.alloc.Reg[v.ID]; ok {
		return reg
	}

	// Spilled values — always load as W64 to avoid partial register issues
	if off, ok := cg.alloc.Spill[v.ID]; ok {
		mem := encoder.MemOp{Base: encoder.RBP, Index: encoder.NoReg, Disp: int32(off), Width: encoder.W64}
		cg.emit(encoder.EncMOV_RM(encoder.RAX, mem))
		return encoder.RAX
	}

	return encoder.RAX
}

func (cg *codeGen) storeResult(result *ir.Value, reg encoder.Reg) {
	if result == nil {
		return
	}
	// Don't store results for alloca values — they are stack addresses,
	// not computational values that need register/spill storage.
	if _, ok := cg.stackSlots[result.ID]; ok {
		return
	}
	if dstReg, ok := cg.alloc.Reg[result.ID]; ok {
		if dstReg != reg {
			cg.emit(encoder.EncMOV_RR(dstReg, reg, encoder.W64))
		}
		return
	}
	if off, ok := cg.alloc.Spill[result.ID]; ok {
		// Always use W64 for spill stores to preserve zero-extended values
		mem := encoder.MemOp{Base: encoder.RBP, Index: encoder.NoReg, Disp: int32(off), Width: encoder.W64}
		cg.emit(encoder.EncMOV_MR(mem, reg))
	}
}

func boolToByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

func widthFor(t *ir.Type) encoder.Width {
	if t == nil {
		return encoder.W64
	}
	switch t.Kind {
	case ir.TyI8, ir.TyU8:
		return encoder.W8
	case ir.TyI16, ir.TyU16:
		return encoder.W16
	case ir.TyI32, ir.TyU32:
		return encoder.W32
	}
	return encoder.W64
}

// isParamStore returns true if the instruction is a store of a param value
// into an alloca slot. These are handled by emitParamSetup and should be
// skipped during normal instruction compilation.
func (cg *codeGen) isParamStore(instr *ir.Instruction) bool {
	if instr.Op != ir.OpStore || len(instr.Args) < 2 {
		return false
	}
	if instr.Args[0].Kind != ir.ValParam {
		return false
	}
	_, isSlot := cg.stackSlots[instr.Args[1].ID]
	return isSlot
}

func (cg *codeGen) compileInstr(instr *ir.Instruction) {
	switch instr.Op {
	case ir.OpAdd, ir.OpSub, ir.OpAnd, ir.OpOr, ir.OpXor:
		cg.compileALU(instr)

	case ir.OpMul:
		if len(instr.Args) == 2 {
			a, b := cg.stageBinaryArgs(instr.Args[0], instr.Args[1])
			cg.emit(encoder.EncIMUL_RR(a, b, encoder.W64))
			cg.storeResult(instr.Result, a)
		}

	case ir.OpCmp:
		cg.compileCmp(instr)

	case ir.OpAlloca:
		// Handled by layoutStackFrame — nothing to emit.
		// The result value is tracked in stackSlots.

	case ir.OpLoad:
		// NOTE: volatile loads (instr.IsVolatile) are codegen-identical to
		// regular loads on x86_64 — MOV is naturally ordered for MMIO.
		// The IsVolatile flag ensures no future optimizer elides or reorders
		// these accesses. For v0.1, this is a no-op.
		if len(instr.Args) == 1 {
			ptr := instr.Args[0]

			// Check if ptr is a stack slot (alloca)
			if off, ok := cg.stackSlots[ptr.ID]; ok {
				mem := encoder.MemOp{Base: encoder.RBP, Index: encoder.NoReg, Disp: off, Width: encoder.W64}
				cg.emit(encoder.EncMOV_RM(encoder.RAX, mem))
			} else {
				ptrReg := cg.regFor(ptr)
				w := widthFor(instr.Result.Type)
				mem := encoder.MemOp{Base: ptrReg, Index: encoder.NoReg, Disp: 0, Width: w}
				cg.emit(encoder.EncMOV_RM(encoder.RAX, mem))
				// Zero-extend sub-64-bit loads to prevent garbage in upper bits
				if w < encoder.W64 {
					cg.emit(encoder.EncMOVZX(encoder.RAX, encoder.RAX, w, encoder.W64))
				}
			}
			// If byte-swap is needed, emit BSWAP immediately after the load
			if instr.NeedsBSwap && instr.Result != nil {
				w := widthFor(instr.Result.Type)
				cg.emit(encoder.EncBSWAP(encoder.RAX, w))
			}
			cg.storeResult(instr.Result, encoder.RAX)
		}

	case ir.OpStore:
		// NOTE: volatile stores (instr.IsVolatile) are codegen-identical to
		// regular stores on x86_64 — MOV is naturally ordered for MMIO.
		// The IsVolatile flag ensures no future optimizer elides or reorders
		// these accesses. For v0.1, this is a no-op.
		if len(instr.Args) == 2 {
			// Skip stores that are param->alloca (handled by emitParamSetup)
			if cg.isParamStore(instr) {
				break
			}

			val := instr.Args[0]
			ptr := instr.Args[1]

			// Check if ptr is a stack slot (alloca)
			if off, ok := cg.stackSlots[ptr.ID]; ok {
				valReg := cg.regFor(val)
				// If byte-swap is needed, emit BSWAP before the store
				if instr.NeedsBSwap {
					if valReg != encoder.RAX {
						cg.emit(encoder.EncMOV_RR(encoder.RAX, valReg, encoder.W64))
						valReg = encoder.RAX
					}
					// Determine width from the AllocType of the target slot
					w := encoder.W64
					if val.Type != nil {
						w = widthFor(val.Type)
					}
					cg.emit(encoder.EncBSWAP(valReg, w))
				}
				mem := encoder.MemOp{Base: encoder.RBP, Index: encoder.NoReg, Disp: off, Width: encoder.W64}
				cg.emit(encoder.EncMOV_MR(mem, valReg))
			} else {
				// ptr is a register containing a memory address.
				// Use PUSH/POP staging (like stageBinaryArgs) to prevent
				// the second regFor from clobbering the first operand.
				valReg := cg.regFor(val)
				cg.emit(encoder.EncPUSH(valReg))
				ptrReg := cg.regFor(ptr)
				cg.emit(encoder.EncPUSH(ptrReg))
				// Pop ptr into R11, val into RAX
				cg.emit(encoder.EncPOP(encoder.R11))
				cg.emit(encoder.EncPOP(encoder.RAX))
				// If byte-swap is needed for non-stack stores
				if instr.NeedsBSwap {
					w := encoder.W64
					if val.Type != nil {
						w = widthFor(val.Type)
					}
					cg.emit(encoder.EncBSWAP(encoder.RAX, w))
				}
				mem := encoder.MemOp{Base: encoder.R11, Index: encoder.NoReg, Disp: 0, Width: encoder.W64}
				cg.emit(encoder.EncMOV_MR(mem, encoder.RAX))
			}
		}

	case ir.OpCall:
		cg.compileCall(instr)

	case ir.OpDiv:
		if len(instr.Args) >= 2 {
			a, b := cg.stageBinaryArgs(instr.Args[0], instr.Args[1])
			// a=R11 (dividend), b=RAX (divisor)
			// Move divisor to RCX to free RAX for dividend
			cg.emit(encoder.EncMOV_RR(encoder.RCX, b, encoder.W64))
			// Move dividend to RAX
			cg.emit(encoder.EncMOV_RR(encoder.RAX, a, encoder.W64))
			// Sign-extend RAX into RDX:RAX (CQO)
			cg.emit([]byte{0x48, 0x99})
			// IDIV RCX: RAX=quotient, RDX=remainder
			cg.emit([]byte{0x48, 0xF7, 0xF9}) // REX.W IDIV RCX
			cg.storeResult(instr.Result, encoder.RAX)
		}

	case ir.OpRem:
		if len(instr.Args) >= 2 {
			a, b := cg.stageBinaryArgs(instr.Args[0], instr.Args[1])
			cg.emit(encoder.EncMOV_RR(encoder.RCX, b, encoder.W64))
			cg.emit(encoder.EncMOV_RR(encoder.RAX, a, encoder.W64))
			cg.emit([]byte{0x48, 0x99})       // CQO
			cg.emit([]byte{0x48, 0xF7, 0xF9}) // REX.W IDIV RCX
			cg.storeResult(instr.Result, encoder.RDX)
		}

	case ir.OpShl:
		if len(instr.Args) >= 2 {
			a, b := cg.stageBinaryArgs(instr.Args[0], instr.Args[1])
			// b (shift count) must be in RCX
			cg.emit(encoder.EncMOV_RR(encoder.RCX, b, encoder.W64))
			cg.emit(encoder.EncSHL_RCL(a, encoder.W64))
			cg.storeResult(instr.Result, a)
		}

	case ir.OpShr:
		if len(instr.Args) >= 2 {
			a, b := cg.stageBinaryArgs(instr.Args[0], instr.Args[1])
			cg.emit(encoder.EncMOV_RR(encoder.RCX, b, encoder.W64))
			cg.emit(encoder.EncSHR_RCL(a, encoder.W64))
			cg.storeResult(instr.Result, a)
		}

	case ir.OpGEP:
		if len(instr.Args) >= 2 {
			elemSize := instr.ElemSize
			if elemSize == 0 {
				elemSize = 8
			}
			base, index := cg.stageBinaryArgs(instr.Args[0], instr.Args[1])

			// SIB only supports scales 1,2,4,8. For other sizes, multiply explicitly.
			switch elemSize {
			case 1, 2, 4, 8:
				mem := encoder.MemOp{Base: base, Index: index, Scale: byte(elemSize), Disp: 0, Width: encoder.W64}
				cg.emit(encoder.EncLEA(encoder.RAX, mem))
			default:
				// result = base + index * elemSize
				cg.emit(encoder.EncMOV_RI(encoder.RAX, elemSize, encoder.W64))
				cg.emit(encoder.EncIMUL_RR(encoder.RAX, index, encoder.W64))
				cg.emit(encoder.EncADD_RR(encoder.RAX, base, encoder.W64))
			}
			cg.storeResult(instr.Result, encoder.RAX)
		}

	case ir.OpExtractField:
		if len(instr.Args) >= 1 {
			base := cg.regFor(instr.Args[0])
			offset := int32(instr.FieldIdx * 8) // 8-byte aligned fields
			mem := encoder.MemOp{Base: base, Index: encoder.NoReg, Disp: offset, Width: encoder.W64}
			cg.emit(encoder.EncMOV_RM(encoder.RAX, mem))
			cg.storeResult(instr.Result, encoder.RAX)
		}

	case ir.OpInsertField:
		// Store value to struct field
		if len(instr.Args) >= 2 {
			base, val := cg.stageBinaryArgs(instr.Args[0], instr.Args[1])
			offset := int32(instr.FieldIdx * 8)
			mem := encoder.MemOp{Base: base, Index: encoder.NoReg, Disp: offset, Width: encoder.W64}
			cg.emit(encoder.EncMOV_MR(mem, val))
		}

	case ir.OpBSwap:
		if len(instr.Args) == 1 && instr.Result != nil {
			src := cg.regFor(instr.Args[0])
			if src != encoder.RAX {
				cg.emit(encoder.EncMOV_RR(encoder.RAX, src, encoder.W64))
			}
			w := widthFor(instr.Result.Type)
			cg.emit(encoder.EncBSWAP(encoder.RAX, w))
			cg.storeResult(instr.Result, encoder.RAX)
		}

	case ir.OpBitcast, ir.OpExtend, ir.OpTruncate:
		// Load the source value and store to the result's location.
		// When the source is a constant and the result has an allocated
		// register, load directly into that register to avoid clobbering
		// RAX (which regFor would otherwise use for all constants).
		if len(instr.Args) == 1 && instr.Result != nil {
			arg := instr.Args[0]
			if arg.Kind == ir.ValConst {
				if dstReg, ok := cg.alloc.Reg[instr.Result.ID]; ok {
					cg.emit(encoder.EncMOV_RI(dstReg, arg.IntVal, widthFor(arg.Type)))
					// Already in the right register — no storeResult needed.
				} else {
					// Spilled result: load into RAX, then store to spill slot
					reg := cg.regFor(arg)
					cg.storeResult(instr.Result, reg)
				}
			} else {
				reg := cg.regFor(arg)
				cg.storeResult(instr.Result, reg)
			}
		}
	}
}

// stageBinaryArgs safely evaluates two operands for a binary operation,
// returning them in R11 and a second register. Uses PUSH/POP to prevent
// the second regFor() from clobbering the first operand's register.
func (cg *codeGen) stageBinaryArgs(arg0, arg1 *ir.Value) (encoder.Reg, encoder.Reg) {
	// Evaluate arg0, save to stack
	src0 := cg.regFor(arg0)
	cg.emit(encoder.EncPUSH(src0))
	// Evaluate arg1 (may clobber the register src0 was in)
	src1 := cg.regFor(arg1)
	cg.emit(encoder.EncPUSH(src1))
	// Pop into safe registers: b=RAX (from arg1), a=R11 (from arg0)
	cg.emit(encoder.EncPOP(encoder.RAX))
	cg.emit(encoder.EncPOP(encoder.R11))
	return encoder.R11, encoder.RAX
}

func (cg *codeGen) compileALU(instr *ir.Instruction) {
	if len(instr.Args) < 2 {
		return
	}
	// Stage both operands to prevent register clobbering.
	// regFor may return a register that the other operand is allocated to.
	a, b := cg.stageBinaryArgs(instr.Args[0], instr.Args[1])

	switch instr.Op {
	case ir.OpAdd:
		cg.emit(encoder.EncADD_RR(a, b, encoder.W64))
	case ir.OpSub:
		cg.emit(encoder.EncSUB_RR(a, b, encoder.W64))
	case ir.OpAnd:
		cg.emit(encoder.EncAND_RR(a, b, encoder.W64))
	case ir.OpOr:
		cg.emit(encoder.EncOR_RR(a, b, encoder.W64))
	case ir.OpXor:
		cg.emit(encoder.EncXOR_RR(a, b, encoder.W64))
	}
	cg.storeResult(instr.Result, a)
}

func (cg *codeGen) compileCmp(instr *ir.Instruction) {
	if len(instr.Args) < 2 {
		return
	}
	a, b := cg.stageBinaryArgs(instr.Args[0], instr.Args[1])
	cg.emit(encoder.EncCMP_RR(a, b, encoder.W64))

	// Set result byte via SETcc
	var setOp byte
	switch instr.CmpOp {
	case ir.CmpEQ:
		setOp = 0x94 // SETE
	case ir.CmpNE:
		setOp = 0x95 // SETNE
	case ir.CmpLT:
		setOp = 0x9C // SETL
	case ir.CmpLE:
		setOp = 0x9E // SETLE
	case ir.CmpGT:
		setOp = 0x9F // SETG
	case ir.CmpGE:
		setOp = 0x9D // SETGE
	case ir.CmpULT:
		setOp = 0x92 // SETB
	case ir.CmpUGT:
		setOp = 0x97 // SETA
	default:
		setOp = 0x94
	}
	// SETCC AL
	cg.emit([]byte{0x0F, setOp, 0xC0}) // ModRM: 11 000 000 = AL
	// MOVZX RAX, AL
	cg.emit(encoder.EncMOVZX(encoder.RAX, encoder.RAX, encoder.W8, encoder.W32))

	if instr.Result != nil {
		cg.storeResult(instr.Result, encoder.RAX)
	}
}

func (cg *codeGen) compileCall(instr *ir.Instruction) {
	// System V AMD64 ABI: args in RDI, RSI, RDX, RCX, R8, R9
	argRegs := []encoder.Reg{encoder.RDI, encoder.RSI, encoder.RDX, encoder.RCX, encoder.R8, encoder.R9}

	nArgs := len(instr.Args)
	if nArgs > len(argRegs) {
		nArgs = len(argRegs)
	}

	// Phase 1: Evaluate all args into R11 (scratch) then push onto stack.
	// Using SUB/MOV instead of PUSH to avoid potential stack alignment issues.
	if nArgs > 0 {
		// Reserve stack space for staging args
		stagingSize := int32(nArgs * 8)
		stagingSize = (stagingSize + 15) &^ 15 // 16-byte align
		cg.emit(encoder.EncSUB_RI(encoder.RSP, stagingSize, encoder.W64))

		for i := 0; i < nArgs; i++ {
			src := cg.regFor(instr.Args[i])
			// Save to staging area: [RSP + i*8]
			if src != encoder.R11 {
				cg.emit(encoder.EncMOV_RR(encoder.R11, src, encoder.W64))
			}
			mem := encoder.MemOp{Base: encoder.RSP, Index: encoder.NoReg, Disp: int32(i * 8), Width: encoder.W64}
			cg.emit(encoder.EncMOV_MR(mem, encoder.R11))
		}

		// Phase 2: Load from staging area into ABI registers.
		for i := 0; i < nArgs; i++ {
			mem := encoder.MemOp{Base: encoder.RSP, Index: encoder.NoReg, Disp: int32(i * 8), Width: encoder.W64}
			cg.emit(encoder.EncMOV_RM(argRegs[i], mem))
		}

		// Release staging area
		cg.emit(encoder.EncADD_RI(encoder.RSP, stagingSize, encoder.W64))
	}

	if instr.Callee != nil {
		// Emit CALL with a placeholder offset -- add relocation
		relocOff := cg.offset() + 1 // offset of the 4-byte relative offset
		cg.emit(encoder.EncCALL_REL32(0))
		cg.relocs = append(cg.relocs, codegen.Reloc{
			Offset: relocOff,
			Symbol: instr.Callee.Name,
			Addend: -4, // standard PC32: S + A - P, A = -4 to account for 4-byte field
			Type:   codegen.RelocPC32,
		})
	}

	if instr.Result != nil {
		// Return value in RAX
		cg.storeResult(instr.Result, encoder.RAX)
	}
}

func (cg *codeGen) compileTerm(term *ir.Terminator) {
	switch term.Kind {
	case ir.TermRet:
		// Move return value to RAX
		if term.RetVal != nil {
			src := cg.regFor(term.RetVal)
			if src != encoder.RAX {
				cg.emit(encoder.EncMOV_RR(encoder.RAX, src, encoder.W64))
			}
		}
		cg.emitEpilogue()
		cg.emit(encoder.EncRET())

	case ir.TermRetVoid:
		cg.emitEpilogue()
		cg.emit(encoder.EncRET())

	case ir.TermBr:
		if term.Then != nil {
			off := cg.offset()
			cg.emit(encoder.EncJMP_REL32(0)) // placeholder
			instrEnd := cg.offset()
			cg.fwdRefs = append(cg.fwdRefs, fwdRef{
				codeOffset: off + 1,
				target:     term.Then.Label,
				instrEnd:   instrEnd,
			})
		}

	case ir.TermCondBr:
		cond := cg.regFor(term.Cond)
		// TEST cond, cond
		cg.emit(encoder.EncCMP_RI(cond, 0, encoder.W64))
		// JNE to Then block
		thenOff := cg.offset()
		cg.emit(encoder.EncJcc_REL32(encoder.CondNE, 0)) // placeholder
		thenEnd := cg.offset()
		cg.fwdRefs = append(cg.fwdRefs, fwdRef{
			codeOffset: thenOff + 2,
			target:     term.Then.Label,
			instrEnd:   thenEnd,
		})
		// JMP to Else block
		if term.Else != nil {
			elseOff := cg.offset()
			cg.emit(encoder.EncJMP_REL32(0))
			elseEnd := cg.offset()
			cg.fwdRefs = append(cg.fwdRefs, fwdRef{
				codeOffset: elseOff + 1,
				target:     term.Else.Label,
				instrEnd:   elseEnd,
			})
		}

	case ir.TermUnreachable:
		// UD2 instruction
		cg.emit([]byte{0x0F, 0x0B})
	}
}

func (cg *codeGen) emitEpilogue() {
	// Restore RSP from RBP — always correct regardless of stack size
	cg.emit(encoder.EncMOV_RR(encoder.RSP, encoder.RBP, encoder.W64))
	cg.emit(encoder.EncPOP(encoder.RBP))
}
