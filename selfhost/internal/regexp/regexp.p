// Package regexp is the part of Go's regexp that pluto test needs: Compile
// and MatchString, for the -run and -bench patterns. It accepts Go's RE2
// syntax (alternation, concatenation, the repetition operators * + ? {n,m}
// and their lazy forms, groups, the flags i, m, s and U, character classes
// with ranges, negation, Perl and POSIX classes, the escapes Go accepts, and
// the empty-width assertions ^ $ \A \z \b \B) and reports the errors Go
// reports for text it rejects, in Go's words.
//
// MatchString only reports whether there is a match, so the matcher needs
// no leftmost-first priority: it is a search over (instruction, position)
// pairs, each visited at most once, which takes time proportional to the
// size of the program times the length of the text.
//
// Everything allocates from alloc.Default() and never frees, as the package
// compat does.
package regexp

import (
	"pluto/strings"
	"pluto/unicode"
	"pluto/unicode/utf8"
	"selfhost/compat"
	"selfhost/pu"
)

// A Regexp is a compiled regular expression.
type Regexp struct {
	expr string
	prog []inst
}

// String returns the source text of re.
func (re *Regexp) String() string { return re.expr }

// An Error is a syntax error in a regular expression, as Go's
// regexp/syntax.Error.
type Error struct {
	Code string // what is wrong, in Go's words
	Expr string // the part of the expression that is wrong
}

func (e *Error) Error() string {
	return compat.Concat("error parsing regexp: ", e.Code, ": `", e.Expr, "`")
}

// The error codes Go's parser reports.
const (
	errMissingParen      = "missing closing )"
	errUnexpectedParen   = "unexpected )"
	errMissingBracket    = "missing closing ]"
	errMissingRepeatArg  = "missing argument to repetition operator"
	errInvalidNestedRep  = "invalid nested repetition operator"
	errInvalidRepeatSize = "invalid repeat count"
	errInvalidEscape     = "invalid escape sequence"
	errInvalidCharRange  = "invalid character class range"
	errTrailingBackslash = "trailing backslash at end of expression"
	errInvalidPerlOp     = "invalid or unsupported Perl syntax"
	errInvalidUTF8       = "invalid UTF-8"
)

// maxRepeat is the largest count Go accepts in {n,m}.
const maxRepeat = 1000

// MustCompile is Compile that panics on an error.
func MustCompile(expr string) *Regexp {
	re, err := Compile(expr)
	if err != nil {
		panic(err.Error())
	}
	return re
}

// Compile parses a regular expression in Go's syntax.
func Compile(expr string) (*Regexp, error) {
	var p parser
	p.src = expr
	root, err := p.parse()
	if err != nil {
		return nil, err
	}
	var c compiler
	c.nodes = p.nodes
	// An unanchored search: skip any number of runes, then match.
	// 0: split 1, 3; 1: any; 2: jmp 0; 3: the expression.
	c.emit(inst{op: iSplit, x: 1, y: 3})
	c.emit(inst{op: iAny, nl: true})
	c.emit(inst{op: iJmp, x: 0})
	c.compile(root)
	c.emit(inst{op: iMatch})
	return pu.New(Regexp{expr: expr, prog: c.prog}), nil
}

// MatchString reports whether s contains a match of re.
func (re *Regexp) MatchString(s string) bool {
	n := len(re.prog)
	seen := compat.Make[bool](n * (len(s) + 1))
	var stack []state
	stack = compat.Append(stack, state{0, 0})
	for len(stack) > 0 {
		st := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		pc, pos := st.pc, st.pos
		for {
			k := pos*n + pc
			if seen[k] {
				break
			}
			seen[k] = true
			in := &re.prog[pc]
			if in.op == iMatch {
				return true
			}
			if in.op == iSplit {
				stack = compat.Append(stack, state{in.y, pos})
				pc = in.x
				continue
			}
			if in.op == iJmp {
				pc = in.x
				continue
			}
			if in.op < iRune {
				// An empty-width assertion.
				if !assert(in.op, s, pos) {
					break
				}
				pc++
				continue
			}
			if pos >= len(s) {
				break
			}
			r, size := utf8.DecodeRuneInString(strings.SliceFrom(s, pos))
			if !in.matches(r) {
				break
			}
			pc++
			pos += size
		}
	}
	return false
}

// A state is a point of the search: an instruction and a position in the
// text.
type state struct{ pc, pos int }

// The instructions of a compiled expression. The empty-width assertions
// come first, then those that consume a rune.
const (
	iBeginText = iota
	iEndText
	iBeginLine
	iEndLine
	iWordBoundary
	iNoWordBoundary
	iSplit // continue at x and at y
	iJmp   // continue at x
	iMatch
	iRune  // the rune r, folding case if fold
	iAny   // any rune, or any but newline unless nl
	iClass // a rune in ranges, or not in them if neg; folding case if fold
)

type inst struct {
	op     int
	x, y   int
	r      rune
	nl     bool
	fold   bool
	neg    bool
	ranges []rune // pairs of inclusive bounds
}

// matches reports whether the rune-consuming instruction in matches r.
func (in *inst) matches(r rune) bool {
	switch in.op {
	case iRune:
		if r == in.r {
			return true
		}
		return in.fold && foldEq(r, in.r)
	case iAny:
		return in.nl || r != '\n'
	case iClass:
		ok := inRanges(in.ranges, r)
		if !ok && in.fold {
			for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
				if inRanges(in.ranges, f) {
					ok = true
					break
				}
			}
		}
		return ok != in.neg
	}
	return false
}

func foldEq(a, b rune) bool {
	for f := unicode.SimpleFold(a); f != a; f = unicode.SimpleFold(f) {
		if f == b {
			return true
		}
	}
	return false
}

func inRanges(ranges []rune, r rune) bool {
	for i := 0; i+1 < len(ranges); i += 2 {
		if ranges[i] <= r && r <= ranges[i+1] {
			return true
		}
	}
	return false
}

// assert reports whether the empty-width assertion op holds at pos in s.
func assert(op int, s string, pos int) bool {
	switch op {
	case iBeginText:
		return pos == 0
	case iEndText:
		return pos == len(s)
	case iBeginLine:
		return pos == 0 || s[pos-1] == '\n'
	case iEndLine:
		return pos == len(s) || s[pos] == '\n'
	case iWordBoundary, iNoWordBoundary:
		before := pos > 0 && isWordByte(s[pos-1])
		after := pos < len(s) && isWordByte(s[pos])
		return (before != after) == (op == iWordBoundary)
	}
	return false
}

func isWordByte(b byte) bool {
	return b == '_' || '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z'
}

// The nodes of a parsed expression.
const (
	nEmpty   = iota // matches the empty string
	nInst           // one instruction: a rune, class, any or assertion
	nConcat         // subs in sequence
	nAlt            // one of subs
	nRepeat         // sub repeated min to max times; max < 0 is no limit
	nGroup          // a parenthesised sub-expression
)

type node struct {
	kind     int
	in       inst
	subs     []int
	min, max int
}

// A parser turns the text of an expression into nodes.
type parser struct {
	src   string
	pos   int
	nodes []node
	fold  bool // flag i
	multi bool // flag m
	dotNL bool // flag s
	lazy  bool // flag U
}

func (p *parser) add(n node) int {
	p.nodes = compat.Append(p.nodes, n)
	return len(p.nodes) - 1
}

func (p *parser) err(code, expr string) error {
	return pu.New(Error{Code: code, Expr: expr})
}

func (p *parser) more() bool { return p.pos < len(p.src) }

func (p *parser) peek() rune {
	r, _ := utf8.DecodeRuneInString(strings.SliceFrom(p.src, p.pos))
	return r
}

func (p *parser) next() rune {
	r, size := utf8.DecodeRuneInString(strings.SliceFrom(p.src, p.pos))
	p.pos += size
	return r
}

func (p *parser) parse() (int, error) {
	if !utf8.ValidString(p.src) {
		return 0, p.err(errInvalidUTF8, p.src)
	}
	n, err := p.parseAlt(0)
	if err != nil {
		return 0, err
	}
	if p.more() {
		// parseAlt stops only at a ')' with no group open.
		return 0, p.err(errUnexpectedParen, p.src)
	}
	return n, nil
}

// parseAlt parses alternatives up to a ')' or the end, at group depth.
func (p *parser) parseAlt(depth int) (int, error) {
	var alts []int
	for {
		n, err := p.parseConcat(depth)
		if err != nil {
			return 0, err
		}
		alts = compat.Append(alts, n)
		if p.more() && p.peek() == '|' {
			p.pos++
			continue
		}
		break
	}
	if len(alts) == 1 {
		return alts[0], nil
	}
	return p.add(node{kind: nAlt, subs: alts}), nil
}

func (p *parser) parseConcat(depth int) (int, error) {
	var items []int
	for p.more() {
		c := p.peek()
		if c == '|' || c == ')' {
			if c == ')' && depth == 0 {
				return 0, p.err(errUnexpectedParen, p.src)
			}
			break
		}
		start := p.pos
		n, isAtom, err := p.parseAtom(depth)
		if err != nil {
			return 0, err
		}
		if n < 0 {
			continue // a flag group that sets flags and matches nothing
		}
		if !isAtom {
			// An operator with nothing before it.
			return 0, p.err(errMissingRepeatArg, strings.Slice(p.src, start, p.pos))
		}
		n, err = p.parseRepeats(n, start)
		if err != nil {
			return 0, err
		}
		items = compat.Append(items, n)
	}
	if len(items) == 1 {
		return items[0], nil
	}
	if len(items) == 0 {
		return p.add(node{kind: nEmpty}), nil
	}
	return p.add(node{kind: nConcat, subs: items}), nil
}

// parseRepeats applies the repetition operators that follow the atom n,
// which began at start.
func (p *parser) parseRepeats(n, start int) (int, error) {
	repeated := false
	for p.more() {
		opStart := p.pos
		min, max := 0, 0
		switch p.peek() {
		case '*':
			p.pos++
			min, max = 0, -1
		case '+':
			p.pos++
			min, max = 1, -1
		case '?':
			p.pos++
			min, max = 0, 1
		case '{':
			lo, hi, ok := p.parseCount()
			if !ok {
				// A '{' that is not a count is a literal, as in Go.
				return n, nil
			}
			if lo < 0 || lo > maxRepeat || hi > maxRepeat || hi >= 0 && hi < lo {
				return 0, p.err(errInvalidRepeatSize, strings.Slice(p.src, opStart, p.pos))
			}
			min, max = lo, hi
		default:
			return n, nil
		}
		if p.more() && p.peek() == '?' {
			p.pos++
		}
		if repeated {
			return 0, p.err(errInvalidNestedRep, strings.Slice(p.src, lastOp(p.src, start, opStart), p.pos))
		}
		repeated = true
		n = p.add(node{kind: nRepeat, subs: compat.Append([]int(nil), n), min: min, max: max})
	}
	return n, nil
}

// lastOp returns where the repetition operator before opStart begins, for
// the text of a nested repetition error: Go quotes both operators.
func lastOp(src string, start, opStart int) int {
	i := opStart - 1
	if i > start && src[i] == '?' && (src[i-1] == '*' || src[i-1] == '+' || src[i-1] == '?' || src[i-1] == '}') {
		i--
	}
	if src[i] == '}' {
		for i > start && src[i] != '{' {
			i--
		}
	}
	return i
}

// parseCount parses {n}, {n,} or {n,m} at p.pos. It reports false, leaving
// p.pos alone, when the text is not a count; hi is -1 for {n,}.
func (p *parser) parseCount() (lo, hi int, ok bool) {
	i := p.pos + 1
	lo, i, ok = number(p.src, i)
	if !ok || i >= len(p.src) {
		return 0, 0, false
	}
	hi = lo
	if p.src[i] == ',' {
		i++
		if i < len(p.src) && p.src[i] == '}' {
			hi = -1
		} else {
			hi, i, ok = number(p.src, i)
			if !ok {
				return 0, 0, false
			}
		}
	}
	if i >= len(p.src) || p.src[i] != '}' {
		return 0, 0, false
	}
	p.pos = i + 1
	return lo, hi, true
}

// number parses decimal digits at i; a count too large is returned as
// maxRepeat+1.
func number(s string, i int) (int, int, bool) {
	start := i
	v := 0
	for i < len(s) && '0' <= s[i] && s[i] <= '9' {
		if v <= maxRepeat {
			v = v*10 + int(s[i]-'0')
		}
		i++
	}
	if i == start {
		return 0, i, false
	}
	if v > maxRepeat {
		v = maxRepeat + 1
	}
	return v, i, true
}

// parseAtom parses one atom. It returns -1 for a flag group that only sets
// flags, and isAtom false for a repetition operator, which cannot start
// one.
func (p *parser) parseAtom(depth int) (int, bool, error) {
	start := p.pos
	c := p.next()
	switch c {
	case '*', '+', '?':
		return 0, false, nil
	case '{':
		p.pos = start
		if _, _, ok := p.parseCount(); ok {
			return 0, false, nil
		}
		p.pos = start + 1
		return p.lit('{'), true, nil
	case '(':
		return p.parseGroup(start, depth)
	case '[':
		return p.parseClass(start)
	case '.':
		return p.add(node{kind: nInst, in: inst{op: iAny, nl: p.dotNL}}), true, nil
	case '^':
		op := iBeginText
		if p.multi {
			op = iBeginLine
		}
		return p.add(node{kind: nInst, in: inst{op: op}}), true, nil
	case '$':
		op := iEndText
		if p.multi {
			op = iEndLine
		}
		return p.add(node{kind: nInst, in: inst{op: op}}), true, nil
	case '\\':
		return p.parseEscape(start)
	}
	return p.lit(c), true, nil
}

func (p *parser) lit(r rune) int {
	return p.add(node{kind: nInst, in: inst{op: iRune, r: r, fold: p.fold && hasFold(r)}})
}

func hasFold(r rune) bool { return unicode.SimpleFold(r) != r }

// parseGroup parses a group after its '('.
func (p *parser) parseGroup(start, depth int) (int, bool, error) {
	saved := [4]bool{p.fold, p.multi, p.dotNL, p.lazy}
	if p.more() && p.peek() == '?' {
		p.pos++
		if p.more() && (p.peek() == 'P' || p.peek() == '<') {
			// A named group: (?P<name>re) or (?<name>re).
			if p.peek() == 'P' {
				p.pos++
			}
			if !p.more() || p.next() != '<' {
				return 0, false, p.err(errInvalidPerlOp, strings.Slice(p.src, start, p.pos))
			}
			end := strings.IndexByte(strings.SliceFrom(p.src, p.pos), '>')
			if end < 0 {
				return 0, false, p.err("invalid named capture", strings.SliceFrom(p.src, start))
			}
			name := strings.Slice(p.src, p.pos, p.pos+end)
			if name == "" || !isGroupName(name) {
				return 0, false, p.err("invalid named capture", strings.Slice(p.src, start, p.pos+end+1))
			}
			p.pos += end + 1
		} else {
			// Flags: (?flags) or (?flags:re).
			neg, sawFlag := false, false
			for {
				if !p.more() {
					return 0, false, p.err(errMissingParen, p.src)
				}
				c := p.next()
				switch c {
				case 'i':
					p.fold = !neg
					sawFlag = true
					continue
				case 'm':
					p.multi = !neg
					sawFlag = true
					continue
				case 's':
					p.dotNL = !neg
					sawFlag = true
					continue
				case 'U':
					p.lazy = !neg
					sawFlag = true
					continue
				case '-':
					if neg {
						return 0, false, p.err("invalid or unsupported Perl syntax", strings.Slice(p.src, start, p.pos))
					}
					neg, sawFlag = true, false
					continue
				case ')':
					if neg && !sawFlag {
						return 0, false, p.err(errInvalidPerlOp, strings.Slice(p.src, start, p.pos))
					}
					// The flags last to the end of the enclosing group.
					return -1, true, nil
				case ':':
					if neg && !sawFlag {
						return 0, false, p.err(errInvalidPerlOp, strings.Slice(p.src, start, p.pos))
					}
				default:
					return 0, false, p.err(errInvalidPerlOp, strings.Slice(p.src, start, p.pos))
				}
				break
			}
		}
	}
	sub, err := p.parseAlt(depth + 1)
	if err != nil {
		return 0, false, err
	}
	if !p.more() {
		return 0, false, p.err(errMissingParen, p.src)
	}
	p.pos++ // ')'
	p.fold, p.multi, p.dotNL, p.lazy = saved[0], saved[1], saved[2], saved[3]
	return p.add(node{kind: nGroup, subs: compat.Append([]int(nil), sub)}), true, nil
}

func isGroupName(name string) bool {
	for i := 0; i < len(name); i++ {
		if !isWordByte(name[i]) {
			return false
		}
	}
	return true
}

// parseEscape parses an escape after its backslash, outside a class.
func (p *parser) parseEscape(start int) (int, bool, error) {
	if !p.more() {
		return 0, false, p.err(errTrailingBackslash, "")
	}
	c := p.peek()
	switch c {
	case 'A':
		p.pos++
		return p.add(node{kind: nInst, in: inst{op: iBeginText}}), true, nil
	case 'z':
		p.pos++
		return p.add(node{kind: nInst, in: inst{op: iEndText}}), true, nil
	case 'b':
		p.pos++
		return p.add(node{kind: nInst, in: inst{op: iWordBoundary}}), true, nil
	case 'B':
		p.pos++
		return p.add(node{kind: nInst, in: inst{op: iNoWordBoundary}}), true, nil
	case 'Q':
		// Literal text up to \E or the end.
		p.pos++
		var items []int
		for p.more() {
			if p.src[p.pos] == '\\' && p.pos+1 < len(p.src) && p.src[p.pos+1] == 'E' {
				p.pos += 2
				break
			}
			items = compat.Append(items, p.lit(p.next()))
		}
		if len(items) == 1 {
			return items[0], true, nil
		}
		return p.add(node{kind: nConcat, subs: items}), true, nil
	}
	if ranges, neg, ok := perlClass(c); ok {
		p.pos++
		return p.add(node{kind: nInst, in: inst{op: iClass, ranges: ranges, neg: neg}}), true, nil
	}
	r, err := p.escapedRune(start)
	if err != nil {
		return 0, false, err
	}
	return p.lit(r), true, nil
}

// escapedRune parses the rune of an escape after its backslash, which is
// at start: punctuation, a control character escape, an octal or hex
// escape.
func (p *parser) escapedRune(start int) (rune, error) {
	c := p.next()
	switch c {
	case 'a':
		return '\a', nil
	case 'f':
		return '\f', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 't':
		return '\t', nil
	case 'v':
		return '\v', nil
	case '0', '1', '2', '3', '4', '5', '6', '7':
		// Octal: \0 alone, or up to three digits; \1-\7 alone are
		// backreferences, which RE2 does not support.
		if c != '0' && (!p.more() || p.peek() < '0' || p.peek() > '7') {
			break
		}
		v := rune(c - '0')
		for i := 1; i < 3 && p.more() && '0' <= p.peek() && p.peek() <= '7'; i++ {
			v = v*8 + p.next() - '0'
		}
		return v, nil
	case 'x':
		if !p.more() {
			break
		}
		if p.peek() == '{' {
			p.pos++
			v, digits := rune(0), 0
			for p.more() && p.peek() != '}' {
				d := hexVal(p.next())
				if d < 0 || v > unicode.MaxRune {
					return 0, p.err(errInvalidEscape, strings.Slice(p.src, start, p.pos))
				}
				v = v*16 + rune(d)
				digits++
			}
			if !p.more() || digits == 0 || v > unicode.MaxRune {
				return 0, p.err(errInvalidEscape, strings.Slice(p.src, start, p.pos))
			}
			p.pos++
			return v, nil
		}
		if p.pos+1 >= len(p.src) {
			break
		}
		hi, lo := hexVal(p.next()), hexVal(p.next())
		if hi < 0 || lo < 0 {
			break
		}
		return rune(hi*16 + lo), nil
	default:
		if c < utf8.RuneSelf && !isWordByte(byte(c)) {
			// Escaped punctuation stands for itself.
			return c, nil
		}
	}
	return 0, p.err(errInvalidEscape, strings.Slice(p.src, start, p.pos))
}

func hexVal(r rune) int {
	switch {
	case '0' <= r && r <= '9':
		return int(r - '0')
	case 'a' <= r && r <= 'f':
		return int(r-'a') + 10
	case 'A' <= r && r <= 'F':
		return int(r-'A') + 10
	}
	return -1
}

// perlClass returns the ranges of the Perl class \c: \d, \s or \w, or
// their negations in upper case.
func perlClass(c rune) ([]rune, bool, bool) {
	switch c {
	case 'd', 'D':
		return compat.Append([]rune(nil), '0', '9'), c == 'D', true
	case 's', 'S':
		return compat.Append([]rune(nil), '\t', '\n', '\f', '\r', ' ', ' '), c == 'S', true
	case 'w', 'W':
		return compat.Append([]rune(nil), '0', '9', 'A', 'Z', '_', '_', 'a', 'z'), c == 'W', true
	}
	return nil, false, false
}

// posixClass returns the ranges of the POSIX class [:name:].
func posixClass(name string) ([]rune, bool) {
	var r []rune
	switch name {
	case "alnum":
		r = compat.Append(r, '0', '9', 'A', 'Z', 'a', 'z')
	case "alpha":
		r = compat.Append(r, 'A', 'Z', 'a', 'z')
	case "ascii":
		r = compat.Append(r, 0, 0x7f)
	case "blank":
		r = compat.Append(r, '\t', '\t', ' ', ' ')
	case "cntrl":
		r = compat.Append(r, 0, 0x1f, 0x7f, 0x7f)
	case "digit":
		r = compat.Append(r, '0', '9')
	case "graph":
		r = compat.Append(r, '!', '~')
	case "lower":
		r = compat.Append(r, 'a', 'z')
	case "print":
		r = compat.Append(r, ' ', '~')
	case "punct":
		r = compat.Append(r, '!', '/', ':', '@', '[', '`', '{', '~')
	case "space":
		r = compat.Append(r, '\t', '\r', ' ', ' ')
	case "upper":
		r = compat.Append(r, 'A', 'Z')
	case "word":
		r = compat.Append(r, '0', '9', 'A', 'Z', '_', '_', 'a', 'z')
	case "xdigit":
		r = compat.Append(r, '0', '9', 'A', 'F', 'a', 'f')
	default:
		return nil, false
	}
	return r, true
}

// negate returns the complement of ranges over all runes.
func negate(ranges []rune) []rune {
	sorted := sortRanges(ranges)
	var out []rune
	next := rune(0)
	for i := 0; i+1 < len(sorted); i += 2 {
		if sorted[i] > next {
			out = compat.Append(out, next, sorted[i]-1)
		}
		if sorted[i+1]+1 > next {
			next = sorted[i+1] + 1
		}
	}
	if next <= unicode.MaxRune {
		out = compat.Append(out, next, unicode.MaxRune)
	}
	return out
}

// sortRanges returns a copy of ranges with the pairs sorted by their low
// bound.
func sortRanges(ranges []rune) []rune {
	s := compat.Append([]rune(nil), ranges...)
	for i := 2; i+1 < len(s); i += 2 {
		for j := i; j >= 2 && s[j] < s[j-2]; j -= 2 {
			s[j], s[j-2] = s[j-2], s[j]
			s[j+1], s[j-1] = s[j-1], s[j+1]
		}
	}
	return s
}

// parseClass parses a character class after its '['.
func (p *parser) parseClass(start int) (int, bool, error) {
	neg := false
	if p.more() && p.peek() == '^' {
		p.pos++
		neg = true
	}
	var ranges []rune
	first := true
	for {
		if !p.more() {
			return 0, false, p.err(errMissingBracket, strings.SliceFrom(p.src, start))
		}
		c := p.peek()
		if c == ']' && !first {
			p.pos++
			break
		}
		first = false
		itemStart := p.pos
		if c == '[' && p.pos+1 < len(p.src) && p.src[p.pos+1] == ':' {
			end := strings.Index(strings.SliceFrom(p.src, p.pos+2), ":]")
			if end >= 0 {
				name := strings.Slice(p.src, p.pos+2, p.pos+2+end)
				negPosix := strings.HasPrefix(name, "^")
				if negPosix {
					name = strings.SliceFrom(name, 1)
				}
				r, ok := posixClass(name)
				if !ok {
					return 0, false, p.err("invalid character class range", strings.Slice(p.src, itemStart, p.pos+4+end))
				}
				if negPosix {
					r = negate(r)
				}
				ranges = compat.Append(ranges, r...)
				p.pos += 4 + end
				continue
			}
		}
		if c == '\\' && p.pos+1 < len(p.src) {
			if r, n, ok := perlClass(rune(p.src[p.pos+1])); ok {
				if n {
					r = negate(r)
				}
				ranges = compat.Append(ranges, r...)
				p.pos += 2
				continue
			}
		}
		lo, err := p.classRune()
		if err != nil {
			return 0, false, err
		}
		hi := lo
		if p.pos+1 < len(p.src) && p.src[p.pos] == '-' && p.src[p.pos+1] != ']' {
			p.pos++
			hi, err = p.classRune()
			if err != nil {
				return 0, false, err
			}
			if hi < lo {
				return 0, false, p.err(errInvalidCharRange, strings.Slice(p.src, itemStart, p.pos))
			}
		}
		ranges = compat.Append(ranges, lo, hi)
	}
	return p.add(node{kind: nInst, in: inst{op: iClass, ranges: ranges, neg: neg, fold: p.fold}}), true, nil
}

// classRune parses one rune of a class: a literal or an escape.
func (p *parser) classRune() (rune, error) {
	start := p.pos
	c := p.next()
	if c != '\\' {
		return c, nil
	}
	if !p.more() {
		return 0, p.err(errTrailingBackslash, "")
	}
	return p.escapedRune(start)
}

// A compiler turns nodes into instructions.
type compiler struct {
	nodes []node
	prog  []inst
}

func (c *compiler) emit(in inst) int {
	c.prog = compat.Append(c.prog, in)
	return len(c.prog) - 1
}

func (c *compiler) compile(i int) {
	n := &c.nodes[i]
	switch n.kind {
	case nEmpty:
	case nInst:
		c.emit(n.in)
	case nGroup:
		c.compile(n.subs[0])
	case nConcat:
		for _, s := range n.subs {
			c.compile(s)
		}
	case nAlt:
		var jumps []int
		for k, s := range n.subs {
			if k == len(n.subs)-1 {
				c.compile(s)
				break
			}
			split := c.emit(inst{op: iSplit})
			c.prog[split].x = split + 1
			c.compile(s)
			jumps = compat.Append(jumps, c.emit(inst{op: iJmp}))
			c.prog[split].y = len(c.prog)
		}
		for _, j := range jumps {
			c.prog[j].x = len(c.prog)
		}
	case nRepeat:
		sub, min, max := n.subs[0], n.min, n.max
		for k := 0; k < min; k++ {
			c.compile(sub)
		}
		if max < 0 {
			// sub*: L: split body, end; body; jmp L.
			loop := c.emit(inst{op: iSplit})
			c.prog[loop].x = loop + 1
			c.compile(sub)
			c.emit(inst{op: iJmp, x: loop})
			c.prog[loop].y = len(c.prog)
			return
		}
		var splits []int
		for k := min; k < max; k++ {
			s := c.emit(inst{op: iSplit})
			c.prog[s].x = s + 1
			splits = compat.Append(splits, s)
			c.compile(sub)
		}
		for _, s := range splits {
			c.prog[s].y = len(c.prog)
		}
	}
}
