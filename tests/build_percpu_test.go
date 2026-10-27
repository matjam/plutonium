// Ported from internal/build/percpu_test.go at the go-cutover tag.

package tests

import (
	"debug/elf"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPerCPUHosted runs testdata/kernel/gsuser, a kernel-mode program that
// runs as a Linux process, points GS at two per-CPU blocks in turn with
// arch_prctl(ARCH_SET_GS), and checks that each block has its own copy of
// every per-CPU variable: scalars, array elements with constant and
// variable indexes, struct fields, large copies and clears, and addresses
// taken and passed to other functions.
func TestPerCPUHosted(t *testing.T) {
	t.Parallel()
	dir, err := filepath.Abs(filepath.Join("..", "testdata", "kernel", "gsuser"))
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 2} {
		out := buildProgram(t, dir, optFlag(level), "--mode", "kernel", "--ldscript", filepath.Join(dir, "user.ld"))
		stderr, status := runExe(t, out, true)
		if status != 0 {
			t.Fatalf("-O%d: exit status %d\n%s", level, status, stderr)
		}
		const want = "cpu0 10 7 0 -3 true 120 0\n" +
			"cpu1 120 9 4 0 false 1\n" +
			"in block 1 true\n" +
			"cpu0 again 111 42 120 9\n"
		if got := stderr; got != want {
			t.Errorf("-O%d: output\n%s\nwant\n%s", level, got, want)
		}
	}
}

// TestPerCPUErrors checks the rules on //pu:percpu variables.
func TestPerCPUErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, decls string
		kernel      bool
		want        string
	}{
		{"hosted", "//pu:percpu\nvar n int", false, "//pu:percpu variable n needs --mode kernel"},
		{"dynamic", "//pu:percpu\nvar n = f()\n\nfunc f() int { return 1 }", true, "per-CPU variable n must have a constant initializer"},
		{"pointer", "var x int\n\n//pu:percpu\nvar p = &x", true, "per-CPU variable p must have a constant initializer"},
		{"multi", "//pu:percpu\nvar a, b = g()\n\nfunc g() (int, int) { return 1, 2 }", true, "per-CPU variable a must have a constant initializer"},
		{"section", "//pu:percpu\n//pu:section .data.x\nvar n int", true, "cannot also have //pu:section"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "package main\n\n" + tc.decls + "\n\nfunc main() {}\n"
			flags := []string{"-O2", "-c", "--brief"}
			if tc.kernel {
				flags = append(flags, "--mode", "kernel")
			}
			r := tryBuild(t, map[string]string{"main.p": src, "panic_handler.p": panicHandlerSource}, flags...)
			if r.status == 0 || !strings.Contains(r.out, tc.want) {
				t.Errorf("exit status %d, errors %q, want %q", r.status, r.out, tc.want)
			}
		})
	}
}

const perCPUSource = `package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:percpu
var counter int64 = 5

//pu:percpu
var name = "cpu"

//pu:percpu
var hist = [3]uint16{1, 2, 3}

//pu:percpu
var current *int

var regular int

func main() {
	rt.DisablePreemption()
	counter++
	hist[1] = uint16(len(name))
	current = &regular
	rt.EnablePreemption()
	x86.Hlt()
}
`

// buildPerCPU builds perCPUSource in kernel mode, with a minimal _start,
// as an object (compileOnly) or linked with the default kernel script.
func buildPerCPU(t *testing.T, compileOnly bool, ldscript string) (exe, errs string) {
	t.Helper()
	files := map[string]string{
		"main.p":        perCPUSource,
		"start_amd64.s": "\t.section .boot, \"ax\"\n\t.globl _start\n_start:\n\thlt\n\t.section .note.GNU-stack,\"\",@progbits\n",
		"panic.p":       panicHandlerSource,
	}
	flags := []string{"-O2", "--mode", "kernel", "--brief"}
	if compileOnly {
		flags = append(flags, "-c")
	}
	if ldscript != "" {
		flags = append(flags, "--ldscript", ldscript)
	}
	r := tryBuild(t, files, flags...)
	if r.status != 0 {
		return "", r.out
	}
	return r.exe, ""
}

// TestPerCPUObject checks the object code for per-CPU variables: the
// .percpu section starts with the self word and holds the constant
// initialisers, and every access is a GS-relative absolute disp32 with an
// R_X86_64_32S relocation.
func TestPerCPUObject(t *testing.T) {
	t.Parallel()
	obj, errs := buildPerCPU(t, true, "")
	if errs != "" {
		t.Fatal(errs)
	}
	f, err := elf.Open(obj)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sec := f.Section(".percpu")
	if sec == nil || sec.Type != elf.SHT_PROGBITS || sec.Flags&elf.SHF_WRITE == 0 {
		t.Fatalf(".percpu section missing or wrong type: %+v", sec)
	}
	data, err := sec.Data()
	if err != nil {
		t.Fatal(err)
	}
	syms, err := f.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	offs := map[string]uint64{}
	for _, s := range syms {
		if int(s.Section) < len(f.Sections) && f.Sections[s.Section] == sec {
			offs[s.Name] = s.Value
		}
	}
	if off, ok := offs["pluto.percpu.self"]; !ok || off != 0 {
		t.Errorf("pluto.percpu.self at %d (present %v), want offset 0", off, ok)
	}
	if off := offs["main.counter"]; binary.LittleEndian.Uint64(data[off:]) != 5 {
		t.Errorf("counter's initial value is not in .percpu: % x", data)
	}
	if off := offs["main.hist"]; binary.LittleEndian.Uint16(data[off+4:]) != 3 {
		t.Errorf("hist's initial value is not in .percpu: % x", data)
	}
	if _, ok := offs["main.regular"]; ok {
		t.Error("an ordinary variable is in .percpu")
	}

	// Accesses from code relocate with R_X86_64_32S against the per-CPU
	// symbols, never PC-relative.
	rela := f.Section(".rela.text")
	rdata, err := rela.Data()
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for i := 0; i+24 <= len(rdata); i += 24 {
		info := binary.LittleEndian.Uint64(rdata[i+8:])
		name := syms[info>>32-1].Name
		if name == "main.counter" || name == "main.hist" || name == "main.current" {
			found++
			if typ := elf.R_X86_64(info & 0xffffffff); typ != elf.R_X86_64_32S {
				t.Errorf("relocation against %s is %v, want R_X86_64_32S", name, typ)
			}
		}
	}
	if found < 3 {
		t.Errorf("found %d relocations against per-CPU variables, want at least 3", found)
	}
}

// TestKernelLink links a kernel with the default linker script and checks
// the layout: code in the top 2 GiB, .percpu at address 0 with its image
// loaded after .data, and the script's symbols.
func TestKernelLink(t *testing.T) {
	t.Parallel()
	exe, errs := buildPerCPU(t, false, "")
	if errs != "" {
		t.Fatal(errs)
	}
	f, err := elf.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if text := f.Section(".text"); text == nil || text.Addr < 0xFFFFFFFF80000000 {
		t.Errorf(".text not in the top 2 GiB: %+v", text)
	}
	if boot := f.Section(".boot"); boot == nil || boot.Addr != 0x100000 {
		t.Errorf(".boot not at 1 MiB: %+v", boot)
	}
	if pc := f.Section(".percpu"); pc == nil || pc.Addr != 0 {
		t.Errorf(".percpu not at 0: %+v", pc)
	}
	syms, err := f.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	vals := map[string]uint64{}
	for _, s := range syms {
		vals[s.Name] = s.Value
	}
	for _, name := range []string{"__percpu_image", "__percpu_load", "__percpu_size", "__bss_start", "__bss_end", "__kernel_phys_end"} {
		if _, ok := vals[name]; !ok {
			t.Errorf("default script does not define %s", name)
		}
	}
	if vals["__percpu_image"]-vals["__percpu_load"] != 0xFFFFFFFF80000000 {
		t.Errorf("__percpu_image %#x and __percpu_load %#x differ by more than the kernel base", vals["__percpu_image"], vals["__percpu_load"])
	}
	if vals["main.counter"] >= vals["__percpu_size"] || vals["main.main"] < 0xFFFFFFFF80000000 {
		t.Errorf("symbol values: counter %#x (size %#x), main %#x", vals["main.counter"], vals["__percpu_size"], vals["main.main"])
	}
}

// TestPerCPUBadScript checks that a linker script that does not put
// .percpu at address 0 is an error rather than a kernel with wrong
// per-CPU accesses.
func TestPerCPUBadScript(t *testing.T) {
	t.Parallel()
	script := filepath.Join(t.TempDir(), "bad.ld")
	const bad = "ENTRY(_start)\nSECTIONS {\n\t. = 0x400000;\n\t.text : { *(.boot) *(.text .text.*) }\n" +
		"\t.data : { *(.data .data.* .rodata .rodata.*) }\n\t.percpu 0x10000 : { *(.percpu) }\n" +
		"\t.bss : { *(.bss .bss.*) }\n}\n"
	if err := os.WriteFile(script, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errs := buildPerCPU(t, false, script)
	if !strings.Contains(errs, "must link section .percpu at address 0") {
		t.Errorf("errors = %q", errs)
	}
}
