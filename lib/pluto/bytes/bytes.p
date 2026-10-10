// Package bytes implements functions for the manipulation of byte slices,
// mirroring Go's bytes package where that is possible without allocating.
// Functions that return part of their argument return a subslice of it, not
// a copy. Buffer is Go's growing buffer, with storage from an allocator.
//
// The searching and trimming functions share their implementation with
// pluto/strings, viewing the slice as a string without copying.
package bytes

import (
	"pluto/strings"
	"unsafe"
)

// str views b as a string without copying. The bytes need not be valid
// UTF-8 (a []byte is any bytes), so the view is used only inside this
// package, to share the searching and comparing code of pluto/strings,
// which reads invalid bytes as U+FFFD runes of one byte, as Go's do; it is
// never returned, stored or handed to code that expects a valid string.
func str(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// sub returns the subslice of b that the string t, a substring of str(b),
// views. An empty t yields nil.
func sub(b []byte, t string) []byte {
	if len(t) == 0 {
		return nil
	}
	off := int(uintptr(unsafe.Pointer(unsafe.StringData(t))) - uintptr(unsafe.Pointer(unsafe.SliceData(b))))
	return b[off : off+len(t)]
}

// Equal reports whether a and b are the same length and contain the same
// bytes. A nil argument is equivalent to an empty slice.
func Equal(a, b []byte) bool { return str(a) == str(b) }

// Compare returns an integer comparing two byte slices lexicographically.
// The result is 0 if a == b, -1 if a < b, and +1 if a > b. A nil argument is
// equivalent to an empty slice.
func Compare(a, b []byte) int { return strings.Compare(str(a), str(b)) }

// HasPrefix reports whether the byte slice s begins with prefix.
func HasPrefix(s, prefix []byte) bool { return strings.HasPrefix(str(s), str(prefix)) }

// HasSuffix reports whether the byte slice s ends with suffix.
func HasSuffix(s, suffix []byte) bool { return strings.HasSuffix(str(s), str(suffix)) }

// IndexByte returns the index of the first instance of c in b, or -1 if c
// is not present in b.
func IndexByte(b []byte, c byte) int { return strings.IndexByte(str(b), c) }

// LastIndexByte returns the index of the last instance of c in s, or -1 if
// c is not present in s.
func LastIndexByte(s []byte, c byte) int { return strings.LastIndexByte(str(s), c) }

// Index returns the index of the first instance of sep in s, or -1 if sep
// is not present in s.
func Index(s, sep []byte) int { return strings.Index(str(s), str(sep)) }

// LastIndex returns the index of the last instance of sep in s, or -1 if
// sep is not present in s.
func LastIndex(s, sep []byte) int { return strings.LastIndex(str(s), str(sep)) }

// IndexRune interprets s as a sequence of UTF-8-encoded code points. It
// returns the byte index of the first occurrence in s of the given rune, or
// -1 if rune is not present in s.
func IndexRune(s []byte, r rune) int { return strings.IndexRune(str(s), r) }

// IndexAny interprets s as a sequence of UTF-8-encoded Unicode code points.
// It returns the byte index of the first occurrence in s of any of the
// Unicode code points in chars, or -1 if none is present.
func IndexAny(s []byte, chars string) int { return strings.IndexAny(str(s), chars) }

// IndexFunc returns the byte index in s of the first Unicode code point
// satisfying f(c), or -1 if none do.
func IndexFunc(s []byte, f func(r rune) bool) int { return strings.IndexFunc(str(s), f) }

// Contains reports whether subslice is within b.
func Contains(b, subslice []byte) bool { return Index(b, subslice) != -1 }

// ContainsAny reports whether any of the UTF-8-encoded code points in chars
// are within b.
func ContainsAny(b []byte, chars string) bool { return IndexAny(b, chars) >= 0 }

// ContainsRune reports whether the rune is contained in the UTF-8-encoded
// byte slice b.
func ContainsRune(b []byte, r rune) bool { return IndexRune(b, r) >= 0 }

// Count counts the number of non-overlapping instances of sep in s. If sep
// is empty, Count returns 1 + the number of UTF-8-encoded code points in s.
func Count(s, sep []byte) int { return strings.Count(str(s), str(sep)) }

// EqualFold reports whether s and t, interpreted as UTF-8 strings, are equal
// under simple case folding; as in pluto/strings, only ASCII letters fold.
func EqualFold(s, t []byte) bool { return strings.EqualFold(str(s), str(t)) }

// Cut slices s around the first instance of sep, returning the text before
// and after sep. The found result reports whether sep appears in s. If sep
// does not appear in s, Cut returns s, nil, false. Both results are
// subslices of s.
func Cut(s, sep []byte) (before, after []byte, found bool) {
	if i := Index(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, nil, false
}

// TrimSpace returns a subslice of s by slicing off all leading and trailing
// white space, as defined by Unicode.
func TrimSpace(s []byte) []byte { return sub(s, strings.TrimSpace(str(s))) }

// Trim returns a subslice of s by slicing off all leading and trailing
// UTF-8-encoded code points contained in cutset.
func Trim(s []byte, cutset string) []byte { return sub(s, strings.Trim(str(s), cutset)) }

// TrimLeft returns a subslice of s by slicing off all leading UTF-8-encoded
// code points contained in cutset.
func TrimLeft(s []byte, cutset string) []byte { return sub(s, strings.TrimLeft(str(s), cutset)) }

// TrimRight returns a subslice of s by slicing off all trailing
// UTF-8-encoded code points that are contained in cutset.
func TrimRight(s []byte, cutset string) []byte { return sub(s, strings.TrimRight(str(s), cutset)) }

// TrimFunc returns a subslice of s by slicing off all leading and trailing
// UTF-8-encoded code points c that satisfy f(c).
func TrimFunc(s []byte, f func(r rune) bool) []byte { return sub(s, strings.TrimFunc(str(s), f)) }

// TrimPrefix returns s without the provided leading prefix. If s doesn't
// start with prefix, s is returned unchanged.
func TrimPrefix(s, prefix []byte) []byte {
	if HasPrefix(s, prefix) {
		return s[len(prefix):]
	}
	return s
}

// TrimSuffix returns s without the provided trailing suffix. If s doesn't
// end with suffix, s is returned unchanged.
func TrimSuffix(s, suffix []byte) []byte {
	if HasSuffix(s, suffix) {
		return s[:len(s)-len(suffix)]
	}
	return s
}
