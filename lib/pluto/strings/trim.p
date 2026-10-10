package strings

import (
	"pluto/rt"
	"pluto/unicode/utf8"
)

// asciiSpace marks the ASCII white space bytes.
var asciiSpace = [256]uint8{'\t': 1, '\n': 1, '\v': 1, '\f': 1, '\r': 1, ' ': 1}

// IsSpace reports whether r is a white space character as defined by
// Unicode's White_Space property (the same set as Go's unicode.IsSpace).
func IsSpace(r rune) bool {
	if uint32(r) <= 0xFF {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', 0x85, 0xA0:
			return true
		}
		return false
	}
	switch {
	case r == 0x1680:
		return true
	case 0x2000 <= r && r <= 0x200A:
		return true
	case r == 0x2028, r == 0x2029, r == 0x202F, r == 0x205F, r == 0x3000:
		return true
	}
	return false
}

// The trimming functions work on bytes and are also used by the bytes
// package on bytes that need not be valid UTF-8, so their slices are not
// checked for rune boundaries (rt.SliceBytewise). Trimming whole runes from
// valid UTF-8 leaves whole runes.

// TrimSpace returns a slice of the string s, with all leading and trailing
// white space removed, as defined by Unicode.
func TrimSpace(s string) string {
	// Fast path for ASCII: look for the first ASCII non-space byte.
	start := 0
	for ; start < len(s); start++ {
		c := s[start]
		if c >= utf8.RuneSelf {
			// If we run into a non-ASCII byte, fall back to the slower
			// unicode-aware method on the remaining bytes.
			return TrimFunc(rt.SliceBytewise(s, start, len(s)), IsSpace)
		}
		if asciiSpace[c] == 0 {
			break
		}
	}

	// Now look for the first ASCII non-space byte from the end.
	stop := len(s)
	for ; stop > start; stop-- {
		c := s[stop-1]
		if c >= utf8.RuneSelf {
			return TrimRightFunc(rt.SliceBytewise(s, start, stop), IsSpace)
		}
		if asciiSpace[c] == 0 {
			break
		}
	}

	// At this point s[start:stop] starts and ends with an ASCII non-space
	// byte, so we're done.
	return rt.SliceBytewise(s, start, stop)
}

// TrimLeftFunc returns a slice of the string s with all leading Unicode code
// points c satisfying f(c) removed.
func TrimLeftFunc(s string, f func(rune) bool) string {
	i := indexFunc(s, f, false)
	if i == -1 {
		return ""
	}
	return rt.SliceBytewise(s, i, len(s))
}

// TrimRightFunc returns a slice of the string s with all trailing Unicode
// code points c satisfying f(c) removed.
func TrimRightFunc(s string, f func(rune) bool) string {
	i := lastIndexFunc(s, f, false)
	if i >= 0 && s[i] >= utf8.RuneSelf {
		_, wid := utf8.DecodeRuneInString(rt.SliceBytewise(s, i, len(s)))
		i += wid
	} else {
		i++
	}
	return rt.SliceBytewise(s, 0, i)
}

// TrimFunc returns a slice of the string s with all leading and trailing
// Unicode code points c satisfying f(c) removed.
func TrimFunc(s string, f func(rune) bool) string {
	return TrimRightFunc(TrimLeftFunc(s, f), f)
}

// Trim returns a slice of the string s with all leading and trailing Unicode
// code points contained in cutset removed.
func Trim(s, cutset string) string {
	if s == "" || cutset == "" {
		return s
	}
	if len(cutset) == 1 && cutset[0] < utf8.RuneSelf {
		return trimLeftByte(trimRightByte(s, cutset[0]), cutset[0])
	}
	if as, ok := makeASCIISet(cutset); ok {
		return trimLeftASCII(trimRightASCII(s, &as), &as)
	}
	return trimLeftUnicode(trimRightUnicode(s, cutset), cutset)
}

// TrimLeft returns a slice of the string s with all leading Unicode code
// points contained in cutset removed.
//
// To remove a prefix, use TrimPrefix instead.
func TrimLeft(s, cutset string) string {
	if s == "" || cutset == "" {
		return s
	}
	if len(cutset) == 1 && cutset[0] < utf8.RuneSelf {
		return trimLeftByte(s, cutset[0])
	}
	if as, ok := makeASCIISet(cutset); ok {
		return trimLeftASCII(s, &as)
	}
	return trimLeftUnicode(s, cutset)
}

func trimLeftByte(s string, c byte) string {
	for len(s) > 0 && s[0] == c {
		s = rt.SliceBytewise(s, 1, len(s))
	}
	return s
}

func trimLeftASCII(s string, as *asciiSet) string {
	for len(s) > 0 {
		if !as.contains(s[0]) {
			break
		}
		s = rt.SliceBytewise(s, 1, len(s))
	}
	return s
}

func trimLeftUnicode(s, cutset string) string {
	for len(s) > 0 {
		r, n := rune(s[0]), 1
		if r >= utf8.RuneSelf {
			r, n = utf8.DecodeRuneInString(s)
		}
		if !ContainsRune(cutset, r) {
			break
		}
		s = rt.SliceBytewise(s, n, len(s))
	}
	return s
}

// TrimRight returns a slice of the string s, with all trailing Unicode code
// points contained in cutset removed.
//
// To remove a suffix, use TrimSuffix instead.
func TrimRight(s, cutset string) string {
	if s == "" || cutset == "" {
		return s
	}
	if len(cutset) == 1 && cutset[0] < utf8.RuneSelf {
		return trimRightByte(s, cutset[0])
	}
	if as, ok := makeASCIISet(cutset); ok {
		return trimRightASCII(s, &as)
	}
	return trimRightUnicode(s, cutset)
}

func trimRightByte(s string, c byte) string {
	for len(s) > 0 && s[len(s)-1] == c {
		s = rt.SliceBytewise(s, 0, len(s)-1)
	}
	return s
}

func trimRightASCII(s string, as *asciiSet) string {
	for len(s) > 0 {
		if !as.contains(s[len(s)-1]) {
			break
		}
		s = rt.SliceBytewise(s, 0, len(s)-1)
	}
	return s
}

func trimRightUnicode(s, cutset string) string {
	for len(s) > 0 {
		r, n := rune(s[len(s)-1]), 1
		if r >= utf8.RuneSelf {
			r, n = utf8.DecodeLastRuneInString(s)
		}
		if !ContainsRune(cutset, r) {
			break
		}
		s = rt.SliceBytewise(s, 0, len(s)-n)
	}
	return s
}

// TrimPrefix returns s without the provided leading prefix string. If s
// doesn't start with prefix, s is returned unchanged.
func TrimPrefix(s, prefix string) string {
	if HasPrefix(s, prefix) {
		return s[len(prefix):]
	}
	return s
}

// TrimSuffix returns s without the provided trailing suffix string. If s
// doesn't end with suffix, s is returned unchanged.
func TrimSuffix(s, suffix string) string {
	if HasSuffix(s, suffix) {
		return s[:len(s)-len(suffix)]
	}
	return s
}
