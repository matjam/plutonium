package diag

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/scanner"
	"github.com/matjam/plutonium/internal/syntax/token"
	typerr "github.com/matjam/plutonium/internal/types2/errors"
)

func pos(line, col int) token.Position {
	return token.Position{Filename: "main.p", Line: line, Column: col}
}

// TestPrint checks the full output of Print for diagnostics with the
// catalog's explanation and with their own, a repeated explanation, other
// errors, and --brief.
func TestPrint(t *testing.T) {
	list := List{
		{Pos: pos(4, 2), Code: CheckerCode(typerr.UnusedVar), Msg: "declared and not used: x",
			Text: "x is declared on line 4, but its value is never used.",
			Fix:  &Fix{Intro: "To keep it for now", Code: "_ = x"}},
		{Pos: pos(5, 2), Code: CheckerCode(typerr.InvalidCond), Msg: "non-boolean condition in if statement"},
		{Pos: pos(9, 2), Code: CheckerCode(typerr.InvalidCond), Msg: "non-boolean condition in if statement"},
	}
	err := errors.Join(list, errors.New("some other failure\n"))
	var b strings.Builder
	Print(&b, err, false)
	want := `main.p:4:2: declared and not used: x
    x is declared on line 4, but its value is never used.
    To keep it for now:
        _ = x
    (see "Variable declarations" in the Plutonium spec, section 10.10)
    (run "pluto explain E0101" for a longer explanation)

main.p:5:2: non-boolean condition in if statement
    The condition of an if or for statement must be a bool. Plutonium, like Go,
    does not treat numbers or pointers as true or false; compare explicitly.
    (see "If statements" in the Plutonium spec, section 12.8)
    (run "pluto explain E0105" for a longer explanation)

main.p:9:2: non-boolean condition in if statement
    (explained above; run "pluto explain E0105" for more)

some other failure
`
	if b.String() != want {
		t.Errorf("Print:\n%s\nwant:\n%s", b.String(), want)
	}

	b.Reset()
	Print(&b, err, true)
	want = "main.p:4:2: declared and not used: x\nmain.p:5:2: non-boolean condition in if statement\n" +
		"main.p:9:2: non-boolean condition in if statement\nsome other failure\n"
	if b.String() != want {
		t.Errorf("Print brief:\n%s\nwant:\n%s", b.String(), want)
	}
	if got := list.Error(); got != "main.p:4:2: declared and not used: x\nmain.p:5:2: non-boolean condition in if statement\nmain.p:9:2: non-boolean condition in if statement" {
		t.Errorf("List.Error() = %q", got)
	}
	if (List{}).Err() != nil || list.Err() == nil {
		t.Error("List.Err")
	}
	var d *Diagnostic
	if !errors.As(err, &d) || d != list[0] {
		t.Error("errors.As does not find the first diagnostic")
	}
}

// TestPrintUncoded checks a diagnostic without a code and a scanner error
// list, which Print explains as syntax errors.
func TestPrintUncoded(t *testing.T) {
	var b strings.Builder
	Print(&b, &Diagnostic{Msg: "no position, no code"}, false)
	el := scanner.ErrorList{{Pos: pos(3, 1), Msg: "expected 'package', found x"}}
	Print(&b, el, false)
	want := "no position, no code\n" +
		"main.p:3:1: expected 'package', found x\n" +
		"    Every source file starts with a package clause, such as package main, before\n" +
		"    anything else except comments. All files in one directory belong to the same\n" +
		"    package.\n" +
		"    (see \"Package clause\" in the Plutonium spec, section 14.2)\n" +
		"    (run \"pluto explain E1009\" for a longer explanation)\n"
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

// TestSyntaxCodes checks how parser and scanner messages are classified.
func TestSyntaxCodes(t *testing.T) {
	tests := []struct {
		msg  string
		code Code
	}{
		{"expected declaration, found x", StmtOutsideFunc},
		{"expected '}', found 'EOF'", UnclosedBlock},
		{"expected ';', found y", MissingSemicolon},
		{"expected operand, found ':='", MissingOperand},
		{"expected 'package', found x", MissingPackage},
		{"unexpected newline, expecting { after if clause", BraceOnNextLine},
		{"missing import path", BadImportDecl},
		{"imports must appear before other declarations", BadImportDecl},
		{"a default value must follow a single parameter name and its type", CheckerCode(typerr.SharedDefault)},
		{"interface method must have no type parameters: x", IfaceMethodTParams},
		{"string literal not terminated", BadLiteral},
		{"illegal character U+0040 '@'", BadLiteral},
		{"expected ')', found 1", UnexpectedToken},
		{"exceeded max nesting depth", SyntaxError},
	}
	for _, tc := range tests {
		if got := syntaxCode(tc.msg); got != tc.code {
			t.Errorf("%q: %s, want %s", tc.msg, got, tc.code)
		}
	}
}

// TestUnclosedBrace checks which { is blamed for a missing }.
func TestUnclosedBrace(t *testing.T) {
	tests := []struct {
		src  string
		line int
	}{
		// The inner block's } is missing; the outer one is closed at the
		// inner block's indentation.
		{"package p\n\nfunc f() {\n\tif x {\n\t\ty()\n}\n", 4},
		// Nothing mismatched: the last open {.
		{"package p\n\nfunc f() {\n\tif x {\n\t\ty()\n\t}\n", 3},
		{"package p\n\nfunc f() {}\n", 0},
	}
	for _, tc := range tests {
		p, ok := unclosedBrace([]byte(tc.src), "x.p")
		if ok != (tc.line > 0) || p.Line != tc.line {
			t.Errorf("%q: line %d (%v), want %d", tc.src, p.Line, ok, tc.line)
		}
	}
	d := FromSyntax(&scanner.Error{Pos: pos(6, 2), Msg: "expected ';', found 'EOF'"}, []byte(tests[0].src))
	if d.Code != UnclosedBlock || !strings.Contains(d.Text, "The { on line 4 has no matching }") {
		t.Errorf("FromSyntax: %s %q", d.Code, d.Text)
	}
}

// TestFromTool checks the messages and codes of failed tool runs.
func TestFromTool(t *testing.T) {
	status := errors.New("exit status 1")
	tests := []struct {
		tool, output string
		err          error
		code         Code
		first        string
		text         string
	}{
		{"ld", "ld: main.o: in function `main.main':\nmain.p:3:(.text+0x1): undefined reference to `main.sum'\n", status,
			UndefinedSymbol, "running ld failed: undefined reference to main.sum", "main.sum looks like a Plutonium function"},
		{"ld", "x.o: undefined reference to `strlen'\n", status,
			UndefinedSymbol, "running ld failed: undefined reference to strlen", "strlen looks like a C symbol"},
		{"ld", "main.o:(.text+0x5): relocation truncated to fit: R_X86_64_32S against symbol `buf' defined in .bss section in main.o\n", status,
			RelocOverflow, "running ld failed: relocation truncated to fit (R_X86_64_32S against buf)", "reaches buf with a R_X86_64_32S relocation"},
		{"ld", "b.o: multiple definition of `memcpy'; a.o: first defined here\n", status,
			MultipleDefs, "running ld failed: multiple definition of memcpy", "define memcpy"},
		{"ld", "ld: cannot open linker script file k.ld: No such file or directory\n", status,
			LinkFailed, "running ld failed: exit status 1", ""},
		{"as", "/tmp/w/p_0.s: Assembler messages:\n/tmp/w/p_0.s:3: Error: no such instruction: `foo'\n", status,
			AssemblerError, "running as failed: exit status 1", ""},
		{"cc", "/tmp/w/p_c0.c:1:1: error: unknown type name 'x'\n", status,
			CCompilerError, "running cc failed: exit status 1", ""},
		{"ld", "", &exec.Error{Name: "ld", Err: exec.ErrNotFound}, ToolNotFound,
			`running ld failed: exec: "ld": executable file not found in $PATH`, "GNU linker (ld)"},
	}
	for _, tc := range tests {
		d := FromTool(tc.tool, tc.err, tc.output, map[string]string{"/tmp/w/p_0.s": "pkg/x.s"})
		first, rest, _ := strings.Cut(d.Msg, "\n")
		if d.Code != tc.code || first != tc.first || !strings.Contains(d.Text, tc.text) {
			t.Errorf("%s %q:\ncode %s, first line %q, text %q", tc.tool, tc.output, d.Code, first, d.Text)
		}
		if tc.output != "" && !strings.HasPrefix(rest, "\t") {
			t.Errorf("%s %q: tool output not indented: %q", tc.tool, tc.output, rest)
		}
		if tc.tool == "as" && !strings.Contains(d.Msg, "pkg/x.s:3: Error") {
			t.Errorf("as: file not renamed: %q", d.Msg)
		}
	}
}

// TestWrap checks that explanations wrap at the width, and that a word
// longer than a line stays whole.
func TestWrap(t *testing.T) {
	var b strings.Builder
	writeParas(&b, strings.Repeat("word ", 30)+"\n"+strings.Repeat("x", 90), "  ", 40)
	for _, line := range strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n") {
		if len(line) > 40 && strings.Contains(line, " word") {
			t.Errorf("line too long: %q", line)
		}
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("line not indented: %q", line)
		}
	}
	if !strings.Contains(b.String(), "\n  "+strings.Repeat("x", 90)+"\n") {
		t.Errorf("long word split:\n%s", b.String())
	}
}

func TestEditDistance(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		d    int
	}{{"count", "cuont", 2}, {"", "abc", 3}, {"len", "len", 0}, {"Println", "Printn", 1}} {
		if d := editDistance(tc.a, tc.b); d != tc.d {
			t.Errorf("editDistance(%q, %q) = %d, want %d", tc.a, tc.b, d, tc.d)
		}
	}
	if got := closest("cuont", []string{"count", "x", "cuont"}); got != "count" {
		t.Errorf("closest = %q", got)
	}
	if got := closest("ab", []string{"xy"}); got != "" {
		t.Errorf("closest of a distant name = %q", got)
	}
}

func ExampleExplanation() {
	d := &Diagnostic{Pos: pos(7, 9), Code: KernelFloat, Msg: "float64 arithmetic is not allowed in kernel mode"}
	fmt.Print(d.Error() + "\n" + Explanation(d)[:60])
	// Output:
	// main.p:7:9: float64 arithmetic is not allowed in kernel mode
	//     In kernel mode, functions may not use floating point. Wh
}
