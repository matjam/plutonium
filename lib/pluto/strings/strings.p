// Package strings implements simple functions to manipulate UTF-8 encoded
// strings. It mirrors Go's strings package:
//
//   - searching, comparison, trimming and Cut work as in Go and return
//     substrings of their argument, which share its buffer;
//   - functions that make a new string (Repeat, Join, Replace, ToUpper,
//     ToLower, Map) return one, counted like every string and allocated
//     from the current allocator; they return their argument itself when
//     nothing changes. Each has an Append form (AppendRepeat, AppendJoin,
//     ...) that writes into a caller's byte slice and allocates nothing;
//   - Split and Fields fill a caller's []string (SplitInto, FieldsInto), are
//     ranged over as in Go 1.24 (SplitSeq, FieldsSeq, Lines), or are walked
//     a step at a time with an explicit iterator (SplitIter, FieldsIter);
//   - a Builder builds a string in a buffer that its String method shares
//     with the result instead of copying.
//
// Strings hold valid UTF-8 (language spec section 6.10): FromUTF8 checks
// bytes, and string(b) replaces what is not. There are no Unicode tables
// yet: white space follows unicode.IsSpace exactly, but case mapping and
// case folding (ToUpper, ToLower, EqualFold) only change ASCII letters.
//
// Parts of this package are adapted from Go's strings package.
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.
package strings

import (
	"pluto/unicode/utf8"
	"unsafe"
)

// Compare returns an integer comparing two strings lexicographically. The
// result is 0 if a == b, -1 if a < b, and +1 if a > b.
func Compare(a, b string) int {
	if a == b {
		return 0
	}
	if a < b {
		return -1
	}
	return +1
}

// The searching functions below compare bytes, so they slice their
// arguments at byte offsets that need not be rune boundaries (//pu:bytewise);
// a match of a valid UTF-8 needle starts and ends at boundaries, so what
// they return is whole runes.

// HasPrefix reports whether s begins with prefix.
//
//pu:bytewise
func HasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// HasSuffix reports whether s ends with suffix.
//
//pu:bytewise
func HasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// IndexByte returns the index of the first instance of c in s, or -1 if c
// is not present in s.
func IndexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// LastIndexByte returns the index of the last instance of c in s, or -1 if
// c is not present in s.
func LastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// Index returns the index of the first instance of substr in s, or -1 if
// substr is not present in s.
//
//pu:bytewise
func Index(s, substr string) int {
	n := len(substr)
	switch {
	case n == 0:
		return 0
	case n == 1:
		return IndexByte(s, substr[0])
	case n == len(s):
		if substr == s {
			return 0
		}
		return -1
	case n > len(s):
		return -1
	}
	c0 := substr[0]
	c1 := substr[1]
	i := 0
	t := len(s) - n + 1
	fails := 0
	for i < t {
		if s[i] != c0 {
			o := IndexByte(s[i+1:t], c0)
			if o < 0 {
				return -1
			}
			i += o + 1
		}
		if s[i+1] == c1 && s[i:i+n] == substr {
			return i
		}
		i++
		fails++
		if fails >= 4+i>>4 && i < t {
			// Too many false positives on the first two bytes: switch to
			// Rabin-Karp, which is linear in the worst case.
			j := indexRabinKarp(s[i:], substr)
			if j < 0 {
				return -1
			}
			return i + j
		}
	}
	return -1
}

// primeRK is the prime base used in Rabin-Karp algorithm.
const primeRK = 16777619

// hashStr returns the hash and the appropriate multiplicative factor for use
// in Rabin-Karp algorithm.
func hashStr(sep string) (uint32, uint32) {
	hash := uint32(0)
	for i := 0; i < len(sep); i++ {
		hash = hash*primeRK + uint32(sep[i])
	}
	var pow, sq uint32 = 1, primeRK
	for i := len(sep); i > 0; i >>= 1 {
		if i&1 != 0 {
			pow *= sq
		}
		sq *= sq
	}
	return hash, pow
}

// hashStrRev returns the hash of the reverse of sep and the appropriate
// multiplicative factor for use in Rabin-Karp algorithm.
func hashStrRev(sep string) (uint32, uint32) {
	hash := uint32(0)
	for i := len(sep) - 1; i >= 0; i-- {
		hash = hash*primeRK + uint32(sep[i])
	}
	var pow, sq uint32 = 1, primeRK
	for i := len(sep); i > 0; i >>= 1 {
		if i&1 != 0 {
			pow *= sq
		}
		sq *= sq
	}
	return hash, pow
}

// indexRabinKarp uses the Rabin-Karp search algorithm to return the index
// of the first occurrence of sep in s, or -1 if not present.
//
//pu:bytewise
func indexRabinKarp(s, sep string) int {
	hashss, pow := hashStr(sep)
	n := len(sep)
	if len(s) < n {
		return -1
	}
	var h uint32
	for i := 0; i < n; i++ {
		h = h*primeRK + uint32(s[i])
	}
	if h == hashss && s[:n] == sep {
		return 0
	}
	for i := n; i < len(s); {
		h *= primeRK
		h += uint32(s[i])
		h -= pow * uint32(s[i-n])
		i++
		if h == hashss && s[i-n:i] == sep {
			return i - n
		}
	}
	return -1
}

// LastIndex returns the index of the last instance of substr in s, or -1 if
// substr is not present in s.
//
//pu:bytewise
func LastIndex(s, substr string) int {
	n := len(substr)
	switch {
	case n == 0:
		return len(s)
	case n == 1:
		return LastIndexByte(s, substr[0])
	case n == len(s):
		if substr == s {
			return 0
		}
		return -1
	case n > len(s):
		return -1
	}
	// Rabin-Karp search from the end of the string
	hashss, pow := hashStrRev(substr)
	last := len(s) - n
	var h uint32
	for i := len(s) - 1; i >= last; i-- {
		h = h*primeRK + uint32(s[i])
	}
	if h == hashss && s[last:] == substr {
		return last
	}
	for i := last - 1; i >= 0; i-- {
		h *= primeRK
		h += uint32(s[i])
		h -= pow * uint32(s[i+n])
		if h == hashss && s[i:i+n] == substr {
			return i
		}
	}
	return -1
}

// IndexRune returns the index of the first instance of the Unicode code
// point r, or -1 if rune is not present in s. If r is utf8.RuneError, it
// returns the first U+FFFD in s (or, for the bytes package's views of bytes
// that are not valid UTF-8, the first invalid byte sequence).
//
//pu:bytewise
func IndexRune(s string, r rune) int {
	switch {
	case 0 <= r && r < utf8.RuneSelf:
		return IndexByte(s, byte(r))
	case r == utf8.RuneError:
		for i := 0; i < len(s); {
			c, size := utf8.DecodeRuneInString(s[i:])
			if c == utf8.RuneError {
				return i
			}
			i += size
		}
		return -1
	case !utf8.ValidRune(r):
		return -1
	default:
		var buf [utf8.UTFMax]byte
		n := utf8.EncodeRune(buf[:], r)
		return Index(s, unsafe.String(&buf[0], n))
	}
}

// IndexFunc returns the index into s of the first Unicode code point
// satisfying f(c), or -1 if none do.
func IndexFunc(s string, f func(rune) bool) int { return indexFunc(s, f, true) }

// LastIndexFunc returns the index into s of the last Unicode code point
// satisfying f(c), or -1 if none do.
func LastIndexFunc(s string, f func(rune) bool) int { return lastIndexFunc(s, f, true) }

// indexFunc is the same as IndexFunc except that if truth==false, the sense
// of the predicate function is inverted.
//
//pu:bytewise
func indexFunc(s string, f func(rune) bool, truth bool) int {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if f(r) == truth {
			return i
		}
		i += size
	}
	return -1
}

// lastIndexFunc is the same as LastIndexFunc except that if truth==false,
// the sense of the predicate function is inverted.
//
//pu:bytewise
func lastIndexFunc(s string, f func(rune) bool, truth bool) int {
	for i := len(s); i > 0; {
		r, size := utf8.DecodeLastRuneInString(s[0:i])
		i -= size
		if f(r) == truth {
			return i
		}
	}
	return -1
}

// IndexAny returns the index of the first instance of any Unicode code point
// from chars in s, or -1 if no Unicode code point from chars is present in
// s.
//
//pu:bytewise
func IndexAny(s, chars string) int {
	if chars == "" {
		return -1
	}
	if len(chars) == 1 {
		r := rune(chars[0])
		if r >= utf8.RuneSelf {
			r = utf8.RuneError
		}
		return IndexRune(s, r)
	}
	if as, isASCII := makeASCIISet(chars); isASCII {
		for i := 0; i < len(s); i++ {
			if as.contains(s[i]) {
				return i
			}
		}
		return -1
	}
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if IndexRune(chars, r) >= 0 {
			return i
		}
		i += size
	}
	return -1
}

// LastIndexAny returns the index of the last instance of any Unicode code
// point from chars in s, or -1 if no Unicode code point from chars is
// present in s.
//
//pu:bytewise
func LastIndexAny(s, chars string) int {
	if chars == "" {
		return -1
	}
	if len(s) == 1 {
		r := rune(s[0])
		if r >= utf8.RuneSelf {
			r = utf8.RuneError
		}
		if IndexRune(chars, r) >= 0 {
			return 0
		}
		return -1
	}
	if as, isASCII := makeASCIISet(chars); isASCII {
		for i := len(s) - 1; i >= 0; i-- {
			if as.contains(s[i]) {
				return i
			}
		}
		return -1
	}
	for i := len(s); i > 0; {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		i -= size
		if IndexRune(chars, r) >= 0 {
			return i
		}
	}
	return -1
}

// Contains reports whether substr is within s.
func Contains(s, substr string) bool { return Index(s, substr) >= 0 }

// ContainsAny reports whether any Unicode code points in chars are within s.
func ContainsAny(s, chars string) bool { return IndexAny(s, chars) >= 0 }

// ContainsRune reports whether the Unicode code point r is within s.
func ContainsRune(s string, r rune) bool { return IndexRune(s, r) >= 0 }

// ContainsFunc reports whether any Unicode code points r within s satisfy
// f(r).
func ContainsFunc(s string, f func(rune) bool) bool { return IndexFunc(s, f) >= 0 }

// Count counts the number of non-overlapping instances of substr in s. If
// substr is an empty string, Count returns 1 + the number of Unicode code
// points in s.
//
//pu:bytewise
func Count(s, substr string) int {
	if len(substr) == 0 {
		return utf8.RuneCountInString(s) + 1
	}
	if len(substr) == 1 {
		c := substr[0]
		n := 0
		for i := 0; i < len(s); i++ {
			if s[i] == c {
				n++
			}
		}
		return n
	}
	n := 0
	for {
		i := Index(s, substr)
		if i == -1 {
			return n
		}
		n++
		s = s[i+len(substr):]
	}
}

// Cut slices s around the first instance of sep, returning the text before
// and after sep. The found result reports whether sep appears in s. If sep
// does not appear in s, Cut returns s, "", false.
func Cut(s, sep string) (before, after string, found bool) {
	if i := Index(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

// CutPrefix returns s without the provided leading prefix string and
// reports whether it found the prefix. If s doesn't start with prefix,
// CutPrefix returns s, false.
func CutPrefix(s, prefix string) (after string, found bool) {
	if !HasPrefix(s, prefix) {
		return s, false
	}
	return s[len(prefix):], true
}

// CutSuffix returns s without the provided ending suffix string and reports
// whether it found the suffix. If s doesn't end with suffix, CutSuffix
// returns s, false.
func CutSuffix(s, suffix string) (before string, found bool) {
	if !HasSuffix(s, suffix) {
		return s, false
	}
	return s[:len(s)-len(suffix)], true
}

// EqualFold reports whether s and t, interpreted as UTF-8 strings, are equal
// under simple case folding. Only ASCII letters fold; other code points must
// match exactly.
//
//pu:bytewise
func EqualFold(s, t string) bool {
	i := 0
	for ; i < len(s) && i < len(t); i++ {
		sr := s[i]
		tr := t[i]
		if sr|tr >= utf8.RuneSelf {
			goto hasUnicode
		}
		if tr == sr {
			continue
		}
		if tr < sr {
			tr, sr = sr, tr
		}
		// ASCII only, sr/tr must be upper/lower case
		if 'A' <= sr && sr <= 'Z' && tr == sr+'a'-'A' {
			continue
		}
		return false
	}
	return len(s) == len(t)

hasUnicode:
	s = s[i:]
	t = t[i:]
	for len(s) > 0 && len(t) > 0 {
		sr, ss := utf8.DecodeRuneInString(s)
		tr, ts := utf8.DecodeRuneInString(t)
		s = s[ss:]
		t = t[ts:]
		if tr == sr {
			continue
		}
		if tr < sr {
			tr, sr = sr, tr
		}
		if 'A' <= sr && sr <= 'Z' && tr == sr+'a'-'A' {
			continue
		}
		return false
	}
	return len(s) == len(t)
}

// asciiSet is a 32-byte value, where each bit represents the presence of a
// given ASCII character in the set. The 128-bits of the lower 16 bytes,
// starting with the least-significant bit of the lowest word to the
// most-significant bit of the highest word, map to the full range of all
// 128 ASCII characters. The 128-bits of the upper 16 bytes will be zeroed,
// ensuring that any non-ASCII character will be reported as not in the set.
type asciiSet [8]uint32

// makeASCIISet creates a set of ASCII characters and reports whether all
// characters in chars are ASCII.
func makeASCIISet(chars string) (as asciiSet, ok bool) {
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		if c >= utf8.RuneSelf {
			return as, false
		}
		as[c/32] |= 1 << (c % 32)
	}
	return as, true
}

// contains reports whether c is inside the set.
func (as *asciiSet) contains(c byte) bool {
	return (as[c/32] & (1 << (c % 32))) != 0
}
