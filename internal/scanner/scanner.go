// Package scanner implements a lexer for Plutonium source files.
// It is adapted from go/scanner but handles //pu: directives and
// the Plutonium-specific token set (including forbidden keywords).
package scanner

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/matjam/plutonium/internal/token"
)

// ErrorHandler is called for each error encountered. The position argument
// is the position of the erroneous character.
type ErrorHandler func(pos token.Pos, msg string)

// Directive represents a //pu: build directive found in the source.
type Directive struct {
	Pos  token.Pos
	Text string // everything after "pu:", trimmed, e.g. "build amd64"
}

// Scanner holds the scanner's internal state while processing a given text.
// It must be initialized via Init before use.
type Scanner struct {
	file       *token.File
	src        []byte
	errHandler ErrorHandler

	ch         rune // current character
	offset     int  // character offset
	rdOffset   int  // reading offset (position after current character)
	lineOffset int  // current line offset

	insertSemi bool // insert a semicolon before next newline

	directives []Directive
	errors     int
}

const bom = 0xFEFF // byte order mark, only permitted as very first character

// Init prepares the scanner s to tokenize the text src by setting the
// scanner at the beginning of src. The scanner uses the file set file
// for position information. Init may be called on s again to scan a
// different text; it resets the scanner state. An error handler errHandler
// may be nil, in which case errors are silently discarded.
func (s *Scanner) Init(file *token.File, src []byte, errHandler ErrorHandler) {
	s.file = file
	s.src = src
	s.errHandler = errHandler

	s.ch = ' '
	s.offset = 0
	s.rdOffset = 0
	s.lineOffset = 0
	s.insertSemi = false
	s.directives = nil
	s.errors = 0

	s.next()
	if s.ch == bom {
		s.next() // ignore BOM at file beginning
	}
}

// Directives returns the list of //pu: directives encountered during scanning.
func (s *Scanner) Directives() []Directive {
	return s.directives
}

// ErrorCount returns the number of errors encountered during scanning.
func (s *Scanner) ErrorCount() int {
	return s.errors
}

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
			// not ASCII
			r, w = utf8.DecodeRune(s.src[s.rdOffset:])
			if r == utf8.RuneError && w == 1 {
				s.error(s.offset, "illegal UTF-8 encoding")
			} else if r == bom && s.offset > 0 {
				s.error(s.offset, "illegal byte order mark")
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
		s.ch = -1 // eof
	}
}

func (s *Scanner) peek() byte {
	if s.rdOffset < len(s.src) {
		return s.src[s.rdOffset]
	}
	return 0
}

func (s *Scanner) skipWhitespace() {
	for s.ch == ' ' || s.ch == '\t' || s.ch == '\r' || (s.ch == '\n' && !s.insertSemi) {
		s.next()
	}
}

func isLetter(ch rune) bool {
	return ch == '_' || unicode.IsLetter(ch)
}

func isDigit(ch rune) bool {
	return unicode.IsDigit(ch)
}

func (s *Scanner) scanIdentifier() string {
	offs := s.offset
	for isLetter(s.ch) || isDigit(s.ch) {
		s.next()
	}
	return string(s.src[offs:s.offset])
}

func digitVal(ch rune) int {
	switch {
	case '0' <= ch && ch <= '9':
		return int(ch - '0')
	case 'a' <= ch && ch <= 'f':
		return int(ch-'a') + 10
	case 'A' <= ch && ch <= 'F':
		return int(ch-'A') + 10
	}
	return 16 // larger than any digit value
}

func (s *Scanner) scanMantissa(base int) {
	for digitVal(s.ch) < base || s.ch == '_' {
		s.next()
	}
}

func (s *Scanner) scanNumber() (token.Token, string) {
	offs := s.offset
	tok := token.INT

	if s.ch == '0' {
		s.next()
		switch s.ch {
		case 'x', 'X':
			// hexadecimal int
			s.next()
			s.scanMantissa(16)
		case 'b', 'B':
			// binary int
			s.next()
			s.scanMantissa(2)
		case 'o', 'O':
			// octal int
			s.next()
			s.scanMantissa(8)
		default:
			// decimal or float starting with 0
			s.scanMantissa(10)
			if s.ch == '.' || s.ch == 'e' || s.ch == 'E' {
				goto fraction
			}
		}
		goto exit
	}

	// decimal int or float
	s.scanMantissa(10)

fraction:
	if s.ch == '.' {
		tok = token.FLOAT
		s.next()
		s.scanMantissa(10)
	}

	if s.ch == 'e' || s.ch == 'E' {
		tok = token.FLOAT
		s.next()
		if s.ch == '-' || s.ch == '+' {
			s.next()
		}
		s.scanMantissa(10)
	}

exit:
	return tok, string(s.src[offs:s.offset])
}

func (s *Scanner) scanEscape(quote rune) bool {
	offs := s.offset

	var n int
	var base, max uint32
	switch s.ch {
	case 'a', 'b', 'f', 'n', 'r', 't', 'v', '\\', quote:
		s.next()
		return true
	case '0', '1', '2', '3', '4', '5', '6', '7':
		n, base, max = 3, 8, 255
	case 'x':
		s.next()
		n, base, max = 2, 16, 255
	case 'u':
		s.next()
		n, base, max = 4, 16, unicode.MaxRune
	case 'U':
		s.next()
		n, base, max = 8, 16, unicode.MaxRune
	default:
		msg := "unknown escape sequence"
		if s.ch < 0 {
			msg = "escape sequence not terminated"
		}
		s.error(offs, msg)
		return false
	}

	var x uint32
	for n > 0 {
		d := uint32(digitVal(s.ch))
		if d >= base {
			msg := fmt.Sprintf("illegal character %#U in escape sequence", s.ch)
			if s.ch < 0 {
				msg = "escape sequence not terminated"
			}
			s.error(s.offset, msg)
			return false
		}
		x = x*base + d
		s.next()
		n--
	}

	if x > max || (0xD800 <= x && x < 0xE000) {
		s.error(offs, "escape sequence is invalid Unicode code point")
		return false
	}
	return true
}

func (s *Scanner) scanString() string {
	// opening quote already consumed
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
	// opening backtick already consumed
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
	// opening quote already consumed
	offs := s.offset - 1

	valid := true
	n := 0
	for {
		ch := s.ch
		if ch == '\n' || ch < 0 {
			// only report error if we don't have one already
			if valid {
				s.error(offs, "rune literal not terminated")
				valid = false
			}
			break
		}
		s.next()
		if ch == '\'' {
			break
		}
		n++
		if ch == '\\' {
			if !s.scanEscape('\'') {
				valid = false
			}
		}
	}

	if valid && n != 1 {
		s.error(offs, "illegal rune literal")
	}

	return string(s.src[offs:s.offset])
}

// scanComment scans a comment. The first '/' has already been consumed.
// Returns (fullText, directiveText) where directiveText is non-empty
// only if this is a //pu: directive line comment.
func (s *Scanner) scanComment() (string, string) {
	// initial '/' already consumed; s.ch == '/' || s.ch == '*'
	offs := s.offset - 1 // position of initial '/'

	if s.ch == '/' {
		// line comment: consume until newline or EOF
		s.next()
		for s.ch != '\n' && s.ch >= 0 {
			s.next()
		}
		comment := string(s.src[offs:s.offset])
		// Check for //pu: directive
		var directive string
		if strings.HasPrefix(comment, "//pu:") {
			directive = strings.TrimSpace(comment[5:]) // everything after "//pu:"
		}
		return comment, directive
	}

	// block comment: consume until */
	terminated := false
	s.next()
	for s.ch >= 0 {
		ch := s.ch
		s.next()
		if ch == '*' && s.ch == '/' {
			s.next()
			terminated = true
			break
		}
	}

	if !terminated {
		s.error(offs, "comment not terminated")
	}

	return string(s.src[offs:s.offset]), ""
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

// Scan scans the next token and returns the token position, the token, and
// its literal string if applicable. The source end is indicated by token.EOF.
//
// If the returned token is a keyword, the literal string is empty.
// If the returned token is token.SEMICOLON, the corresponding literal string
// is either ";" or "\n". The latter occurs when a newline triggers semicolon
// insertion.
//
// For all other tokens the returned literal string is the corresponding
// substring of the source string. If the returned token is ILLEGAL,
// the literal string is the offending character.
//
// In all cases, Scan advances the Scanner past the scanned token.
func (s *Scanner) Scan() (pos token.Pos, tok token.Token, lit string) {
scanAgain:
	s.skipWhitespace()

	// current token start
	pos = s.file.Pos(s.offset)

	// determine token value
	insertSemi := false
	switch ch := s.ch; {
	case isLetter(ch):
		lit = s.scanIdentifier()
		tok = token.Lookup(lit)
		switch tok {
		case token.IDENT,
			token.BREAK, token.CONTINUE, token.FALLTHROUGH, token.RETURN:
			insertSemi = true
		}
		if tok.IsKeyword() {
			lit = "" // keywords don't carry a literal string
		}
	case '0' <= ch && ch <= '9':
		insertSemi = true
		tok, lit = s.scanNumber()
	default:
		s.next() // always make progress
		switch ch {
		case -1:
			if s.insertSemi {
				s.insertSemi = false // EOF consumed insertSemi
				return pos, token.SEMICOLON, "\n"
			}
			tok = token.EOF
		case '\n':
			// we only reach here if s.insertSemi was set
			s.insertSemi = false // newline consumed insertSemi
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
				// comment — collect directives, then re-scan for the next token
				_, directive := s.scanComment()
				if directive != "" {
					s.directives = append(s.directives, Directive{
						Pos:  pos,
						Text: directive,
					})
				}
				// insertSemi is preserved; a newline after a line comment
				// will trigger semicolon insertion on the next call.
				goto scanAgain
			}
			tok = s.switch2(token.QUO, token.QUO_ASSIGN)
		case '.':
			// fractions starting with a dot, e.g. .5, .5e10
			if '0' <= s.ch && s.ch <= '9' {
				insertSemi = true
				tok = token.FLOAT
				floatOffs := s.offset - 1 // include the leading '.'
				s.scanMantissa(10)
				if s.ch == 'e' || s.ch == 'E' {
					s.next()
					if s.ch == '-' || s.ch == '+' {
						s.next()
					}
					s.scanMantissa(10)
				}
				lit = string(s.src[floatOffs:s.offset])
				break
			}
			if s.ch == '.' && s.peek() == '.' {
				s.next()
				s.next() // consume second and third '.'
				tok = token.ELLIPSIS
			} else {
				tok = token.PERIOD
			}
		case ',':
			tok = token.COMMA
		case ';':
			tok = token.SEMICOLON
			lit = ";"
		case ':':
			tok = s.switch2(token.COLON, token.DEFINE)
		case '(':
			tok = token.LPAREN
		case '[':
			tok = token.LBRACK
		case '{':
			tok = token.LBRACE
		case ')':
			insertSemi = true
			tok = token.RPAREN
		case ']':
			insertSemi = true
			tok = token.RBRACK
		case '}':
			insertSemi = true
			tok = token.RBRACE
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
		case '&':
			if s.ch == '^' {
				s.next()
				tok = s.switch2(token.AND_NOT, token.AND_NOT_ASSIGN)
			} else {
				tok = s.switch3(token.AND, token.AND_ASSIGN, '&', token.LAND)
			}
		case '|':
			tok = s.switch3(token.OR, token.OR_ASSIGN, '|', token.LOR)
		default:
			// next reports unexpected BOM - don't repeat
			if ch != bom {
				s.error(s.file.Offset(pos), fmt.Sprintf("illegal character %#U", ch))
			}
			insertSemi = s.insertSemi // preserve insertSemi info
			tok = token.ILLEGAL
			lit = string(ch)
		}
	}

	s.insertSemi = insertSemi
	return
}
