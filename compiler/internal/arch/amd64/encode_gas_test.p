// Ported from internal/arch/amd64/encode_gas_test.go at the go-cutover tag.
//
// The Go test read the object file with debug/elf; Plutonium has no such
// reader, so the little this test needs (the section, symbol and relocation
// tables of an ELF64 object) is decoded here.

package amd64

import (
	"pluto/bytes"
	"pluto/os"
	"pluto/os/exec"
	"pluto/testing"

	"compiler/compat"
	"compiler/elf"
	"compiler/internal/ssa"
	"compiler/pu"
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

func (r gasReloc) String() string {
	return compat.Sprintfb("{%d %d %s %d}", compat.ArgI(int64(r.off)), compat.ArgI(int64(r.typ)), compat.ArgS(r.sym), compat.ArgI(r.addend))
}

func relocsString(rs []gasReloc) string {
	var parts []string
	for _, r := range rs {
		parts = compat.Append(parts, r.String())
	}
	return compat.Concat("[", compat.Join(parts, " "), "]")
}

func sameRelocs(a, b []gasReloc) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func le16(b []byte, off int) int { return int(b[off]) | int(b[off+1])<<8 }
func le32(b []byte, off int) int { return le16(b, off) | le16(b, off+2)<<16 }
func le64(b []byte, off int) int64 {
	return int64(le32(b, off)) | int64(le32(b, off+4))<<32
}

// cstring returns the NUL-terminated string at off in b.
func cstring(b []byte, off int) string {
	end := off
	for b[end] != 0 {
		end++
	}
	return compat.String(b[off:end])
}

// elfSection is the part of an ELF64 section header the test reads.
type elfSection struct {
	name       string
	nameOff    int
	typ        int
	off, size  int
	link, info int
}

// elfSections decodes the section headers of an ELF64 object.
func elfSections(obj []byte) []elfSection {
	shoff := int(le64(obj, 0x28))
	shentsize := le16(obj, 0x3A)
	shnum := le16(obj, 0x3C)
	shstrndx := le16(obj, 0x3E)
	var out []elfSection
	for i := 0; i < shnum; i++ {
		h := shoff + i*shentsize
		out = compat.Append(out, elfSection{
			nameOff: le32(obj, h), typ: le32(obj, h+4),
			off: int(le64(obj, h+24)), size: int(le64(obj, h+32)),
			link: le32(obj, h+40), info: le32(obj, h+44),
		})
	}
	// The names are offsets into the section header string table.
	strtab := out[shstrndx].off
	for i := range out {
		out[i].name = cstring(obj, strtab+out[i].nameOff)
	}
	return out
}

// elfSym is a symbol table entry.
type elfSym struct {
	name  string
	value int
}

// elfSymbols decodes the symbol table in section s, with the names from its
// string table.
func elfSymbols(obj []byte, secs []elfSection, s elfSection) []elfSym {
	strtab := secs[s.link].off
	var out []elfSym
	for j := 0; j+24 <= s.size; j += 24 {
		e := s.off + j
		out = compat.Append(out, elfSym{name: cstring(obj, strtab+le32(obj, e)), value: int(le64(obj, e+8))})
	}
	return out
}

// removeFiles removes the named files, ignoring errors.
func removeFiles(names ...string) {
	for _, n := range names {
		_ = os.Remove(n)
	}
}

// assembleCases assembles every case with GNU as and returns each one's
// bytes and relocations. It skips the test when as is not installed.
func assembleCases(t *testing.T, cases []gasCase) ([][]byte, [][]gasReloc) {
	t.Helper()
	if _, err := exec.LookPath("as"); err != nil {
		t.Skip("GNU as not installed")
	}
	var src compat.Buffer
	_, _ = src.WriteString("\t.text\n")
	for i, c := range cases {
		_, _ = src.WriteString(compat.Sprintfb("c%d:\n\t%s\n", compat.ArgI(int64(i)), compat.ArgS(c.asm)))
	}
	_, _ = src.WriteString("cend:\n")
	tmp, err := CreateTempName("amd64-gas-*.s")
	if err != nil {
		t.Fatal(err)
	}
	s := tmp
	o := compat.Concat(tmp, ".o")
	defer removeFiles(s, o)
	if err := os.WriteFile(s, src.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("as", "--64", "-o", o, s)
	var out compat.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	cmd.Drop()
	if err != nil {
		t.Fatalf("as: %s\n%s\n%s", err.Error(), out.String(), src.String())
	}
	obj, err := compat.ReadFile(o)
	if err != nil {
		t.Fatal(err)
	}
	secs := elfSections(obj)
	var text elfSection
	var syms []elfSym
	var rela elfSection
	haveRela := false
	for _, sec := range secs {
		switch {
		case sec.name == ".text":
			text = sec
		case sec.typ == int(elf.SHT_SYMTAB):
			syms = elfSymbols(obj, secs, sec)
		case sec.name == ".rela.text":
			rela, haveRela = sec, true
		}
	}
	starts := compat.Make[int](len(cases) + 1)
	for _, sym := range syms {
		if sym.name == "cend" {
			starts[len(cases)] = sym.value
			continue
		}
		if len(sym.name) < 2 || sym.name[0] != 'c' {
			continue
		}
		i, ok := 0, true
		for k := 1; k < len(sym.name); k++ {
			d := sym.name[k]
			if d < '0' || d > '9' {
				ok = false
				break
			}
			i = i*10 + int(d-'0')
		}
		if ok && i < len(cases) {
			starts[i] = sym.value
		}
	}
	data := obj[text.off : text.off+text.size]
	code := compat.Make[[]byte](len(cases))
	for i := range cases {
		code[i] = data[starts[i]:starts[i+1]]
	}
	relocs := compat.Make[[]gasReloc](len(cases))
	if haveRela {
		for j := 0; j+24 <= rela.size; j += 24 {
			e := rela.off + j
			off := int(le64(obj, e))
			info := le64(obj, e+8)
			addend := le64(obj, e+16)
			i := 0
			for i < len(cases) && starts[i+1] <= off {
				i++
			}
			relocs[i] = compat.Append(relocs[i], gasReloc{off - starts[i], elf.R_X86_64(info & 0xffffffff), syms[int(info>>32)].name, addend})
		}
	}
	return code, relocs
}

// CreateTempName makes a new empty file in the temporary directory from
// pattern and returns its name.
func CreateTempName(pattern string) (string, error) {
	f, err := compat.CreateTemp(compat.TempDir(), pattern)
	if err != nil {
		return "", err
	}
	name := compat.Concat(f.Name())
	_ = f.Close()
	return name, nil
}

// checkAgainstGAS encodes each case and compares it with GNU as.
func checkAgainstGAS(t *testing.T, cases []gasCase) {
	t.Helper()
	want, wantRelocs := assembleCases(t, cases)
	for i, c := range cases {
		var e encoder
		e.encode(c.in)
		if e.err != nil {
			t.Errorf("%s: %s", c.asm, errString(e.err))
			continue
		}
		if !bytes.Equal(e.code, want[i]) {
			t.Errorf("%s: encoded %s, as gives %s", c.asm, compat.HexEncode(e.code), compat.HexEncode(want[i]))
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
			got = compat.Append(got, gasReloc{r.Off, typ, r.Sym.Name, r.Addend})
		}
		if !sameRelocs(got, wantRelocs[i]) {
			t.Errorf("%s: relocations %s, as gives %s", c.asm, relocsString(got), relocsString(wantRelocs[i]))
		}
	}
}

// TestEncodeSystemInstructions checks the encoding of every instruction the
// pluto/arch/x86 intrinsics use against GNU as.
func TestEncodeSystemInstructions(t *testing.T) {
	mem := func(base Reg, disp int64) Operand { return MemOp(BaseMem(base, disp)) }
	op := func(o MOp) *MInst { return newInst(MInst{Op: o}) }
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
		{newInst(MInst{Op: MSAVEFLAGS, Dst: RegOp(RAX)}), "pushfq; pop %rax"},
		{newInst(MInst{Op: MSAVEFLAGS, Dst: RegOp(R12)}), "pushfq; pop %r12"},
		{newInst(MInst{Op: MRESTOREFLAGS, Src: RegOp(RDI)}), "push %rdi; popfq"},
		{newInst(MInst{Op: MRESTOREFLAGS, Src: RegOp(R9)}), "push %r9; popfq"},
		{newInst(MInst{Op: MIN, Size: 1}), "inb %dx, %al"},
		{newInst(MInst{Op: MIN, Size: 2}), "inw %dx, %ax"},
		{newInst(MInst{Op: MIN, Size: 4}), "inl %dx, %eax"},
		{newInst(MInst{Op: MIN, Size: 1, Src: ImmOp(0x80)}), "inb $0x80, %al"},
		{newInst(MInst{Op: MIN, Size: 2, Src: ImmOp(0x60)}), "inw $0x60, %ax"},
		{newInst(MInst{Op: MIN, Size: 4, Src: ImmOp(0xCF)}), "inl $0xcf, %eax"},
		{newInst(MInst{Op: MOUT, Size: 1}), "outb %al, %dx"},
		{newInst(MInst{Op: MOUT, Size: 2}), "outw %ax, %dx"},
		{newInst(MInst{Op: MOUT, Size: 4}), "outl %eax, %dx"},
		{newInst(MInst{Op: MOUT, Size: 1, Src: ImmOp(0x80)}), "outb %al, $0x80"},
		{newInst(MInst{Op: MOUT, Size: 2, Src: ImmOp(0x70)}), "outw %ax, $0x70"},
		{newInst(MInst{Op: MOUT, Size: 4, Src: ImmOp(0xF4)}), "outl %eax, $0xf4"},
		{newInst(MInst{Op: MRDCR, Imm: 0, Dst: RegOp(RAX)}), "mov %cr0, %rax"},
		{newInst(MInst{Op: MRDCR, Imm: 2, Dst: RegOp(RBX)}), "mov %cr2, %rbx"},
		{newInst(MInst{Op: MRDCR, Imm: 3, Dst: RegOp(R8)}), "mov %cr3, %r8"},
		{newInst(MInst{Op: MRDCR, Imm: 4, Dst: RegOp(R15)}), "mov %cr4, %r15"},
		{newInst(MInst{Op: MWRCR, Imm: 0, Src: RegOp(RDI)}), "mov %rdi, %cr0"},
		{newInst(MInst{Op: MWRCR, Imm: 3, Src: RegOp(RAX)}), "mov %rax, %cr3"},
		{newInst(MInst{Op: MWRCR, Imm: 4, Src: RegOp(R11)}), "mov %r11, %cr4"},
		{newInst(MInst{Op: MINVLPG, Src: mem(RDI, 0)}), "invlpg (%rdi)"},
		{newInst(MInst{Op: MINVLPG, Src: mem(R12, 0)}), "invlpg (%r12)"},
		{newInst(MInst{Op: MINVLPG, Src: mem(RSP, 8)}), "invlpg 8(%rsp)"},
		{newInst(MInst{Op: MLGDT, Src: mem(RAX, 0)}), "lgdt (%rax)"},
		{newInst(MInst{Op: MLGDT, Src: mem(R13, 0)}), "lgdt (%r13)"},
		{newInst(MInst{Op: MLIDT, Src: mem(RSP, 16)}), "lidt 16(%rsp)"},
		{newInst(MInst{Op: MLIDT, Src: mem(RBP, 0x200)}), "lidt 0x200(%rbp)"},
		{newInst(MInst{Op: MLTR, Size: 2, Src: RegOp(RDI)}), "ltr %di"},
		{newInst(MInst{Op: MLTR, Size: 2, Src: RegOp(R10)}), "ltr %r10w"},
		{newInst(MInst{Op: MFXSAVE, Src: mem(RDI, 0)}), "fxsave64 (%rdi)"},
		{newInst(MInst{Op: MFXSAVE, Src: mem(R12, 32)}), "fxsave64 32(%r12)"},
		{newInst(MInst{Op: MFXRSTOR, Src: mem(RSP, 0)}), "fxrstor64 (%rsp)"},
		{newInst(MInst{Op: MFXRSTOR, Src: mem(R9, 0)}), "fxrstor64 (%r9)"},
	}
	checkAgainstGAS(t, cases)
}

// TestEncodeGS checks GS-relative operands, used by the GS intrinsics and
// per-CPU variables, against GNU as, relocations included.
func TestEncodeGS(t *testing.T) {
	sym := pu.New(ssa.Sym{Name: "pcpu"})
	gs := func(m Mem) Operand {
		m.Seg = SegGS
		return MemOp(m)
	}
	abs := func(disp int64) Mem { return Mem{Base: NoReg, Index: NoReg, Disp: disp} }
	symMem := func(disp int64, index Reg, scale uint8) Mem {
		return Mem{Base: NoReg, Index: index, Scale: scale, Sym: sym, Disp: disp}
	}
	cases := []gasCase{
		{newInst(MInst{Op: MMOV, Size: 8, Dst: RegOp(RAX), Src: gs(BaseMem(RDI, 0))}), "mov %gs:(%rdi), %rax"},
		{newInst(MInst{Op: MMOV, Size: 4, Dst: RegOp(RAX), Src: gs(BaseMem(RSI, 8))}), "mov %gs:8(%rsi), %eax"},
		{newInst(MInst{Op: MMOV, Size: 8, Dst: gs(BaseMem(RDI, 0)), Src: RegOp(RSI)}), "mov %rsi, %gs:(%rdi)"},
		{newInst(MInst{Op: MMOV, Size: 4, Dst: gs(BaseMem(R12, 0)), Src: RegOp(R9)}), "mov %r9d, %gs:(%r12)"},
		{newInst(MInst{Op: MMOV, Size: 8, Dst: RegOp(RAX), Src: gs(abs(0))}), "mov %gs:0, %rax"},
		{newInst(MInst{Op: MMOV, Size: 8, Dst: RegOp(R10), Src: gs(abs(0x40))}), "mov %gs:0x40, %r10"},
		{newInst(MInst{Op: MMOV, Size: 8, Dst: gs(abs(16)), Src: ImmOp(5)}), "movq $5, %gs:16"},
		{newInst(MInst{Op: MMOV, Size: 8, Dst: RegOp(RAX), Src: gs(symMem(0, NoReg, 0))}), "mov %gs:pcpu, %rax"},
		{newInst(MInst{Op: MMOV, Size: 4, Dst: RegOp(R11), Src: gs(symMem(4, NoReg, 0))}), "mov %gs:pcpu+4, %r11d"},
		{newInst(MInst{Op: MMOV, Size: 8, Dst: gs(symMem(8, NoReg, 0)), Src: RegOp(R9)}), "mov %r9, %gs:pcpu+8"},
		{newInst(MInst{Op: MMOV, Size: 1, Dst: gs(symMem(0, NoReg, 0)), Src: ImmOp(1)}), "movb $1, %gs:pcpu"},
		{newInst(MInst{Op: MMOV, Size: 8, Dst: RegOp(RDX), Src: gs(symMem(0, RCX, 8))}), "mov %gs:pcpu(,%rcx,8), %rdx"},
		{newInst(MInst{Op: MADD, Size: 8, Dst: RegOp(RAX), Src: gs(symMem(0, NoReg, 0))}), "add %gs:pcpu, %rax"},
		{newInst(MInst{Op: MMOVZX, Size: 4, SrcSize: 1, Dst: RegOp(RAX), Src: gs(symMem(0, NoReg, 0))}), "movzbl %gs:pcpu, %eax"},
		{newInst(MInst{Op: MLEA, Size: 8, Dst: RegOp(RAX), Src: MemOp(Mem{Base: RAX, Index: NoReg, Sym: sym})}), "lea pcpu(%rax), %rax"},
		{newInst(MInst{Op: MLGDT, Src: gs(symMem(0, NoReg, 0))}), "lgdt %gs:pcpu"},
		{newInst(MInst{Op: MLEA, Size: 8, Dst: RegOp(RCX), Src: MemOp(Mem{Base: NoReg, Index: NoReg, Sym: sym, Abs: true})}), "lea pcpu, %rcx"},
	}
	checkAgainstGAS(t, cases)
}
