# Plutonium v0.1 Compiler Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a working Plutonium compiler that can compile kernel-safe `.pu` source files to x86_64 ELF object files and link them into standalone Linux binaries with no libc dependency.

**Architecture:** Clean from-scratch pipeline: scanner → parser → AST → semantic analysis → monomorphization → IR → x86_64 codegen → ELF object writer. Pure-Go throughout; no LLVM, no CGo. Each compiler stage is an isolated package under `internal/`. The backend implements a `Backend` interface so aarch64 can be added later without touching the frontend.

**Tech Stack:** Go 1.22+, Cobra (CLI), `golang.org/x/arch/x86/x86asm` (x86 encoding reference), `debug/elf` (ELF layout), `encoding/binary` (binary I/O). Go stdlib `go/token` and `go/scanner` adapted as starting points.

---

## Task 1: Repository Bootstrap

**Files:**
- Create: `go.mod`
- Create: `go.sum`
- Create: `cmd/pluto/main.go`
- Create: `README.md`

**Step 1: Initialize the Go module**

```bash
cd /home/matjam/src/Plutonium
go mod init github.com/matjam/plutonium
```

Expected: `go.mod` created with module path `github.com/matjam/plutonium`.

**Step 2: Add dependencies**

```bash
go get github.com/spf13/cobra@latest
go get golang.org/x/arch@latest
```

**Step 3: Create the cobra root command**

Create `cmd/pluto/main.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "pluto",
	Short: "The Plutonium compiler",
	Long:  "pluto is the compiler for the Plutonium kernel-safe systems language.",
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

**Step 4: Verify it builds and runs**

```bash
go run ./cmd/pluto/
```

Expected: prints usage text, exits 0.

**Step 5: Commit**

```bash
git init
git add .
git commit -m "chore: initialize plutonium repository"
```

---

## Task 2: Token Package

**Files:**
- Create: `internal/token/token.go`
- Create: `internal/token/token_test.go`
- Create: `internal/token/position.go`

**Goal:** Define all Plutonium token kinds and source position types. Adapted from `go/token` but stripped of Go-specific tokens and extended with `//pu:` directive handling.

**Step 1: Write the failing test**

Create `internal/token/token_test.go`:

```go
package token_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/token"
)

func TestTokenStrings(t *testing.T) {
	cases := []struct {
		tok  token.Token
		want string
	}{
		{token.ILLEGAL, "ILLEGAL"},
		{token.EOF, "EOF"},
		{token.IDENT, "IDENT"},
		{token.INT, "INT"},
		{token.FUNC, "func"},
		{token.RETURN, "return"},
		{token.IF, "if"},
		{token.FOR, "for"},
		{token.PACKAGE, "package"},
		{token.IMPORT, "import"},
	}
	for _, c := range cases {
		if got := c.tok.String(); got != c.want {
			t.Errorf("token %d: got %q, want %q", c.tok, got, c.want)
		}
	}
}

func TestForbiddenTokens(t *testing.T) {
	// These tokens must exist so the parser can produce good error messages
	forbidden := []token.Token{
		token.GO,
		token.CHAN,
		token.SELECT,
		token.MAP,
	}
	for _, tok := range forbidden {
		if tok.String() == "" {
			t.Errorf("forbidden token %d has empty string", tok)
		}
		if !tok.IsForbidden() {
			t.Errorf("token %q should be forbidden", tok)
		}
	}
}

func TestKeywordLookup(t *testing.T) {
	cases := []struct {
		ident string
		want  token.Token
	}{
		{"func", token.FUNC},
		{"return", token.RETURN},
		{"if", token.IF},
		{"else", token.ELSE},
		{"for", token.FOR},
		{"range", token.RANGE},
		{"package", token.PACKAGE},
		{"import", token.IMPORT},
		{"var", token.VAR},
		{"const", token.CONST},
		{"type", token.TYPE},
		{"struct", token.STRUCT},
		{"interface", token.INTERFACE},
		{"go", token.GO},
		{"chan", token.CHAN},
		{"select", token.SELECT},
		{"map", token.MAP},
		{"notakeyword", token.IDENT},
	}
	for _, c := range cases {
		if got := token.Lookup(c.ident); got != c.want {
			t.Errorf("Lookup(%q): got %v, want %v", c.ident, got, c.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/token/
```

Expected: FAIL — package does not exist yet.

**Step 3: Implement `internal/token/token.go`**

```go
package token

// Token is the set of lexical tokens of the Plutonium language.
type Token int

const (
	// Special tokens
	ILLEGAL Token = iota
	EOF
	COMMENT

	// Literals
	literal_beg
	IDENT  // main
	INT    // 12345
	FLOAT  // 123.45
	IMAG   // 123.45i
	CHAR   // 'a'
	STRING // "abc"
	literal_end

	// Operators and punctuation
	operator_beg
	ADD // +
	SUB // -
	MUL // *
	QUO // /
	REM // %

	AND     // &
	OR      // |
	XOR     // ^
	SHL     // <<
	SHR     // >>
	AND_NOT // &^

	ADD_ASSIGN // +=
	SUB_ASSIGN // -=
	MUL_ASSIGN // *=
	QUO_ASSIGN // /=
	REM_ASSIGN // %=

	AND_ASSIGN     // &=
	OR_ASSIGN      // |=
	XOR_ASSIGN     // ^=
	SHL_ASSIGN     // <<=
	SHR_ASSIGN     // >>=
	AND_NOT_ASSIGN // &^=

	LAND  // &&
	LOR   // ||
	ARROW // <-
	INC   // ++
	DEC   // --

	EQL    // ==
	LSS    // <
	GTR    // >
	ASSIGN // =
	NOT    // !

	NEQ      // !=
	LEQ      // <=
	GEQ      // >=
	DEFINE   // :=
	ELLIPSIS // ...

	LPAREN // (
	LBRACK // [
	LBRACE // {
	COMMA  // ,
	PERIOD // .

	RPAREN    // )
	RBRACK    // ]
	RBRACE    // }
	SEMICOLON // ;
	COLON     // :
	operator_end

	// Keywords
	keyword_beg
	BREAK
	CASE
	CONST
	CONTINUE
	DEFAULT
	DEFER
	ELSE
	FALLTHROUGH
	FOR
	FUNC
	GOTO
	IF
	IMPORT
	INTERFACE
	PACKAGE
	RANGE
	RETURN
	STRUCT
	SWITCH
	TYPE
	VAR

	// Forbidden keywords — parsed but rejected by sema
	forbidden_beg
	CHAN
	GO
	MAP
	SELECT
	forbidden_end

	keyword_end
)

var tokens = [...]string{
	ILLEGAL: "ILLEGAL",
	EOF:     "EOF",
	COMMENT: "COMMENT",

	IDENT:  "IDENT",
	INT:    "INT",
	FLOAT:  "FLOAT",
	IMAG:   "IMAG",
	CHAR:   "CHAR",
	STRING: "STRING",

	ADD: "+",
	SUB: "-",
	MUL: "*",
	QUO: "/",
	REM: "%",

	AND:     "&",
	OR:      "|",
	XOR:     "^",
	SHL:     "<<",
	SHR:     ">>",
	AND_NOT: "&^",

	ADD_ASSIGN: "+=",
	SUB_ASSIGN: "-=",
	MUL_ASSIGN: "*=",
	QUO_ASSIGN: "/=",
	REM_ASSIGN: "%=",

	AND_ASSIGN:     "&=",
	OR_ASSIGN:      "|=",
	XOR_ASSIGN:     "^=",
	SHL_ASSIGN:     "<<=",
	SHR_ASSIGN:     ">>=",
	AND_NOT_ASSIGN: "&^=",

	LAND:  "&&",
	LOR:   "||",
	ARROW: "<-",
	INC:   "++",
	DEC:   "--",

	EQL:    "==",
	LSS:    "<",
	GTR:    ">",
	ASSIGN: "=",
	NOT:    "!",

	NEQ:      "!=",
	LEQ:      "<=",
	GEQ:      ">=",
	DEFINE:   ":=",
	ELLIPSIS: "...",

	LPAREN: "(",
	LBRACK: "[",
	LBRACE: "{",
	COMMA:  ",",
	PERIOD: ".",

	RPAREN:    ")",
	RBRACK:    "]",
	RBRACE:    "}",
	SEMICOLON: ";",
	COLON:     ":",

	BREAK:       "break",
	CASE:        "case",
	CONST:       "const",
	CONTINUE:    "continue",
	DEFAULT:     "default",
	DEFER:       "defer",
	ELSE:        "else",
	FALLTHROUGH: "fallthrough",
	FOR:         "for",
	FUNC:        "func",
	GOTO:        "goto",
	IF:          "if",
	IMPORT:      "import",
	INTERFACE:   "interface",
	PACKAGE:     "package",
	RANGE:       "range",
	RETURN:      "return",
	STRUCT:      "struct",
	SWITCH:      "switch",
	TYPE:        "type",
	VAR:         "var",

	CHAN:   "chan",
	GO:     "go",
	MAP:    "map",
	SELECT: "select",
}

func (tok Token) String() string {
	if int(tok) < len(tokens) {
		return tokens[tok]
	}
	return ""
}

// IsLiteral returns true for literal tokens.
func (tok Token) IsLiteral() bool { return literal_beg < tok && tok < literal_end }

// IsOperator returns true for operator and punctuation tokens.
func (tok Token) IsOperator() bool { return operator_beg < tok && tok < operator_end }

// IsKeyword returns true for keyword tokens (including forbidden ones).
func (tok Token) IsKeyword(tok Token) bool { return keyword_beg < tok && tok < keyword_end }

// IsForbidden returns true for keywords that are parsed but rejected by the
// semantic analysis pass as not permitted in Plutonium.
func (tok Token) IsForbidden() bool { return forbidden_beg < tok && tok < forbidden_end }

var keywords map[string]Token

func init() {
	keywords = make(map[string]Token, keyword_end-keyword_beg)
	for i := keyword_beg + 1; i < keyword_end; i++ {
		keywords[tokens[i]] = Token(i)
	}
}

// Lookup maps an identifier string to its token. Returns IDENT if not a keyword.
func Lookup(ident string) Token {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}

// IsKeywordString returns true if the given string is a Plutonium keyword.
func IsKeywordString(s string) bool {
	_, ok := keywords[s]
	return ok
}
```

**Step 4: Implement `internal/token/position.go`**

```go
package token

import (
	"fmt"
	"sync"
)

// Pos is a compact encoding of a source position: an index into a FileSet.
// The zero value is NoPos; valid positions are > 0.
type Pos int

const NoPos Pos = 0

func (p Pos) IsValid() bool { return p != NoPos }

// Position is a human-readable source location.
type Position struct {
	Filename string
	Offset   int
	Line     int
	Column   int
}

func (pos Position) IsValid() bool { return pos.Line > 0 }

func (pos Position) String() string {
	s := pos.Filename
	if pos.IsValid() {
		if s != "" {
			s += ":"
		}
		s += fmt.Sprintf("%d", pos.Line)
		if pos.Column != 0 {
			s += fmt.Sprintf(":%d", pos.Column)
		}
	}
	if s == "" {
		s = "-"
	}
	return s
}

// File represents a source file registered with a FileSet.
type File struct {
	name  string
	base  int
	size  int
	mu    sync.Mutex
	lines []int // byte offsets of line starts
}

func (f *File) Name() string { return f.name }
func (f *File) Base() int    { return f.base }
func (f *File) Size() int    { return f.size }

func (f *File) LineCount() int {
	f.mu.Lock()
	n := len(f.lines)
	f.mu.Unlock()
	return n
}

func (f *File) AddLine(offset int) {
	f.mu.Lock()
	if i := len(f.lines); (i == 0 || f.lines[i-1] < offset) && offset < f.size {
		f.lines = append(f.lines, offset)
	}
	f.mu.Unlock()
}

func (f *File) Pos(offset int) Pos {
	if offset < 0 || offset > f.size {
		panic(fmt.Sprintf("invalid file offset %d (file size %d)", offset, f.size))
	}
	return Pos(f.base + offset)
}

func (f *File) Position(p Pos) Position {
	offset := int(p) - f.base
	return Position{
		Filename: f.name,
		Offset:   offset,
		Line:     f.lineFor(offset),
		Column:   f.columnFor(offset),
	}
}

func (f *File) lineFor(offset int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	lo, hi := 0, len(f.lines)
	for lo < hi {
		mid := (lo + hi) / 2
		if f.lines[mid] <= offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

func (f *File) columnFor(offset int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	lo, hi := 0, len(f.lines)
	for lo < hi {
		mid := (lo + hi) / 2
		if f.lines[mid] <= offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	lineStart := 0
	if lo > 0 {
		lineStart = f.lines[lo-1]
	}
	return offset - lineStart + 1
}

// FileSet is a set of source files.
type FileSet struct {
	mu   sync.RWMutex
	base int
	files []*File
}

func NewFileSet() *FileSet {
	return &FileSet{base: 1}
}

func (s *FileSet) AddFile(filename string, base, size int) *File {
	s.mu.Lock()
	defer s.mu.Unlock()
	if base < 0 {
		base = s.base
	}
	f := &File{
		name:  filename,
		base:  base,
		size:  size,
		lines: []int{0},
	}
	s.files = append(s.files, f)
	s.base = base + size + 1
	return f
}

func (s *FileSet) File(p Pos) *File {
	if p == NoPos {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, f := range s.files {
		if Pos(f.base) <= p && p <= Pos(f.base+f.size) {
			return f
		}
	}
	return nil
}

func (s *FileSet) Position(p Pos) Position {
	if f := s.File(p); f != nil {
		return f.Position(p)
	}
	return Position{}
}
```

**Step 5: Fix the test compile error and run**

Note: `IsKeyword` in the token.go above has a bug (duplicate parameter). Fix:

```go
func (tok Token) IsKeyword() bool { return keyword_beg < tok && tok < keyword_end }
```

```bash
go test ./internal/token/ -v
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/token/
git commit -m "feat(token): add token types and source position tracking"
```

---

## Task 3: Scanner (Lexer)

**Files:**
- Create: `internal/scanner/scanner.go`
- Create: `internal/scanner/scanner_test.go`

**Goal:** Tokenize `.pu` source files. Adapted from `go/scanner` but handles `//pu:` directives and Plutonium-specific token set.

**Step 1: Write the failing tests**

Create `internal/scanner/scanner_test.go`:

```go
package scanner_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/scanner"
	"github.com/matjam/plutonium/internal/token"
)

func scan(src string) []token.Token {
	fset := token.NewFileSet()
	file := fset.AddFile("test.pu", -1, len(src))
	var s scanner.Scanner
	var toks []token.Token
	s.Init(file, []byte(src), nil)
	for {
		_, tok, _ := s.Scan()
		toks = append(toks, tok)
		if tok == token.EOF {
			break
		}
	}
	return toks
}

func TestScanBasic(t *testing.T) {
	toks := scan("package main")
	want := []token.Token{token.PACKAGE, token.IDENT, token.SEMICOLON, token.EOF}
	if len(toks) != len(want) {
		t.Fatalf("got %v, want %v", toks, want)
	}
	for i, tok := range toks {
		if tok != want[i] {
			t.Errorf("tok[%d]: got %v, want %v", i, tok, want[i])
		}
	}
}

func TestScanIntegers(t *testing.T) {
	toks := scan("42 0xFF 0b1010 0o777")
	for i, tok := range toks {
		if tok == token.EOF {
			break
		}
		if tok != token.INT {
			t.Errorf("tok[%d]: got %v, want INT", i, tok)
		}
	}
}

func TestScanString(t *testing.T) {
	toks := scan(`"hello world"`)
	if toks[0] != token.STRING {
		t.Errorf("got %v, want STRING", toks[0])
	}
}

func TestScanForbiddenKeywords(t *testing.T) {
	for _, kw := range []string{"go", "chan", "select", "map"} {
		toks := scan(kw)
		tok := toks[0]
		if !tok.IsForbidden() {
			t.Errorf("keyword %q: expected forbidden token, got %v", kw, tok)
		}
	}
}

func TestScanDirective(t *testing.T) {
	src := "//pu:build amd64\npackage main"
	fset := token.NewFileSet()
	file := fset.AddFile("test.pu", -1, len(src))
	var s scanner.Scanner
	var directives []string
	s.Init(file, []byte(src), func(pos token.Pos, msg string) {
		directives = append(directives, msg)
	})
	for {
		_, tok, _ := s.Scan()
		if tok == token.EOF {
			break
		}
	}
	if len(s.Directives()) == 0 {
		t.Error("expected at least one //pu: directive")
	}
}
```

**Step 2: Run to verify failure**

```bash
go test ./internal/scanner/
```

Expected: FAIL — package does not exist.

**Step 3: Implement `internal/scanner/scanner.go`**

The scanner is adapted from `go/scanner`. Key differences:
- `//pu:` comment lines are collected as directives rather than skipped
- Forbidden keywords are returned as their own token types (not IDENT) so the parser can emit helpful errors
- No special handling for Go-specific syntax like raw string backticks being different (backtick raw strings are kept)

```go
package scanner

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/matjam/plutonium/internal/token"
)

// ErrorHandler is called for each error encountered.
type ErrorHandler func(pos token.Pos, msg string)

// Directive represents a //pu: compiler directive.
type Directive struct {
	Pos  token.Pos
	Text string // e.g. "build amd64"
}

// Scanner tokenizes Plutonium source text.
type Scanner struct {
	file       *token.File
	src        []byte
	errHandler ErrorHandler

	ch         rune
	offset     int
	rdOffset   int
	lineOffset int
	insertSemi bool

	directives []Directive
	errors     int
}

// Init prepares the scanner to tokenize src.
func (s *Scanner) Init(file *token.File, src []byte, errHandler ErrorHandler) {
	s.file = file
	s.src = src
	s.errHandler = errHandler
	s.ch = ' '
	s.offset = 0
	s.rdOffset = 0
	s.lineOffset = 0
	s.insertSemi = false
	s.directives = s.directives[:0]
	s.errors = 0
	s.next()
	if s.ch == '\uFEFF' {
		s.next() // skip BOM
	}
}

// Directives returns all //pu: directives encountered during scanning.
func (s *Scanner) Directives() []Directive { return s.directives }

// ErrorCount returns the number of errors encountered.
func (s *Scanner) ErrorCount() int { return s.errors }

func (s *Scanner) error(offset int, msg string) {
	s.errors++
	if s.errHandler != nil {
		s.errHandler(s.file.Pos(offset), msg)
	}
}

func (s *Scanner) next() {
	if s.rdOffset < len(s.src) {
		s.offset = s.rdOffset
		if s.ch == '\n' {
			s.lineOffset = s.offset
			s.file.AddLine(s.offset)
		}
		r, w := rune(s.src[s.rdOffset]), 1
		switch {
		case r == 0:
			s.error(s.offset, "illegal character NUL")
		case r >= utf8.RuneSelf:
			r, w = utf8.DecodeRune(s.src[s.rdOffset:])
			if r == utf8.RuneError && w == 1 {
				s.error(s.offset, "illegal UTF-8 encoding")
			}
		}
		s.rdOffset += w
		s.ch = r
	} else {
		s.offset = len(s.src)
		if s.ch == '\n' {
			s.lineOffset = s.offset
			s.file.AddLine(s.offset)
		}
		s.ch = -1 // EOF
	}
}

func (s *Scanner) peek() byte {
	if s.rdOffset < len(s.src) {
		return s.src[s.rdOffset]
	}
	return 0
}

// Scan returns the next token: (position, token, literal).
// For IDENT, INT, FLOAT, CHAR, STRING literals, the literal string is returned.
// For all other tokens, literal is "".
func (s *Scanner) Scan() (pos token.Pos, tok token.Token, lit string) {
	s.skipWhitespace()

	pos = s.file.Pos(s.offset)

	insertSemi := false

	switch ch := s.ch; {
	case isLetter(ch):
		lit = s.scanIdentifier()
		if len(lit) > 1 {
			tok = token.Lookup(lit)
			switch tok {
			case token.IDENT, token.BREAK, token.CONTINUE, token.FALLTHROUGH, token.RETURN:
				insertSemi = true
			}
		} else {
			insertSemi = true
			tok = token.IDENT
		}
	case isDecimalDigit(ch):
		insertSemi = true
		tok, lit = s.scanNumber()
	default:
		s.next()
		switch ch {
		case -1:
			if s.insertSemi {
				s.insertSemi = false
				return pos, token.SEMICOLON, "\n"
			}
			tok = token.EOF
		case '\n':
			s.insertSemi = false
			return pos, token.SEMICOLON, "\n"
		case '"':
			insertSemi = true
			tok = token.STRING
			lit = s.scanString()
		case '`':
			insertSemi = true
			tok = token.STRING
			lit = s.scanRawString()
		case '\'':
			insertSemi = true
			tok = token.CHAR
			lit = s.scanChar()
		case '/':
			if s.ch == '/' || s.ch == '*' {
				// comment
				if s.insertSemi && s.ch == '/' {
					// check for //pu: directive before consuming
				}
				comment, directive := s.scanComment()
				if directive != "" {
					s.directives = append(s.directives, Directive{Pos: pos, Text: directive})
				}
				_ = comment
				tok = token.COMMENT
				// re-scan after comment
				return s.Scan()
			}
			tok = s.switch2(token.QUO, token.QUO_ASSIGN)
		case '+':
			tok = s.switch3(token.ADD, token.ADD_ASSIGN, '+', token.INC)
			if tok == token.INC {
				insertSemi = true
			}
		case '-':
			tok = s.switch3(token.SUB, token.SUB_ASSIGN, '-', token.DEC)
			if tok == token.DEC {
				insertSemi = true
			}
		case '*':
			tok = s.switch2(token.MUL, token.MUL_ASSIGN)
		case '%':
			tok = s.switch2(token.REM, token.REM_ASSIGN)
		case '&':
			if s.ch == '^' {
				s.next()
				tok = s.switch2(token.AND_NOT, token.AND_NOT_ASSIGN)
			} else {
				tok = s.switch3(token.AND, token.AND_ASSIGN, '&', token.LAND)
			}
		case '|':
			tok = s.switch3(token.OR, token.OR_ASSIGN, '|', token.LOR)
		case '^':
			tok = s.switch2(token.XOR, token.XOR_ASSIGN)
		case '<':
			if s.ch == '-' {
				s.next()
				tok = token.ARROW
			} else {
				tok = s.switch4(token.LSS, token.LEQ, '<', token.SHL, token.SHL_ASSIGN)
			}
		case '>':
			tok = s.switch4(token.GTR, token.GEQ, '>', token.SHR, token.SHR_ASSIGN)
		case '=':
			tok = s.switch2(token.ASSIGN, token.EQL)
		case '!':
			tok = s.switch2(token.NOT, token.NEQ)
		case '(':
			tok = token.LPAREN
		case '[':
			tok = token.LBRACK
		case '{':
			tok = token.LBRACE
		case ',':
			tok = token.COMMA
		case '.':
			if s.ch == '.' && s.peek() == '.' {
				s.next()
				s.next()
				tok = token.ELLIPSIS
			} else {
				tok = token.PERIOD
			}
		case ')':
			insertSemi = true
			tok = token.RPAREN
		case ']':
			insertSemi = true
			tok = token.RBRACK
		case '}':
			insertSemi = true
			tok = token.RBRACE
		case ';':
			tok = token.SEMICOLON
			lit = ";"
		case ':':
			tok = s.switch2(token.COLON, token.DEFINE)
		default:
			if ch != '\uFEFF' {
				s.error(s.file.Offset(pos), fmt.Sprintf("illegal character %#U", ch))
			}
			insertSemi = s.insertSemi
			tok = token.ILLEGAL
			lit = string(ch)
		}
	}

	s.insertSemi = insertSemi
	return
}

func (s *Scanner) scanComment() (string, string) {
	offs := s.offset - 1
	var directive string

	if s.ch == '/' {
		// line comment
		s.next()
		// check for //pu: directive
		startOff := s.offset
		for s.ch != '\n' && s.ch >= 0 {
			s.next()
		}
		text := string(s.src[startOff:s.offset])
		if strings.HasPrefix(text, "pu:") {
			directive = strings.TrimSpace(text[3:])
		}
		return string(s.src[offs:s.offset]), directive
	}

	// block comment
	s.next()
	for s.ch >= 0 {
		ch := s.ch
		s.next()
		if ch == '*' && s.ch == '/' {
			s.next()
			break
		}
	}
	return string(s.src[offs:s.offset]), ""
}

func (s *Scanner) scanIdentifier() string {
	offs := s.offset
	for isLetter(s.ch) || isDigit(s.ch) {
		s.next()
	}
	return string(s.src[offs:s.offset])
}

func (s *Scanner) scanNumber() (token.Token, string) {
	offs := s.offset
	tok := token.INT

	if s.ch == '0' {
		s.next()
		switch s.ch {
		case 'x', 'X':
			s.next()
			for isHexDigit(s.ch) || s.ch == '_' {
				s.next()
			}
		case 'b', 'B':
			s.next()
			for s.ch == '0' || s.ch == '1' || s.ch == '_' {
				s.next()
			}
		case 'o', 'O':
			s.next()
			for isOctalDigit(s.ch) || s.ch == '_' {
				s.next()
			}
		default:
			// decimal or float starting with 0
			for isDecimalDigit(s.ch) || s.ch == '_' {
				s.next()
			}
		}
	} else {
		for isDecimalDigit(s.ch) || s.ch == '_' {
			s.next()
		}
	}

	if s.ch == '.' || s.ch == 'e' || s.ch == 'E' {
		tok = token.FLOAT
		if s.ch == '.' {
			s.next()
			for isDecimalDigit(s.ch) || s.ch == '_' {
				s.next()
			}
		}
		if s.ch == 'e' || s.ch == 'E' {
			s.next()
			if s.ch == '+' || s.ch == '-' {
				s.next()
			}
			for isDecimalDigit(s.ch) {
				s.next()
			}
		}
	}

	return tok, string(s.src[offs:s.offset])
}

func (s *Scanner) scanString() string {
	offs := s.offset - 1
	for {
		ch := s.ch
		if ch == '\n' || ch < 0 {
			s.error(offs, "string literal not terminated")
			break
		}
		s.next()
		if ch == '"' {
			break
		}
		if ch == '\\' {
			s.scanEscape('"')
		}
	}
	return string(s.src[offs:s.offset])
}

func (s *Scanner) scanRawString() string {
	offs := s.offset - 1
	for {
		ch := s.ch
		if ch < 0 {
			s.error(offs, "raw string literal not terminated")
			break
		}
		s.next()
		if ch == '`' {
			break
		}
	}
	return string(s.src[offs:s.offset])
}

func (s *Scanner) scanChar() string {
	offs := s.offset - 1
	if s.ch == '\\' {
		s.next()
		s.scanEscape('\'')
	} else {
		s.next()
	}
	if s.ch == '\'' {
		s.next()
	} else {
		s.error(offs, "char literal not terminated")
	}
	return string(s.src[offs:s.offset])
}

func (s *Scanner) scanEscape(quote rune) {
	offs := s.offset
	switch s.ch {
	case 'a', 'b', 'f', 'n', 'r', 't', 'v', '\\', quote:
		s.next()
	case '0', '1', '2', '3', '4', '5', '6', '7':
		for i := 0; i < 3; i++ {
			s.next()
		}
	case 'x':
		s.next()
		for i := 0; i < 2; i++ {
			s.next()
		}
	case 'u':
		s.next()
		for i := 0; i < 4; i++ {
			s.next()
		}
	case 'U':
		s.next()
		for i := 0; i < 8; i++ {
			s.next()
		}
	default:
		s.error(offs, fmt.Sprintf("unknown escape sequence: \\%c", s.ch))
	}
}

func (s *Scanner) skipWhitespace() {
	for s.ch == ' ' || s.ch == '\t' || s.ch == '\r' || (s.ch == '\n' && !s.insertSemi) {
		s.next()
	}
}

func (s *Scanner) switch2(tok0, tok1 token.Token) token.Token {
	if s.ch == '=' {
		s.next()
		return tok1
	}
	return tok0
}

func (s *Scanner) switch3(tok0, tok1 token.Token, ch2 rune, tok2 token.Token) token.Token {
	if s.ch == '=' {
		s.next()
		return tok1
	}
	if s.ch == ch2 {
		s.next()
		return tok2
	}
	return tok0
}

func (s *Scanner) switch4(tok0, tok1 token.Token, ch2 rune, tok2, tok3 token.Token) token.Token {
	if s.ch == '=' {
		s.next()
		return tok1
	}
	if s.ch == ch2 {
		s.next()
		if s.ch == '=' {
			s.next()
			return tok3
		}
		return tok2
	}
	return tok0
}

func isLetter(ch rune) bool {
	return ch == '_' || unicode.IsLetter(ch)
}

func isDigit(ch rune) bool {
	return unicode.IsDigit(ch)
}

func isDecimalDigit(ch rune) bool {
	return ch >= '0' && ch <= '9'
}

func isOctalDigit(ch rune) bool {
	return ch >= '0' && ch <= '7'
}

func isHexDigit(ch rune) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

// Offset returns the byte offset of p relative to the file base.
// Added as a helper since token.File doesn't expose this directly.
func (f *token.File) Offset(p token.Pos) int {
	return int(p) - f.Base()
}

// suppress unused import
var _ = filepath.Join
```

**Step 4: Run tests**

```bash
go test ./internal/scanner/ -v
```

Fix any compile errors. Expected: PASS.

**Step 5: Commit**

```bash
git add internal/scanner/
git commit -m "feat(scanner): add Plutonium lexer adapted from go/scanner"
```

---

## Task 4: AST Package

**Files:**
- Create: `internal/ast/ast.go`
- Create: `internal/ast/decl.go`
- Create: `internal/ast/expr.go`
- Create: `internal/ast/stmt.go`
- Create: `internal/ast/walk.go`

**Goal:** Define all AST node types for Plutonium. Modeled on `go/ast` but stripped of runtime-dependent constructs (no ChanType, no GoStmt, no SelectStmt, no MapType as a language node).

**Step 1: Write a smoke test**

Create `internal/ast/ast_test.go`:

```go
package ast_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/token"
)

func TestNodeInterfaces(t *testing.T) {
	// Verify key types implement the Node interface
	var _ ast.Node = (*ast.File)(nil)
	var _ ast.Node = (*ast.FuncDecl)(nil)
	var _ ast.Node = (*ast.BasicLit)(nil)
	var _ ast.Node = (*ast.Ident)(nil)
	var _ ast.Expr = (*ast.BinaryExpr)(nil)
	var _ ast.Stmt = (*ast.ReturnStmt)(nil)
	var _ ast.Decl = (*ast.FuncDecl)(nil)
}

func TestIdentCreation(t *testing.T) {
	id := &ast.Ident{NamePos: token.Pos(1), Name: "myVar"}
	if id.Name != "myVar" {
		t.Errorf("got %q, want %q", id.Name, "myVar")
	}
}
```

**Step 2: Implement `internal/ast/ast.go`**

```go
package ast

import (
	"github.com/matjam/plutonium/internal/token"
)

// Node is the base interface for all AST nodes.
type Node interface {
	Pos() token.Pos // position of first character
	End() token.Pos // position of first character after the node
}

// Expr is an expression node.
type Expr interface {
	Node
	exprNode()
}

// Stmt is a statement node.
type Stmt interface {
	Node
	stmtNode()
}

// Decl is a declaration node.
type Decl interface {
	Node
	declNode()
}

// File represents a parsed Plutonium source file.
type File struct {
	Package    token.Pos    // position of "package" keyword
	Name       *Ident       // package name
	Decls      []Decl       // top-level declarations
	Directives []Directive  // //pu: directives from this file
	Imports    []*ImportSpec
}

func (f *File) Pos() token.Pos { return f.Package }
func (f *File) End() token.Pos {
	if n := len(f.Decls); n > 0 {
		return f.Decls[n-1].End()
	}
	return f.Name.End()
}

// Directive represents a //pu: compiler directive.
type Directive struct {
	Pos  token.Pos
	Text string // e.g. "build amd64"
}
```

**Step 3: Implement `internal/ast/expr.go`**

Define all expression node types: `Ident`, `BasicLit`, `CompositeLit`, `FuncLit`, `ParenExpr`, `SelectorExpr`, `IndexExpr`, `SliceExpr`, `TypeAssertExpr` (parsed but rejected by sema), `CallExpr`, `StarExpr`, `UnaryExpr`, `BinaryExpr`, `KeyValueExpr`.

Type expression nodes: `ArrayType`, `StructType`, `FuncType`, `InterfaceType`, `SliceType`, `PointerType`, `FuncTypeParam` (generics).

```go
package ast

import "github.com/matjam/plutonium/internal/token"

// --- Identifiers ---

type Ident struct {
	NamePos token.Pos
	Name    string
	Obj     *Object // resolved object (set by type checker)
}

func (x *Ident) Pos() token.Pos { return x.NamePos }
func (x *Ident) End() token.Pos { return token.Pos(int(x.NamePos) + len(x.Name)) }
func (x *Ident) exprNode()      {}
func (x *Ident) String() string { return x.Name }

// --- Literals ---

type BasicLit struct {
	ValuePos token.Pos
	Kind     token.Token // INT, FLOAT, CHAR, STRING
	Value    string
}

func (x *BasicLit) Pos() token.Pos { return x.ValuePos }
func (x *BasicLit) End() token.Pos { return token.Pos(int(x.ValuePos) + len(x.Value)) }
func (x *BasicLit) exprNode()      {}

// --- Composite literal ---

type CompositeLit struct {
	Type       Expr   // nil means implicit type
	Lbrace     token.Pos
	Elts       []Expr
	Rbrace     token.Pos
	Incomplete bool
}

func (x *CompositeLit) Pos() token.Pos {
	if x.Type != nil {
		return x.Type.Pos()
	}
	return x.Lbrace
}
func (x *CompositeLit) End() token.Pos { return x.Rbrace + 1 }
func (x *CompositeLit) exprNode()      {}

// --- Function literal ---

type FuncLit struct {
	Type *FuncType
	Body *BlockStmt
}

func (x *FuncLit) Pos() token.Pos { return x.Type.Pos() }
func (x *FuncLit) End() token.Pos { return x.Body.End() }
func (x *FuncLit) exprNode()      {}

// --- Parenthesized expression ---

type ParenExpr struct {
	Lparen token.Pos
	X      Expr
	Rparen token.Pos
}

func (x *ParenExpr) Pos() token.Pos { return x.Lparen }
func (x *ParenExpr) End() token.Pos { return x.Rparen + 1 }
func (x *ParenExpr) exprNode()      {}

// --- Selector expression (x.y) ---

type SelectorExpr struct {
	X   Expr
	Sel *Ident
}

func (x *SelectorExpr) Pos() token.Pos { return x.X.Pos() }
func (x *SelectorExpr) End() token.Pos { return x.Sel.End() }
func (x *SelectorExpr) exprNode()      {}

// --- Index expression (x[i] or x[T] for generics) ---

type IndexExpr struct {
	X      Expr
	Lbrack token.Pos
	Index  Expr
	Rbrack token.Pos
}

func (x *IndexExpr) Pos() token.Pos { return x.X.Pos() }
func (x *IndexExpr) End() token.Pos { return x.Rbrack + 1 }
func (x *IndexExpr) exprNode()      {}

// --- Slice expression (x[lo:hi:max]) ---

type SliceExpr struct {
	X      Expr
	Lbrack token.Pos
	Low    Expr // nil means 0
	High   Expr // nil means len(x)
	Max    Expr // nil means cap(x); only valid in 3-index slice
	Slice3 bool
	Rbrack token.Pos
}

func (x *SliceExpr) Pos() token.Pos { return x.X.Pos() }
func (x *SliceExpr) End() token.Pos { return x.Rbrack + 1 }
func (x *SliceExpr) exprNode()      {}

// --- Type assertion (parsed, rejected by sema for interface{}) ---

type TypeAssertExpr struct {
	X      Expr
	Lparen token.Pos
	Type   Expr // nil means .(type)
	Rparen token.Pos
}

func (x *TypeAssertExpr) Pos() token.Pos { return x.X.Pos() }
func (x *TypeAssertExpr) End() token.Pos { return x.Rparen + 1 }
func (x *TypeAssertExpr) exprNode()      {}

// --- Call expression ---

type CallExpr struct {
	Fun      Expr
	Lparen   token.Pos
	Args     []Expr
	Ellipsis token.Pos // position of "..." (token.NoPos if not present)
	Rparen   token.Pos
}

func (x *CallExpr) Pos() token.Pos { return x.Fun.Pos() }
func (x *CallExpr) End() token.Pos { return x.Rparen + 1 }
func (x *CallExpr) exprNode()      {}

// --- Star expression (*x, or pointer type *T) ---

type StarExpr struct {
	Star token.Pos
	X    Expr
}

func (x *StarExpr) Pos() token.Pos { return x.Star }
func (x *StarExpr) End() token.Pos { return x.X.End() }
func (x *StarExpr) exprNode()      {}

// --- Unary expression ---

type UnaryExpr struct {
	OpPos token.Pos
	Op    token.Token
	X     Expr
}

func (x *UnaryExpr) Pos() token.Pos { return x.OpPos }
func (x *UnaryExpr) End() token.Pos { return x.X.End() }
func (x *UnaryExpr) exprNode()      {}

// --- Binary expression ---

type BinaryExpr struct {
	X     Expr
	OpPos token.Pos
	Op    token.Token
	Y     Expr
}

func (x *BinaryExpr) Pos() token.Pos { return x.X.Pos() }
func (x *BinaryExpr) End() token.Pos { return x.Y.End() }
func (x *BinaryExpr) exprNode()      {}

// --- Key-value expression (in composite literals) ---

type KeyValueExpr struct {
	Key   Expr
	Colon token.Pos
	Value Expr
}

func (x *KeyValueExpr) Pos() token.Pos { return x.Key.Pos() }
func (x *KeyValueExpr) End() token.Pos { return x.Value.End() }
func (x *KeyValueExpr) exprNode()      {}

// --- Type nodes ---

type ArrayType struct {
	Lbrack token.Pos
	Len    Expr // nil for slice type []T
	Elt    Expr
}

func (x *ArrayType) Pos() token.Pos { return x.Lbrack }
func (x *ArrayType) End() token.Pos { return x.Elt.End() }
func (x *ArrayType) exprNode()      {}

type StructType struct {
	Struct     token.Pos
	Fields     *FieldList
	Incomplete bool
}

func (x *StructType) Pos() token.Pos { return x.Struct }
func (x *StructType) End() token.Pos { return x.Fields.End() }
func (x *StructType) exprNode()      {}

type FuncType struct {
	Func       token.Pos
	TypeParams *FieldList // generic type parameters; nil if not generic
	Params     *FieldList
	Results    *FieldList
}

func (x *FuncType) Pos() token.Pos {
	if x.Func.IsValid() {
		return x.Func
	}
	return x.Params.Pos()
}
func (x *FuncType) End() token.Pos {
	if x.Results != nil {
		return x.Results.End()
	}
	return x.Params.End()
}
func (x *FuncType) exprNode() {}

type InterfaceType struct {
	Interface  token.Pos
	Methods    *FieldList
	Incomplete bool
}

func (x *InterfaceType) Pos() token.Pos { return x.Interface }
func (x *InterfaceType) End() token.Pos { return x.Methods.End() }
func (x *InterfaceType) exprNode()      {}

type PointerType struct {
	Star token.Pos
	X    Expr
}

func (x *PointerType) Pos() token.Pos { return x.Star }
func (x *PointerType) End() token.Pos { return x.X.End() }
func (x *PointerType) exprNode()      {}

// Field and FieldList

type Field struct {
	Names  []*Ident  // nil for embedded fields or unnamed results
	Type   Expr
	Tag    *BasicLit // nil if no tag
	Comment *CommentGroup
}

func (f *Field) Pos() token.Pos {
	if len(f.Names) > 0 {
		return f.Names[0].Pos()
	}
	return f.Type.Pos()
}
func (f *Field) End() token.Pos {
	if f.Tag != nil {
		return f.Tag.End()
	}
	return f.Type.End()
}

type FieldList struct {
	Opening token.Pos
	List    []*Field
	Closing token.Pos
}

func (f *FieldList) Pos() token.Pos {
	if f.Opening.IsValid() {
		return f.Opening
	}
	if len(f.List) > 0 {
		return f.List[0].Pos()
	}
	return token.NoPos
}
func (f *FieldList) End() token.Pos {
	if f.Closing.IsValid() {
		return f.Closing + 1
	}
	if n := len(f.List); n > 0 {
		return f.List[n-1].End()
	}
	return token.NoPos
}
func (f *FieldList) NumFields() int {
	if f == nil {
		return 0
	}
	n := 0
	for _, g := range f.List {
		m := len(g.Names)
		if m == 0 {
			m = 1 // anonymous field
		}
		n += m
	}
	return n
}

// CommentGroup represents a sequence of comments.
type CommentGroup struct {
	List []*Comment
}

func (g *CommentGroup) Pos() token.Pos { return g.List[0].Pos() }
func (g *CommentGroup) End() token.Pos { return g.List[len(g.List)-1].End() }
func (g *CommentGroup) Text() string {
	var s string
	for _, c := range g.List {
		s += c.Text + "\n"
	}
	return s
}

type Comment struct {
	Slash token.Pos
	Text  string
}

func (c *Comment) Pos() token.Pos { return c.Slash }
func (c *Comment) End() token.Pos { return token.Pos(int(c.Slash) + len(c.Text)) }

// Object represents a declared entity.
type Object struct {
	Kind ObjKind
	Name string
	Decl interface{} // *Field, *FuncDecl, *LabeledStmt, *AssignStmt, *Scope
	Data interface{}
	Type interface{}
}

type ObjKind int

const (
	Bad ObjKind = iota
	Pkg         // package
	Con         // constant
	Typ         // type
	Var         // variable
	Fun         // function or method
	Lbl         // label
)

// Scope represents a declaration scope.
type Scope struct {
	Outer   *Scope
	Objects map[string]*Object
}

func NewScope(outer *Scope) *Scope {
	return &Scope{Outer: outer, Objects: make(map[string]*Object)}
}

func (s *Scope) Lookup(name string) *Object {
	return s.Objects[name]
}

func (s *Scope) Insert(obj *Object) (alt *Object) {
	if alt = s.Objects[obj.Name]; alt == nil {
		s.Objects[obj.Name] = obj
	}
	return
}
```

**Step 4: Implement `internal/ast/stmt.go` and `internal/ast/decl.go`**

`stmt.go` should define: `BadStmt`, `DeclStmt`, `EmptyStmt`, `LabeledStmt`, `ExprStmt`, `SendStmt` (parsed, rejected), `IncDecStmt`, `AssignStmt`, `GoStmt` (parsed, rejected), `ReturnStmt`, `BranchStmt`, `BlockStmt`, `IfStmt`, `CaseClause`, `SwitchStmt`, `TypeSwitchStmt`, `CommClause` (parsed, rejected), `SelectStmt` (parsed, rejected), `ForStmt`, `RangeStmt`, `DeferStmt`.

`decl.go` should define: `BadDecl`, `GenDecl` (for `import`, `const`, `type`, `var`), `FuncDecl`, `ImportSpec`, `ValueSpec`, `TypeSpec`.

These follow `go/ast` structure closely. Implement them following the same pattern as the expr.go above.

**Step 5: Implement `internal/ast/walk.go`**

```go
package ast

// Visitor is called for each node during a Walk.
type Visitor interface {
	Visit(node Node) (w Visitor)
}

// Walk traverses the AST depth-first. If v.Visit(node) returns non-nil,
// Walk recurses into child nodes then calls v.Visit(nil).
func Walk(v Visitor, node Node) {
	if v = v.Visit(node); v == nil {
		return
	}
	// recurse into children based on node type
	switch n := node.(type) {
	case *File:
		for _, d := range n.Decls {
			Walk(v, d)
		}
	case *Ident:
		// leaf
	case *BasicLit:
		// leaf
	case *BinaryExpr:
		Walk(v, n.X)
		Walk(v, n.Y)
	case *UnaryExpr:
		Walk(v, n.X)
	case *CallExpr:
		Walk(v, n.Fun)
		for _, a := range n.Args {
			Walk(v, a)
		}
	case *FuncDecl:
		if n.Recv != nil {
			Walk(v, n.Recv)
		}
		Walk(v, n.Name)
		Walk(v, n.Type)
		if n.Body != nil {
			Walk(v, n.Body)
		}
	case *BlockStmt:
		for _, s := range n.List {
			Walk(v, s)
		}
	case *ReturnStmt:
		for _, r := range n.Results {
			Walk(v, r)
		}
	case *IfStmt:
		if n.Init != nil {
			Walk(v, n.Init)
		}
		Walk(v, n.Cond)
		Walk(v, n.Body)
		if n.Else != nil {
			Walk(v, n.Else)
		}
	case *ForStmt:
		if n.Init != nil {
			Walk(v, n.Init)
		}
		if n.Cond != nil {
			Walk(v, n.Cond)
		}
		if n.Post != nil {
			Walk(v, n.Post)
		}
		Walk(v, n.Body)
	case *AssignStmt:
		for _, l := range n.Lhs {
			Walk(v, l)
		}
		for _, r := range n.Rhs {
			Walk(v, r)
		}
	case *FieldList:
		for _, f := range n.List {
			Walk(v, f)
		}
	case *Field:
		for _, name := range f.Names {
			Walk(v, name)
		}
		Walk(v, f.Type)
	// ... add remaining cases
	}
	v.Visit(nil)
}
```

**Step 6: Run tests**

```bash
go test ./internal/ast/ -v
```

Expected: PASS.

**Step 7: Commit**

```bash
git add internal/ast/
git commit -m "feat(ast): add Plutonium AST node types"
```

---

## Task 5: Parser

**Files:**
- Create: `internal/parser/parser.go`
- Create: `internal/parser/parser_test.go`

**Goal:** Recursive-descent parser that produces an AST from a token stream. Parses all Plutonium syntax including generics. Immediately errors on forbidden keywords used in forbidden positions (e.g. `go` as a statement, `chan` as a type).

**Step 1: Write failing tests**

```go
package parser_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/parser"
	"github.com/matjam/plutonium/internal/token"
)

func parseSource(t *testing.T, src string) {
	t.Helper()
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "test.pu", src, 0)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
}

func parseExpectError(t *testing.T, src string) {
	t.Helper()
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "test.pu", src, 0)
	if err == nil {
		t.Fatal("expected parse error, got none")
	}
}

func TestParsePackage(t *testing.T) {
	parseSource(t, `package main`)
}

func TestParseFuncDecl(t *testing.T) {
	parseSource(t, `
package main

func add(a, b int32) int32 {
	return a + b
}
`)
}

func TestParseVarDecl(t *testing.T) {
	parseSource(t, `
package main

var x int64
var y = int32(42)
`)
}

func TestParseIfFor(t *testing.T) {
	parseSource(t, `
package main

func foo(n int32) int32 {
	if n < 0 {
		return -1
	}
	for i := int32(0); i < n; i++ {
		n--
	}
	return n
}
`)
}

func TestParseGenericFunc(t *testing.T) {
	parseSource(t, `
package main

func Max[T int32 | int64](a, b T) T {
	if a > b {
		return a
	}
	return b
}
`)
}

func TestForbiddenGoKeyword(t *testing.T) {
	parseExpectError(t, `
package main

func foo() {
	go bar()
}
`)
}

func TestForbiddenChan(t *testing.T) {
	parseExpectError(t, `
package main

var c chan int
`)
}

func TestForbiddenSelect(t *testing.T) {
	parseExpectError(t, `
package main

func foo() {
	select {}
}
`)
}

func TestAssemblyStub(t *testing.T) {
	// Function with no body = assembly stub
	parseSource(t, `
package main

func cpuid(leaf uint32) (eax, ebx, ecx, edx uint32)
`)
}
```

**Step 2: Run to verify failure**

```bash
go test ./internal/parser/
```

Expected: FAIL — package does not exist.

**Step 3: Implement the parser**

The parser is a standard recursive-descent parser. Key methods:

```go
package parser

import (
	"fmt"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/scanner"
	"github.com/matjam/plutonium/internal/token"
)

// Mode flags for ParseFile.
type Mode uint

const (
	ParseComments Mode = 1 << iota // include comments in AST
)

// ParseFile parses a single .pu source file and returns its AST.
func ParseFile(fset *token.FileSet, filename, src string, mode Mode) (*ast.File, error) {
	// ... implementation
}

type parser struct {
	file    *token.File
	errors  errorList
	scanner scanner.Scanner

	pos token.Pos
	tok token.Token
	lit string

	mode    Mode
	indent  int
}

func (p *parser) init(fset *token.FileSet, filename, src string, mode Mode) {
	// initialize scanner, advance to first token
}

func (p *parser) next() {
	// advance scanner, handle semicolon insertion
}

func (p *parser) expect(tok token.Token) token.Pos {
	pos := p.pos
	if p.tok != tok {
		p.errorExpected(pos, "'"+tok.String()+"'")
	}
	p.next()
	return pos
}

func (p *parser) parseFile() *ast.File {
	// package clause
	// import declarations
	// top-level declarations
}

func (p *parser) parseFuncDecl() *ast.FuncDecl {
	// func keyword
	// optional receiver
	// name
	// optional type parameters [T constraint]
	// parameters
	// optional results
	// optional body (nil = assembly stub)
}

// ... parseStmt, parseExpr, parseType, etc.
```

The parser should call `p.error()` with a clear message when it encounters a forbidden keyword:

```go
case token.GO:
    p.error(p.pos, "goroutines are not permitted in Plutonium: 'go' statement rejected")
    p.next()
    return &ast.BadStmt{From: pos, To: p.pos}
case token.CHAN:
    p.error(p.pos, "channels are not permitted in Plutonium: 'chan' type rejected")
    // ... recover
case token.SELECT:
    p.error(p.pos, "select is not permitted in Plutonium: 'select' statement rejected")
```

**Step 4: Run tests**

```bash
go test ./internal/parser/ -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/parser/
git commit -m "feat(parser): add recursive-descent Plutonium parser"
```

---

## Task 6: Diagnostics Package

**Files:**
- Create: `internal/diag/diag.go`
- Create: `internal/diag/diag_test.go`

**Goal:** Unified error and warning reporting with source locations, colored output, and `//pu:` directive context. Used by all compiler passes.

**Step 1: Write failing test**

```go
package diag_test

import (
	"bytes"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/token"
)

func TestErrorFormatting(t *testing.T) {
	fset := token.NewFileSet()
	fset.AddFile("main.pu", 1, 100)

	var buf bytes.Buffer
	d := diag.New(fset, &buf)
	d.Errorf(token.Pos(5), "unexpected token %q", "chan")

	out := buf.String()
	if out == "" {
		t.Error("expected non-empty diagnostic output")
	}
}

func TestErrorCount(t *testing.T) {
	fset := token.NewFileSet()
	var buf bytes.Buffer
	d := diag.New(fset, &buf)
	d.Errorf(token.NoPos, "error one")
	d.Errorf(token.NoPos, "error two")
	if d.ErrorCount() != 2 {
		t.Errorf("got %d errors, want 2", d.ErrorCount())
	}
}
```

**Step 2: Implement `internal/diag/diag.go`**

```go
package diag

import (
	"fmt"
	"io"

	"github.com/matjam/plutonium/internal/token"
)

// Diag is a diagnostic reporter.
type Diag struct {
	fset   *token.FileSet
	w      io.Writer
	errors int
	warns  int
}

func New(fset *token.FileSet, w io.Writer) *Diag {
	return &Diag{fset: fset, w: w}
}

func (d *Diag) Errorf(pos token.Pos, format string, args ...interface{}) {
	d.errors++
	msg := fmt.Sprintf(format, args...)
	if pos.IsValid() {
		p := d.fset.Position(pos)
		fmt.Fprintf(d.w, "%s: error: %s\n", p, msg)
	} else {
		fmt.Fprintf(d.w, "error: %s\n", msg)
	}
}

func (d *Diag) Warnf(pos token.Pos, format string, args ...interface{}) {
	d.warns++
	msg := fmt.Sprintf(format, args...)
	if pos.IsValid() {
		p := d.fset.Position(pos)
		fmt.Fprintf(d.w, "%s: warning: %s\n", p, msg)
	} else {
		fmt.Fprintf(d.w, "warning: %s\n", msg)
	}
}

func (d *Diag) ErrorCount() int { return d.errors }
func (d *Diag) WarnCount() int  { return d.warns }
func (d *Diag) HasErrors() bool { return d.errors > 0 }
```

**Step 3: Run tests, commit**

```bash
go test ./internal/diag/ -v
git add internal/diag/
git commit -m "feat(diag): add diagnostic reporting package"
```

---

## Task 7: Type System and Type Checker

**Files:**
- Create: `internal/types/types.go`
- Create: `internal/types/checker.go`
- Create: `internal/types/checker_test.go`

**Goal:** Define the Plutonium type system and implement a type checker that resolves types, validates expressions, and enforces kernel-safety rules. This is the largest single task in the compiler.

**Step 1: Write failing tests for type checking**

```go
package types_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/parser"
	"github.com/matjam/plutonium/internal/token"
	"github.com/matjam/plutonium/internal/types"
)

func typecheck(t *testing.T, src string) *types.Package {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.pu", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := types.Check("test", fset, []*ast.File{f})
	if err != nil {
		t.Fatalf("type error: %v", err)
	}
	return pkg
}

func typecheckExpectError(t *testing.T, src string) {
	t.Helper()
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "test.pu", src, 0)
	if f == nil {
		return // parse error already
	}
	_, err := types.Check("test", fset, []*ast.File{f})
	if err == nil {
		t.Fatal("expected type error, got none")
	}
}

func TestBasicTypeCheck(t *testing.T) {
	typecheck(t, `
package main

func add(a, b int32) int32 {
	return a + b
}
`)
}

func TestRejectNew(t *testing.T) {
	typecheckExpectError(t, `
package main

func foo() *int32 {
	return new(int32)
}
`)
}

func TestRejectMake(t *testing.T) {
	typecheckExpectError(t, `
package main

func foo() []int32 {
	return make([]int32, 10)
}
`)
}

func TestRejectAny(t *testing.T) {
	typecheckExpectError(t, `
package main

var x any
`)
}

func TestRejectPanic(t *testing.T) {
	typecheckExpectError(t, `
package main

func foo() {
	panic("oops")
}
`)
}
```

**Step 2: Implement `internal/types/types.go`**

Define the type representation:

```go
package types

// Type is the interface for all Plutonium types.
type Type interface {
	typeNode()
	String() string
	Underlying() Type
}

// Basic types
type BasicKind int

const (
	Invalid BasicKind = iota
	Bool
	Int8; Int16; Int32; Int64
	Uint8; Uint16; Uint32; Uint64
	Int; Uint; Uintptr
	Float32; Float64
	Byte   // alias for Uint8
	Rune   // alias for Int32
	String // immutable []byte essentially
	UnsafePointer
)

type Basic struct {
	kind BasicKind
	name string
}

// Pointer, Array, Slice, Struct, Func, Interface, Named, TypeParam types
// follow standard type theory patterns
```

**Step 3: Implement `internal/types/checker.go`**

The checker walks the AST and:
1. Resolves all identifiers to their declarations
2. Infers and checks types of all expressions
3. Validates all statements
4. Enforces kernel-safety rules (reject `new`, `make` heap forms, `any`, `panic`, `recover`, `reflect`)

This is a multi-week implementation task. For v0.1 the checker can be incomplete but must handle:
- Basic type inference for arithmetic
- Function call type checking
- Variable declarations
- The kernel-safety rejection rules

**Step 4: Run tests, fix, commit**

```bash
go test ./internal/types/ -v
git add internal/types/
git commit -m "feat(types): add type system and kernel-safety type checker"
```

---

## Task 8: Semantic Analysis (Kernel-Safety Pass)

**Files:**
- Create: `internal/sema/sema.go`
- Create: `internal/sema/sema_test.go`

**Goal:** A separate AST walk pass after type checking that enforces all kernel-safety rules as a distinct, auditable pass. This is separate from the type checker so the rules are easy to find, modify, and test in isolation.

Rules enforced:
- No `new(T)` calls
- No `make([]T, n)` calls (heap form)
- No `interface{}` or `any` type usage
- No `panic` or `recover` calls
- No `reflect` package imports
- No string concatenation of non-const strings with `+`
- No `append` without explicit pre-allocated backing slice
- No closures with heap-escaped captures
- No `map[K]V` type expressions (error message: "use pluto/hash instead")

```go
package sema

import (
	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/token"
	"github.com/matjam/plutonium/internal/types"
)

// Check performs kernel-safety validation on a type-checked package.
func Check(pkg *types.Package, d *diag.Diag) error {
	v := &validator{pkg: pkg, diag: d}
	for _, file := range pkg.Files() {
		ast.Walk(v, file)
	}
	if d.HasErrors() {
		return fmt.Errorf("kernel-safety validation failed with %d errors", d.ErrorCount())
	}
	return nil
}
```

**Step 5: Commit**

```bash
git add internal/sema/
git commit -m "feat(sema): add kernel-safety validation pass"
```

---

## Task 9: Monomorphization Pass

**Files:**
- Create: `internal/mono/mono.go`
- Create: `internal/mono/mono_test.go`

**Goal:** Walk the type-checked AST, find all calls to generic functions and uses of generic types with concrete type arguments, and produce a new AST with all generics expanded into concrete instantiations. After this pass there are no type parameters in the AST.

```go
package mono

// Monomorphize takes a type-checked package and returns a new package
// with all generic instantiations expanded. The result contains no
// TypeParam nodes.
func Monomorphize(pkg *types.Package) (*types.Package, error)
```

Example: `Max[int32](a, b)` and `Max[int64](x, y)` produce two concrete functions `Max__int32` and `Max__int64` in the output package.

**Step 5: Commit**

```bash
git add internal/mono/
git commit -m "feat(mono): add generic monomorphization pass"
```

---

## Task 10: Plutonium IR Builder

**Files:**
- Create: `internal/ir/ir.go`
- Create: `internal/ir/builder.go`
- Create: `internal/ir/ir_test.go`

**Goal:** Define all IR types and a builder API that the IR lowering pass uses to construct IR from the monomorphized AST.

```go
package ir

// Program is the root IR node.
type Program struct {
	Packages []*Package
}

// Package groups functions and globals.
type Package struct {
	Name      string
	Functions []*Function
	Globals   []*Global
}

// Function is a compiled function.
type Function struct {
	Name       string
	Params     []*Value
	ReturnType Type
	Blocks     []*BasicBlock
}

// BasicBlock is a sequence of instructions ending in a terminator.
type BasicBlock struct {
	Label        string
	Instructions []Instruction
	Terminator   Terminator
}

// Value represents a typed SSA-like value (register or stack slot).
type Value struct {
	ID   int
	Type Type
	Name string // optional debug name
}

// Instruction types: AllocaInstr, LoadInstr, StoreInstr,
// BinopInstr, CmpInstr, ConvertInstr, GEPInstr, CallInstr, AsmInstr, etc.
```

The builder provides a fluent API:

```go
b := ir.NewBuilder(fn)
slot := b.Alloca(ir.I32, "x")
val  := b.Load(ir.I32, slot)
sum  := b.Add(ir.I32, val, b.ConstInt(ir.I32, 1))
b.Store(ir.I32, sum, slot)
b.Ret(ir.I32, sum)
```

**Step 5: Commit**

```bash
git add internal/ir/
git commit -m "feat(ir): add Plutonium IR types and builder"
```

---

## Task 11: AST → IR Lowering

**Files:**
- Create: `internal/ir/lower.go`
- Create: `internal/ir/lower_test.go`

**Goal:** Walk the monomorphized AST and emit IR using the builder. This is the bridge between the language frontend and the machine backend.

Key lowering rules:
- `var x int32 = 5` → `alloca i32`, `store i32 5 → %x`
- `a + b` → `%v = add i32 %a %b`
- `if cond { ... } else { ... }` → `condbr`, two basic blocks, merge block
- `for init; cond; post { body }` → loop header, body, post, exit blocks
- `return x` → `ret i32 %x`
- `f(args...)` → `call`
- Slice fat pointer construction → struct with ptr/len/cap fields
- `defer f()` → push onto a stack frame defer list; emit cleanup at all return points

**Step 5: Commit**

```bash
git add internal/ir/lower.go
git commit -m "feat(ir): add AST-to-IR lowering pass"
```

---

## Task 12: x86_64 Instruction Encoder

**Files:**
- Create: `internal/codegen/x86_64/encoder/encode.go`
- Create: `internal/codegen/x86_64/encoder/encode_test.go`
- Create: `internal/codegen/x86_64/encoder/regs.go`
- Create: `internal/codegen/x86_64/encoder/opcodes.go`

**Goal:** Pure-Go x86_64 instruction encoder. Given an instruction (opcode + operands), produces the correct byte sequence. Uses `golang.org/x/arch/x86/x86asm` opcode tables as reference.

Minimum instruction set to support v0.1:
- MOV (reg-reg, reg-imm, reg-mem, mem-reg) for all widths (8/16/32/64 bit)
- ADD, SUB, IMUL, IDIV (reg, reg-imm)
- AND, OR, XOR, NOT, NEG
- SHL, SHR, SAR
- CMP, TEST
- Jcc (all condition codes: JE, JNE, JL, JLE, JG, JGE, JA, JAE, JB, JBE)
- JMP (short, near)
- CALL, RET
- PUSH, POP
- LEA
- SYSCALL
- NOP

```go
package encoder

// Reg represents an x86_64 register.
type Reg uint8

const (
	RAX Reg = iota; RCX; RDX; RBX; RSP; RBP; RSI; RDI
	R8;  R9;  R10; R11; R12; R13; R14; R15
	// 32-bit aliases
	EAX = RAX | 0x80
	// etc.
)

// Operand types
type Operand interface{ operand() }
type RegOperand struct{ Reg Reg; Width uint8 }
type ImmOperand struct{ Val int64; Width uint8 }
type MemOperand struct{ Base Reg; Index Reg; Scale uint8; Disp int32; Width uint8 }

// Encode encodes a single instruction to bytes.
func Encode(op Opcode, operands ...Operand) ([]byte, error)
```

**Step 5: Test with known encodings**

```go
func TestEncodeMovRegReg(t *testing.T) {
	// MOV RAX, RBX = 48 89 D8
	b, err := encoder.Encode(encoder.MOV,
		encoder.RegOperand{Reg: encoder.RAX, Width: 64},
		encoder.RegOperand{Reg: encoder.RBX, Width: 64})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x48, 0x89, 0xD8}
	if !bytes.Equal(b, want) {
		t.Errorf("got %x, want %x", b, want)
	}
}
```

**Step 6: Commit**

```bash
git add internal/codegen/x86_64/encoder/
git commit -m "feat(x86_64/encoder): add pure-Go x86_64 instruction encoder"
```

---

## Task 13: Linear Scan Register Allocator

**Files:**
- Create: `internal/codegen/x86_64/regalloc/regalloc.go`
- Create: `internal/codegen/x86_64/regalloc/regalloc_test.go`

**Goal:** Map IR virtual registers to physical x86_64 registers using linear scan allocation. Spill to stack when registers are exhausted.

Available caller-saved registers for allocation: RAX, RCX, RDX, RSI, RDI, R8, R9, R10, R11.
Callee-saved (save/restore around calls): RBX, R12, R13, R14, R15.
Reserved: RSP (stack pointer), RBP (frame pointer).

```go
package regalloc

// Allocation maps IR value IDs to physical locations.
type Allocation struct {
	Reg   map[int]encoder.Reg  // value ID → physical register
	Spill map[int]int          // value ID → stack offset (negative from RBP)
}

// Allocate performs linear scan register allocation over a function's IR.
func Allocate(fn *ir.Function) (*Allocation, error)
```

**Step 5: Commit**

```bash
git add internal/codegen/x86_64/regalloc/
git commit -m "feat(x86_64/regalloc): add linear scan register allocator"
```

---

## Task 14: x86_64 Code Generator

**Files:**
- Create: `internal/codegen/codegen.go` (Backend interface)
- Create: `internal/codegen/x86_64/codegen.go`
- Create: `internal/codegen/x86_64/codegen_test.go`

**Goal:** Walk IR instructions, use the register allocator's output, and emit x86_64 machine code bytes via the encoder. Produce a relocatable object section (`.text`) with a symbol table.

```go
package x86_64

import (
	"github.com/matjam/plutonium/internal/codegen"
	"github.com/matjam/plutonium/internal/ir"
	"github.com/matjam/plutonium/internal/obj"
)

type Backend struct{}

func (b *Backend) Name() string                               { return "x86_64" }
func (b *Backend) Triple() string                             { return "x86_64-linux" }
func (b *Backend) WordSize() int                              { return 8 }
func (b *Backend) Endianness() binary.ByteOrder               { return binary.LittleEndian }
func (b *Backend) Compile(fn *ir.Function) (*obj.Section, error) { ... }
```

System V AMD64 ABI calling convention:
- Integer args: RDI, RSI, RDX, RCX, R8, R9
- Return: RAX (and RDX for second value)
- Stack 16-byte aligned before CALL
- Red zone: 128 bytes below RSP (for leaf functions, no need to adjust RSP)

**Step 5: Commit**

```bash
git add internal/codegen/
git commit -m "feat(codegen): add x86_64 code generation backend"
```

---

## Task 15: ELF Object File Writer

**Files:**
- Create: `internal/obj/elf/writer.go`
- Create: `internal/obj/elf/writer_test.go`

**Goal:** Write a relocatable ELF64 `.o` file from compiled sections and a symbol table. Uses `debug/elf` constants for type definitions.

Sections emitted:
- `.text` — machine code
- `.data` — initialized static data
- `.bss` — zero-initialized static data
- `.rodata` — read-only data (string literals, constants)
- `.symtab` — symbol table
- `.strtab` — string table
- `.shstrtab` — section name string table
- `.rela.text` — relocations for the text section

```go
package elf

import (
	"debug/elf"
	"encoding/binary"
)

// Writer writes a relocatable ELF64 object file.
type Writer struct {
	// ...
}

func NewWriter() *Writer

func (w *Writer) AddSection(name string, typ elf.SectionType, flags elf.SectionFlag, data []byte) int
func (w *Writer) AddSymbol(name string, section int, offset uint64, size uint64, bind elf.SymBind, typ elf.SymType) int
func (w *Writer) AddRelocation(section int, offset uint64, sym int, typ elf.R_X86_64, addend int64)
func (w *Writer) Write(out io.Writer) error
```

**Step 5: Verify with readelf**

```bash
go test ./internal/obj/elf/ -v
# After test generates a .o file:
readelf -a /tmp/test.o
```

Expected: valid ELF64 relocatable object, correct section headers and symbol table.

**Step 6: Commit**

```bash
git add internal/obj/
git commit -m "feat(obj/elf): add ELF64 relocatable object file writer"
```

---

## Task 16: Build Tag Evaluator

**Files:**
- Create: `internal/buildtag/buildtag.go`
- Create: `internal/buildtag/buildtag_test.go`

**Goal:** Parse and evaluate `//pu:build` constraint expressions. Follows Go 1.17+ boolean syntax exactly.

```go
package buildtag

// Eval evaluates a build tag expression against a set of active tags.
// expr examples: "amd64", "amd64 && linux", "amd64 || arm64", "!cgo"
func Eval(expr string, tags map[string]bool) (bool, error)

// FileMatches returns true if a file's build constraints are satisfied.
// Checks both //pu:build directives and filename suffixes (_amd64.pu, etc.)
func FileMatches(filename string, directives []string, tags map[string]bool) (bool, error)
```

```go
func TestEvalSimple(t *testing.T) {
	tags := map[string]bool{"amd64": true, "linux": true}
	ok, _ := buildtag.Eval("amd64", tags)
	if !ok { t.Error("amd64 should match") }

	ok, _ = buildtag.Eval("amd64 && linux", tags)
	if !ok { t.Error("amd64 && linux should match") }

	ok, _ = buildtag.Eval("arm64", tags)
	if ok { t.Error("arm64 should not match") }

	ok, _ = buildtag.Eval("amd64 || arm64", tags)
	if !ok { t.Error("amd64 || arm64 should match") }

	ok, _ = buildtag.Eval("!freestanding", tags)
	if !ok { t.Error("!freestanding should match when freestanding not set") }
}
```

**Step 5: Commit**

```bash
git add internal/buildtag/
git commit -m "feat(buildtag): add //pu:build tag parser and evaluator"
```

---

## Task 17: CLI Commands

**Files:**
- Create: `cmd/pluto/build.go`
- Create: `cmd/pluto/compile.go`
- Create: `cmd/pluto/version.go`
- Modify: `cmd/pluto/main.go`

**Goal:** Wire up the `pluto build`, `pluto compile`, and `pluto version` commands using Cobra. `pluto build` drives the full pipeline end-to-end.

```go
// build.go
var buildCmd = &cobra.Command{
	Use:   "build [packages]",
	Short: "Compile and link Plutonium packages",
	RunE:  runBuild,
}

func init() {
	buildCmd.Flags().StringP("output", "o", "", "output file")
	buildCmd.Flags().String("target", "x86_64-linux", "target triple")
	buildCmd.Flags().String("mode", "hosted", "hosted or freestanding")
	buildCmd.Flags().StringSlice("tags", nil, "build tags")
	buildCmd.Flags().String("ldscript", "", "linker script (freestanding)")
	buildCmd.Flags().String("format", "elf", "output format: elf, binary, pe32+")
	buildCmd.Flags().BoolP("verbose", "v", false, "verbose output")
	buildCmd.Flags().StringP("opt", "O", "0", "optimization level: 0, 1, 2, s")
	rootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	// 1. Parse flags into config.BuildConfig
	// 2. Find .pu and .s files in the package path
	// 3. Evaluate build tags to filter files
	// 4. Parse all .pu files → ASTs
	// 5. Type check
	// 6. Sema validation
	// 7. Monomorphize
	// 8. Lower to IR
	// 9. Codegen → object sections
	// 10. Write ELF .o files
	// 11. Invoke linker
	return nil
}
```

**Step 5: Commit**

```bash
git add cmd/pluto/
git commit -m "feat(cli): add pluto build, compile, version commands"
```

---

## Task 18: End-to-End Test — Hello World

**Files:**
- Create: `testdata/programs/hello/main.pu`
- Create: `testdata/programs/hello/hello_test.go`

**Goal:** Compile and run a minimal "hello world" program that writes to stdout via a direct Linux syscall. This validates the entire pipeline end-to-end.

**`testdata/programs/hello/main.pu`:**

```go
package main

import "pluto/os"

func main() {
	os.Write(os.Stdout, []byte("hello, plutonium\n"))
	os.Exit(0)
}
```

**`testdata/programs/hello/hello_test.go`:**

```go
package hello_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestHelloWorld(t *testing.T) {
	// Build the program
	build := exec.Command("go", "run", "../../../cmd/pluto", "build",
		"-o", "/tmp/hello_pu", ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	// Run it
	run := exec.Command("/tmp/hello_pu")
	out, err := run.Output()
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(string(out), "hello, plutonium") {
		t.Errorf("unexpected output: %q", out)
	}
}
```

**Step 5: Commit**

```bash
git add testdata/programs/hello/
git commit -m "test: add hello world end-to-end test"
```

---

## Task 19: Phase 1a `pluto` Standard Library

**Files:**
- Create: `pluto/syscall/syscall_amd64.s`
- Create: `pluto/syscall/syscall.pu`
- Create: `pluto/os/os.pu`
- Create: `pluto/io/io.pu`
- Create: `pluto/fmt/fmt.pu`
- Create: `pluto/mem/mem.pu`
- Create: `pluto/strings/strings.pu`
- Create: `pluto/bytes/bytes.pu`

**Goal:** Implement enough of the `pluto` standard library to support the hello world program and basic kernel-safe programming.

**`pluto/syscall/syscall_amd64.s`:**

```asm
//pu:build amd64

// func Syscall(num, a1, a2, a3 uintptr) (r1, r2 uintptr, err uintptr)
TEXT ·Syscall(SB), NOSPLIT, $0-56
    MOVQ num+0(FP),  AX
    MOVQ a1+8(FP),   DI
    MOVQ a2+16(FP),  SI
    MOVQ a3+24(FP),  DX
    SYSCALL
    MOVQ AX, r1+32(FP)
    MOVQ DX, r2+40(FP)
    MOVQ $0, err+48(FP)
    RET
```

**`pluto/os/os.pu`:**

```go
package os

import "pluto/syscall"

const (
	Stdin  = 0
	Stdout = 1
	Stderr = 2
)

func Write(fd int, p []byte) (int, error) {
	n, _, errno := syscall.Syscall(
		syscall.SYS_WRITE,
		uintptr(fd),
		uintptr(unsafe.Pointer(&p[0])),
		uintptr(len(p)),
	)
	if errno != 0 {
		return int(n), syscall.Errno(errno)
	}
	return int(n), nil
}

func Exit(code int) {
	syscall.Syscall(syscall.SYS_EXIT_GROUP, uintptr(code), 0, 0)
}
```

**Step 5: Commit**

```bash
git add pluto/
git commit -m "feat(pluto): add Phase 1a standard library"
```

---

## Task 20: Integration, Cleanup, and README

**Files:**
- Modify: `README.md`
- Create: `docs/spec/language.md`
- Create: `docs/spec/abi.md`

**Goal:** Write the README with build instructions, write a minimal language spec, document the x86_64 ABI used by Plutonium.

**Step 1: Write README**

Cover: what Plutonium is, how to build the compiler, how to compile a hello world program, what the language rejects and why.

**Step 2: Write language spec**

Expand `docs/spec/language.md` from the design document, adding grammar excerpts and concrete examples.

**Step 3: Write ABI spec**

Document:
- Calling convention (System V AMD64)
- Stack frame layout
- Slice fat pointer layout
- String representation

**Step 4: Final test run**

```bash
go test ./...
go build ./cmd/pluto/
```

Expected: all tests pass, compiler builds.

**Step 5: Commit**

```bash
git add .
git commit -m "docs: add README, language spec, and ABI documentation"
```

---

## Summary

| Task | Component | Estimated complexity |
|---|---|---|
| 1 | Repository bootstrap | Small |
| 2 | Token package | Small |
| 3 | Scanner/lexer | Medium |
| 4 | AST package | Medium |
| 5 | Parser | Large |
| 6 | Diagnostics | Small |
| 7 | Type system + checker | Very Large |
| 8 | Sema kernel-safety pass | Medium |
| 9 | Monomorphization | Large |
| 10 | IR types + builder | Medium |
| 11 | AST → IR lowering | Large |
| 12 | x86_64 encoder | Large |
| 13 | Register allocator | Large |
| 14 | x86_64 code generator | Large |
| 15 | ELF object writer | Medium |
| 16 | Build tag evaluator | Small |
| 17 | CLI commands | Medium |
| 18 | End-to-end hello world | Medium |
| 19 | pluto stdlib Phase 1a | Medium |
| 20 | Docs + cleanup | Small |

The critical path is: Token → Scanner → AST → Parser → Types → Sema → Mono → IR → x86_64 encoder → ELF writer → CLI → hello world.

Tasks 12 (x86_64 encoder) and 7 (type checker) are the most technically demanding. The type checker in particular is a multi-week effort to get right.
