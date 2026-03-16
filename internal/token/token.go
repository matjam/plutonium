// Package token defines constants representing the lexical tokens of the
// Plutonium programming language and basic operations on tokens.
package token

import "strconv"

// Token is the set of lexical tokens of Plutonium.
type Token int

// The list of tokens.
const (
	// Special tokens
	ILLEGAL Token = iota
	EOF
	COMMENT

	literal_beg
	// Identifiers and basic type literals (these tokens stand for classes of literals)
	IDENT  // main
	INT    // 12345
	FLOAT  // 123.45
	IMAG   // 123.45i
	CHAR   // 'a'
	STRING // "abc"
	literal_end

	operator_beg
	// Operators and delimiters
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

	keyword_beg
	// Keywords
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

	// Systems programming keywords
	PACKED // packed
	BITS   // bits
	BE     // be
	LE     // le

	forbidden_beg
	// Forbidden keywords — valid Go keywords that Plutonium does not support.
	// They are still recognized so the lexer/parser can produce helpful errors.
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

	LAND:     "&&",
	LOR:      "||",
	ARROW:    "<-",
	INC:      "++",
	DEC:      "--",
	EQL:      "==",
	LSS:      "<",
	GTR:      ">",
	ASSIGN:   "=",
	NOT:      "!",
	NEQ:      "!=",
	LEQ:      "<=",
	GEQ:      ">=",
	DEFINE:   ":=",
	ELLIPSIS: "...",

	LPAREN:    "(",
	LBRACK:    "[",
	LBRACE:    "{",
	COMMA:     ",",
	PERIOD:    ".",
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

	PACKED: "packed",
	BITS:   "bits",
	BE:     "be",
	LE:     "le",

	CHAN:   "chan",
	GO:     "go",
	MAP:    "map",
	SELECT: "select",
}

// String returns the string corresponding to the token tok.
// For operators, delimiters, and keywords the string is the actual
// token character sequence (e.g., for the token ADD, the string is
// "+"). For all other tokens the string corresponds to the token
// constant name (e.g. for the token IDENT, the string is "IDENT").
func (tok Token) String() string {
	s := ""
	if 0 <= tok && int(tok) < len(tokens) {
		s = tokens[tok]
	}
	if s == "" {
		s = "token(" + strconv.Itoa(int(tok)) + ")"
	}
	return s
}

// IsLiteral returns true for tokens corresponding to identifiers
// and basic type literals; it returns false otherwise.
func (tok Token) IsLiteral() bool { return literal_beg < tok && tok < literal_end }

// IsOperator returns true for tokens corresponding to operators and
// delimiters; it returns false otherwise.
func (tok Token) IsOperator() bool { return operator_beg < tok && tok < operator_end }

// IsKeyword returns true for tokens corresponding to keywords;
// it returns false otherwise. This includes forbidden keywords.
func (tok Token) IsKeyword() bool { return keyword_beg < tok && tok < keyword_end }

// IsForbidden returns true for Go keywords that are not supported by Plutonium.
func (tok Token) IsForbidden() bool { return forbidden_beg < tok && tok < forbidden_end }

// keywords maps keyword strings to their token constants.
var keywords map[string]Token

func init() {
	keywords = make(map[string]Token)
	for i := keyword_beg + 1; i < keyword_end; i++ {
		if i == forbidden_beg || i == forbidden_end {
			continue
		}
		keywords[tokens[i]] = i
	}
}

// Lookup maps an identifier to its keyword token or IDENT (if not a keyword).
func Lookup(ident string) Token {
	if tok, is_keyword := keywords[ident]; is_keyword {
		return tok
	}
	return IDENT
}

// IsKeywordString reports whether s is a Plutonium keyword (including forbidden ones).
func IsKeywordString(s string) bool {
	_, ok := keywords[s]
	return ok
}
