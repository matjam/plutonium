package strings

import "pluto/unicode/utf8"

// SplitInto slices s into substrings separated by sep, stores them in out
// and returns how many it stored. It behaves like Go's SplitN(s, sep,
// len(out)): at most len(out) substrings are produced and the last one holds
// the unsplit remainder. If sep is empty, SplitInto splits after each UTF-8
// sequence. With an empty out it returns 0.
func SplitInto(s, sep string, out []string) int { return genSplit(s, sep, 0, out) }

// SplitAfterInto is like SplitInto but keeps each sep at the end of the
// substring before it, like Go's SplitAfterN(s, sep, len(out)).
func SplitAfterInto(s, sep string, out []string) int { return genSplit(s, sep, len(sep), out) }

// genSplit splits s around each instance of sep, including sepSave bytes of
// sep in the subarrays.
func genSplit(s, sep string, sepSave int, out []string) int {
	n := len(out)
	if n == 0 {
		return 0
	}
	if sep == "" {
		return explode(s, out)
	}
	if n > len(s)+1 {
		n = len(s) + 1
	}
	n--
	i := 0
	for i < n {
		m := Index(s, sep)
		if m < 0 {
			break
		}
		out[i] = Slice(s, 0, m+sepSave)
		s = SliceFrom(s, m+len(sep))
		i++
	}
	out[i] = s
	return i + 1
}

// explode splits s into UTF-8 sequences, one per element of out, the last
// element taking the unsplit remainder.
func explode(s string, out []string) int {
	n := utf8.RuneCountInString(s)
	if n > len(out) {
		n = len(out)
	}
	for i := 0; i < n-1; i++ {
		_, size := utf8.DecodeRuneInString(s)
		out[i] = Slice(s, 0, size)
		s = SliceFrom(s, size)
	}
	if n > 0 {
		out[n-1] = s
	}
	return n
}

// FieldsInto splits s around each run of white space (as defined by IsSpace)
// and stores the first len(out) fields in out. It returns the number of
// fields in s, which is more than len(out) if out was too small.
func FieldsInto(s string, out []string) int { return FieldsFuncInto(s, IsSpace, out) }

// FieldsFuncInto splits s at each run of code points c satisfying f(c),
// stores the first len(out) fields in out and returns the number of fields
// in s.
func FieldsFuncInto(s string, f func(rune) bool, out []string) int {
	it := FieldsFuncIter(s, f)
	n := 0
	for {
		field, ok := it.Next()
		if !ok {
			return n
		}
		if n < len(out) {
			out[n] = field
		}
		n++
	}
}

// A SplitIterator walks the substrings of a string separated by a
// separator, as Go's SplitSeq and SplitAfterSeq do. Create one with
// SplitIter or SplitAfterIter.
type SplitIterator struct {
	s       string
	sep     string
	sepSave int
	done    bool
}

// SplitIter returns an iterator over the substrings of s separated by sep.
// It yields the same strings as Go's Split(s, sep):
//
//	it := strings.SplitIter("a,b,c", ",")
//	for part, ok := it.Next(); ok; part, ok = it.Next() {
//		...
//	}
func SplitIter(s, sep string) SplitIterator {
	return SplitIterator{s: s, sep: sep}
}

// SplitAfterIter returns an iterator over the substrings of s split after
// each instance of sep, as Go's SplitAfter(s, sep).
func SplitAfterIter(s, sep string) SplitIterator {
	return SplitIterator{s: s, sep: sep, sepSave: len(sep)}
}

// Next returns the next substring, or "", false when there are no more.
func (it *SplitIterator) Next() (string, bool) {
	if it.done {
		return "", false
	}
	if it.sep == "" {
		// Split after each UTF-8 sequence.
		if it.s == "" {
			it.done = true
			return "", false
		}
		_, size := utf8.DecodeRuneInString(it.s)
		r := Slice(it.s, 0, size)
		it.s = SliceFrom(it.s, size)
		return r, true
	}
	m := Index(it.s, it.sep)
	if m < 0 {
		it.done = true
		return it.s, true
	}
	r := Slice(it.s, 0, m+it.sepSave)
	it.s = SliceFrom(it.s, m+len(it.sep))
	return r, true
}

// Rest returns the part of the string not yet returned by Next.
func (it *SplitIterator) Rest() string {
	if it.done {
		return ""
	}
	return it.s
}

// A FieldsIterator walks the fields of a string: the non-empty runs of code
// points between separators. Create one with FieldsIter or FieldsFuncIter.
type FieldsIterator struct {
	s string
	f func(rune) bool
}

// FieldsIter returns an iterator over the fields of s separated by runs of
// white space, the same strings Go's Fields(s) returns.
func FieldsIter(s string) FieldsIterator { return FieldsIterator{s: s, f: IsSpace} }

// FieldsFuncIter returns an iterator over the fields of s separated by runs
// of code points satisfying f, the same strings Go's FieldsFunc(s, f)
// returns. The iterator keeps f, so it must not outlive f.
func FieldsFuncIter(s string, f func(rune) bool) FieldsIterator {
	return FieldsIterator{s: s, f: f}
}

// Next returns the next field, or "", false when there are no more.
func (it *FieldsIterator) Next() (string, bool) {
	s := it.s
	// Skip separators.
	start := -1
	i := 0
	for i < len(s) {
		r, size := rune(s[i]), 1
		if r >= utf8.RuneSelf {
			r, size = utf8.DecodeRuneInString(SliceFrom(s, i))
		}
		if !it.f(r) {
			start = i
			i += size
			break
		}
		i += size
	}
	if start < 0 {
		it.s = ""
		return "", false
	}
	// Find the end of the field.
	for i < len(s) {
		r, size := rune(s[i]), 1
		if r >= utf8.RuneSelf {
			r, size = utf8.DecodeRuneInString(SliceFrom(s, i))
		}
		if it.f(r) {
			it.s = SliceFrom(s, i+size)
			return Slice(s, start, i), true
		}
		i += size
	}
	it.s = ""
	return SliceFrom(s, start), true
}
