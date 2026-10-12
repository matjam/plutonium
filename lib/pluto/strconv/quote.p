package strconv

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/unicode/utf8"
	"pluto/vec"
)

// The classification of runes that quoting needs, from Go's
// strconv/quote.go, with the tables in isprint.p. fmt quotes strings and
// runes itself, through its fixed buffer, with IsPrint and CanBackquote.

// CanBackquote reports whether the string s can be represented
// unchanged as a single-line backquoted string without control
// characters other than tab. fmt passes it views of byte slices, whose
// bytes need not be valid UTF-8 (rt.SliceBytewise).
func CanBackquote(s string) bool {
	for len(s) > 0 {
		r, wid := utf8.DecodeRuneInString(s)
		s = rt.SliceBytewise(s, wid, len(s))
		if wid > 1 {
			if r == 0xFEFF {
				return false // BOMs are invisible and should not be quoted.
			}
			continue // All other multibyte runes are correctly encoded and assumed printable.
		}
		if r == utf8.RuneError {
			return false
		}
		if (r < ' ' && r != '\t') || r == '`' || r == '\u007F' {
			return false
		}
	}
	return true
}

// index returns the first index of the byte c in s, or -1 if it is not there.
func index(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// contains reports whether s contains the byte c.
func contains(s string, c byte) bool { return index(s, c) != -1 }

// unhex reports whether b is a hexadecimal digit, and its value.
func unhex(b byte) (v rune, ok bool) {
	c := rune(b)
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// UnquoteChar returns the character or Unicode code point represented by the
// quoted code point or byte sequence s, and the rest of s after it.
// multibyte reports whether c needed more than one byte in the sequence.
//
// If the quoted sequence is syntactically invalid, UnquoteChar returns
// ErrSyntax. It returns one of four kinds of result, depending on the quoted
// sequence:
//
//	'"' or '\''   the quote character itself; not multibyte
//	'\a'          a known escape sequence; not multibyte
//	'\x..'        a byte escape sequence; not multibyte
//	'\u....'      a rune; multibyte
//
// A \xNN escape with NN >= 0x80 names a byte that is not a rune; UnquoteChar
// returns it as a rune, not multibyte, the same value Go returns. It is the
// caller's responsibility to make it valid UTF-8 (section 7.3) before it is
// written into a string; Unquote does that, with U+FFFD.
func UnquoteChar(s string, quote byte) (value rune, multibyte bool, tail string, err error) {
	if len(s) == 0 {
		err = ErrSyntax
		return
	}
	c := s[0]
	if c == quote && (quote == '\'' || quote == '"') {
		err = ErrSyntax
		return
	}
	if c >= utf8.RuneSelf {
		r, size := utf8.DecodeRuneInString(s)
		return r, true, s[size:], nil
	}
	if c != '\\' {
		return rune(s[0]), false, s[1:], nil
	}

	if len(s) <= 1 {
		err = ErrSyntax
		return
	}
	c = s[1]
	s = s[2:]

	switch c {
	case 'a':
		value = '\a'
	case 'b':
		value = '\b'
	case 'f':
		value = '\f'
	case 'n':
		value = '\n'
	case 'r':
		value = '\r'
	case 't':
		value = '\t'
	case 'v':
		value = '\v'
	case 'x', 'u', 'U':
		n := 0
		if c == 'x' {
			n = 2
		} else if c == 'u' {
			n = 4
		} else {
			n = 8
		}
		var v rune
		if len(s) < n {
			err = ErrSyntax
			return
		}
		for j := 0; j < n; j++ {
			x, ok := unhex(s[j])
			if !ok {
				err = ErrSyntax
				return
			}
			v = v<<4 | x
		}
		s = s[n:]
		if c == 'x' {
			value = v
		} else {
			if !utf8.ValidRune(v) {
				err = ErrSyntax
				return
			}
			value = v
			multibyte = true
		}
	case '0', '1', '2', '3', '4', '5', '6', '7':
		v := rune(c) - '0'
		if len(s) < 2 {
			err = ErrSyntax
			return
		}
		for j := 0; j < 2; j++ {
			x := rune(s[j]) - '0'
			if x < 0 || x > 7 {
				err = ErrSyntax
				return
			}
			v = (v << 3) | x
		}
		s = s[2:]
		if v > 255 {
			err = ErrSyntax
			return
		}
		value = v
	case '\\':
		value = '\\'
	case '\'', '"':
		if c != quote {
			err = ErrSyntax
			return
		}
		value = rune(c)
	default:
		err = ErrSyntax
		return
	}
	tail = s
	return
}

// unquote is the core of Unquote and QuotedPrefix. With unescape true it
// returns the value s represents, escapes expanded; with it false it returns
// the quoted prefix as the original text. rem is the rest of s after the
// quoted string. The result is always valid UTF-8 (section 7.3): a byte that
// does not begin a valid UTF-8 sequence is replaced by U+FFFD.
func unquote(in string, unescape bool) (out string, rem string, err error) {
	// buf holds the unescaped bytes on the paths that build them. It is at the
	// top level so the deferred Free (which runs when the function returns) may
	// use it; paths that do not build bytes leave it zero and never defer.
	var buf vec.Vec[byte]
	if len(in) < 2 {
		return "", in, ErrSyntax
	}
	quote := in[0]
	end := index(in[1:], quote)
	if end < 0 {
		return "", in, ErrSyntax
	}
	end += 2

	switch quote {
	case '`':
		if !unescape {
			out = in[:end]
		} else if !contains(in[:end], '\r') {
			out = in[1 : end-1]
		} else {
			buf = vec.New[byte](alloc.Default())
			defer buf.Free()
			for i := 1; i < end-1; i++ {
				if in[i] != '\r' {
					buf.Push(in[i])
				}
			}
			out = rt.StringFromSlice(buf.Slice(), alloc.Default())
		}
		return out, in[end:], nil

	case '"', '\'':
		if !contains(in[:end], '\\') && !contains(in[:end], '\n') {
			var valid bool
			if quote == '"' {
				valid = utf8.ValidString(in[1 : end-1])
			} else {
				r, n := utf8.DecodeRuneInString(in[1 : end-1])
				valid = 1+n+1 == end && (r != utf8.RuneError || n != 1)
			}
			if valid {
				out = in[:end]
				if unescape {
					out = out[1 : end-1]
				}
				return out, in[end:], nil
			}
		}

		in0 := in
		if unescape {
			buf = vec.WithCapacity[byte](len(in0))
			defer buf.Free()
		}
		in = in[1:]
		for len(in) > 0 && in[0] != quote {
			r, multibyte, rest, e := UnquoteChar(in, quote)
			if in[0] == '\n' || e != nil {
				return "", in0, ErrSyntax
			}
			in = rest
			if unescape {
				if r < utf8.RuneSelf || !multibyte {
					buf.Push(byte(r))
				} else {
					utf8.AppendRune(&buf, r)
				}
			}
			if quote == '\'' {
				break
			}
		}
		if !(len(in) > 0 && in[0] == quote) {
			return "", in0, ErrSyntax
		}
		in = in[1:]
		if unescape {
			return rt.StringFromSlice(buf.Slice(), alloc.Default()), in, nil
		}
		return in0[:len(in0)-len(in)], in, nil
	}
	return "", in, ErrSyntax
}

// QuotedPrefix returns the prefix of s that holds a quoted string, including
// the quotes, as the original text with escapes not expanded. The second
// value is an error if s does not hold a valid quoted string.
func QuotedPrefix(s string) (string, error) {
	out, _, err := unquote(s, false)
	return out, err
}

// Unquote interprets s as a quoted Plutonium string and returns the value it
// represents, with the surrounding quotes removed and escapes expanded.
//
// Unquote accepts single-quoted character literals as well as double-quoted
// string literals, and backquoted raw strings. A single-quoted literal must
// hold exactly one character; the empty character literal is the empty string.
//
// Plutonium strings are valid UTF-8 (section 7.3). Where Go's result would
// hold an invalid byte (a \xNN escape with NN >= 0x80), Unquote replaces each
// such byte with U+FFFD.
func Unquote(s string) (string, error) {
	out, rem, err := unquote(s, true)
	if err != nil || rem != "" {
		return "", ErrSyntax
	}
	return out, nil
}

// bsearch16 returns the first index i such that s[i] >= v, and whether
// s[i] == v.
func bsearch16(s []uint16, v uint16) (int, bool) {
	n := len(s)
	i, j := 0, n
	for i < j {
		h := i + (j-i)>>1
		if s[h] < v {
			i = h + 1
		} else {
			j = h
		}
	}
	return i, i < n && s[i] == v
}

// bsearch32 is bsearch16 for uint32 values.
func bsearch32(s []uint32, v uint32) (int, bool) {
	n := len(s)
	i, j := 0, n
	for i < j {
		h := i + (j-i)>>1
		if s[h] < v {
			i = h + 1
		} else {
			j = h
		}
	}
	return i, i < n && s[i] == v
}

// IsPrint reports whether the rune is defined as printable by Go, with
// the same definition as unicode.IsPrint: letters, numbers, punctuation,
// symbols and ASCII space.
func IsPrint(r rune) bool {
	// Fast check for Latin-1
	if r <= 0xFF {
		if 0x20 <= r && r <= 0x7E {
			// All the ASCII is printable from space through DEL-1.
			return true
		}
		if 0xA1 <= r && r <= 0xFF {
			// Similarly for ¡ through ÿ...
			return r != 0xAD // ...except for the bizarre soft hyphen.
		}
		return false
	}

	// Same algorithm, either on uint16 or uint32 value.
	// First, find first i such that isPrint[i] >= x.
	// This is the index of either the start or end of a pair that might span x.
	// The start is even (isPrint[i&^1]) and the end is odd (isPrint[i|1]).
	// If we find x in a range, make sure x is not in isNotPrint list.

	if 0 <= r && r < 1<<16 {
		rr := uint16(r)
		isPrint := isPrint16[:]
		i, _ := bsearch16(isPrint, rr)
		if i >= len(isPrint) || rr < isPrint[i&^1] || isPrint[i|1] < rr {
			return false
		}
		_, found := bsearch16(isNotPrint16[:], rr)
		return !found
	}

	rr := uint32(r)
	isPrint := isPrint32[:]
	i, _ := bsearch32(isPrint, rr)
	if i >= len(isPrint) || rr < isPrint[i&^1] || isPrint[i|1] < rr {
		return false
	}
	if r >= 0x20000 {
		return true
	}
	r -= 0x10000
	_, found := bsearch16(isNotPrint32[:], uint16(r))
	return !found
}

// IsGraphic reports whether the rune is defined as a Graphic by Unicode. Such
// characters include letters, marks, numbers, punctuation, symbols, and
// spaces, from categories L, M, N, P, S, and Zs.
func IsGraphic(r rune) bool {
	if IsPrint(r) {
		return true
	}
	// isGraphic holds only runes below 0x10000, which IsPrint rejects.
	if r > 0xFFFF {
		return false
	}
	_, found := bsearch16(isGraphic[:], uint16(r))
	return found
}
