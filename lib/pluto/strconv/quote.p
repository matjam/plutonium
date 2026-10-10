package strconv

import "pluto/unicode/utf8"

// The classification of runes that quoting needs, from Go's
// strconv/quote.go, with the tables in isprint.p. fmt quotes strings and
// runes itself, through its fixed buffer, with IsPrint and CanBackquote.

// CanBackquote reports whether the string s can be represented
// unchanged as a single-line backquoted string without control
// characters other than tab. fmt passes it views of byte slices, whose
// bytes need not be valid UTF-8 (//pu:bytewise).
//
//pu:bytewise
func CanBackquote(s string) bool {
	for len(s) > 0 {
		r, wid := utf8.DecodeRuneInString(s)
		s = s[wid:]
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
