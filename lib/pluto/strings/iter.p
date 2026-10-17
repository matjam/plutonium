package strings

import "pluto/iter"

// Iterators over parts of a string, as Go 1.24 has them. Each returns
// substrings of its argument without allocating, and can be walked more
// than once. The explicit iterators SplitIter, SplitAfterIter, FieldsIter
// and FieldsFuncIter walk the same substrings a step at a time.

// Lines returns an iterator over the newline-terminated lines in s. The
// lines include their terminating newline; if s does not end in a newline,
// the last line has none. An empty s yields no lines.
func Lines(s string) iter.Seq[string] {
	return func(yield func(string) bool) {
		rest := s
		for len(rest) > 0 {
			var line string
			if i := IndexByte(rest, '\n'); i >= 0 {
				line, rest = Slice(rest, 0, i+1), SliceFrom(rest, i+1)
			} else {
				line, rest = rest, ""
			}
			if !yield(line) {
				return
			}
		}
	}
}

// SplitSeq returns an iterator over the substrings of s separated by sep:
// the strings Go's Split(s, sep) returns. An empty sep splits after each
// UTF-8 sequence.
func SplitSeq(s, sep string) iter.Seq[string] {
	return func(yield func(string) bool) {
		it := SplitIter(s, sep)
		for part, ok := it.Next(); ok; part, ok = it.Next() {
			if !yield(part) {
				return
			}
		}
	}
}

// SplitAfterSeq returns an iterator over the substrings of s split after
// each instance of sep: the strings Go's SplitAfter(s, sep) returns.
func SplitAfterSeq(s, sep string) iter.Seq[string] {
	return func(yield func(string) bool) {
		it := SplitAfterIter(s, sep)
		for part, ok := it.Next(); ok; part, ok = it.Next() {
			if !yield(part) {
				return
			}
		}
	}
}

// FieldsSeq returns an iterator over the fields of s separated by runs of
// white space (as defined by IsSpace): the strings Go's Fields(s) returns.
func FieldsSeq(s string) iter.Seq[string] {
	return func(yield func(string) bool) {
		it := FieldsIter(s)
		for field, ok := it.Next(); ok; field, ok = it.Next() {
			if !yield(field) {
				return
			}
		}
	}
}

// FieldsFuncSeq returns an iterator over the fields of s separated by runs
// of code points satisfying f: the strings Go's FieldsFunc(s, f) returns.
func FieldsFuncSeq(s string, f func(rune) bool) iter.Seq[string] {
	return func(yield func(string) bool) {
		it := FieldsFuncIter(s, f)
		for field, ok := it.Next(); ok; field, ok = it.Next() {
			if !yield(field) {
				return
			}
		}
	}
}
