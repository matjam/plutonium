package compat

import (
	"pluto/alloc"
	"pluto/errors"
	"pluto/fmt"
	"pluto/unicode/utf8"
	"unsafe"
)

// A Buffer is Go's bytes.Buffer and strings.Builder for writing: its zero
// value is ready to use and grows from alloc.Default(). It implements
// io.Writer.
type Buffer struct {
	buf []byte
}

// Builder is strings.Builder.
type Builder = Buffer

// Write appends p.
func (b *Buffer) Write(p []byte) (int, error) {
	b.buf = Append(b.buf, p...)
	return len(p), nil
}

// WriteString appends s.
func (b *Buffer) WriteString(s string) (int, error) {
	b.buf = AppendString(b.buf, s)
	return len(s), nil
}

// WriteByte appends c.
func (b *Buffer) WriteByte(c byte) error {
	b.buf = Append(b.buf, c)
	return nil
}

// WriteRune appends the UTF-8 encoding of r.
func (b *Buffer) WriteRune(r rune) (int, error) {
	var tmp [utf8.UTFMax]byte
	n := utf8.EncodeRune(tmp[:], r)
	b.buf = Append(b.buf, tmp[:n]...)
	return n, nil
}

// Len returns the number of bytes written.
func (b *Buffer) Len() int { return len(b.buf) }

// Bytes returns the contents, a view valid until the next write.
func (b *Buffer) Bytes() []byte { return b.buf }

// String returns the contents. Unlike Bytes it stays valid after later
// writes: a write that grows the buffer copies it, leaving the old bytes
// in place, and writes never change bytes already written.
func (b *Buffer) String() string {
	if len(b.buf) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b.buf), len(b.buf))
}

// Truncate discards all but the first n bytes.
func (b *Buffer) Truncate(n int) { b.buf = b.buf[:n] }

// Reset empties the buffer, keeping its storage.
func (b *Buffer) Reset() { b.buf = b.buf[:0] }

// Grow makes room for n more bytes.
func (b *Buffer) Grow(n int) {
	if len(b.buf)+n > cap(b.buf) {
		b.buf = grow(b.buf, len(b.buf)+n)
	}
}

// Sprintf formats into a new string from alloc.Default().
func Sprintf(format string, args ...any) string {
	var b Buffer
	fmt.Fprintf(&b, format, args...)
	return b.String()
}

// Sprint formats with %v into a new string.
func Sprint(args ...any) string {
	var b Buffer
	fmt.Fprint(&b, args...)
	return b.String()
}

// Sprintln formats with %v, spaces and a newline into a new string.
func Sprintln(args ...any) string {
	var b Buffer
	fmt.Fprintln(&b, args...)
	return b.String()
}

// NewError is errors.New called at run time. Plutonium's errors.New takes
// a constant message and makes an error of a type of its own per call site (spec
// section 16.3), so an error whose message is made each time a function
// runs is allocated instead, from alloc.Default() like fmt.Errorf's. (fmt.Errorf, errors.Is, As, Unwrap
// and Join need no translation: pluto/fmt and pluto/errors follow Go.)
func NewError(text string) error {
	e := alloc.New[errors.Text]()
	e.Msg = text
	return e
}
