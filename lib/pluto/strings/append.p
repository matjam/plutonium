package strings

import "pluto/unicode/utf8"

// AppendUpper appends s with all ASCII letters mapped to upper case and
// returns the extended buffer. Other bytes, including non-ASCII UTF-8, are
// copied unchanged. Like append, it needs room in dst's capacity.
func AppendUpper(dst []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'a' <= c && c <= 'z' {
			c -= 'a' - 'A'
		}
		dst = append(dst, c)
	}
	return dst
}

// AppendLower appends s with all ASCII letters mapped to lower case and
// returns the extended buffer. Other bytes, including non-ASCII UTF-8, are
// copied unchanged. Like append, it needs room in dst's capacity.
func AppendLower(dst []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		dst = append(dst, c)
	}
	return dst
}

// AppendRepeat appends count copies of s to dst and returns the extended
// buffer. It panics if count is negative.
func AppendRepeat(dst []byte, s string, count int) []byte {
	if count < 0 {
		panic("strings: negative Repeat count")
	}
	for i := 0; i < count; i++ {
		dst = append(dst, s...)
	}
	return dst
}

// AppendJoin appends the elements of elems separated by sep to dst and
// returns the extended buffer.
func AppendJoin(dst []byte, elems []string, sep string) []byte {
	for i, e := range elems {
		if i > 0 {
			dst = append(dst, sep...)
		}
		dst = append(dst, e...)
	}
	return dst
}

// AppendReplace appends s to dst with the first n non-overlapping instances
// of old replaced by new, and returns the extended buffer. If old is empty,
// it matches at the beginning of the string and after each UTF-8 sequence,
// yielding up to k+1 replacements for a k-rune string. If n < 0, there is
// no limit on the number of replacements. It is Go's Replace, appending
// instead of allocating.
func AppendReplace(dst []byte, s, old, new string, n int) []byte {
	if old == new || n == 0 {
		return append(dst, s...)
	}
	start := 0
	for i := 0; i != n; i++ {
		j := start
		if len(old) == 0 {
			if i > 0 {
				if start >= len(s) {
					break
				}
				_, wid := utf8.DecodeRuneInString(s[start:])
				j += wid
			}
		} else {
			k := Index(s[start:], old)
			if k < 0 {
				break
			}
			j += k
		}
		dst = append(dst, s[start:j]...)
		dst = append(dst, new...)
		start = j + len(old)
	}
	return append(dst, s[start:]...)
}
