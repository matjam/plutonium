// Ported from internal/arch/amd64/interrupt_test.go at the go-cutover tag.

package amd64

import (
	"pluto/bytes"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/pu"
)

// TestInterruptFrameAlignment checks the stack arithmetic of interrupt
// handlers. The CPU aligns RSP to 16 and pushes five words (six with an
// error code), so RSP is 8 or 0 modulo 16 on entry; after the pushes and the
// frame it must be 16-aligned for calls. The handler's parameters address
// the CPU's frame directly above the saved registers, without a return
// address.
func TestInterruptFrameAlignment(t *testing.T) {
	for _, errCode := range []bool{false, true} {
		for nSaved := 0; nSaved <= 15; nSaved++ {
			for _, slotSize := range []int64{0, 8, 16, 24} {
				f := newMFunc("handler")
				f.interrupt = true
				f.errCode = errCode
				for i := range nSaved {
					f.usedSaved = compat.AppendPlain(f.usedSaved, Reg(i))
				}
				if slotSize > 0 {
					f.newSlot("local", slotSize, 8)
				}
				lea := newInst(MInst{Op: MLEA, Size: 8, Dst: RegOp(RDI), Src: MemOp(Mem{Base: NoReg, Index: NoReg, ArgSlot: true, ArgOff: 8})})
				code := newInst(MInst{Op: MMOV, Size: 8, Dst: RegOp(RSI), Src: MemOp(Mem{Base: NoReg, Index: NoReg, ArgSlot: true})})
				b := pu.New(MBlock{})
				b.Insts = insts(lea, code, newInst(MInst{Op: MCALL, Sym: pu.New(ssa.Sym{Name: "f"})}))
				f.Blocks = compat.Append(f.Blocks, b)
				layoutFrame(f)

				pushed := int64(40) // SS, RSP, RFLAGS, CS, RIP
				if errCode {
					pushed += 8
				}
				// RSP at a call, relative to the 16-aligned RSP before the
				// CPU's pushes.
				rsp := -pushed - 8*int64(nSaved) - f.frameSize
				if rsp%16 != 0 {
					t.Errorf("errCode=%v saved=%d slot=%d: frame %d leaves RSP misaligned by %d", errCode, nSaved, slotSize, f.frameSize, -rsp%16)
				}
				if f.frameSize < slotSize {
					t.Errorf("frame %d smaller than its slot %d", f.frameSize, slotSize)
				}
				// The error code is the last word pushed; the frame follows.
				wantCode := f.frameSize + 8*int64(nSaved)
				if code.Src.Mem.Base != RSP || code.Src.Mem.Disp != wantCode {
					t.Errorf("error code at %s, want %d(%%rsp)", code.Src.Mem.String(), wantCode)
				}
				if lea.Src.Mem.Base != RSP || lea.Src.Mem.Disp != wantCode+8 {
					t.Errorf("frame at %s, want %d(%%rsp)", lea.Src.Mem.String(), wantCode+8)
				}
			}
		}
	}
}

// listing returns the assembly text of each instruction, joined with "; ".
func listing(is []*MInst) string {
	var parts []string
	for _, in := range is {
		parts = compat.Append(parts, in.String())
	}
	return compat.Join(parts, "; ")
}

// TestInterruptFramePointer checks parameter addressing in a handler that
// keeps RBP as a frame pointer (an over-aligned local): RBP+8 is the first
// word the CPU pushed.
func TestInterruptFramePointer(t *testing.T) {
	f := newMFunc("handler")
	f.interrupt = true
	f.errCode = true
	f.frameFP = true
	f.newSlot("aligned", 64, 64)
	code := newInst(MInst{Op: MMOV, Size: 8, Dst: RegOp(RSI), Src: MemOp(Mem{Base: NoReg, Index: NoReg, ArgSlot: true})})
	b := pu.New(MBlock{})
	b.Insts = insts(code)
	f.Blocks = compat.Append(f.Blocks, b)
	layoutFrame(f)
	if code.Src.Mem.Base != RBP || code.Src.Mem.Disp != 8 {
		t.Errorf("error code at %s, want 8(%%rbp)", code.Src.Mem.String())
	}
	ep := epilogue(f)
	if n := len(ep); n < 3 || ep[n-1].Op != MIRETQ || ep[n-2].Op != MADD || ep[n-3].Op != MPOP || ep[n-3].Dst.Reg != RBP {
		t.Errorf("epilogue %s: want pop %%rbp; add $8, %%rsp; iretq", listing(ep))
	}
}

// TestInterruptPrologueEpilogue checks the entry and exit sequences: pushes
// of every saved register, cld, XMM saves into the frame, and the mirror
// image ending in iretq, with the error code dropped first.
func TestInterruptPrologueEpilogue(t *testing.T) {
	f := newMFunc("handler")
	f.interrupt = true
	f.errCode = true
	f.usedSaved = regs(RAX, RCX, RBX)
	f.savedXMM = regs(X0, X9)
	f.Blocks = compat.Append(f.Blocks, pu.New(MBlock{}))
	layoutFrame(f)

	pro := listing(prologue(f))
	epi := listing(epilogue(f))
	xmm := f.xmmSave.Offset
	wantPro := compat.Join([]string{"push8 %rax", "push8 %rcx", "push8 %rbx", "sub8 $40, %rsp", "cld",
		"movups %xmm0, " + BaseMem(RSP, xmm).String(), "movups %xmm9, " + BaseMem(RSP, xmm+16).String()}, "; ")
	wantEpi := compat.Join([]string{"movups " + BaseMem(RSP, xmm).String() + ", %xmm0", "movups " + BaseMem(RSP, xmm+16).String() + ", %xmm9",
		"add8 $40, %rsp", "pop8 %rbx", "pop8 %rcx", "pop8 %rax", "add8 $8, %rsp", "iretq"}, "; ")
	if pro != wantPro {
		t.Errorf("prologue\n%s\nwant\n%s", pro, wantPro)
	}
	if epi != wantEpi {
		t.Errorf("epilogue\n%s\nwant\n%s", epi, wantEpi)
	}
}

// TestInterruptEncoding checks the bytes of the instructions only interrupt
// handlers use.
func TestInterruptEncoding(t *testing.T) {
	type encCase struct {
		in   *MInst
		want []byte
	}
	cases := []encCase{
		{newInst(MInst{Op: MIRETQ}), []byte{0x48, 0xCF}},
		{newInst(MInst{Op: MCLD}), []byte{0xFC}},
		{newInst(MInst{Op: MMOVUPS, Dst: MemOp(BaseMem(RSP, 16)), Src: RegOp(X1)}), []byte{0x0F, 0x11, 0x4C, 0x24, 0x10}},
		{newInst(MInst{Op: MMOVUPS, Dst: MemOp(BaseMem(RSP, 0x100)), Src: RegOp(X8)}), []byte{0x44, 0x0F, 0x11, 0x84, 0x24, 0x00, 0x01, 0x00, 0x00}},
		{newInst(MInst{Op: MMOVUPS, Dst: RegOp(X15), Src: MemOp(BaseMem(RSP, 32))}), []byte{0x44, 0x0F, 0x10, 0x7C, 0x24, 0x20}},
		{newInst(MInst{Op: MMOVUPS, Dst: RegOp(X0), Src: MemOp(BaseMem(RSP, 0))}), []byte{0x0F, 0x10, 0x04, 0x24}},
	}
	for _, c := range cases {
		var e encoder
		e.encode(c.in)
		if e.err != nil {
			t.Errorf("%s: %s", c.in.String(), errString(e.err))
			continue
		}
		if !bytes.Equal(e.code, c.want) {
			t.Errorf("%s: %s, want %s", c.in.String(), compat.HexEncode(e.code), compat.HexEncode(c.want))
		}
	}
}
