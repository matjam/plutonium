package amd64

import (
	"bytes"
	"testing"

	"github.com/matjam/plutonium/internal/ssa"
)

// TestBranchRelaxation checks that near branches get the 2-byte forms, far
// ones keep rel32, and code and relocations after a shortened branch move
// with it.
func TestBranchRelaxation(t *testing.T) {
	b0 := &MBlock{ID: 0}
	b1 := &MBlock{ID: 1}
	b2 := &MBlock{ID: 2}
	b3 := &MBlock{ID: 3}
	sym := &ssa.Sym{Name: "callee"}
	// b0: jl b2 (near); jmp b3 (far, past 64 ud2s)
	b0.Insts = []*MInst{
		{Op: MJCC, Cond: CondL, Target: b2},
		{Op: MJMP, Target: b3},
	}
	// b1: call callee; jmp b0 (backwards, near)
	b1.Insts = []*MInst{
		{Op: MCALL, Sym: sym},
		{Op: MJMP, Target: b0},
	}
	b2.Insts = []*MInst{{Op: MRET}}
	for range 64 {
		b2.Insts = append(b2.Insts, &MInst{Op: MUD2})
	}
	b3.Insts = []*MInst{{Op: MRET}}

	code, err := encodeFunc(&MFunc{}, []*MBlock{b0, b1, b2, b3})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x7C, 0x0C, // jl b2 (+12)
		0xE9, 0x88, 0x00, 0x00, 0x00, // jmp b3 (+136)
		0xE8, 0, 0, 0, 0, // call callee
		0xEB, 0xF2, // jmp b0 (-14)
		0xC3, // b2: ret
	}
	if !bytes.Equal(code.Bytes[:len(want)], want) {
		t.Errorf("code % x\nwant % x", code.Bytes[:len(want)], want)
	}
	if got := len(code.Bytes); got != len(want)+64*2+1 {
		t.Errorf("length %d, want %d", got, len(want)+64*2+1)
	}
	if len(code.Relocs) != 1 || code.Relocs[0].Off != 8 {
		t.Errorf("relocs %+v, want one at offset 8", code.Relocs)
	}
}

// TestPackedEncoding checks the SSE2 packed double encodings against GNU
// as, and their listing names.
func TestPackedEncoding(t *testing.T) {
	x := func(n int) Operand { return RegOp(X0 + Reg(n)) }
	tests := []struct {
		in   MInst
		want []byte
		asm  string
	}{
		{MInst{Op: MADDSF, Size: 16, Dst: x(0), Src: x(1)}, []byte{0x66, 0x0f, 0x58, 0xc1}, "addpd %xmm1, %xmm0"},
		{MInst{Op: MSUBSF, Size: 16, Dst: x(2), Src: x(9)}, []byte{0x66, 0x41, 0x0f, 0x5c, 0xd1}, "subpd %xmm9, %xmm2"},
		{MInst{Op: MMULSF, Size: 16, Dst: x(8), Src: x(1)}, []byte{0x66, 0x44, 0x0f, 0x59, 0xc1}, "mulpd %xmm1, %xmm8"},
		{MInst{Op: MDIVSF, Size: 16, Dst: x(4), Src: x(3)}, []byte{0x66, 0x0f, 0x5e, 0xe3}, "divpd %xmm3, %xmm4"},
		{MInst{Op: MSQRTSF, Size: 16, Dst: x(2), Src: x(1)}, []byte{0x66, 0x0f, 0x51, 0xd1}, "sqrtpd %xmm1, %xmm2"},
		{MInst{Op: MMOVSF, Size: 16, Dst: x(1), Src: MemOp(BaseMem(RAX, 0))}, []byte{0x66, 0x0f, 0x10, 0x08}, "movupd (%rax), %xmm1"},
		{MInst{Op: MMOVSF, Size: 16, Dst: MemOp(BaseMem(RAX, 0)), Src: x(1)}, []byte{0x66, 0x0f, 0x11, 0x08}, "movupd %xmm1, (%rax)"},
		{MInst{Op: MMOVSF, Size: 16, Dst: MemOp(BaseMem(R12, 16)), Src: x(10)}, []byte{0x66, 0x45, 0x0f, 0x11, 0x54, 0x24, 0x10}, "movupd %xmm10, 16(%r12)"},
		{MInst{Op: MUNPCKLPD, Size: 16, Dst: x(1), Src: x(2)}, []byte{0x66, 0x0f, 0x14, 0xca}, "unpcklpd %xmm2, %xmm1"},
		{MInst{Op: MPSHUFD, Size: 16, Dst: x(3), Src: x(9), Imm: 0xEE}, []byte{0x66, 0x41, 0x0f, 0x70, 0xd9, 0xee}, "pshufd $0xee, %xmm9, %xmm3"},
		{MInst{Op: MMOVHPD, Size: 8, Dst: x(1), Src: MemOp(BaseMem(RAX, 8))}, []byte{0x66, 0x0f, 0x16, 0x48, 0x08}, "movhpd 8(%rax), %xmm1"},
		{MInst{Op: MMOVHPD, Size: 8, Dst: MemOp(BaseMem(RAX, 8)), Src: x(1)}, []byte{0x66, 0x0f, 0x17, 0x48, 0x08}, "movhpd %xmm1, 8(%rax)"},
	}
	// Not packed, but new alongside: the bit test.
	tests = append(tests, struct {
		in   MInst
		want []byte
		asm  string
	}{MInst{Op: MBT, Size: 8, Dst: RegOp(RAX), Src: RegOp(R13)}, []byte{0x4c, 0x0f, 0xa3, 0xe8}, "bt8 %r13, %rax"})
	for _, tc := range tests {
		var e encoder
		e.encode(&tc.in)
		if e.err != nil {
			t.Errorf("%s: %v", tc.asm, e.err)
			continue
		}
		if !bytes.Equal(e.code, tc.want) {
			t.Errorf("%s: code % x, want % x", tc.asm, e.code, tc.want)
		}
		if got := tc.in.String(); got != tc.asm {
			t.Errorf("listing %q, want %q", got, tc.asm)
		}
	}
	// A movhpd load keeps the low lane, so it reads its destination as
	// well as writing it.
	ld := MInst{Op: MMOVHPD, Size: 8, Dst: x(1), Src: MemOp(BaseMem(RAX, 8))}
	if !ld.readsDst() {
		t.Error("movhpd load does not read its destination")
	}
}
