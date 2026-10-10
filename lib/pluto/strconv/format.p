package strconv

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/unicode/utf8"
	"unsafe"
)

// The functions that return new strings (language spec section 6.10). Each
// formats into a buffer on the stack with its Append form and copies the
// result into a string from the current allocator; FormatBool, and Itoa and
// FormatInt for 0 to 99, return static strings and allocate nothing.

const smallsString = "00010203040506070809" +
	"10111213141516171819" +
	"20212223242526272829" +
	"30313233343536373839" +
	"40414243444546474849" +
	"50515253545556575859" +
	"60616263646566676869" +
	"70717273747576777879" +
	"80818283848586878889" +
	"90919293949596979899"

// small returns the static string of 0 <= i < 100.
func small(i int) string {
	if i < 10 {
		return digits[i : i+1]
	}
	return smallsString[i*2 : i*2+2]
}

// Itoa returns the decimal text of i, FormatInt(int64(i), 10).
func Itoa(i int) string { return FormatInt(int64(i), 10) }

// FormatInt returns the text of i in the given base (2 to 36), with
// lower-case letters for digits above 9.
func FormatInt(i int64, base int) string {
	if 0 <= i && i < 100 && base == 10 {
		return small(int(i))
	}
	var buf [65]byte
	return string(AppendInt(buf[:0], i, base))
}

// FormatUint returns the text of u in the given base (2 to 36).
func FormatUint(u uint64, base int) string {
	if u < 100 && base == 10 {
		return small(int(u))
	}
	var buf [64]byte
	return string(AppendUint(buf[:0], u, base))
}

// FormatBool returns "true" or "false", static strings.
func FormatBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Quote returns a double-quoted Go string literal representing s, escaping
// control characters, quotes and backslashes; other runes, printable or
// not, appear as themselves. (There are no Unicode tables to tell which are
// printable.)
func Quote(s string) string {
	n := 2
	for i := 0; i < len(s); i++ {
		n += quotedLen(s[i])
	}
	if n == len(s)+2 {
		return "\"" + s + "\""
	}
	// Quote straight into the new string's buffer, n bytes from the
	// current allocator.
	a := alloc.Default()
	if a == nil {
		panic("strconv: no allocator: call alloc.SetDefault before building strings")
	}
	p, ctl := rt.NewStringBuffer(n, a)
	b := AppendQuote(unsafe.Slice(p, n)[:0], s)
	q := rt.MakeString(p, len(b), ctl, rt.KnownASCII(s))
	rt.ReleaseString(ctl)
	return q
}

// AppendQuote appends the double-quoted Go string literal representing s,
// as made by Quote. Like append, it needs room in dst's capacity.
func AppendQuote(dst []byte, s string) []byte {
	const hex = "0123456789abcdef"
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"', '\\':
			dst = append(dst, '\\', c)
		case '\a':
			dst = append(dst, '\\', 'a')
		case '\b':
			dst = append(dst, '\\', 'b')
		case '\f':
			dst = append(dst, '\\', 'f')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		case '\v':
			dst = append(dst, '\\', 'v')
		default:
			if c < 0x20 || c == 0x7F {
				dst = append(dst, '\\', 'x', hex[c>>4], hex[c&15])
			} else {
				dst = append(dst, c)
			}
		}
	}
	return append(dst, '"')
}

// quotedLen returns the length of byte c in a quoted string.
func quotedLen(c byte) int {
	switch {
	case c == '"' || c == '\\' || c == '\a' || c == '\b' || c == '\f' || c == '\n' || c == '\r' || c == '\t' || c == '\v':
		return 2
	case c < 0x20 || c == 0x7F:
		return 4
	}
	return 1
}

// QuoteRune returns a single-quoted Go character literal representing r.
func QuoteRune(r rune) string {
	var buf [16]byte
	b := append(buf[:0], '\'')
	switch r {
	case '\'', '\\':
		b = append(b, '\\', byte(r))
	case '\n':
		b = append(b, '\\', 'n')
	case '\t':
		b = append(b, '\\', 't')
	case '\r':
		b = append(b, '\\', 'r')
	default:
		b = utf8.AppendRune(b, r)
	}
	b = append(b, '\'')
	return string(b)
}
