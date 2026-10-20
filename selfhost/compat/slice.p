// Package compat gives code translated from Go by tools/go2pu the Go
// semantics it relies on, on top of Plutonium's allocators: append that
// grows, string concatenation and conversions that copy, fmt.Sprintf that
// returns a new string, and errors made at run time. Go's maps need nothing
// here: they translate to Plutonium's map[K]V as they are.
//
// Everything here allocates from alloc.Default() and never frees. That is
// Go's garbage-collected model with the collector replaced by an arena: the
// compiler installs an arena with alloc.Use for each phase and releases it
// when the phase's results are no longer needed (see
// docs/plans/2026-10-09-self-hosting-plan.md). Hand-written Plutonium should
// use pluto/hash, pluto/vec and pluto/strings directly instead.
package compat

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/strconv"
	"pluto/strings"
	"pluto/unicode/utf8"
	"pluto/vec"
	"unsafe"
)

// Append is Go's append: it stores xs in s's spare capacity, or, when they
// do not fit, copies s into a new array from alloc.Default() at least twice
// as large. The old array is not freed; it may be shared, and the arena
// reclaims it with everything else.
func Append[T any](s []T, xs ...T) []T {
	n := len(s)
	if need := n + len(xs); need > cap(s) {
		s = grow(s, need)
	}
	s = s[:n+len(xs)]
	copy(s[n:], xs)
	return s
}

// Clone returns a copy of s in memory from alloc.Default(). It translates
// a slice literal []T{...} that Go's escape analysis moves to the heap: in
// Plutonium its backing array is in the frame.
func Clone[S ~[]E, E any](s S) S {
	if s == nil {
		return nil
	}
	c := Make[E](len(s))
	copy(c, s)
	return c
}

// Make is Go's make([]T, n): n zero elements in memory from
// alloc.Default(), which nothing frees; the arena reclaims it with
// everything else.
func Make[T any](n int) []T { return MakeCap[T](n, n) }

// MakeCap is Go's make([]T, n, c).
func MakeCap[T any](n, c int) []T {
	if n < 0 || c < n {
		panic("compat.MakeCap: len out of range")
	}
	var zero T
	size := uintptr(c) * unsafe.Sizeof(zero)
	if size == 0 {
		size = 1
	}
	p := alloc.Raw(size, unsafe.Alignof(zero))
	return unsafe.Slice((*T)(p), c)[:n]
}

// Insert is Go's slices.Insert: it inserts v into s at index i, growing as
// Append grows, and returns the modified slice.
func Insert[S ~[]E, E any](s S, i int, v ...E) S {
	_ = s[i:] // bounds check
	m := len(v)
	if m == 0 {
		return s
	}
	n := len(s)
	if n+m > cap(s) {
		s = S(grow([]E(s), n+m))
	}
	s = s[:n+m]
	copy(s[i+m:], s[i:n])
	copy(s[i:], v)
	return s
}

// AppendString is append(b, s...) for a string s.
func AppendString(b []byte, s string) []byte {
	n := len(b)
	if need := n + len(s); need > cap(b) {
		b = grow(b, need)
	}
	b = b[:n+len(s)]
	copy(b[n:], s)
	return b
}

// AppendInt is Go's strconv.AppendInt: the text of i in base, appended to
// dst as Append appends.
func AppendInt(dst []byte, i int64, base int) []byte {
	var mem [65]byte
	f := vec.NewFixed(mem[:])
	strconv.AppendInt(&f, i, base)
	return Append(dst, f.Slice()...)
}

// AppendFloat is Go's strconv.AppendFloat, appending as Append does.
func AppendFloat(dst []byte, x float64, verb byte, prec, bitSize int) []byte {
	v := vec.New[byte]()
	strconv.AppendFloat(&v, x, verb, prec, bitSize)
	dst = Append(dst, v.Slice()...)
	v.Free()
	return dst
}

// AppendRune is Go's utf8.AppendRune, appending as Append does.
func AppendRune(p []byte, r rune) []byte {
	var mem [utf8.UTFMax]byte
	f := vec.NewFixed(mem[:])
	utf8.AppendRune(&f, r)
	return Append(p, f.Slice()...)
}

// Appendf is Go's fmt.Appendf, appending as Append does.
func Appendf(b []byte, format string, args ...any) []byte {
	v := vec.New[byte]()
	fmt.Appendf(&v, format, args...)
	b = Append(b, v.Slice()...)
	v.Free()
	return b
}

// grow returns a copy of s with capacity for at least need elements.
func grow[T any](s []T, need int) []T {
	c := 2 * cap(s)
	if c < need {
		c = need
	}
	if c < 4 {
		c = 4
	}
	ns := MakeCap[T](len(s), c)
	copy(ns, s)
	return ns
}

// String returns a copy of b as a string, for Go's string(b). Plutonium has
// no string(b), because a string holds only valid UTF-8; go2pu translates
// the conversion mechanically, so this is the lossy form: each byte that is
// not valid UTF-8 becomes U+FFFD. Code that must keep arbitrary bytes keeps
// them in a []byte.
func String(b []byte) string { return strings.FromUTF8Lossy(b) }

// Bytes returns a copy of s as a byte slice, for Go's []byte(s).
func Bytes(s string) []byte {
	c := Make[byte](len(s))
	copy(c, s)
	return c
}

// Runes returns the runes of s as a slice in memory from alloc.Default(),
// for Go's []rune(s). Nothing frees it; the arena reclaims it with
// everything else.
func Runes(s string) []rune {
	r := Make[rune](utf8.RuneCountInString(s))
	i := 0
	for _, c := range s {
		r[i] = c
		i++
	}
	return r
}

// Concat returns the concatenation of parts, for Go's s + t.
func Concat(parts ...string) string {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	if n == 0 {
		return ""
	}
	b := Make[byte](n)
	k := 0
	for _, p := range parts {
		k += copy(b[k:], p)
	}
	return unsafe.String(unsafe.SliceData(b), n)
}

// RuneString returns the UTF-8 encoding of r, for Go's string(r).
func RuneString(r rune) string {
	var b Buffer
	b.WriteRune(r)
	return b.String()
}

// Join concatenates elems with sep between them, as strings.Join.
func Join(elems []string, sep string) string {
	var b Buffer
	for i, e := range elems {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(e)
	}
	return b.String()
}

// Split slices s into all substrings separated by sep, as strings.Split. The
// result is a new slice from alloc.Default().
func Split(s, sep string) []string { return SplitN(s, sep, -1) }

// SplitN is strings.SplitN: at most n substrings (all of them if n < 0), the
// last one holding the unsplit remainder; nil if n is 0.
func SplitN(s, sep string, n int) []string {
	if n == 0 {
		return nil
	}
	if n < 0 || n > len(s)+1 {
		if sep == "" {
			n = utf8.RuneCountInString(s)
		} else {
			n = strings.Count(s, sep) + 1
		}
	}
	out := Make[string](n)
	return out[:strings.SplitInto(s, sep, out)]
}

// Fields splits s around each run of white space, as strings.Fields. The
// result is a new slice from alloc.Default().
func Fields(s string) []string {
	out := Make[string](strings.FieldsInto(s, nil))
	strings.FieldsInto(s, out)
	return out
}

// PutUint16 stores v in b[0:2], little-endian.
func PutUint16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

// PutUint32 stores v in b[0:4], little-endian.
func PutUint32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

// PutUint64 stores v in b[0:8], little-endian.
func PutUint64(b []byte, v uint64) {
	PutUint32(b, uint32(v))
	PutUint32(b[4:], uint32(v>>32))
}

// AppendUint16 appends v to b, little-endian.
func AppendUint16(b []byte, v uint16) []byte {
	return Append(b, byte(v), byte(v>>8))
}

// AppendUint32 appends v to b, little-endian.
func AppendUint32(b []byte, v uint32) []byte {
	return Append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

// AppendUint64 appends v to b, little-endian.
func AppendUint64(b []byte, v uint64) []byte {
	return Append(AppendUint32(b, uint32(v)), byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56))
}
