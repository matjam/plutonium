package bytes

import (
	"pluto/iter"
	"pluto/strings"
	"unsafe"
)

// Iterators over parts of a byte slice, as Go 1.24 has them. Each yields
// subslices of its argument, without allocating, whose capacity ends where
// they do, so that appending to one cannot overwrite the rest of s.

// part returns the subslice of b that t, a substring of str(b), views, with
// its capacity cut to its length.
func part(b []byte, t string) []byte {
	if len(t) == 0 {
		return b[:0:0]
	}
	off := int(uintptr(unsafe.Pointer(unsafe.StringData(t))) - uintptr(unsafe.Pointer(unsafe.SliceData(b))))
	return b[off : off+len(t) : off+len(t)]
}

// Lines returns an iterator over the newline-terminated lines in s, as
// strings.Lines.
func Lines(s []byte) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		for line := range strings.Lines(str(s)) {
			if !yield(part(s, line)) {
				return
			}
		}
	}
}

// SplitSeq returns an iterator over the subslices of s separated by sep, as
// strings.SplitSeq.
func SplitSeq(s, sep []byte) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		for p := range strings.SplitSeq(str(s), str(sep)) {
			if !yield(part(s, p)) {
				return
			}
		}
	}
}

// SplitAfterSeq returns an iterator over the subslices of s split after
// each instance of sep, as strings.SplitAfterSeq.
func SplitAfterSeq(s, sep []byte) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		for p := range strings.SplitAfterSeq(str(s), str(sep)) {
			if !yield(part(s, p)) {
				return
			}
		}
	}
}

// FieldsSeq returns an iterator over the fields of s separated by runs of
// white space, as strings.FieldsSeq.
func FieldsSeq(s []byte) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		for f := range strings.FieldsSeq(str(s)) {
			if !yield(part(s, f)) {
				return
			}
		}
	}
}

// FieldsFuncSeq returns an iterator over the fields of s separated by runs
// of code points satisfying f, as strings.FieldsFuncSeq.
func FieldsFuncSeq(s []byte, f func(rune) bool) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		for field := range strings.FieldsFuncSeq(str(s), f) {
			if !yield(part(s, field)) {
				return
			}
		}
	}
}
