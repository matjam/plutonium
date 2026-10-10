package diag

import (
	"fmt"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/scanner"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// Syntax errors come from the parser and scanner, forked from Go's, as
// messages without codes. FromSyntax gives each a code from its message
// and, where the source is at hand, an explanation that points at the
// cause, such as the line of a { that is never closed.

const goSpecFollows = "; Plutonium follows Go here"

func init() {
	register(
		&Entry{
			Code: SyntaxError, Name: "SyntaxError",
			Text: "The parser could not read this part of the file as Plutonium. The message says what it expected and what it found instead. The mistake is often just before the position shown, such as a missing bracket or operator on the line before.",
			Spec: "\"Notation\" and the grammar in the Go spec" + goSpecFollows,
		},
		&Entry{
			Code: UnexpectedToken, Name: "UnexpectedToken",
			Text:    "The parser expected one thing here and found another. The mistake is often just before the position shown: a missing comma, bracket or operator, or a keyword used as a name.",
			Spec:    "the grammar in the Go spec" + goSpecFollows,
			Example: "f(a b)",
			Fixed:   "f(a, b)",
		},
		&Entry{
			Code: UnclosedBlock, Name: "UnclosedBlock",
			Text:    "The file ended while a block was still open: a { has no matching }. The parser reports the error at the end of the file, but the missing } belongs somewhere earlier.",
			Spec:    "\"Blocks\" in the Go spec" + goSpecFollows,
			Example: "func main() {\n\tif ok {\n\t\trun()\n}",
			Fixed:   "func main() {\n\tif ok {\n\t\trun()\n\t}\n}",
		},
		&Entry{
			Code: StmtOutsideFunc, Name: "StmtOutsideFunc",
			Text:    "Outside a function, a file may contain only declarations: package, import, const, var, type and func. Statements such as assignments, calls and if or for statements must be inside a function body.\nThe short form x := value is a statement too, so at package level write var x = value.",
			Fix:     &Fix{Intro: "Declare a package-level variable with var", Code: "var count = 1"},
			Spec:    "\"Declarations and scope\" and \"Short variable declarations\" in the Go spec" + goSpecFollows,
			Example: "count := 1\n\nfunc main() {}",
			Fixed:   "var count = 1\n\nfunc main() {}",
		},
		&Entry{
			Code: MissingSemicolon, Name: "MissingSemicolon",
			Text:    "Statements end at the end of a line, or at a semicolon. The parser found the end of one statement and then more text on the same line that does not start a new one. This usually means a missing operator, comma or bracket, or two statements on one line.",
			Spec:    "\"Semicolons\" in the Go spec" + goSpecFollows,
			Example: "x := a b",
			Fixed:   "x := a + b",
		},
		&Entry{
			Code: MissingOperand, Name: "MissingOperand",
			Text:    "The parser expected a value here, such as a name, a number or a call, and found an operator or keyword instead. An operand is missing, or there is an extra operator.",
			Spec:    "\"Operands\" in the Go spec" + goSpecFollows,
			Example: "x := := 1",
			Fixed:   "x := 1",
		},
		&Entry{
			Code: BraceOnNextLine, Name: "BraceOnNextLine",
			Text:    "The { that opens a block must be on the same line as the if, for, switch or func that it belongs to. A line break ends the statement there, so a { on the next line is read as a separate block.",
			Spec:    "\"Semicolons\" in the Go spec" + goSpecFollows,
			Example: "if ok\n{\n\trun()\n}",
			Fixed:   "if ok {\n\trun()\n}",
		},
		&Entry{
			Code: BadImportDecl, Name: "BadImportDecl",
			Text:    "An import names a package with a string in double quotes, and all imports come right after the package clause, before any other declaration.",
			Spec:    "\"Import declarations\" in the Go spec" + goSpecFollows,
			Example: "import pluto/fmt",
			Fixed:   "import \"pluto/fmt\"",
		},
		&Entry{
			Code: BadLiteral, Name: "BadLiteral",
			Text:    "A string, rune or number is not written correctly, or the file contains a character that is not allowed outside strings and comments. Strings in double quotes must end on the same line; use a raw string in backquotes for text over several lines. Rune literals in single quotes hold exactly one character. Curly quotation marks, often pasted from a word processor, are not quotes to the compiler.",
			Spec:    "\"Lexical elements\" in the Go spec" + goSpecFollows,
			Example: "s := \"unterminated\nt := 'ab'",
			Fixed:   "s := \"terminated\"\nt := \"ab\"",
		},
		&Entry{
			Code: MissingPackage, Name: "MissingPackage",
			Text:  "Every source file starts with a package clause, such as package main, before anything else except comments. All files in one directory belong to the same package.",
			Spec:  "\"Package clause\" in the Go spec" + goSpecFollows,
			Fixed: "package main\n\nfunc main() {}",
		},
		&Entry{
			Code: BadDefaultSyntax, Name: "BadDefaultSyntax",
			Text:    "A parameter with a default value needs a name and type of its own. In a, b int = 1 it is unclear whether the default belongs to a, to b, or to both.",
			Spec:    pspec("5.1", "Default parameter values"),
			Example: "func f(a, b int = 1)",
			Fixed:   "func f(a int = 1, b int = 1)",
		},
		&Entry{
			Code: IfaceMethodTParams, Name: "IfaceMethodTParams",
			Text: "Methods may have type parameters of their own, but an interface cannot declare such a method. A call through an interface does not know which type's method it reaches, so it could not pick an instance of a generic method.",
			Spec: pspec("5.7", "Methods with type parameters"),
		},
	)
}

// FromSyntax returns the diagnostic for a syntax error. src, if not nil, is
// the source of the file the error is in, used to find the cause.
func FromSyntax(e *scanner.Error, src []byte) *Diagnostic {
	d := &Diagnostic{Pos: e.Pos, Msg: e.Msg, Code: syntaxCode(e.Msg)}
	switch d.Code {
	case StmtOutsideFunc:
		if line := sourceLine(src, e.Pos.Line); strings.Contains(line, ":=") {
			name, value, _ := strings.Cut(line, ":=")
			name, value = strings.TrimSpace(name), strings.TrimSpace(value)
			d.Text = "This line uses :=, the short variable declaration, outside a function. := is a statement, and statements may only appear inside a function body. Outside a function, a file may contain only declarations: package, import, const, var, type and func."
			d.Fix = &Fix{Intro: "To declare a package-level variable, use var", Code: "var " + name + " = " + value}
		}
	case UnclosedBlock, MissingSemicolon, UnexpectedToken:
		if strings.HasSuffix(e.Msg, "found 'EOF'") {
			if open, ok := unclosedBrace(src, e.Pos.Filename); ok {
				d.Code = UnclosedBlock
				d.Text = fmt.Sprintf("The file ended while a block was still open. The { on line %d has no matching }, so the parser reached the end of the file looking for it. Add the missing } where the block should end; the mistake is somewhere between line %d and the end of the file.", open.Line, open.Line)
			}
		}
	}
	return d
}

// syntaxCode classifies a parser or scanner message.
func syntaxCode(msg string) Code {
	has := func(s string) bool { return strings.Contains(msg, s) }
	switch {
	case strings.HasPrefix(msg, "expected declaration"):
		return StmtOutsideFunc
	case strings.HasPrefix(msg, "expected '}', found 'EOF'"):
		return UnclosedBlock
	case strings.HasPrefix(msg, "expected ';'"):
		return MissingSemicolon
	case strings.HasPrefix(msg, "expected operand"):
		return MissingOperand
	case strings.HasPrefix(msg, "expected 'package'"):
		return MissingPackage
	case has("expecting { after") || has("newline before {"):
		return BraceOnNextLine
	case has("import path") || has("imports must appear"):
		return BadImportDecl
	case has("a default value must follow"):
		return BadDefaultSyntax
	case has("interface method must have no type parameters"):
		return IfaceMethodTParams
	case has("literal not terminated") || has("illegal character") || has("illegal rune") ||
		has("invalid digit") || has("has no digits") || has("exponent") || has("escape sequence") ||
		has("curly quotation") || has("comment not terminated") || has("radix point") ||
		has("must separate successive digits") || has("illegal UTF-8"):
		return BadLiteral
	case strings.HasPrefix(msg, "expected "):
		return UnexpectedToken
	}
	return SyntaxError
}

// sourceLine returns line n (counting from 1) of src, or "".
func sourceLine(src []byte, n int) string {
	if src == nil || n < 1 {
		return ""
	}
	lines := strings.Split(string(src), "\n")
	if n > len(lines) {
		return ""
	}
	return lines[n-1]
}

// unclosedBrace returns the position of the { in src that most likely
// lacks its }. Formatted code closes a block at the indentation of the
// line that opened it, so the first } whose indentation differs from its
// {'s line marks a { whose own } is missing; failing that, it is the last
// { left open.
func unclosedBrace(src []byte, filename string) (token.Position, bool) {
	if src == nil {
		return token.Position{}, false
	}
	fset := token.NewFileSet()
	file := fset.AddFile(filename, -1, len(src))
	var s scanner.Scanner
	s.Init(file, src, func(token.Position, string) {}, 0)
	indentOf := func(pos token.Pos) string {
		line := sourceLine(src, fset.Position(pos).Line)
		return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	}
	type brace struct {
		pos    token.Pos
		indent string
	}
	var open []brace
	var suspect *brace
	for {
		pos, tok, _ := s.Scan()
		switch tok {
		case token.EOF:
			if len(open) == 0 {
				return token.Position{}, false
			}
			if suspect == nil {
				suspect = &open[len(open)-1]
			}
			return fset.Position(suspect.pos), true
		case token.LBRACE:
			open = append(open, brace{pos, indentOf(pos)})
		case token.RBRACE:
			if len(open) > 0 {
				top := open[len(open)-1]
				open = open[:len(open)-1]
				if suspect == nil && indentOf(pos) != top.indent && fset.Position(pos).Line != fset.Position(top.pos).Line {
					suspect = &top
				}
			}
		}
	}
}
