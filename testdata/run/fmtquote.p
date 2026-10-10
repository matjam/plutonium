// Quoted strings and runes through fmt: %q, %+q, %#q, %x, %X, % x, %s,
// %c and %U on strings, byte slices and integers, with widths,
// precisions and the - and 0 flags, including invalid UTF-8 and runes
// that are not printable or not valid code points, and the 0 flag on
// other values (integers with a prefix among them). The last part quotes
// every code point and every two-byte sequence into a hash, which checks
// the printable-rune tables against Go's. Compared with Go's output.

package main

import (
	"pluto/fmt"
)

type label int

func (l label) String() string {
	switch l {
	case 1:
		return "<é>"
	case 2:
		return "t\tb"
	case 3:
		return "abcdef"
	}
	return "ab"
}

type quoteErr struct{}

func (quoteErr) Error() string { return "bad\tthing" }

type bstr []byte

// hasher is an FNV-1a hash of everything written to it.
type hasher struct {
	h uint64
	n int
}

func (w *hasher) Write(b []byte) (int, error) {
	for _, c := range b {
		w.h ^= uint64(c)
		w.h *= 1099511628211
	}
	w.n += len(b)
	return len(b), nil
}

// encode writes the UTF-8 encoding of r, which must be a valid code
// point, into b and returns its length.
func encode(b *[4]byte, r int) int {
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

var strs = [...]string{
	"",
	"a",
	"hello",
	"héllo",
	"日本語",
	"tab\there",
	"nl\n",
	"quote\"'`",
	"back\\slash",
	"\x00\x01\x7f",
	"\xff",
	"a\xe4\xb8b",
	"\xed\xa0\x80",
	"\xf4\x90\x80\x80",
	"\xc2\xa0nbsp",
	"\xc2\xadsoft",
	"\xef\xbb\xbfbom",
	"\U0001F600",
	"\U000e0001",
	"\xe2\x80\xa8",
	"\a\b\f\v\r",
	"\U0010ffff",
	"\xef\xbf\xbd",
	"`",
	"a\tb",
}

// Formats with a width argument, then with a width and a precision.
var strW = [...]string{
	"%*q", "%+*q", "%#*q", "%-*q", "%0*q", "%+#*q", "%-+*q", "%-#*q", "%0-*q", "%#0*q", "% *q",
	"%*x", "%*X", "% *x", "%#*x", "%# *X", "%-*x", "%0*x", "%-# *x",
	"%*s", "%-*s", "%0*s", "%*v", "%#*v", "%+*v", "%-#*v", "%0#*v",
}

var strWP = [...]string{
	"%*.*q", "%+*.*q", "%#*.*q", "%-*.*q", "%0*.*q", "%-+#*.*q",
	"%*.*x", "% *.*X", "%#*.*x", "%-# *.*x", "%0*.*x",
	"%*.*s", "%-*.*s", "%0*.*s", "%#*.*v", "%-*.*v",
}

var widths = [...]int{0, 1, 5, 14, -14}

var precs = [...]int{0, 1, 2, 4}

var runes = [...]int64{
	'a', ' ', '\'', '"', '\\', '`', '\n', '\t', '\a', 0, 0x1b, 0x7f, 0x80, 0x9f, 0xa0, 0xad,
	'é', 'ÿ', 0x100, '日', 0x2028, 0xfeff, 0xfffd, 0xffff, 0xd800, 0xdfff,
	0x10000, 0x1F600, 0xe0001, 0x10ffff, 0x110000, 0x7fffffff, -1, -0x80000000,
}

var runeW = [...]string{
	"%*c", "%-*c", "%0*c", "%-0*c",
	"%*q", "%+*q", "%#*q", "%-*q", "%0*q", "%+-*q", "%#+0*q",
	"%*U", "%#*U", "%-#*U", "%0*U", "%+*U", "%0#*U",
	"%*d", "%0*x", "%-*X", "%#*o",
}

var runeWP = [...]string{
	"%*.*U", "%#*.*U", "%-*.*U", "%0#*.*U", "%*.*c", "%*.*q",
}

var runeP = [...]int{0, 4, 6, 9}

func main() {
	for _, s := range strs {
		fmt.Printf("== %q (% x)\n", s, s)
		for _, f := range strW {
			fmt.Printf("%-8s", f)
			for _, w := range widths {
				fmt.Printf(" [")
				fmt.Printf(f, w, s)
				fmt.Printf("]")
			}
			fmt.Printf("\n")
		}
		for _, f := range strWP {
			for _, pr := range precs {
				fmt.Printf("%-8s .%d", f, pr)
				for _, w := range widths {
					fmt.Printf(" [")
					fmt.Printf(f, w, pr, s)
					fmt.Printf("]")
				}
				fmt.Printf("\n")
			}
		}
	}

	for _, r := range runes {
		fmt.Printf("== %d\n", r)
		for _, f := range runeW {
			fmt.Printf("%-8s", f)
			for _, w := range widths {
				fmt.Printf(" [")
				fmt.Printf(f, w, rune(r))
				fmt.Printf("]")
			}
			fmt.Printf("\n")
		}
		for _, f := range runeWP {
			for _, pr := range runeP {
				fmt.Printf("%-8s .%d", f, pr)
				for _, w := range widths {
					fmt.Printf(" [")
					fmt.Printf(f, w, pr, rune(r))
					fmt.Printf("]")
				}
				fmt.Printf("\n")
			}
		}
	}

	// Other integer types: the value's bits decide, not its sign.
	fmt.Printf("[%c|%q|%U|%#U]\n", int8(-1), int8(-1), int8(-1), int8(-1))
	fmt.Printf("[%c|%q|%U|%#U]\n", int16(-2), int32(-3), int64(-4), int(-5))
	fmt.Printf("[%c|%q|%U|%#U]\n", uint8('x'), uint16(0x263a), uint32(0x1F600), uint64(0x100000041))
	fmt.Printf("[%c|%q|%U|%#U]\n", uint(0xd800), uintptr(0x41), uint64(1<<63), byte(0xad))
	fmt.Printf("[%5c|%-5q|%#12U|%.10U]\n", byte('z'), uint(0x110000), uint(0x10ffff), uint64(1<<63))
	fmt.Printf("[%#.20U|%-30.20U|%025U]\n", -1, uint64(0x1F600), 'x')

	// Byte slices, arrays and named byte slices.
	bs := []byte{'h', 0xff, 0xc3, 0xa9, '"', 0, '`'}
	var nb []byte
	arr := [3]byte{'a', 0x80, '\n'}
	fmt.Printf("[%q|%+q|%#q|%x|% x|%#x|%# x|%X|% X|%#X|%s]\n", bs, bs, bs, bs, bs, bs, bs, bs, bs, bs, bs)
	fmt.Printf("[%10q|%-10q|%010q|%.2q|%12.3x|%-12.3x|%012x|%.1s|%6.2s]\n", bs, bs, bs, bs, bs, bs, bs, bs, bs)
	fmt.Printf("[%q|%x|%5x|%-5X|%05x|%s|%5s|%#q|%v|%#v]\n", nb, nb, nb, nb, nb, nb, nb, nb, nb, nb)
	fmt.Printf("[%q|%+q|%x|% X|%s|%8q|%-8x]\n", arr, arr, arr, arr, arr, arr, arr)
	ab := bstr{'a', 'b'}
	fmt.Printf("[%q|%#q|%x|%s|%v|%c|%U|%5d]\n", bstr{'a', '`', 'b'}, ab, ab, bstr{0xc3, 0xa9}, ab, ab, ab, ab)
	fmt.Printf("[%q|%x|%c]\n", []byte{}, [0]byte{}, []byte{'h', 0xc3, 0xa9})

	// Stringers and errors: the method's result is quoted and padded.
	fmt.Printf("[%10q|%-12q|%#q|%+q|%x|% X|%.3q|%08s]\n", label(1), label(0), label(2), label(1), label(0), label(0), label(3), label(0))
	fmt.Printf("[%q|%#q|%+20q|%-14x|%.2s]\n", quoteErr{}, quoteErr{}, quoteErr{}, quoteErr{}, quoteErr{})

	// Strings inside composite values take the verb and its flags.
	fmt.Printf("[%q|%#q|%+q|%6q|%-6q]\n", []string{"a", "é\n"}, []string{"`", "x"}, []string{"é"}, []string{"a"}, [2]string{"b"})
	fmt.Printf("[%#v|%#8v|%x|% x|%X]\n", []string{"a\x00", "é"}, []string{"a"}, []string{"ab", "c"}, []string{"ab"}, [1]string{"hi"})
	fmt.Printf("[%q|%#v|%+v]\n", struct {
		S string
		B []byte
	}{"s\t", []byte{'b'}}, struct{ S string }{"\xff"}, struct{ S string }{"x"})
	fmt.Printf("[%q|%x|%c|%U]\n", []rune{'a', 0x1F600}, []rune{'a'}, []int32{0x263a}, []rune{'é'})

	// Other padded values: the 0 flag pads strings and booleans with zeros.
	fmt.Printf("[%07v|%-07v|%07t|%.2T|%8T|%05v]\n", nil, nil, true, 0, "", []string{"a"})
	fmt.Printf("[%08.3f|%08v|%-08v|%08v|%+08v]\n", 1.5, 1.0/3, 2.5, -1.0/3, 0.0)

	// Integers: the 0 flag's zeros go between the prefix and the digits,
	// and neither the width nor the precision has a limit.
	fmt.Printf("[%#08x|%08b|%#08o|%O|%08O|%+08d|%-08d|%08.3d|%.0d|%5.0d|%0*d|%#x]\n", 255, 5, 8, 8, 8, 5, 5, 5, 0, 0, -6, 7, -1)
	fmt.Printf("[%120d]\n[%.130d|%0130d]\n", 1, -1, -2)
	fmt.Printf("[%#O|%#o|%#o|%#.3o|%#08X|%# x|% 08d|%+.3x|%#b|%#08b|%x|%X|%#v|%#08v]\n", 8, 0, 8, 8, 255, 255, -3, 10, 5, 5, -255, -255, uint(10), uint8(7))
	fmt.Printf("[%05.0d|%-5.0d|%05d|%-+5d|%v]\n", 0, 0, -42, 42, []uint8{1, 2})

	// Every code point, quoted and encoded, and every two-byte sequence,
	// hashed one plane at a time.
	var total hasher
	total.h = 14695981039346656037
	for plane := 0; plane <= 0x10; plane++ {
		var h hasher
		h.h = 14695981039346656037
		var b [4]byte
		for r := plane << 16; r < (plane+1)<<16; r++ {
			fmt.Fprintf(&h, "%q%+q%#U%c", r, r, r, r)
			if r >= 0xd800 && r < 0xe000 {
				continue
			}
			n := encode(&b, r)
			fmt.Fprintf(&h, "%q%+q%#q%x", b[:n], b[:n], b[:n], b[:n])
		}
		fmt.Printf("plane %#x: %#016x %d\n", plane, h.h, h.n)
		fmt.Fprintf(&total, "%x", h.h)
	}
	var h hasher
	h.h = 14695981039346656037
	for i := 0; i < 1<<16; i++ {
		b := [2]byte{byte(i >> 8), byte(i)}
		fmt.Fprintf(&h, "%q%+q%#q%3.1q", b[:], b[:], b[:], b[:])
	}
	for b0 := 0xe0; b0 <= 0xf4; b0++ {
		for b1 := 0; b1 < 256; b1++ {
			b := [4]byte{byte(b0), byte(b1), 0x80, 0xbf}
			fmt.Fprintf(&h, "%q%+q%.2q%x", b[:], b[:], b[:3], b[:3])
		}
	}
	fmt.Printf("bytes: %#016x %d\n", h.h, h.n)
	fmt.Printf("total: %#016x\n", total.h)
}
