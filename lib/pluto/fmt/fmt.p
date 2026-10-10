// Package fmt formats values, in the style of Go's fmt, without
// allocating: output goes to an io.Writer through a fixed buffer, or into a
// caller's byte slice.
//
// Verbs: %v %d %b %o %O %x %X %c %q %U %e %E %f %F %g %G %s %t %p %T %%.
// Flags: + - # 0 space. Width and precision may be numbers or *.
//
// %v prints basic values, errors (Error) and Stringers (String). Other
// structured values print as their type name in braces, since Plutonium has
// no reflection. %s, %q, %x and %X apply to an error's message. %w is
// accepted only by Errorf and ErrorBuf.Errorf (see errorf.p),
// where it prints the error like %v and wraps it.
package fmt

import (
	"pluto/io"
	"pluto/rt"
	"pluto/strconv"
	"unsafe"
)

// A Stringer has a String method used by %v and %s.
type Stringer interface {
	String() string
}

// printer accumulates output and flushes it to a writer or a byte slice.
type printer struct {
	buf [256]byte
	n   int

	w   io.Writer // destination, or nil when appending to dst
	dst []byte    // destination for Bprintf

	written int
	err     error

	count    bool    // only count the output (the first pass of Errorf)
	wrapErrs bool    // %w is allowed (Errorf)
	wrapped  []error // the %w operands, within the slice's capacity

	// current directive
	plus, minus, sharp, space, zero bool
	wid, prec                       int
	hasWid, hasPrec                 bool
}

func (p *printer) flush() {
	if p.n == 0 {
		return
	}
	if p.count {
		p.written += p.n
	} else if p.w != nil {
		if p.err == nil {
			m, err := p.w.Write(p.buf[:p.n])
			p.written += m
			p.err = err
		}
	} else {
		room := cap(p.dst) - len(p.dst)
		k := min(room, p.n)
		p.dst = append(p.dst, p.buf[:k]...)
		p.written += k
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

// field writes s padded to the current width.
func (p *printer) field(s []byte) {
	n := runeCount(s)
	if !p.hasWid || n >= p.wid {
		p.writeBytes(s)
		return
	}
	if p.minus {
		p.writeBytes(s)
		p.pad(p.wid-n, ' ')
		return
	}
	p.pad(p.wid-n, ' ')
	p.writeBytes(s)
}

func (p *printer) fieldString(s string) {
	p.field(unsafe.Slice(unsafe.StringData(s), len(s)))
}

func runeCount(b []byte) int {
	n := 0
	for _, c := range b {
		if c&0xC0 != 0x80 {
			n++
		}
	}
	return n
}

// ----------------------------------------------------------------------------
// Integers

func (p *printer) fmtInteger(u uint64, neg bool, base int, verb rune, upper bool) {
	var digits [80]byte
	n := strconv.PutUint(digits[:], u, base)
	body := digits[len(digits)-n:]
	if upper {
		for i := range body {
			if body[i] >= 'a' && body[i] <= 'z' {
				body[i] -= 'a' - 'A'
			}
		}
	}
	// Precision: minimum number of digits.
	var out [100]byte
	i := len(out)
	for k := len(body) - 1; k >= 0; k-- {
		i--
		out[i] = body[k]
	}
	if p.hasPrec {
		if p.prec == 0 && u == 0 {
			i = len(out)
		}
		for len(out)-i < p.prec && i > 4 {
			i--
			out[i] = '0'
		}
	}
	// Zero padding to width (not with precision or left alignment).
	zeroPad := p.zero && p.hasWid && !p.minus && !p.hasPrec
	prefixLen := 0
	if neg || p.plus || p.space {
		prefixLen++
	}
	if p.sharp || verb == 'O' {
		switch base {
		case 2, 16:
			prefixLen += 2
		case 8:
			if verb == 'O' {
				prefixLen += 2
			} else if out[i] != '0' {
				prefixLen++
			}
		}
	}
	if zeroPad {
		for len(out)-i+prefixLen < p.wid && i > 4 {
			i--
			out[i] = '0'
		}
	}
	if p.sharp || verb == 'O' {
		switch base {
		case 16:
			i--
			if upper {
				out[i] = 'X'
			} else {
				out[i] = 'x'
			}
			i--
			out[i] = '0'
		case 2:
			i--
			out[i] = 'b'
			i--
			out[i] = '0'
		case 8:
			if verb == 'O' {
				i--
				out[i] = 'o'
				i--
				out[i] = '0'
			} else if out[i] != '0' {
				i--
				out[i] = '0'
			}
		}
	}
	switch {
	case neg:
		i--
		out[i] = '-'
	case p.plus:
		i--
		out[i] = '+'
	case p.space:
		i--
		out[i] = ' '
	}
	p.field(out[i:])
}

func (p *printer) printInt(v int64, verb rune) {
	neg := v < 0
	u := uint64(v)
	if neg {
		u = uint64(-v)
	}
	p.printNumber(u, neg, verb)
}

func (p *printer) printNumber(u uint64, neg bool, verb rune) {
	switch verb {
	case 'd', 'v':
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
		p.fmtChar(rune(u))
	case 'q':
		p.fmtQuoteChar(rune(u))
	case 'U':
		p.fmtUnicode(u)
	default:
		p.badVerb(verb, "int")
	}
}

func (p *printer) fmtChar(r rune) {
	var b [4]byte
	n := encodeRune(b[:], r)
	p.field(b[:n])
}

func (p *printer) fmtQuoteChar(r rune) {
	var b [16]byte
	n := 0
	b[n] = '\''
	n++
	switch r {
	case '\'':
		b[n], b[n+1] = '\\', '\''
		n += 2
	case '\\':
		b[n], b[n+1] = '\\', '\\'
		n += 2
	case '\n':
		b[n], b[n+1] = '\\', 'n'
		n += 2
	case '\t':
		b[n], b[n+1] = '\\', 't'
		n += 2
	default:
		n += encodeRune(b[n:], r)
	}
	b[n] = '\''
	n++
	p.field(b[:n])
}

func (p *printer) fmtUnicode(u uint64) {
	var b [32]byte
	b[0], b[1] = 'U', '+'
	var hex [16]byte
	n := strconv.PutUint(hex[:], u, 16)
	i := 2
	for k := n; k < 4; k++ {
		b[i] = '0'
		i++
	}
	for _, c := range hex[len(hex)-n:] {
		if c >= 'a' {
			c -= 'a' - 'A'
		}
		b[i] = c
		i++
	}
	p.field(b[:i])
}

func encodeRune(b []byte, r rune) int {
	switch {
	case r < 0x80:
		b[0] = byte(r)
		return 1
	case r < 0x800:
		b[0] = byte(0xC0 | r>>6)
		b[1] = byte(0x80 | r&0x3F)
		return 2
	case r < 0x10000:
		b[0] = byte(0xE0 | r>>12)
		b[1] = byte(0x80 | (r>>6)&0x3F)
		b[2] = byte(0x80 | r&0x3F)
		return 3
	}
	b[0] = byte(0xF0 | r>>18)
	b[1] = byte(0x80 | (r>>12)&0x3F)
	b[2] = byte(0x80 | (r>>6)&0x3F)
	b[3] = byte(0x80 | r&0x3F)
	return 4
}

// ----------------------------------------------------------------------------
// Strings

func (p *printer) printString(s string, verb rune) {
	switch verb {
	case 'v', 's':
		if p.hasPrec && p.prec < len(s) {
			// Truncate to prec runes.
			n, i := 0, 0
			for i < len(s) && n < p.prec {
				i++
				for i < len(s) && s[i]&0xC0 == 0x80 {
					i++
				}
				n++
			}
			s = s[:i]
		}
		p.fieldString(s)
	case 'q':
		p.writeQuoted(s)
	case 'x', 'X':
		const lower = "0123456789abcdef"
		const upper = "0123456789ABCDEF"
		hex := lower
		if verb == 'X' {
			hex = upper
		}
		for i := 0; i < len(s); i++ {
			p.writeByte(hex[s[i]>>4])
			p.writeByte(hex[s[i]&15])
		}
	default:
		p.badVerb(verb, "string")
	}
}

func (p *printer) writeQuoted(s string) {
	p.writeByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"', '\\':
			p.writeByte('\\')
			p.writeByte(c)
		case '\n':
			p.writeString(`\n`)
		case '\t':
			p.writeString(`\t`)
		case '\r':
			p.writeString(`\r`)
		default:
			if c < 0x20 || c == 0x7F {
				const hex = "0123456789abcdef"
				p.writeString(`\x`)
				p.writeByte(hex[c>>4])
				p.writeByte(hex[c&15])
			} else {
				p.writeByte(c)
			}
		}
	}
	p.writeByte('"')
}

func (p *printer) printBool(b bool, verb rune) {
	if verb != 'v' && verb != 't' {
		p.badVerb(verb, "bool")
		return
	}
	if b {
		p.fieldString("true")
	} else {
		p.fieldString("false")
	}
}

func (p *printer) badVerb(verb rune, what string) {
	p.writeString("%!")
	var b [4]byte
	n := encodeRune(b[:], verb)
	p.writeBytes(b[:n])
	p.writeByte('(')
	p.writeString(what)
	p.writeByte(')')
}

// ----------------------------------------------------------------------------
// Values

// printArg formats one argument for a verb.
func (p *printer) printArg(arg any, verb rune) {
	if verb == 'T' {
		_, _, name, _ := rt.TypeInfo(arg)
		if name == "" {
			name = "<nil>"
		}
		p.fieldString(name)
		return
	}
	switch v := arg.(type) {
	case nil:
		p.fieldString("<nil>")
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
		if verb == 's' || verb == 'v' || verb == 'q' || verb == 'x' || verb == 'X' {
			p.printString(unsafe.String(unsafe.SliceData(v), len(v)), verb)
		} else {
			p.badVerb(verb, "[]byte")
		}
	case error:
		switch verb {
		case 'w':
			if !p.wrapErrs {
				p.badVerb(verb, "error")
				return
			}
			if len(p.wrapped) < cap(p.wrapped) {
				p.wrapped = append(p.wrapped, v)
			}
			p.printString(v.Error(), 'v')
		case 'v', 's', 'q', 'x', 'X':
			p.printString(v.Error(), verb)
		default:
			p.printOther(arg, verb)
		}
	case Stringer:
		if verb == 'v' || verb == 's' || verb == 'q' {
			p.printString(v.String(), verb)
		} else {
			p.printOther(arg, verb)
		}
	default:
		p.printOther(arg, verb)
	}
}

// printOther formats values of named or composite types from their kind.
func (p *printer) printOther(arg any, verb rune) {
	kind, size, name, data := rt.TypeInfo(arg)
	word := uint64(uintptr(data))
	switch kind {
	case rt.KindInt, rt.KindInt8, rt.KindInt16, rt.KindInt32, rt.KindInt64:
		// Sign-extend from the value's size.
		shift := 64 - 8*size
		p.printInt(int64(word<<shift)>>shift, verb)
	case rt.KindUint, rt.KindUint8, rt.KindUint16, rt.KindUint32, rt.KindUint64, rt.KindUintptr:
		p.printNumber(word, false, verb)
	case rt.KindBool:
		p.printBool(word != 0, verb)
	case rt.KindFloat64:
		p.printFloatBits(word, 64, verb)
	case rt.KindFloat32:
		p.printFloatBits(uint64(uint32(word)), 32, verb)
	case rt.KindString:
		p.printString(*(*string)(data), verb)
	case rt.KindPointer, rt.KindUnsafePointer, rt.KindFunc, rt.KindOwned:
		if verb == 'p' || verb == 'v' {
			p.writeString("0x")
			p.fmtInteger(word, false, 16, 'x', false)
		} else {
			p.printNumber(word, false, verb)
		}
	case rt.KindPacked:
		p.printNumber(word, false, verb)
	default:
		p.writeByte('{')
		p.writeString(name)
		p.writeByte('}')
	}
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
		p.hasWid, p.hasPrec = false, false
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

// Bprintf appends formatted text to buf within its capacity; text that does
// not fit is dropped. It returns the extended slice.
func Bprintf(buf []byte, format string, args ...any) []byte {
	var p printer
	p.dst = buf
	p.doPrintf(format, args)
	p.flush()
	return p.dst
}

// Bprint is Bprintf with %v formatting.
func Bprint(buf []byte, args ...any) []byte {
	var p printer
	p.dst = buf
	p.doPrint(args, false)
	p.flush()
	return p.dst
}
