package strings

import (
	"pluto/unicode/utf8"
	"pluto/vec"
)

// The Append forms of the functions that make new strings. Each appends to
// dst, a *vec.Vec[byte] (which grows from its allocator) or a
// *vec.Fixed[byte] (which keeps what fits in the caller's array and never
// allocates), and updates it in place.

// AppendUpper appends s to dst with all ASCII letters mapped to upper case.
// Other bytes, including non-ASCII UTF-8, are copied unchanged.
func AppendUpper[D vec.Appender[byte]](dst D, s string) {
	var buf [256]byte
	for len(s) > 0 {
		n := min(len(s), len(buf))
		upperInto(buf[:n], Slice(s, 0, n))
		dst.Append(buf[:n]...)
		s = SliceFrom(s, n)
	}
}

// AppendLower appends s to dst with all ASCII letters mapped to lower case.
// Other bytes, including non-ASCII UTF-8, are copied unchanged.
func AppendLower[D vec.Appender[byte]](dst D, s string) {
	var buf [256]byte
	for len(s) > 0 {
		n := min(len(s), len(buf))
		lowerInto(buf[:n], Slice(s, 0, n))
		dst.Append(buf[:n]...)
		s = SliceFrom(s, n)
	}
}

// AppendRepeat appends count copies of s to dst. It panics if count is
// negative.
func AppendRepeat[D vec.Appender[byte]](dst D, s string, count int) {
	if count < 0 {
		panic("strings: negative Repeat count")
	}
	for i := 0; i < count; i++ {
		vec.AppendString(dst, s)
	}
}

// upperInto stores s with ASCII letters upper-cased in dst[:len(s)]. The
// string is viewed as bytes, so a cut through a rune does not matter.
func upperInto(dst []byte, s string) {
	dst = dst[:len(s)]
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'a' <= c && c <= 'z' {
			c -= 'a' - 'A'
		}
		dst[i] = c
	}
}

// lowerInto stores s with ASCII letters lower-cased in dst[:len(s)].
func lowerInto(dst []byte, s string) {
	dst = dst[:len(s)]
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		dst[i] = c
	}
}

// repeatInto fills dst, whose length is a multiple of len(s) > 0, with
// copies of s, doubling what it has copied each time.
func repeatInto(dst []byte, s string) {
	n := copy(dst, s)
	for n < len(dst) {
		n += copy(dst[n:], dst[:n])
	}
}

// AppendJoin appends the elements of elems separated by sep to dst.
func AppendJoin[D vec.Appender[byte]](dst D, elems []string, sep string) {
	for i, e := range elems {
		if i > 0 {
			vec.AppendString(dst, sep)
		}
		vec.AppendString(dst, e)
	}
}

// AppendReplace appends s to dst with the first n non-overlapping instances
// of old replaced by new. If old is empty, it matches at the beginning of
// the string and after each UTF-8 sequence, yielding up to k+1 replacements
// for a k-rune string. If n < 0, there is no limit on the number of
// replacements. It is Go's Replace, appending instead of allocating.
func AppendReplace[D vec.Appender[byte]](dst D, s, old, new string, n int) {
	if old == new || n == 0 {
		vec.AppendString(dst, s)
		return
	}
	start := 0
	for i := 0; i != n; i++ {
		j := start
		if len(old) == 0 {
			if i > 0 {
				if start >= len(s) {
					break
				}
				_, wid := utf8.DecodeRuneInString(SliceFrom(s, start))
				j += wid
			}
		} else {
			k := Index(SliceFrom(s, start), old)
			if k < 0 {
				break
			}
			j += k
		}
		vec.AppendString(dst, Slice(s, start, j))
		vec.AppendString(dst, new)
		start = j + len(old)
	}
	vec.AppendString(dst, SliceFrom(s, start))
}
