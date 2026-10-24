package compat

import (
	"pluto/io"
	"pluto/os"
	"pluto/strconv"
	"pluto/strings"
	"pluto/unicode/utf8"
	"unsafe"
)

// Formatting of basic operands without pluto/fmt (tools/go2pu
// stringers.go). A call of a fmt printing function whose operands are all
// strings, integers and booleans (after the operands with a String or Error
// method have been replaced by its result) is translated to Sprintfb,
// Fprintfb or Printfb with each operand an Arg. The operands are never
// converted to interfaces and no method is called, so the escape analysis
// sees what the formatting does with them: it copies their bytes into a new
// string.
//
// go2pu routes a call here only when its format is a constant whose verbs
// are those below, one per operand: Sprintfb prints what fmt.Sprintf would
// for these, and nothing is defined for other formats.
//
//	string   %v %s %T (flags -, width, precision), %q (-, width), %x %X
//	         (%T: the name compat.TypeName gives of a %T operand)
//	integer  %d (flags - 0 + space, width), %v %x %X (- 0, width), %c %q (-, width)
//	bool     %v %t (-, width)

// An Arg is one operand of Sprintfb.
type Arg struct {
	kind uint8
	s    string
	u    uint64 // an integer's bits; 1 for true
}

const (
	argString uint8 = iota + 1
	argInt
	argUint
	argBool
)

// ArgS is a string operand.
func ArgS(s string) Arg { return Arg{kind: argString, s: s} }

// ArgI is a signed integer operand.
func ArgI(i int64) Arg { return Arg{kind: argInt, u: uint64(i)} }

// ArgU is an unsigned integer operand.
func ArgU(u uint64) Arg { return Arg{kind: argUint, u: u} }

// ArgB is a bool operand.
func ArgB(b bool) Arg {
	if b {
		return Arg{kind: argBool, u: 1}
	}
	return Arg{kind: argBool}
}

// Sprintfb is fmt.Sprintf of basic operands, into a new string from
// alloc.Default().
func Sprintfb(format string, args ...Arg) string {
	var b fbuf
	printfb(&b, format, args)
	if b.big.Len() > 0 {
		return b.big.String()
	}
	if b.n == 0 {
		return ""
	}
	out := Make[byte](b.n)
	copy(out, b.small[:b.n])
	return unsafe.String(unsafe.SliceData(out), b.n)
}

// Fprintfb is fmt.Fprintf of basic operands: one write of the whole text.
func Fprintfb(w io.Writer, format string, args ...Arg) (int, error) {
	var b fbuf
	printfb(&b, format, args)
	if b.big.Len() > 0 {
		return w.Write(b.big.Bytes())
	}
	return w.Write(b.small[:b.n])
}

// fbuf is the text being formatted: in the frame while it is short, so
// that a short result is one allocation of its size (memory from
// alloc.Default() is not freed, and a Buffer grown byte by byte leaves
// every smaller array behind), and in a Buffer past that.
type fbuf struct {
	small [256]byte
	n     int
	big   Buffer
}

func (b *fbuf) spill(more int) {
	b.big.Grow(2 * (b.n + more))
	b.big.Write(b.small[:b.n])
	b.n = 0
}

// Write appends p.
func (b *fbuf) Write(p []byte) {
	if b.big.Len() == 0 && b.n+len(p) <= len(b.small) {
		b.n += copy(b.small[b.n:], p)
		return
	}
	if b.big.Len() == 0 {
		b.spill(len(p))
	}
	b.big.Write(p)
}

// WriteString appends s.
func (b *fbuf) WriteString(s string) {
	if b.big.Len() == 0 && b.n+len(s) <= len(b.small) {
		b.n += copy(b.small[b.n:], s)
		return
	}
	if b.big.Len() == 0 {
		b.spill(len(s))
	}
	b.big.WriteString(s)
}

// WriteByte appends c.
func (b *fbuf) WriteByte(c byte) {
	if b.big.Len() == 0 && b.n < len(b.small) {
		b.small[b.n] = c
		b.n++
		return
	}
	if b.big.Len() == 0 {
		b.spill(1)
	}
	b.big.WriteByte(c)
}

// WriteRune appends the UTF-8 encoding of r.
func (b *fbuf) WriteRune(r rune) {
	var tmp [utf8.UTFMax]byte
	n := utf8.EncodeRune(tmp[:], r)
	b.Write(tmp[:n])
}

// Printfb is fmt.Printf of basic operands.
func Printfb(format string, args ...Arg) (int, error) {
	return Fprintfb(os.Stdout, format, args...)
}

// argFlags are the flags of one verb.
type argFlags struct {
	minus, zero, plus, space bool
	wid, prec                int // -1: absent
}

func printfb(b *fbuf, format string, args []Arg) {
	n := 0
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' {
			b.WriteByte(c)
			continue
		}
		i++
		var f argFlags
		f.wid, f.prec = -1, -1
		for more := true; more && i < len(format); {
			switch format[i] {
			case '-':
				f.minus = true
				f.zero = false // fmt pads with zeros only to the left
			case '0':
				f.zero = !f.minus
			case '+':
				f.plus = true
			case ' ':
				f.space = true
			default:
				more = false
			}
			if more {
				i++
			}
		}
		for ; i < len(format) && format[i] >= '0' && format[i] <= '9'; i++ {
			if f.wid < 0 {
				f.wid = 0
			}
			f.wid = f.wid*10 + int(format[i]-'0')
		}
		if i < len(format) && format[i] == '.' {
			i++
			f.prec = 0
			for ; i < len(format) && format[i] >= '0' && format[i] <= '9'; i++ {
				f.prec = f.prec*10 + int(format[i]-'0')
			}
		}
		if i >= len(format) {
			break
		}
		verb := format[i]
		if verb == '%' {
			b.WriteByte('%')
			continue
		}
		if n >= len(args) {
			break
		}
		a := args[n]
		n++
		switch a.kind {
		case argString:
			fmtString(b, &f, verb, a.s)
		case argBool:
			if a.u != 0 {
				padb(b, &f, "true")
			} else {
				padb(b, &f, "false")
			}
		case argInt, argUint:
			fmtInt(b, &f, verb, a.u, a.kind == argInt)
		}
	}
}

func fmtString(b *fbuf, f *argFlags, verb byte, s string) {
	switch verb {
	case 'q':
		padb(b, f, strconv.Quote(s))
	case 'x', 'X':
		digits := "0123456789abcdef"
		if verb == 'X' {
			digits = "0123456789ABCDEF"
		}
		for i := 0; i < len(s); i++ {
			b.WriteByte(digits[s[i]>>4])
			b.WriteByte(digits[s[i]&0xF])
		}
	default:
		if f.prec >= 0 {
			// The precision is a number of runes.
			k := 0
			for i := range s {
				if k == f.prec {
					s = strings.Slice(s, 0, i)
					break
				}
				k++
			}
		}
		padb(b, f, s)
	}
}

func fmtInt(b *fbuf, f *argFlags, verb byte, u uint64, signed bool) {
	switch verb {
	case 'c':
		// U+FFFD past the last code point, as fmt's %c.
		r := rune(utf8.RuneError)
		if u <= utf8.MaxRune {
			r = rune(u)
		}
		if !f.minus {
			writeSpaces(b, f.wid-1)
		}
		b.WriteRune(r)
		if f.minus {
			writeSpaces(b, f.wid-1)
		}
		return
	case 'q':
		r := rune(utf8.RuneError)
		if u <= utf8.MaxRune {
			r = rune(u)
		}
		padb(b, f, strconv.QuoteRune(r))
		return
	}
	base := uint64(10)
	digits := "0123456789abcdef"
	switch verb {
	case 'x':
		base = 16
	case 'X':
		base = 16
		digits = "0123456789ABCDEF"
	}
	negative := signed && int64(u) < 0
	if negative {
		u = ^u + 1
	}
	prec := 0
	if f.zero && !f.minus && f.wid >= 0 {
		prec = f.wid
		if negative || f.plus || f.space {
			prec-- // the sign takes a column
		}
	}
	var buf [68]byte
	i := len(buf)
	for u >= base {
		i--
		buf[i] = digits[u%base]
		u /= base
	}
	i--
	buf[i] = digits[u]
	for i > 0 && prec > len(buf)-i {
		i--
		buf[i] = '0'
	}
	switch {
	case negative:
		i--
		buf[i] = '-'
	case f.plus:
		i--
		buf[i] = '+'
	case f.space:
		i--
		buf[i] = ' '
	}
	// Zero padding is done: what is left of the width is spaces.
	zero := f.zero
	f.zero = false
	padBytes(b, f, buf[i:])
	f.zero = zero
}

// padb writes s padded with spaces to the width, counted in runes.
func padb(b *fbuf, f *argFlags, s string) {
	w := f.wid - utf8.RuneCountInString(s)
	if !f.minus {
		writeSpaces(b, w)
	}
	b.WriteString(s)
	if f.minus {
		writeSpaces(b, w)
	}
}

// padBytes is padb of ASCII bytes.
func padBytes(b *fbuf, f *argFlags, p []byte) {
	w := f.wid - len(p)
	if !f.minus {
		writeSpaces(b, w)
	}
	b.Write(p)
	if f.minus {
		writeSpaces(b, w)
	}
}

func writeSpaces(b *fbuf, n int) {
	for ; n > 0; n-- {
		b.WriteByte(' ')
	}
}
