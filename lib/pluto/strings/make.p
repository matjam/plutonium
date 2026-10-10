package strings

import (
	"pluto/alloc"
	"pluto/errors"
	"pluto/rt"
	"pluto/unicode/utf8"
	"pluto/vec"
	"unsafe"
)

// The functions that make new strings. Each allocates one buffer of the
// exact size from the current allocator, alloc.Default(), and returns its
// argument itself when the result would be equal to it. The Append forms in
// append.p write into a destination the caller declares instead.

// ErrInvalidUTF8 is the error FromUTF8 returns for bytes that are not valid
// UTF-8.
var ErrInvalidUTF8 = errors.New("strings: invalid UTF-8")

// newBuf is a new string buffer being filled: its bytes so far, and the
// reference to it.
type newBuf struct {
	b   vec.Fixed[byte]
	ctl uintptr
}

// allocBuf allocates a buffer for a string of n > 0 bytes.
func allocBuf(n int) newBuf {
	a := alloc.Default()
	if a == nil {
		panic("strings: no allocator: call alloc.SetDefault before building strings")
	}
	p, ctl := rt.NewStringBuffer(n, a)
	return newBuf{b: vec.NewFixed(unsafe.Slice(p, n)), ctl: ctl}
}

// room returns the whole buffer, for code that fills it by index and then
// calls filled instead of appending to nb.b.
func (nb *newBuf) room() []byte {
	return unsafe.Slice(unsafe.SliceData(nb.b.Slice()), nb.b.Cap())
}

// filled is done for a buffer filled whole through room.
func (nb *newBuf) filled(ascii bool) string {
	s := rt.MakeString(unsafe.SliceData(nb.b.Slice()), nb.b.Cap(), nb.ctl, ascii)
	rt.ReleaseString(nb.ctl)
	return s
}

// done returns the string of the bytes written, which must be valid UTF-8,
// and drops the buffer's own reference: the string holds the only one.
func (nb *newBuf) done(ascii bool) string {
	s := rt.MakeString(unsafe.SliceData(nb.b.Slice()), nb.b.Len(), nb.ctl, ascii)
	rt.ReleaseString(nb.ctl)
	return s
}

// Clone returns a fresh copy of s, in a buffer of its own. A substring keeps
// the whole buffer it was cut from alive; a clone of it does not.
func Clone(s string) string {
	if s == "" {
		return ""
	}
	nb := allocBuf(len(s))
	vec.AppendString(&nb.b, s)
	return nb.done(rt.KnownASCII(s))
}

// FromUTF8 returns the bytes of b as a string if they are valid UTF-8, and
// ErrInvalidUTF8 otherwise. string(b) never fails: it replaces what is not
// valid with U+FFFD.
func FromUTF8(b []byte) (string, error) {
	if !utf8.Valid(b) {
		return "", ErrInvalidUTF8
	}
	return string(b), nil
}

// Repeat returns a new string consisting of count copies of s. It panics if
// count is negative or if the result would be too long.
func Repeat(s string, count int) string {
	switch {
	case count < 0:
		panic("strings: negative Repeat count")
	case count == 0 || s == "":
		return ""
	case count == 1:
		return s
	}
	if len(s) > (1<<47)/count {
		panic("strings: Repeat output length overflow")
	}
	nb := allocBuf(len(s) * count)
	repeatInto(nb.room(), s)
	return nb.filled(rt.KnownASCII(s))
}

// Join concatenates the elements of elems to create a single string, with
// sep placed between them.
func Join(elems []string, sep string) string {
	switch len(elems) {
	case 0:
		return ""
	case 1:
		return elems[0]
	}
	n := len(sep) * (len(elems) - 1)
	ascii := rt.KnownASCII(sep)
	for i := range elems {
		n += len(elems[i])
		ascii = ascii && rt.KnownASCII(elems[i])
	}
	if n == 0 {
		return ""
	}
	nb := allocBuf(n)
	b := nb.room()
	w := copy(b, elems[0])
	for i := 1; i < len(elems); i++ {
		w += copy(b[w:], sep)
		w += copy(b[w:], elems[i])
	}
	return nb.filled(ascii)
}

// Replace returns a copy of s with the first n non-overlapping instances of
// old replaced by new. If old is empty, it matches at the beginning of the
// string and after each UTF-8 sequence, yielding up to k+1 replacements for
// a k-rune string. If n < 0, there is no limit on the number of
// replacements. It returns s itself if nothing is replaced.
func Replace(s, old, new string, n int) string {
	if old == new || n == 0 {
		return s
	}
	m := Count(s, old)
	if m == 0 {
		return s
	}
	if n < 0 || m < n {
		n = m
	}
	size := len(s) + n*(len(new)-len(old))
	if size == 0 {
		return ""
	}
	nb := allocBuf(size)
	AppendReplace(&nb.b, s, old, new, n)
	return nb.done(rt.KnownASCII(s) && rt.KnownASCII(new))
}

// ReplaceAll returns a copy of s with all non-overlapping instances of old
// replaced by new, as Replace with n < 0 does.
func ReplaceAll(s, old, new string) string { return Replace(s, old, new, -1) }

// ToUpper returns s with all ASCII letters mapped to upper case; other
// runes are unchanged (there are no Unicode case tables yet). It returns s
// itself if it has no lower-case ASCII letter.
func ToUpper(s string) string {
	i := 0
	for i < len(s) && !('a' <= s[i] && s[i] <= 'z') {
		i++
	}
	if i == len(s) {
		return s
	}
	nb := allocBuf(len(s))
	upperInto(nb.room(), s)
	return nb.filled(rt.KnownASCII(s))
}

// ToLower returns s with all ASCII letters mapped to lower case; other
// runes are unchanged. It returns s itself if it has no upper-case ASCII
// letter.
func ToLower(s string) string {
	i := 0
	for i < len(s) && !('A' <= s[i] && s[i] <= 'Z') {
		i++
	}
	if i == len(s) {
		return s
	}
	nb := allocBuf(len(s))
	lowerInto(nb.room(), s)
	return nb.filled(rt.KnownASCII(s))
}

// Map returns a copy of s with each rune modified by mapping. A negative
// result drops the rune; an invalid one becomes U+FFFD. It returns s itself
// if mapping changes nothing.
func Map(mapping func(rune) rune, s string) string {
	for i, c := range s {
		r := mapping(c)
		if r == c {
			continue
		}
		// The first change: build the rest.
		var b Builder
		b.Grow(len(s))
		b.WriteString(s[:i])
		if r >= 0 {
			b.WriteRune(r)
		}
		for _, c := range s[i+utf8.RuneLen(c):] {
			if r := mapping(c); r >= 0 {
				b.WriteRune(r)
			}
		}
		out := b.String()
		b.Free()
		return out
	}
	return s
}
