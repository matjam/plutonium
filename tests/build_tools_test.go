package tests

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestToolErrors checks the diagnostics for failures of the system tools
// a build runs (spec section 20.4): the assembler, the C compiler and the
// linker. Each failure has its own code, and the code is in the output.
func TestToolErrors(t *testing.T) {
	t.Parallel()
	const mainSrc = "package main\n\nfunc main() {}\n"
	const noteGNUStack = "\t.section .note.GNU-stack,\"\",@progbits\n"
	dup := "\t.text\n\t.globl dupsym\ndupsym:\n\tret\n" + noteGNUStack
	cases := []struct {
		name  string
		files map[string]string
		env   []string
		flags []string // $DIR is the program's directory
		want  []string // in the output
	}{
		{"assembler", map[string]string{
			"main.p":      mainSrc,
			"bad_amd64.s": "\t.text\n\tnotaninstruction %eax\n" + noteGNUStack,
		}, nil, nil, []string{"bad_amd64.s: running as failed", "pluto explain E5004"}},
		{"C compiler", map[string]string{
			"main.p": mainSrc,
			"bad.c":  "int f( {\n",
		}, nil, nil, []string{"bad.c: running cc failed", "pluto explain E5005"}},
		{"no C compiler", map[string]string{
			"main.p": mainSrc,
			"ok.c":   "int f(void) { return 1; }\n",
		}, []string{"CC=pluto-test-no-such-cc"}, nil, []string{"pluto explain E5008"}},
		{"multiple definitions", map[string]string{
			"main.p":    mainSrc,
			"a_amd64.s": dup,
			"b_amd64.s": dup,
		}, nil, nil, []string{"multiple definition of dupsym", "pluto explain E5003"}},
		// A 32-bit offset to a symbol 3 GiB past the code does not fit.
		{"relocation overflow", map[string]string{
			"main.p":      mainSrc,
			"far_amd64.s": "\t.text\n\t.globl farref\nfarref:\n\tleaq far(%rip), %rax\n\tret\n\t.bss\n\t.zero 0xC0000000\nfar:\n\t.zero 8\n" + noteGNUStack,
		}, nil, nil, []string{"relocation truncated to fit", "pluto explain E5002"}},
		{"other linker failure", map[string]string{
			"main.p":    mainSrc,
			"broken.ld": "THIS IS NOT { A LINKER SCRIPT\n",
		}, nil, []string{"--ldscript", "$DIR/broken.ld"}, []string{"running ld failed", "pluto explain E5006"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := writeTree(t, c.files)
			args := []string{"build"}
			for _, f := range c.flags {
				args = append(args, strings.ReplaceAll(f, "$DIR", dir))
			}
			args = append(args, "-o", filepath.Join(t.TempDir(), "prog"), ".")
			out, status := runPluto(t, dir, c.env, args...)
			if status == 0 {
				t.Fatalf("build succeeded, want an error")
			}
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("output does not contain %q:\n%s", w, out)
				}
			}
		})
	}
}
