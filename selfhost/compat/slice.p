package compat

import "unsafe"

// Append is Go's append: it stores xs in s's spare capacity, or, when they
// do not fit, copies s into a new array from alloc.Default() at least twice
// as large. The old array is not freed; it may be shared, and the arena
// reclaims it with everything else.
func Append[T any](s []T, xs ...T) []T {
	if need := len(s) + len(xs); need > cap(s) {
		s = grow(s, need)
	}
	return append(s, xs...)
}

// Clone returns a copy of s in memory from alloc.Default(). It translates
// a slice literal []T{...} that Go's escape analysis moves to the heap: in
// Plutonium its backing array is in the frame.
func Clone[T any](s []T) []T {
	if s == nil {
		return nil
	}
	c := make([]T, len(s))
	copy(c, s)
	return c
}

// AppendString is append(b, s...) for a string s.
func AppendString(b []byte, s string) []byte {
	if need := len(b) + len(s); need > cap(b) {
		b = grow(b, need)
	}
	return append(b, s...)
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
	ns := make([]T, len(s), c)
	copy(ns, s)
	return ns
}

// String returns a copy of b as a string, for Go's string(b).
func String(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	c := make([]byte, len(b))
	copy(c, b)
	return unsafe.String(unsafe.SliceData(c), len(c))
}

// Bytes returns a copy of s as a byte slice, for Go's []byte(s).
func Bytes(s string) []byte {
	c := make([]byte, len(s))
	copy(c, s)
	return c
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
	b := make([]byte, 0, n)
	for _, p := range parts {
		b = append(b, p...)
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
