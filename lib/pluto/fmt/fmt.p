// Package fmt formats values, in the style of Go's fmt, without
// allocating: output goes to an io.Writer through a fixed buffer, or into a
// caller's byte slice.
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
// Formatting never allocates: output goes through the printer's fixed
// buffer.
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

// A GoStringer has a GoString method used by %#v.
type GoStringer interface {
	GoString() string
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
	sharpV, plusV                   bool // %#v and %+v (sharp and plus are then false)

	// the value being printed, for %!verb(type=value). curP is the
	// value's address, often a local variable of the function printing it,
	// and is valid only while that function runs: printValue restores the
	// previous one before returning. It is kept as a uintptr so that the
	// compiler does not count it as a pointer kept beyond the call
	// (language spec section 6.3); nothing reads it outside that window.
	curT     *rt.Type
	curP     uintptr
	erroring bool // printing a bad verb's value: call no methods
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
	if verb == 'v' {
		// Signed integers print in decimal even for %#v.
		p.fmtInteger(u, neg, 10, verb, false)
		return
	}
	p.printNumber(u, neg, verb)
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
		p.fmtChar(rune(u))
	case 'q':
		p.fmtQuoteChar(rune(u))
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
	if verb == 'v' && p.sharpV {
		p.writeQuoted(s)
		return
	}
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
			p.pad(p.wid, ' ')
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
		p.pad(p.wid-width, ' ')
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
		p.pad(p.wid-width, ' ')
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
	var b [4]byte
	n := encodeRune(b[:], verb)
	p.writeBytes(b[:n])
	p.writeByte('(')
	if t := p.curT; t != nil {
		p.writeString(t.Name())
		p.writeByte('=')
		p.printValue(t, unsafe.Pointer(p.curP), 'v', 0, false)
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
		p.curT, p.curP = nil, 0
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
	p.curT, p.curP = t, uintptr(ptr)
	switch verb {
	case 'T':
		p.fieldString(t.Name())
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
		if verb == 'w' {
			// %w takes an error operand and is otherwise %v. Only an
			// operand is wrapped, never a value found inside one.
			if !p.erroring {
				err, ok := arg.(error)
				if !ok || !p.wrapErrs {
					p.badVerb(verb)
					return
				}
				if len(p.wrapped) < cap(p.wrapped) {
					p.wrapped = append(p.wrapped, err)
				}
			}
			verb = 'v'
		}
		if !p.handleMethods(arg, verb) {
			p.printValue(t, ptr, verb, 0, false)
		}
	}
}

// handleMethods prints arg with its Error, String or GoString method if the
// verb calls for one, as Go's handleMethods does, and reports whether it
// did. A %w operand is wrapped by printArg, which calls this with %v.
func (p *printer) handleMethods(arg any, verb rune) bool {
	if p.erroring {
		return false
	}
	if verb == 'w' {
		p.badVerb(verb)
		return true
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
