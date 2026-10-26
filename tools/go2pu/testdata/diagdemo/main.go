// Command diagdemo exercises the diagnostics package (internal/diag): it
// prints the long explanation of every code of the catalog, every code
// rendered as an error in the full and the brief form, the diagnostics of
// the programs under tests/diag (records.go, written by
// TestDiagDemoRecords from the compiler's own output, so that the text
// printed here is the text of the golden files), the diagnostics made from
// syntax errors and from failed runs of the assembler and linker, and lists
// and joined errors. go2pu translates it with the package, and
// TestDifferential requires the Go and the Plutonium build to print the same
// text at -O0 and -O2.
package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/syntax/scanner"
	"github.com/matjam/plutonium/internal/syntax/token"
)

func main() {
	var out strings.Builder
	explainAll(&out)
	renderCatalog(&out)
	renderRecords(&out)
	syntaxCases(&out)
	toolCases(&out)
	listCases(&out)
	os.Stdout.Write([]byte(out.String()))
}

func section(out *strings.Builder, title string) {
	out.WriteString("\n######## " + title + "\n")
}

// explainAll prints the explanation of every code, and of one that does not
// exist.
func explainAll(out *strings.Builder) {
	section(out, "explain")
	codes := diag.Codes()
	out.WriteString(strconv.Itoa(len(codes)) + " codes\n")
	for _, c := range codes {
		text, err := diag.Explain(c)
		if err != nil {
			out.WriteString("error: " + err.Error() + "\n")
			continue
		}
		out.WriteString(text)
		out.WriteString("----\n")
	}
	_, err := diag.Explain("e0001")
	fmt.Fprintf(out, "lower case: %v\n", err)
	_, err = diag.Explain("E9999x")
	fmt.Fprintf(out, "unknown: %v\n", err)
}

// renderCatalog prints each code as an error with no explanation of its own
// (the catalog's text), in both forms, and with an explanation of its own.
func renderCatalog(out *strings.Builder) {
	section(out, "catalog")
	for i, c := range diag.Codes() {
		e := diag.Lookup(c)
		pos := token.Position{Filename: "demo/" + strconv.Itoa(i%7) + ".p", Offset: i * 13, Line: i%90 + 1, Column: i%40 + 1}
		d := &diag.Diagnostic{Pos: pos, Code: c, Msg: "message of " + e.Name}
		diag.Print(out, d, false)
		diag.Print(out, d, true)
		own := &diag.Diagnostic{Pos: pos, Code: c, Msg: "own message of " + e.Name + "\n\tmore detail",
			Text: "This one is about x and y.\nIt has two paragraphs, and the second one is long enough that it has to be wrapped at the width the printer uses for explanations.",
			Fix:  &diag.Fix{Intro: "To fix it", Code: "x := 1\n\ty := 2"},
			More: []*diag.Fix{{Intro: "Or write", Code: "z := 3"}, {Intro: "Or just think about it"}}}
		diag.Print(out, own, false)
		fmt.Fprintf(out, "%s\n", own.Error())
	}
	// A diagnostic with no code, and with no position.
	diag.Print(out, &diag.Diagnostic{Msg: "no code, no position"}, false)
	diag.Print(out, &diag.Diagnostic{Code: "E0000", Msg: "unknown code", Text: "Its own text."}, false)
	diag.Print(out, &diag.Diagnostic{Pos: token.Position{Line: 3, Column: 4}, Code: diag.InternalError, Msg: "line only"}, false)
	diag.Print(out, errors.New("a plain error\n"), false)
	diag.Print(out, nil, false)
}

// A record is a diagnostic as the compiler made it (records.go).
type record struct {
	d     *diag.Diagnostic
	plain bool // a plain error with only a message
}

// renderRecords prints the diagnostics of each program of the golden files
// in the default and the brief form, through one printer per form.
func renderRecords(out *strings.Builder) {
	section(out, "records")
	for _, prog := range strings.Split(records, "\x1d") {
		name, body, _ := strings.Cut(prog, "\x1c")
		out.WriteString("== " + name + "\n")
		var recs []record
		for _, r := range strings.Split(body, "\x1e") {
			if r != "" {
				recs = append(recs, decode(r))
			}
		}
		for _, brief := range []bool{false, true} {
			p := &diag.Printer{W: out, Brief: brief}
			for _, r := range recs {
				if r.plain {
					p.Print(errors.New(r.d.Msg))
				} else {
					p.Print(r.d)
				}
			}
		}
	}
}

// decode reads one record: its kind ("d" for a diagnostic, "p" for a plain
// error with only a message), the position, code, message and text, and the
// fixes, each a flag (1 if there is one), an intro and code, with every field
// separated by 0x1f.
func decode(s string) record {
	f := strings.Split(s, "\x1f")
	atoi := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil {
			panic(err)
		}
		return n
	}
	d := &diag.Diagnostic{
		Pos:  token.Position{Filename: f[1], Offset: atoi(f[2]), Line: atoi(f[3]), Column: atoi(f[4])},
		Code: diag.Code(f[5]),
		Msg:  f[6],
		Text: f[7],
	}
	n := atoi(f[8])
	for i := 0; i < n; i++ {
		var fix *diag.Fix
		if f[9+3*i] == "1" {
			fix = &diag.Fix{Intro: f[10+3*i], Code: f[11+3*i]}
		}
		if i == 0 {
			d.Fix = fix
		} else {
			d.More = append(d.More, fix)
		}
	}
	return record{d: d, plain: f[0] == "p"}
}

// syntaxCases makes diagnostics from syntax errors of each kind.
func syntaxCases(out *strings.Builder) {
	section(out, "syntax")
	mk := func(line, col int, msg string) *scanner.Error {
		return &scanner.Error{Pos: token.Position{Filename: "s.p", Offset: 10, Line: line, Column: col}, Msg: msg}
	}
	srcs := map[string]string{
		"decl":   "package main\n\nx := 1\n",
		"eof":    "package main\n\nfunc main() {\n\tif ok {\n\t\trun()\n}\n",
		"eofnoc": "package main\n\nfunc main() {}\n",
		"str":    "package main\n\nvar s = \"{\"\nfunc f() {\n",
		"none":   "",
	}
	cases := []struct {
		msg, src string
		line     int
	}{
		{"expected declaration, found x", "decl", 3},
		{"expected declaration, found 'EOF'", "none", 1},
		{"expected '}', found 'EOF'", "eof", 7},
		{"expected ';', found 'EOF'", "eof", 7},
		{"expected ';', found b", "none", 2},
		{"expected operand, found ':='", "none", 3},
		{"expected 'package', found 'import'", "none", 1},
		{"expecting { after if clause", "none", 4},
		{"newline before { in function", "none", 4},
		{"import path must be a string", "none", 3},
		{"imports must appear before other declarations", "none", 3},
		{"a default value must follow shared", "none", 3},
		{"interface method must have no type parameters", "none", 3},
		{"go starts no task; write: task worker(jobs)", "none", 3},
		{"go starts no task", "none", 3},
		{"the operand of or must be a call", "none", 3},
		{"string literal not terminated", "none", 3},
		{"illegal character U+0023 '#'", "none", 3},
		{"invalid digit '8' in octal literal", "none", 3},
		{"expected ')', found b", "none", 3},
		{"expected expression", "eofnoc", 3},
		{"something unusual", "str", 4},
		{"expected ';', found 'EOF'", "str", 5},
	}
	for _, c := range cases {
		src := srcs[c.src]
		var b []byte
		if src != "" {
			b = []byte(src)
		}
		d := diag.FromSyntax(mk(c.line, 2, c.msg), b)
		diag.Print(out, d, false)
		diag.Print(out, d, true)
		fmt.Fprintf(out, "code %s\n", d.Code)
	}
	// With no source at all.
	diag.Print(out, diag.FromSyntax(mk(3, 1, "expected declaration, found y"), nil), false)
}

// toolCases makes diagnostics from failed runs of the assembler, the linker
// and the C compiler.
func toolCases(out *strings.Builder) {
	section(out, "tools")
	rename := map[string]string{"/tmp/w/p_0.s": "pkg/x.s"}
	cases := []struct {
		tool     string
		notFound bool
		cause    string
		output   string
	}{
		{"ld", false, "exit status 1", "ld: main.o: in function `main.main':\nmain.p:3:(.text+0x1): undefined reference to `main.sum'\n"},
		{"ld", false, "exit status 1", "x.o: undefined reference to `strlen'\n"},
		{"ld", false, "exit status 1", "x.o: undefined reference to `unterminated\n"},
		{"ld", false, "exit status 1", "main.o:(.text+0x5): relocation truncated to fit: R_X86_64_32S against symbol `buf' defined in .bss section in main.o\n"},
		{"ld", false, "exit status 1", "main.o:(.text+0x5): relocation truncated to fit: R_X86_64_PC32 against `.text'\n"},
		{"ld", false, "exit status 1", "b.o: multiple definition of `memcpy'; a.o: first defined here\n"},
		{"ld", false, "exit status 1", "ld: cannot open linker script file k.ld: No such file or directory\n"},
		{"as", false, "exit status 1", "/tmp/w/p_0.s: Assembler messages:\n/tmp/w/p_0.s:3: Error: no such instruction: `foo'\n"},
		{"cc", false, "exit status 1", "/tmp/w/p_c0.c:1:1: error: unknown type name 'x'\n"},
		{"ld", true, `exec: "ld": executable file not found in $PATH`, ""},
		{"gcc", true, `exec: "gcc": executable file not found in $PATH`, ""},
		{"as", true, `exec: "as": executable file not found in $PATH`, ""},
	}
	for _, c := range cases {
		d := diag.FromTool(c.tool, c.notFound, c.cause, c.output, rename)
		diag.Print(out, d, false)
		fmt.Fprintf(out, "code %s\n", d.Code)
	}
}

// listCases prints lists, joined errors and scanner error lists.
func listCases(out *strings.Builder) {
	section(out, "lists")
	pos := func(l int) token.Position { return token.Position{Filename: "l.p", Offset: l, Line: l, Column: 1} }
	var list diag.List
	fmt.Fprintf(out, "empty: %v\n", list.Err() == nil)
	list = append(list, diag.New(pos(4), diag.Code("E0101"), "declared and not used: %s", "x"))
	list = append(list, diag.New(pos(5), diag.Code("E0105"), "non-boolean condition in %s statement", "if"))
	list = append(list, diag.New(pos(9), diag.Code("E0105"), "non-boolean condition in %s statement", "if"))
	err := list.Err()
	fmt.Fprintf(out, "error:\n%s\n", err.Error())
	var l *diag.List
	if errors.As(err, &l) {
		fmt.Fprintf(out, "as list: %d\n", len(*l))
	}
	var d *diag.Diagnostic
	if errors.As(err, &d) {
		fmt.Fprintf(out, "as diagnostic: %s\n", d.Error())
	}
	diag.Print(out, err, false)
	diag.Print(out, err, true)
	joined := errors.Join(err, errors.New("some other failure\n"), diag.New(pos(12), diag.Code("E0105"), "another"))
	diag.Print(out, joined, false)
	diag.Print(out, joined, true)
	var sl scanner.ErrorList
	sl.Add(token.Position{Filename: "q.p", Offset: 3, Line: 2, Column: 3}, "expected ';', found b")
	sl.Add(token.Position{Filename: "q.p", Offset: 9, Line: 3, Column: 1}, "expected declaration, found x")
	diag.Print(out, sl.Err(), false)
	diag.Print(out, sl.Err(), true)
}
