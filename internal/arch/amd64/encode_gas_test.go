package amd64

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/ssa"
)

// gasCase is one instruction, as MIR and as GNU assembler source.
type gasCase struct {
	in  *MInst
	asm string
}

// gasReloc is a relocation found in assembled code, relative to the start
// of its case.
type gasReloc struct {
	off    int
	typ    elf.R_X86_64
	sym    string
	addend int64
}

// assembleCases assembles every case with GNU as and returns each one's
// bytes and relocations. It skips the test when as is not installed.
func assembleCases(t *testing.T, cases []gasCase) ([][]byte, [][]gasReloc) {
	t.Helper()
	if _, err := exec.LookPath("as"); err != nil {
		t.Skip("GNU as not installed")
	}
	var src strings.Builder
	src.WriteString("\t.text\n")
	for i, c := range cases {
		fmt.Fprintf(&src, "c%d:\n\t%s\n", i, c.asm)
	}
	src.WriteString("cend:\n")
	dir := t.TempDir()
	s, o := filepath.Join(dir, "x.s"), filepath.Join(dir, "x.o")
	if err := os.WriteFile(s, []byte(src.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("as", "--64", "-o", o, s).CombinedOutput(); err != nil {
		t.Fatalf("as: %v\n%s\n%s", err, out, src.String())
	}
	f, err := elf.Open(o)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	text, err := f.Section(".text").Data()
	if err != nil {
		t.Fatal(err)
	}
	syms, err := f.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	starts := make([]int, len(cases)+1)
	for _, sym := range syms {
		var i int
		if sym.Name == "cend" {
			starts[len(cases)] = int(sym.Value)
		} else if _, err := fmt.Sscanf(sym.Name, "c%d", &i); err == nil && i < len(cases) {
			starts[i] = int(sym.Value)
		}
	}
	code := make([][]byte, len(cases))
	for i := range cases {
		code[i] = text[starts[i]:starts[i+1]]
	}
	relocs := make([][]gasReloc, len(cases))
	if rs := f.Section(".rela.text"); rs != nil {
		data, err := rs.Data()
		if err != nil {
			t.Fatal(err)
		}
		for j := 0; j+24 <= len(data); j += 24 {
			off := int(binary.LittleEndian.Uint64(data[j:]))
			info := binary.LittleEndian.Uint64(data[j+8:])
			addend := int64(binary.LittleEndian.Uint64(data[j+16:]))
			i := sort.Search(len(cases), func(k int) bool { return starts[k+1] > off })
			relocs[i] = append(relocs[i], gasReloc{off - starts[i], elf.R_X86_64(info & 0xffffffff), syms[info>>32-1].Name, addend})
		}
	}
	return code, relocs
}

// checkAgainstGAS encodes each case and compares it with GNU as.
func checkAgainstGAS(t *testing.T, cases []gasCase) {
	t.Helper()
	want, wantRelocs := assembleCases(t, cases)
	for i, c := range cases {
		e := &encoder{}
		e.encode(c.in)
		if e.err != nil {
			t.Errorf("%s: %v", c.asm, e.err)
			continue
		}
		if !bytes.Equal(e.code, want[i]) {
			t.Errorf("%s: encoded % x, as gives % x", c.asm, e.code, want[i])
		}
		var got []gasReloc
		for _, r := range e.relocs {
			typ := elf.R_X86_64_PC32
			switch r.Type {
			case RelAbs32S:
				typ = elf.R_X86_64_32S
			case Rel64:
				typ = elf.R_X86_64_64
			case RelPLT32:
				typ = elf.R_X86_64_PLT32
			}
			got = append(got, gasReloc{r.Off, typ, r.Sym.Name, r.Addend})
		}
		if fmt.Sprint(got) != fmt.Sprint(wantRelocs[i]) {
			t.Errorf("%s: relocations %v, as gives %v", c.asm, got, wantRelocs[i])
		}
	}
}

// TestEncodeSystemInstructions checks the encoding of every instruction the
// pluto/arch/x86 intrinsics use against GNU as.
func TestEncodeSystemInstructions(t *testing.T) {
	mem := func(base Reg, disp int64) Operand { return MemOp(BaseMem(base, disp)) }
	op := func(o MOp) *MInst { return &MInst{Op: o} }
	cases := []gasCase{
		{op(MCLI), "cli"},
		{op(MSTI), "sti"},
		{op(MSTIHLT), "sti; hlt"},
		{op(MHLT), "hlt"},
		{op(MPAUSE), "pause"},
		{op(MSWAPGS), "swapgs"},
		{op(MCLTS), "clts"},
		{op(MRDMSR), "rdmsr"},
		{op(MWRMSR), "wrmsr"},
		{op(MCPUID), "cpuid"},
		{op(MRDTSC), "rdtsc"},
		{op(MRDTSCP), "rdtscp"},
		{&MInst{Op: MSAVEFLAGS, Dst: RegOp(RAX)}, "pushfq; pop %rax"},
		{&MInst{Op: MSAVEFLAGS, Dst: RegOp(R12)}, "pushfq; pop %r12"},
		{&MInst{Op: MRESTOREFLAGS, Src: RegOp(RDI)}, "push %rdi; popfq"},
		{&MInst{Op: MRESTOREFLAGS, Src: RegOp(R9)}, "push %r9; popfq"},
		{&MInst{Op: MIN, Size: 1}, "inb %dx, %al"},
		{&MInst{Op: MIN, Size: 2}, "inw %dx, %ax"},
		{&MInst{Op: MIN, Size: 4}, "inl %dx, %eax"},
		{&MInst{Op: MIN, Size: 1, Src: ImmOp(0x80)}, "inb $0x80, %al"},
		{&MInst{Op: MIN, Size: 2, Src: ImmOp(0x60)}, "inw $0x60, %ax"},
		{&MInst{Op: MIN, Size: 4, Src: ImmOp(0xCF)}, "inl $0xcf, %eax"},
		{&MInst{Op: MOUT, Size: 1}, "outb %al, %dx"},
		{&MInst{Op: MOUT, Size: 2}, "outw %ax, %dx"},
		{&MInst{Op: MOUT, Size: 4}, "outl %eax, %dx"},
		{&MInst{Op: MOUT, Size: 1, Src: ImmOp(0x80)}, "outb %al, $0x80"},
		{&MInst{Op: MOUT, Size: 2, Src: ImmOp(0x70)}, "outw %ax, $0x70"},
		{&MInst{Op: MOUT, Size: 4, Src: ImmOp(0xF4)}, "outl %eax, $0xf4"},
		{&MInst{Op: MRDCR, Imm: 0, Dst: RegOp(RAX)}, "mov %cr0, %rax"},
		{&MInst{Op: MRDCR, Imm: 2, Dst: RegOp(RBX)}, "mov %cr2, %rbx"},
		{&MInst{Op: MRDCR, Imm: 3, Dst: RegOp(R8)}, "mov %cr3, %r8"},
		{&MInst{Op: MRDCR, Imm: 4, Dst: RegOp(R15)}, "mov %cr4, %r15"},
		{&MInst{Op: MWRCR, Imm: 0, Src: RegOp(RDI)}, "mov %rdi, %cr0"},
		{&MInst{Op: MWRCR, Imm: 3, Src: RegOp(RAX)}, "mov %rax, %cr3"},
		{&MInst{Op: MWRCR, Imm: 4, Src: RegOp(R11)}, "mov %r11, %cr4"},
		{&MInst{Op: MINVLPG, Src: mem(RDI, 0)}, "invlpg (%rdi)"},
		{&MInst{Op: MINVLPG, Src: mem(R12, 0)}, "invlpg (%r12)"},
		{&MInst{Op: MINVLPG, Src: mem(RSP, 8)}, "invlpg 8(%rsp)"},
		{&MInst{Op: MLGDT, Src: mem(RAX, 0)}, "lgdt (%rax)"},
		{&MInst{Op: MLGDT, Src: mem(R13, 0)}, "lgdt (%r13)"},
		{&MInst{Op: MLIDT, Src: mem(RSP, 16)}, "lidt 16(%rsp)"},
		{&MInst{Op: MLIDT, Src: mem(RBP, 0x200)}, "lidt 0x200(%rbp)"},
		{&MInst{Op: MLTR, Size: 2, Src: RegOp(RDI)}, "ltr %di"},
		{&MInst{Op: MLTR, Size: 2, Src: RegOp(R10)}, "ltr %r10w"},
		{&MInst{Op: MFXSAVE, Src: mem(RDI, 0)}, "fxsave64 (%rdi)"},
		{&MInst{Op: MFXSAVE, Src: mem(R12, 32)}, "fxsave64 32(%r12)"},
		{&MInst{Op: MFXRSTOR, Src: mem(RSP, 0)}, "fxrstor64 (%rsp)"},
		{&MInst{Op: MFXRSTOR, Src: mem(R9, 0)}, "fxrstor64 (%r9)"},
	}
	checkAgainstGAS(t, cases)
}

// TestEncodeGS checks GS-relative operands, used by the GS intrinsics and
// per-CPU variables, against GNU as, relocations included.
func TestEncodeGS(t *testing.T) {
	sym := &ssa.Sym{Name: "pcpu"}
	gs := func(m Mem) Operand {
		m.Seg = SegGS
		return MemOp(m)
	}
	abs := func(disp int64) Mem { return Mem{Base: NoReg, Index: NoReg, Disp: disp} }
	symMem := func(disp int64, index Reg, scale uint8) Mem {
		return Mem{Base: NoReg, Index: index, Scale: scale, Sym: sym, Disp: disp}
	}
	cases := []gasCase{
		{&MInst{Op: MMOV, Size: 8, Dst: RegOp(RAX), Src: gs(BaseMem(RDI, 0))}, "mov %gs:(%rdi), %rax"},
		{&MInst{Op: MMOV, Size: 4, Dst: RegOp(RAX), Src: gs(BaseMem(RSI, 8))}, "mov %gs:8(%rsi), %eax"},
		{&MInst{Op: MMOV, Size: 8, Dst: gs(BaseMem(RDI, 0)), Src: RegOp(RSI)}, "mov %rsi, %gs:(%rdi)"},
		{&MInst{Op: MMOV, Size: 4, Dst: gs(BaseMem(R12, 0)), Src: RegOp(R9)}, "mov %r9d, %gs:(%r12)"},
		{&MInst{Op: MMOV, Size: 8, Dst: RegOp(RAX), Src: gs(abs(0))}, "mov %gs:0, %rax"},
		{&MInst{Op: MMOV, Size: 8, Dst: RegOp(R10), Src: gs(abs(0x40))}, "mov %gs:0x40, %r10"},
		{&MInst{Op: MMOV, Size: 8, Dst: gs(abs(16)), Src: ImmOp(5)}, "movq $5, %gs:16"},
		{&MInst{Op: MMOV, Size: 8, Dst: RegOp(RAX), Src: gs(symMem(0, NoReg, 0))}, "mov %gs:pcpu, %rax"},
		{&MInst{Op: MMOV, Size: 4, Dst: RegOp(R11), Src: gs(symMem(4, NoReg, 0))}, "mov %gs:pcpu+4, %r11d"},
		{&MInst{Op: MMOV, Size: 8, Dst: gs(symMem(8, NoReg, 0)), Src: RegOp(R9)}, "mov %r9, %gs:pcpu+8"},
		{&MInst{Op: MMOV, Size: 1, Dst: gs(symMem(0, NoReg, 0)), Src: ImmOp(1)}, "movb $1, %gs:pcpu"},
		{&MInst{Op: MMOV, Size: 8, Dst: RegOp(RDX), Src: gs(symMem(0, RCX, 8))}, "mov %gs:pcpu(,%rcx,8), %rdx"},
		{&MInst{Op: MADD, Size: 8, Dst: RegOp(RAX), Src: gs(symMem(0, NoReg, 0))}, "add %gs:pcpu, %rax"},
		{&MInst{Op: MMOVZX, Size: 4, SrcSize: 1, Dst: RegOp(RAX), Src: gs(symMem(0, NoReg, 0))}, "movzbl %gs:pcpu, %eax"},
		{&MInst{Op: MLEA, Size: 8, Dst: RegOp(RAX), Src: MemOp(Mem{Base: RAX, Index: NoReg, Sym: sym})}, "lea pcpu(%rax), %rax"},
		{&MInst{Op: MLGDT, Src: gs(symMem(0, NoReg, 0))}, "lgdt %gs:pcpu"},
		{&MInst{Op: MLEA, Size: 8, Dst: RegOp(RCX), Src: MemOp(Mem{Base: NoReg, Index: NoReg, Sym: sym, Abs: true})}, "lea pcpu, %rcx"},
	}
	checkAgainstGAS(t, cases)
}
