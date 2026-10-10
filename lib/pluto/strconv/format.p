package strconv

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/unicode/utf8"
	"pluto/vec"
	"unsafe"
)

// The functions that return new strings (language spec section 6.10). Each
// formats into a buffer on the stack and copies the result into a string
// from the current allocator, or formats straight into a new string's
// buffer of the size it has measured; FormatBool, and Itoa and
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
	n := putInt(buf[:], i, base)
	return string(buf[len(buf)-n:])
}

// FormatUint returns the text of u in the given base (2 to 36).
func FormatUint(u uint64, base int) string {
	if u < 100 && base == 10 {
		return small(int(u))
	}
	var buf [64]byte
	n := PutUint(buf[:], u, base)
	return string(buf[len(buf)-n:])
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
	b := vec.NewFixed(unsafe.Slice(p, n))
	AppendQuote(&b, s)
	q := rt.MakeString(p, b.Len(), ctl, rt.KnownASCII(s))
	rt.ReleaseString(ctl)
	return q
}

// AppendQuote appends to dst the double-quoted Go string literal
// representing s, as made by Quote.
func AppendQuote[D vec.Appender[byte]](dst D, s string) {
	const hex = "0123456789abcdef"
	dst.Push('"')
	start := 0 // s[start:i] is still to be appended as it is
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quotedLen(c) == 1 {
			continue
		}
		// c is ASCII, so i and i+1 are rune boundaries.
		vec.AppendString(dst, s[start:i])
		start = i + 1
		switch c {
		case '"', '\\':
			dst.Append('\\', c)
		case '\a':
			dst.Append('\\', 'a')
		case '\b':
			dst.Append('\\', 'b')
		case '\f':
			dst.Append('\\', 'f')
		case '\n':
			dst.Append('\\', 'n')
		case '\r':
			dst.Append('\\', 'r')
		case '\t':
			dst.Append('\\', 't')
		case '\v':
			dst.Append('\\', 'v')
		default:
			dst.Append('\\', 'x', hex[c>>4], hex[c&15])
		}
	}
	vec.AppendString(dst, s[start:])
	dst.Push('"')
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
	b := vec.NewFixed(buf[:])
	b.Push('\'')
	switch r {
	case '\'', '\\':
		b.Append('\\', byte(r))
	case '\n':
		b.Append('\\', 'n')
	case '\t':
		b.Append('\\', 't')
	case '\r':
		b.Append('\\', 'r')
	default:
		utf8.AppendRune(&b, r)
	}
	b.Push('\'')
	return string(b.Slice())
}
