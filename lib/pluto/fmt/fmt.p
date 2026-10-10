// Package fmt formats values, in the style of Go's fmt. Fprintf and Printf
// write to an io.Writer through a fixed buffer without allocating; Appendf,
// Append and Appendln append to a *vec.Vec[byte], which grows, or a
// *vec.Fixed[byte], which keeps what fits in a caller's array and never
// allocates; Sprintf, Sprint and Sprintln return a new string, allocated
// from the current allocator.
//
// Verbs: %v %d %b %o %O %x %X %c %q %U %e %E %f %F %g %G %s %t %p %T %%.
// Flags: + - # 0 space. Width and precision may be numbers or *.
//
// %v prints values as Go's fmt does: basic values, errors (Error),
// Stringers (String), and arrays, slices, structs, pointers to them and
// interfaces by walking their type descriptors (value.p). %+v adds struct
// field names and %#v prints Go syntax (main.T{A:1, B:[]int{2}}), unless
// the build strips field names (--fieldnames=false, the default in kernel
// mode). Containers print through their methods: a Mapper (hash.Map) like
// a Go map, map[k:v] with sorted keys, and a Lister (vec.Vec) like a slice.
// A union without a String method prints as the variant it holds, followed
// by its payload in parentheses: Some(5), None. %s, %q, %x and %X apply to
// an error's message. %w is accepted only by Errorf and ErrorBuf.Errorf
// (see errorf.p), where it prints the error like %v and wraps it.
//
// Strings and runes quote as Go's do: %q uses strconv.Quote's escapes,
// %+q escapes everything outside ASCII, %#q backquotes a string that
// strconv.CanBackquote accepts, and %#U adds the quoted character if it
// is printable. Width, precision and the - and 0 flags apply as in Go;
// widths count runes, and a byte that is not valid UTF-8 is one rune.
//
// Formatting never allocates: output goes through the printer's fixed
// buffer. Only its destination may (a growing vec.Vec, or the string of
// Sprintf).
package fmt

import (
	"pluto/io"
	"pluto/rt"
	"pluto/strconv"
	"pluto/unicode/utf8"
	"pluto/vec"
	"unsafe"
)

// A Stringer has a String method used by %v and %s.
type Stringer interface {
	String() string
}

// A GoStringer has a GoString method used by %#v.
type GoStringer interface {
	GoString() string
}

// printer accumulates output and flushes it to a writer or a bounded
// vector.
type printer struct {
	buf [256]byte
	n   int

	w     io.Writer        // destination, or nil when appending to fixed
	fixed *vec.Fixed[byte] // destination of Appendf into a Fixed, and ErrorBuf

	written int
	err     error

	wrapErrs bool             // %w is allowed (Errorf)
	wrapped  vec.Fixed[error] // the %w operands, as many as fit

	// current directive
	plus, minus, sharp, space, zero bool
	wid, prec                       int
	hasWid, hasPrec                 bool
	sharpV, plusV                   bool // %#v and %+v (sharp and plus are then false)

	// the value being printed, for %!verb(type=value)
	curT     *rt.Type
	curP     unsafe.Pointer
	erroring bool // printing a bad verb's value: call no methods
}

func (p *printer) flush() {
	if p.n == 0 {
		return
	}
	if p.w != nil {
		if p.err == nil {
			m, err := p.w.Write(p.buf[:p.n])
			p.written += m
			p.err = err
		}
	} else {
		k := p.fixed.Len()
		p.fixed.Append(p.buf[:p.n]...)
		p.written += p.fixed.Len() - k
	}
	p.n = 0
}

func (p *printer) writeByte(c byte) {
	if p.n == len(p.buf) {
		p.flush()
	}
	p.buf[p.n] = c
	p.n++
}

func (p *printer) writeString(s string) {
	for i := 0; i < len(s); i++ {
		p.writeByte(s[i])
	}
}

func (p *printer) writeBytes(b []byte) {
	for _, c := range b {
		p.writeByte(c)
	}
}

func (p *printer) pad(n int, c byte) {
	for i := 0; i < n; i++ {
		p.writeByte(c)
	}
}

// writePadding writes n bytes of padding, as Go's writePadding does: zeros
// with the 0 flag unless the field is left-justified, spaces otherwise.
func (p *printer) writePadding(n int) {
	if p.zero && !p.minus {
		p.pad(n, '0')
	} else {
		p.pad(n, ' ')
	}
}

// padBefore writes the padding that goes before a field of n runes, and
// padAfter the padding that goes after it (the - flag).
func (p *printer) padBefore(n int) {
	if p.hasWid && !p.minus {
		p.writePadding(p.wid - n)
	}
}

func (p *printer) padAfter(n int) {
	if p.hasWid && p.minus {
		p.writePadding(p.wid - n)
	}
}

// field writes s padded to the current width in runes, as Go's pad does.
// Bytes that are not valid UTF-8 count as one rune each.
func (p *printer) field(s []byte) {
	n := 0
	if p.hasWid {
		n = utf8.RuneCount(s)
	}
	p.padBefore(n)
	p.writeBytes(s)
	p.padAfter(n)
}

// fieldNoZero is field for numbers, which handle the 0 flag themselves
// (or ignore it): any padding it adds is spaces.
func (p *printer) fieldNoZero(s []byte) {
	zero := p.zero
	p.zero = false
	p.field(s)
	p.zero = zero
}

func (p *printer) fieldString(s string) {
	p.field(unsafe.Slice(unsafe.StringData(s), len(s)))
}

// ----------------------------------------------------------------------------
// Integers

// fmtInteger formats the magnitude u of an integer whose sign is neg, as
// Go's fmtInteger does. The precision, or the width with the 0 flag, is
// the minimum number of digits (less one for a sign); the # flag adds a
// 0b, 0 or 0x prefix and %O a 0o prefix, outside those digits. The
// leading zeros are written as they go, so neither has a limit.
func (p *printer) fmtInteger(u uint64, neg bool, base int, verb rune, upper bool) {
	prec := 0
	if p.hasPrec {
		prec = p.prec
		// Precision 0 and value 0 print nothing but the padding.
		if prec == 0 && u == 0 {
			zero := p.zero
			p.zero = false
			p.writePadding(p.wid)
			p.zero = zero
			return
		}
	} else if p.zero && !p.minus && p.hasWid {
		prec = p.wid
		if neg || p.plus || p.space {
			prec-- // leave room for the sign
		}
	}
	var digits [64]byte
	n := strconv.PutUint(digits[:], u, base)
	body := digits[len(digits)-n:]
	if upper {
		for i := range body {
			if body[i] >= 'a' && body[i] <= 'z' {
				body[i] -= 'a' - 'A'
			}
		}
	}
	zeros := max(prec-n, 0)

	// The prefix, in the order it is written: sign, 0o, then # prefix.
	var prefix [5]byte
	k := 0
	switch {
	case neg:
		prefix[k] = '-'
		k++
	case p.plus:
		prefix[k] = '+'
		k++
	case p.space:
		prefix[k] = ' '
		k++
	}
	if verb == 'O' {
		prefix[k], prefix[k+1] = '0', 'o'
		k += 2
	}
	if p.sharp {
		switch base {
		case 2:
			prefix[k], prefix[k+1] = '0', 'b'
			k += 2
		case 8:
			// A leading 0, unless there is one already.
			if zeros == 0 && body[0] != '0' {
				prefix[k] = '0'
				k++
			}
		case 16:
			prefix[k] = '0'
			if upper {
				prefix[k+1] = 'X'
			} else {
				prefix[k+1] = 'x'
			}
			k += 2
		}
	}

	width := k + zeros + n
	zero := p.zero
	p.zero = false
	p.padBefore(width)
	p.writeBytes(prefix[:k])
	p.pad(zeros, '0')
	p.writeBytes(body)
	p.padAfter(width)
	p.zero = zero
}

func (p *printer) printInt(v int64, verb rune) {
	neg := v < 0
	u := uint64(v)
	if neg {
		u = uint64(-v)
	}
	switch verb {
	case 'v':
		// Signed integers print in decimal even for %#v.
		p.fmtInteger(u, neg, 10, verb, false)
	case 'c', 'q', 'U':
		// These take the value's bits, as Go's do: a negative value is
		// beyond utf8.MaxRune, and %U prints its two's complement.
		p.printNumber(uint64(v), false, verb)
	default:
		p.printNumber(u, neg, verb)
	}
}

// printNumber formats an unsigned integer, or the magnitude of a signed
// one whose sign is neg.
func (p *printer) printNumber(u uint64, neg bool, verb rune) {
	switch verb {
	case 'v':
		if p.sharpV {
			p.fmt0x64(u, true)
		} else {
			p.fmtInteger(u, neg, 10, verb, false)
		}
	case 'd':
		p.fmtInteger(u, neg, 10, verb, false)
	case 'b':
		p.fmtInteger(u, neg, 2, verb, false)
	case 'o', 'O':
		p.fmtInteger(u, neg, 8, verb, false)
	case 'x':
		p.fmtInteger(u, neg, 16, verb, false)
	case 'X':
		p.fmtInteger(u, neg, 16, verb, true)
	case 'c':
		p.fmtC(u)
	case 'q':
		p.fmtQc(u)
	case 'U':
		p.fmtUnicode(u)
	default:
		p.badVerb(verb)
	}
}

// fmt0x64 formats u in hexadecimal, with a leading 0x if leading0x.
func (p *printer) fmt0x64(u uint64, leading0x bool) {
	sharp := p.sharp
	p.sharp = leading0x
	p.fmtInteger(u, false, 16, 'v', false)
	p.sharp = sharp
}

// toRune converts the value of %c or %q to a rune, as Go's fmtC and fmtQc
// do: a value beyond utf8.MaxRune is utf8.RuneError.
func toRune(u uint64) rune {
	if u > utf8.MaxRune {
		return utf8.RuneError
	}
	return rune(u)
}

// writeRune writes the UTF-8 encoding of r, or of utf8.RuneError if r is
// not a valid code point.
func (p *printer) writeRune(r rune) {
	var b [utf8.UTFMax]byte
	n := utf8.EncodeRune(b[:], r)
	p.writeBytes(b[:n])
}

// fmtC formats u as the character it encodes (%c).
func (p *printer) fmtC(u uint64) {
	var b [utf8.UTFMax]byte
	n := utf8.EncodeRune(b[:], toRune(u))
	p.field(b[:n])
}

// fmtQc formats u as a single-quoted Go character literal (%q), as
// strconv.QuoteRune does, or strconv.QuoteRuneToASCII with the + flag. A
// value that is not a valid code point is quoted as utf8.RuneError.
func (p *printer) fmtQc(u uint64) {
	r := toRune(u)
	if !utf8.ValidRune(r) {
		r = utf8.RuneError
	}
	n := 0
	if p.hasWid {
		n = 2 + p.escapedRune(r, '\'', false)
	}
	p.padBefore(n)
	p.writeByte('\'')
	p.escapedRune(r, '\'', true)
	p.writeByte('\'')
	p.padAfter(n)
}

// fmtUnicode formats u as a Unicode code point, U+0078, with at least
// the precision's hexadecimal digits (four by default). The # flag adds
// the character, quoted, if it is printable: U+0078 'x'. The 0 flag does
// not apply.
func (p *printer) fmtUnicode(u uint64) {
	prec := 4
	if p.hasPrec && p.prec > 4 {
		prec = p.prec
	}
	digits := 1
	for v := u; v >= 16; v >>= 4 {
		digits++
	}
	zeros := max(prec-digits, 0)
	quoted := p.sharp && u <= utf8.MaxRune && strconv.IsPrint(rune(u))
	n := 2 + zeros + digits
	if quoted {
		n += 4 // " 'x'"
	}
	zero := p.zero
	p.zero = false
	p.padBefore(n)
	p.writeString("U+")
	p.pad(zeros, '0')
	const udigits = "0123456789ABCDEF"
	for s := (digits - 1) * 4; s >= 0; s -= 4 {
		p.writeByte(udigits[u>>uint(s)&0xF])
	}
	if quoted {
		p.writeString(" '")
		p.writeRune(rune(u))
		p.writeByte('\'')
	}
	p.padAfter(n)
	p.zero = zero
}

// ----------------------------------------------------------------------------
// Strings

func (p *printer) printString(s string, verb rune) {
	switch verb {
	case 'v':
		if p.sharpV {
			p.fmtQ(s)
		} else {
			p.fmtS(s)
		}
	case 's':
		p.fmtS(s)
	case 'q':
		p.fmtQ(s)
	case 'x', 'X':
		p.fmtSbx(s, verb == 'X')
	default:
		p.badVerb(verb)
	}
}

// fmtSbx formats the bytes of s in hexadecimal, as Go's %x does: # adds a
// leading 0x (before each byte with the space flag), and space separates
// the bytes.
func (p *printer) fmtSbx(s string, upper bool) {
	hex := "0123456789abcdef"
	x := byte('x')
	if upper {
		hex = "0123456789ABCDEF"
		x = 'X'
	}
	length := len(s)
	if p.hasPrec && p.prec < length {
		length = p.prec
	}
	width := 2 * length
	if width == 0 {
		if p.hasWid {
			p.writePadding(p.wid)
		}
		return
	}
	if p.space {
		if p.sharp {
			width *= 2
		}
		width += length - 1
	} else if p.sharp {
		width += 2
	}
	if p.hasWid && p.wid > width && !p.minus {
		p.writePadding(p.wid - width)
	}
	if p.sharp {
		p.writeByte('0')
		p.writeByte(x)
	}
	for i := 0; i < length; i++ {
		if p.space && i > 0 {
			p.writeByte(' ')
			if p.sharp {
				p.writeByte('0')
				p.writeByte(x)
			}
		}
		p.writeByte(hex[s[i]>>4])
		p.writeByte(hex[s[i]&15])
	}
	if p.hasWid && p.wid > width && p.minus {
		p.writePadding(p.wid - width)
	}
}

// truncate cuts s to the precision, if any, in runes; a byte that is not
// valid UTF-8 counts as one rune. s may view a []byte, so its bytes need
// not be valid UTF-8 (rt.SliceBytewise).
func (p *printer) truncate(s string) string {
	if p.hasPrec {
		n := p.prec
		for i := 0; i < len(s); {
			n--
			if n < 0 {
				return rt.SliceBytewise(s, 0, i)
			}
			_, w := utf8.DecodeRuneInString(rt.SliceBytewise(s, i, len(s)))
			i += w
		}
	}
	return s
}

// fmtS formats a string (%s, %v): truncated to the precision, then padded.
func (p *printer) fmtS(s string) {
	p.fieldString(p.truncate(s))
}

// fmtQ formats a string as a double-quoted Go string literal (%q), as Go's
// fmtQ does: truncated to the precision first, then quoted by
// strconv.Quote's rules, or strconv.QuoteToASCII's with the + flag. With
// the # flag a string that strconv.CanBackquote accepts is backquoted
// instead. The output is streamed: padding a field counts its runes in a
// first pass that writes nothing.
func (p *printer) fmtQ(s string) {
	s = p.truncate(s)
	if p.sharp && strconv.CanBackquote(s) {
		n := 0
		if p.hasWid {
			n = 2 + utf8.RuneCountInString(s)
		}
		p.padBefore(n)
		p.writeByte('`')
		p.writeString(s)
		p.writeByte('`')
		p.padAfter(n)
		return
	}
	n := 0
	if p.hasWid {
		n = p.quote(s, false)
	}
	p.padBefore(n)
	p.quote(s, true)
	p.padAfter(n)
}

// quote writes s as a double-quoted string literal and returns the number
// of runes in the literal; with emit false it only counts them. A byte
// that is not valid UTF-8 is written as \x and two hex digits.
func (p *printer) quote(s string, emit bool) int {
	n := 2
	if emit {
		p.writeByte('"')
	}
	for i := 0; i < len(s); {
		r, w := utf8.DecodeRuneInString(rt.SliceBytewise(s, i, len(s)))
		if w == 1 && r == utf8.RuneError {
			n += p.escapeHex('x', uint64(s[i]), 2, emit)
		} else {
			n += p.escapedRune(r, '"', emit)
		}
		i += w
	}
	if emit {
		p.writeByte('"')
	}
	return n
}

// escapedRune writes r as it appears inside a literal delimited by quote,
// as strconv's appendEscapedRune does: printable runes as themselves
// (only ASCII ones with the + flag), others as escapes. It returns the
// number of runes written; with emit false it only counts them.
func (p *printer) escapedRune(r rune, quote byte, emit bool) int {
	if r == rune(quote) || r == '\\' { // always backslashed
		if emit {
			p.writeByte('\\')
			p.writeByte(byte(r))
		}
		return 2
	}
	if p.plus {
		if r < utf8.RuneSelf && strconv.IsPrint(r) {
			if emit {
				p.writeByte(byte(r))
			}
			return 1
		}
	} else if strconv.IsPrint(r) {
		if emit {
			p.writeRune(r)
		}
		return 1
	}
	var c byte
	switch r {
	case '\a':
		c = 'a'
	case '\b':
		c = 'b'
	case '\f':
		c = 'f'
	case '\n':
		c = 'n'
	case '\r':
		c = 'r'
	case '\t':
		c = 't'
	case '\v':
		c = 'v'
	}
	if c != 0 {
		if emit {
			p.writeByte('\\')
			p.writeByte(c)
		}
		return 2
	}
	switch {
	case r < ' ' || r == 0x7f:
		return p.escapeHex('x', uint64(byte(r)), 2, emit)
	case !utf8.ValidRune(r):
		return p.escapeHex('u', 0xFFFD, 4, emit)
	case r < 0x10000:
		return p.escapeHex('u', uint64(r), 4, emit)
	}
	return p.escapeHex('U', uint64(r), 8, emit)
}

// escapeHex writes a backslash, c and v in n lowercase hex digits, and
// returns the number of bytes; with emit false it only counts them.
func (p *printer) escapeHex(c byte, v uint64, n int, emit bool) int {
	if emit {
		const lowerhex = "0123456789abcdef"
		p.writeByte('\\')
		p.writeByte(c)
		for s := (n - 1) * 4; s >= 0; s -= 4 {
			p.writeByte(lowerhex[v>>uint(s)&0xF])
		}
	}
	return 2 + n
}

func (p *printer) printBool(b bool, verb rune) {
	if verb != 'v' && verb != 't' {
		p.badVerb(verb)
		return
	}
	if b {
		p.fieldString("true")
	} else {
		p.fieldString("false")
	}
}

// badVerb reports a verb that does not apply to the value being printed, as
// Go does: %!d(string=hi), or %!d(<nil>) for a nil interface.
func (p *printer) badVerb(verb rune) {
	p.erroring = true
	p.writeString("%!")
	p.writeRune(verb)
	p.writeByte('(')
	if t := p.curT; t != nil {
		p.writeString(t.Name())
		p.writeByte('=')
		p.printValue(t, p.curP, 'v', 0, false)
	} else {
		p.writeString("<nil>")
	}
	p.writeByte(')')
	p.erroring = false
}

// ----------------------------------------------------------------------------
// Values

// printArg formats one argument for a verb, as Go's printArg does: basic
// types directly, then the argument's Error or String method, then its
// value from its type descriptor.
func (p *printer) printArg(arg any, verb rune) {
	t, word := rt.TypeOf(arg)
	if t == nil {
		p.curT, p.curP = nil, nil
		if verb == 'T' || verb == 'v' {
			p.fieldString("<nil>")
		} else {
			p.badVerb(verb)
		}
		return
	}
	// The value in memory: a direct value is the data word itself.
	ptr := word
	if t.Direct() {
		ptr = unsafe.Pointer(&word)
	}
	p.curT, p.curP = t, ptr
	switch verb {
	case 'T':
		p.fmtS(t.Name())
		return
	case 'p':
		p.fmtPointer(t, ptr, 'p')
		return
	}
	switch v := arg.(type) {
	case bool:
		p.printBool(v, verb)
	case int:
		p.printInt(int64(v), verb)
	case int8:
		p.printInt(int64(v), verb)
	case int16:
		p.printInt(int64(v), verb)
	case int32:
		p.printInt(int64(v), verb)
	case int64:
		p.printInt(v, verb)
	case uint:
		p.printNumber(uint64(v), false, verb)
	case uint8:
		p.printNumber(uint64(v), false, verb)
	case uint16:
		p.printNumber(uint64(v), false, verb)
	case uint32:
		p.printNumber(uint64(v), false, verb)
	case uint64:
		p.printNumber(v, false, verb)
	case uintptr:
		p.printNumber(uint64(v), false, verb)
	case float32:
		// Floats pass as bits, which kernel builds can move without the
		// FPU (float.p, float_kernel.p).
		p.printFloatBits(uint64(*(*uint32)(unsafe.Pointer(&v))), 32, verb)
	case float64:
		p.printFloatBits(*(*uint64)(unsafe.Pointer(&v)), 64, verb)
	case string:
		p.printString(v, verb)
	case []byte:
		p.fmtBytes(v, verb, "[]byte", t, ptr)
	default:
		if !p.handleMethods(arg, verb) {
			p.printValue(t, ptr, verb, 0, false)
		}
	}
}

// handleMethods prints arg with its Error, String or GoString method if the
// verb calls for one, as Go's handleMethods does, and reports whether it
// did. %w takes an error and is otherwise %v.
func (p *printer) handleMethods(arg any, verb rune) bool {
	if p.erroring {
		return false
	}
	if verb == 'w' {
		err, ok := arg.(error)
		if !ok || !p.wrapErrs {
			p.badVerb(verb)
			return true
		}
		p.wrapped.TryPush(err)
		verb = 'v'
	}
	if p.sharpV {
		if g, ok := arg.(GoStringer); ok {
			p.printString(g.GoString(), 's')
			return true
		}
		return false
	}
	switch verb {
	case 'v', 's', 'x', 'X', 'q':
		switch v := arg.(type) {
		case error:
			p.printString(v.Error(), verb)
			return true
		case Stringer:
			p.printString(v.String(), verb)
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------------
// Directives

func (p *printer) doPrintf(format string, args []any) {
	argi := 0
	for i := 0; i < len(format); {
		c := format[i]
		if c != '%' {
			p.writeByte(c)
			i++
			continue
		}
		i++
		p.plus, p.minus, p.sharp, p.space, p.zero = false, false, false, false, false
		p.hasWid, p.hasPrec, p.wid, p.prec = false, false, 0, 0
	flags:
		for i < len(format) {
			switch format[i] {
			case '+':
				p.plus = true
			case '-':
				p.minus = true
			case '#':
				p.sharp = true
			case ' ':
				p.space = true
			case '0':
				p.zero = true
			default:
				break flags
			}
			i++
		}
		// Width.
		if i < len(format) && format[i] == '*' {
			i++
			if argi < len(args) {
				p.wid, p.hasWid = intArg(args[argi])
				argi++
				if p.wid < 0 {
					p.minus = true
					p.zero = false // Do not pad with zeros to the right.
					p.wid = -p.wid
				}
			}
		} else {
			for i < len(format) && format[i] >= '0' && format[i] <= '9' {
				p.wid = p.wid*10 + int(format[i]-'0')
				p.hasWid = true
				i++
			}
		}
		// Precision.
		if i < len(format) && format[i] == '.' {
			i++
			p.hasPrec = true
			if i < len(format) && format[i] == '*' {
				i++
				if argi < len(args) {
					p.prec, _ = intArg(args[argi])
					argi++
					if p.prec < 0 {
						p.hasPrec = false
						p.prec = 0
					}
				}
			} else {
				for i < len(format) && format[i] >= '0' && format[i] <= '9' {
					p.prec = p.prec*10 + int(format[i]-'0')
					i++
				}
			}
		}
		if i >= len(format) {
			p.writeString("%!(NOVERB)")
			break
		}
		verb := rune(format[i])
		i++
		if verb == '%' {
			p.writeByte('%')
			continue
		}
		if argi >= len(args) {
			p.writeString("%!")
			p.writeByte(byte(verb))
			p.writeString("(MISSING)")
			continue
		}
		// %#v is Go syntax and %+v adds field names; neither keeps its
		// flag's other meaning.
		p.sharpV, p.plusV = false, false
		if verb == 'v' {
			p.sharpV, p.sharp = p.sharp, false
			p.plusV, p.plus = p.plus, false
		}
		p.printArg(args[argi], verb)
		argi++
	}
	if argi < len(args) {
		p.writeString("%!(EXTRA)")
	}
}

func intArg(a any) (int, bool) {
	switch v := a.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case int32:
		return int(v), true
	case uint:
		return int(v), true
	}
	return 0, false
}

// doPrint formats args with %v, adding spaces between operands when neither
// is a string (Go's Print rule), or always when ln.
func (p *printer) doPrint(args []any, ln bool) {
	for i, a := range args {
		if i > 0 {
			_, aIsString := a.(string)
			_, prevIsString := args[i-1].(string)
			if ln || !aIsString && !prevIsString {
				p.writeByte(' ')
			}
		}
		p.plus, p.minus, p.sharp, p.space, p.zero = false, false, false, false, false
		p.hasWid, p.hasPrec, p.sharpV, p.plusV = false, false, false, false
		p.printArg(a, 'v')
	}
	if ln {
		p.writeByte('\n')
	}
}

// ----------------------------------------------------------------------------
// Entry points

// Fprintf formats to w.
func Fprintf(w io.Writer, format string, args ...any) (int, error) {
	var p printer
	p.w = w
	p.doPrintf(format, args)
	p.flush()
	return p.written, p.err
}

// Fprint formats args with %v to w.
func Fprint(w io.Writer, args ...any) (int, error) {
	var p printer
	p.w = w
	p.doPrint(args, false)
	p.flush()
	return p.written, p.err
}

// Fprintln formats args with %v to w, separated by spaces and followed by
// a newline.
func Fprintln(w io.Writer, args ...any) (int, error) {
	var p printer
	p.w = w
	p.doPrint(args, true)
	p.flush()
	return p.written, p.err
}

// Appendf formats according to a format specifier and appends the result
// to dst: a *vec.Vec[byte], which grows from its allocator, or a
// *vec.Fixed[byte], which keeps what fits in the caller's array, records
// the rest as truncated, and never allocates.
func Appendf[D vec.Appender[byte]](dst D, format string, args ...any) {
	var p printer
	var w appendWriter[D]
	p.to(dst, &w)
	p.doPrintf(format, args)
	p.flush()
}

// Append formats args with %v and appends the result to dst, adding spaces
// between operands when neither is a string.
func Append[D vec.Appender[byte]](dst D, args ...any) {
	var p printer
	var w appendWriter[D]
	p.to(dst, &w)
	p.doPrint(args, false)
	p.flush()
}

// Appendln formats args with %v, separated by spaces and followed by a
// newline, and appends the result to dst.
func Appendln[D vec.Appender[byte]](dst D, args ...any) {
	var p printer
	var w appendWriter[D]
	p.to(dst, &w)
	p.doPrint(args, true)
	p.flush()
}

// to makes dst the printer's destination: a Fixed directly, and any other
// Appender through w.
func (p *printer) to[D vec.Appender[byte]](dst D, w *appendWriter[D]) {
	if f, ok := any(dst).(*vec.Fixed[byte]); ok {
		p.fixed = f
		return
	}
	w.dst = dst
	p.w = w
}

// An appendWriter is an io.Writer that appends to an Appender.
type appendWriter[D vec.Appender[byte]] struct {
	dst D
}

func (w *appendWriter[D]) Write(b []byte) (int, error) {
	n := w.dst.Len()
	w.dst.Append(b...)
	return w.dst.Len() - n, nil
}
