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
	// Map basic block label → code offset (for branch patching)
	blockOffsets map[string]int
	// Pending forward references: [(code offset of imm32, target block label)]
	fwdRefs []fwdRef
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

func (cg *codeGen) compile() ([]byte, []codegen.Reloc, error) {
	cg.blockOffsets = make(map[string]int)
	cg.fwdRefs = nil

	// Function prologue
	cg.emit(encoder.EncPUSH(encoder.RBP))
	cg.emit(encoder.EncMOV_RR(encoder.RBP, encoder.RSP, encoder.W64))
	if cg.alloc.StackSize > 0 {
		// Align stack to 16 bytes
		stackSize := (cg.alloc.StackSize + 15) &^ 15
		cg.emit(encoder.EncSUB_RI(encoder.RSP, int32(stackSize), encoder.W64))
	}

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
	if v.Kind == ir.ValConst {
		// Load constant into RAX
		cg.emit(encoder.EncMOV_RI(encoder.RAX, v.IntVal, widthFor(v.Type)))
		return encoder.RAX
	}
	if reg, ok := cg.alloc.Reg[v.ID]; ok {
		return reg
	}
	// Spilled: load from stack
	off := cg.alloc.Spill[v.ID]
	mem := encoder.MemOp{Base: encoder.RBP, Disp: int32(off), Width: widthFor(v.Type)}
	cg.emit(encoder.EncMOV_RM(encoder.RAX, mem))
	return encoder.RAX
}

func (cg *codeGen) storeResult(result *ir.Value, reg encoder.Reg) {
	if result == nil {
		return
	}
	if dstReg, ok := cg.alloc.Reg[result.ID]; ok {
		if dstReg != reg {
			cg.emit(encoder.EncMOV_RR(dstReg, reg, encoder.W64))
		}
		return
	}
	if off, ok := cg.alloc.Spill[result.ID]; ok {
		mem := encoder.MemOp{Base: encoder.RBP, Disp: int32(off), Width: widthFor(result.Type)}
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

func (cg *codeGen) compileInstr(instr *ir.Instruction) {
	switch instr.Op {
	case ir.OpAdd, ir.OpSub, ir.OpAnd, ir.OpOr, ir.OpXor:
		cg.compileALU(instr)

	case ir.OpMul:
		if len(instr.Args) == 2 {
			a := cg.regFor(instr.Args[0])
			b := cg.regFor(instr.Args[1])
			cg.emit(encoder.EncIMUL_RR(a, b, encoder.W64))
			cg.storeResult(instr.Result, a)
		}

	case ir.OpCmp:
		cg.compileCmp(instr)

	case ir.OpAlloca:
		// Already handled in prologue via StackSize — just record offset
		// The result value will be in alloc.Spill

	case ir.OpLoad:
		if len(instr.Args) == 1 {
			src := cg.regFor(instr.Args[0])
			mem := encoder.MemOp{Base: src, Disp: 0, Width: widthFor(instr.Result.Type)}
			cg.emit(encoder.EncMOV_RM(encoder.RAX, mem))
			cg.storeResult(instr.Result, encoder.RAX)
		}

	case ir.OpStore:
		if len(instr.Args) == 2 {
			val := cg.regFor(instr.Args[0])
			ptr := cg.regFor(instr.Args[1])
			mem := encoder.MemOp{Base: ptr, Disp: 0}
			cg.emit(encoder.EncMOV_MR(mem, val))
		}

	case ir.OpCall:
		cg.compileCall(instr)

	case ir.OpDiv:
		if len(instr.Args) >= 2 {
			// Move dividend to RAX
			dividend := cg.regFor(instr.Args[0])
			if dividend != encoder.RAX {
				cg.emit(encoder.EncMOV_RR(encoder.RAX, dividend, encoder.W64))
			}
			// Sign-extend RAX into RDX:RAX (CQO)
			cg.emit([]byte{0x48, 0x99})
			// Move divisor to a register (not RAX or RDX)
			divisor := cg.regFor(instr.Args[1])
			if divisor == encoder.RAX || divisor == encoder.RDX {
				cg.emit(encoder.EncMOV_RR(encoder.RCX, divisor, encoder.W64))
				divisor = encoder.RCX
			}
			// IDIV divisor: RAX=quotient, RDX=remainder
			// Encoding: REX.W + F7 /7 modrm(11, 7, rm)
			cg.emit([]byte{
				0x48 | (boolToByte(divisor.IsExtended()) & 0x01),
				0xF7,
				0xF8 | divisor.Low3(), // modrm(11, 7, rm) = 11_111_rm
			})
			cg.storeResult(instr.Result, encoder.RAX)
		}

	case ir.OpRem:
		if len(instr.Args) >= 2 {
			// Move dividend to RAX
			dividend := cg.regFor(instr.Args[0])
			if dividend != encoder.RAX {
				cg.emit(encoder.EncMOV_RR(encoder.RAX, dividend, encoder.W64))
			}
			// Sign-extend RAX into RDX:RAX (CQO)
			cg.emit([]byte{0x48, 0x99})
			// Move divisor to a register (not RAX or RDX)
			divisor := cg.regFor(instr.Args[1])
			if divisor == encoder.RAX || divisor == encoder.RDX {
				cg.emit(encoder.EncMOV_RR(encoder.RCX, divisor, encoder.W64))
				divisor = encoder.RCX
			}
			// IDIV divisor: RAX=quotient, RDX=remainder
			cg.emit([]byte{
				0x48 | (boolToByte(divisor.IsExtended()) & 0x01),
				0xF7,
				0xF8 | divisor.Low3(),
			})
			// Result is remainder in RDX
			cg.storeResult(instr.Result, encoder.RDX)
		}

	case ir.OpShl:
		if len(instr.Args) >= 2 {
			val := cg.regFor(instr.Args[0])
			shift := cg.regFor(instr.Args[1])
			if shift != encoder.RCX {
				cg.emit(encoder.EncMOV_RR(encoder.RCX, shift, encoder.W64))
			}
			cg.emit(encoder.EncSHL_RCL(val, encoder.W64))
			cg.storeResult(instr.Result, val)
		}

	case ir.OpShr:
		if len(instr.Args) >= 2 {
			val := cg.regFor(instr.Args[0])
			shift := cg.regFor(instr.Args[1])
			if shift != encoder.RCX {
				cg.emit(encoder.EncMOV_RR(encoder.RCX, shift, encoder.W64))
			}
			cg.emit(encoder.EncSHR_RCL(val, encoder.W64))
			cg.storeResult(instr.Result, val)
		}

	case ir.OpGEP:
		if len(instr.Args) >= 2 {
			base := cg.regFor(instr.Args[0])
			index := cg.regFor(instr.Args[1])
			// Simple case: base + index * 8 (pointer-sized elements)
			if index != encoder.RCX {
				// LEA result, [base + index*8] using SIB
				mem := encoder.MemOp{Base: base, Index: index, Scale: 8, Disp: 0, Width: encoder.W64}
				cg.emit(encoder.EncLEA(encoder.RAX, mem))
				cg.storeResult(instr.Result, encoder.RAX)
			} else {
				// RCX can still be used as SIB index, but use ADD as fallback
				result := base
				if base == encoder.RAX {
					result = encoder.R11
					cg.emit(encoder.EncMOV_RR(result, base, encoder.W64))
				}
				cg.emit(encoder.EncADD_RR(result, index, encoder.W64))
				cg.storeResult(instr.Result, result)
			}
		}

	case ir.OpExtractField:
		if len(instr.Args) >= 1 {
			base := cg.regFor(instr.Args[0])
			offset := int32(instr.FieldIdx * 8) // 8-byte aligned fields
			mem := encoder.MemOp{Base: base, Disp: offset, Width: encoder.W64}
			cg.emit(encoder.EncMOV_RM(encoder.RAX, mem))
			cg.storeResult(instr.Result, encoder.RAX)
		}

	case ir.OpInsertField:
		// Store value to struct field
		if len(instr.Args) >= 2 {
			base := cg.regFor(instr.Args[0])
			val := cg.regFor(instr.Args[1])
			offset := int32(instr.FieldIdx * 8)
			mem := encoder.MemOp{Base: base, Disp: offset, Width: encoder.W64}
			cg.emit(encoder.EncMOV_MR(mem, val))
		}

	case ir.OpBitcast, ir.OpExtend, ir.OpTruncate:
		// Simplified: just move
		if len(instr.Args) == 1 && instr.Result != nil {
			reg := cg.regFor(instr.Args[0])
			cg.storeResult(instr.Result, reg)
		}
	}
}

func (cg *codeGen) compileALU(instr *ir.Instruction) {
	if len(instr.Args) < 2 {
		return
	}
	a := cg.regFor(instr.Args[0])
	b := cg.regFor(instr.Args[1])

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
	a := cg.regFor(instr.Args[0])
	b := cg.regFor(instr.Args[1])
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
	for i, arg := range instr.Args {
		if i >= len(argRegs) {
			break // TODO: stack args
		}
		src := cg.regFor(arg)
		if src != argRegs[i] {
			cg.emit(encoder.EncMOV_RR(argRegs[i], src, encoder.W64))
		}
	}

	if instr.Callee != nil {
		// Emit CALL with a placeholder offset — add relocation
		relocOff := cg.offset() + 1 // offset of the 4-byte relative offset
		cg.emit(encoder.EncCALL_REL32(0))
		instrEnd := cg.offset()
		cg.relocs = append(cg.relocs, codegen.Reloc{
			Offset: relocOff,
			Symbol: instr.Callee.Name,
			Addend: int64(-instrEnd), // rel = sym_addr - (call_end)
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
	if cg.alloc.StackSize > 0 {
		stackSize := (cg.alloc.StackSize + 15) &^ 15
		cg.emit(encoder.EncADD_RI(encoder.RSP, int32(stackSize), encoder.W64))
	}
	cg.emit(encoder.EncPOP(encoder.RBP))
}
