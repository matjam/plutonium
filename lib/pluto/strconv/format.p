package strconv

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/unicode/utf8"
	"pluto/vec"
	"unsafe"
)

// The functions that return new strings (language spec section 18.8). Each
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
	return rt.StringFromSlice(buf[len(buf)-n:], alloc.Default())
}

// FormatUint returns the text of u in the given base (2 to 36).
func FormatUint(u uint64, base int) string {
	if u < 100 && base == 10 {
		return small(int(u))
	}
	var buf [64]byte
	n := PutUint(buf[:], u, base)
	return rt.StringFromSlice(buf[len(buf)-n:], alloc.Default())
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
	b := unsafe.Slice(p, n)
	b[0] = '"'
	quoteInto(b[1:n-1], s)
	b[n-1] = '"'
	q := rt.MakeString(p, n, ctl, rt.KnownASCII(s))
	rt.ReleaseString(ctl)
	return q
}

// AppendQuote appends to dst the double-quoted Go string literal
// representing s, as made by Quote.
func AppendQuote[D vec.Appender[byte]](dst D, s string) {
	// Quoting is bytewise, so s is quoted in pieces of up to 64 bytes,
	// each of which takes at most 4*64 bytes quoted.
	var buf [4 * 64]byte
	dst.Push('"')
	for len(s) > 0 {
		k := min(len(s), 64)
		dst.Append(buf[:quoteInto(buf[:], s[:k])]...)
		s = s[k:]
	}
	dst.Push('"')
}

// quoteInto writes the escaped bytes of s, without the quotes, at the start
// of dst, which must have room for them, and returns how many it wrote.
// It works on bytes: a rune cut at either end of s is copied as it is.
func quoteInto(dst []byte, s string) int {
	const hex = "0123456789abcdef"
	w := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"', '\\':
			dst[w], dst[w+1] = '\\', c
		case '\a':
			dst[w], dst[w+1] = '\\', 'a'
		case '\b':
			dst[w], dst[w+1] = '\\', 'b'
		case '\f':
			dst[w], dst[w+1] = '\\', 'f'
		case '\n':
			dst[w], dst[w+1] = '\\', 'n'
		case '\r':
			dst[w], dst[w+1] = '\\', 'r'
		case '\t':
			dst[w], dst[w+1] = '\\', 't'
		case '\v':
			dst[w], dst[w+1] = '\\', 'v'
		default:
			if c < 0x20 || c == 0x7F {
				dst[w], dst[w+1], dst[w+2], dst[w+3] = '\\', 'x', hex[c>>4], hex[c&15]
				w += 4
			} else {
				dst[w] = c
				w++
			}
			continue
		}
		w += 2
	}
	return w
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
	buf[0] = '\''
	n := 1
	switch r {
	case '\'', '\\':
		buf[1], buf[2] = '\\', byte(r)
		n = 3
	case '\n':
		buf[1], buf[2] = '\\', 'n'
		n = 3
	case '\t':
		buf[1], buf[2] = '\\', 't'
		n = 3
	case '\r':
		buf[1], buf[2] = '\\', 'r'
		n = 3
	default:
		n += utf8.EncodeRune(buf[1:], r)
	}
	buf[n] = '\''
	return rt.StringFromSlice(buf[:n+1], alloc.Default())
}
